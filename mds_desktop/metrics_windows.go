//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

func collectPlatformMetrics() (metricsSnapshot, error) {
	ctx, cancel := requestContext()
	defer cancel()
	script := `$ErrorActionPreference = "Stop"
$disk = [System.IO.DriveInfo]::new($env:SystemDrive)
$cpu = 0
try { $cpu = (Get-Counter '\Processor(_Total)\% Processor Time').CounterSamples[0].CookedValue } catch {}
$processes = @(Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 8 ProcessName,Id,WorkingSet64)
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class MdsForegroundWindow {
  [StructLayout(LayoutKind.Sequential)] struct MemoryStatusEx {
    public uint Length;
    public uint MemoryLoad;
    public ulong TotalPhys;
    public ulong AvailablePhys;
    public ulong TotalPageFile;
    public ulong AvailablePageFile;
    public ulong TotalVirtual;
    public ulong AvailableVirtual;
    public ulong AvailableExtendedVirtual;
  }
  [DllImport("kernel32.dll")] static extern bool GlobalMemoryStatusEx(ref MemoryStatusEx status);
  [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr handle, out uint processId);
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetWindowText(IntPtr handle, StringBuilder text, int count);
  public static object ReadMemory() {
    var status = new MemoryStatusEx { Length = (uint)Marshal.SizeOf(typeof(MemoryStatusEx)) };
    if (!GlobalMemoryStatusEx(ref status)) return null;
    return new { total = status.TotalPhys, free = status.AvailablePhys };
  }
  public static object Read() {
    var handle = GetForegroundWindow();
    if (handle == IntPtr.Zero) return null;
    uint processId;
    GetWindowThreadProcessId(handle, out processId);
    var title = new StringBuilder(512);
    GetWindowText(handle, title, title.Capacity);
    return new { pid = processId, title = title.ToString() };
  }
}
'@
$memory = [MdsForegroundWindow]::ReadMemory()
$foregroundWindow = [MdsForegroundWindow]::Read()
$foreground = $null
if ($foregroundWindow -ne $null) {
  $process = Get-Process -Id $foregroundWindow.pid -ErrorAction SilentlyContinue
  if ($process -ne $null) {
    $processName = $process.ProcessName + ".exe"
    $displayName = ""
    try { $displayName = [string]$process.MainModule.FileVersionInfo.FileDescription } catch {}
    if ([string]::IsNullOrWhiteSpace($displayName)) { $displayName = $processName }
    $foreground = [pscustomobject]@{
      name = $displayName
      process_name = $processName
      title = $foregroundWindow.title
      captured_at = [DateTimeOffset]::UtcNow.ToString("o")
    }
  }
}
[pscustomobject]@{
  cpu_percent = [double]$cpu
  memory_total_bytes = [uint64]$memory.total
  memory_free_bytes = [uint64]$memory.free
  disk_total_bytes = [uint64]$disk.TotalSize
  disk_free_bytes = [uint64]$disk.AvailableFreeSpace
  foreground_app = $foreground
  processes = $processes
} | ConvertTo-Json -Compress -Depth 4`
	command := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	output, err := command.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return metricsSnapshot{}, fmt.Errorf("windows metrics: %w", ctx.Err())
		}
		if detail := strings.TrimSpace(string(output)); detail != "" {
			return metricsSnapshot{}, fmt.Errorf("windows metrics: %w: %s", err, detail)
		}
		return metricsSnapshot{}, fmt.Errorf("windows metrics: %w", err)
	}
	var raw struct {
		CPUPercent       float64         `json:"cpu_percent"`
		MemoryTotalBytes uint64          `json:"memory_total_bytes"`
		MemoryFreeBytes  uint64          `json:"memory_free_bytes"`
		DiskTotalBytes   uint64          `json:"disk_total_bytes"`
		DiskFreeBytes    uint64          `json:"disk_free_bytes"`
		ForegroundApp    json.RawMessage `json:"foreground_app"`
		Processes        json.RawMessage `json:"processes"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return metricsSnapshot{}, fmt.Errorf("decode windows metrics: %w", err)
	}
	result := metricsSnapshot{
		CPUPercent:       raw.CPUPercent,
		MemoryTotalBytes: raw.MemoryTotalBytes,
		MemoryUsedBytes:  raw.MemoryTotalBytes - minUint64(raw.MemoryFreeBytes, raw.MemoryTotalBytes),
		DiskTotalBytes:   raw.DiskTotalBytes,
		DiskFreeBytes:    raw.DiskFreeBytes,
		ForegroundApp:    decodeWindowsForeground(raw.ForegroundApp),
		Processes:        decodeWindowsProcesses(raw.Processes),
	}
	if result.MemoryTotalBytes > 0 {
		result.MemoryPercent = float64(result.MemoryUsedBytes) / float64(result.MemoryTotalBytes) * 100
	}
	if result.DiskTotalBytes > 0 {
		result.DiskUsedPercent = float64(result.DiskTotalBytes-result.DiskFreeBytes) / float64(result.DiskTotalBytes) * 100
	}
	return result, nil
}

func decodeWindowsForeground(data json.RawMessage) *appSnapshot {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var value appSnapshot
	if json.Unmarshal(data, &value) != nil || strings.TrimSpace(value.Name) == "" {
		return nil
	}
	return &value
}

func decodeWindowsProcesses(data json.RawMessage) []processSnapshot {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	var values []struct {
		Name        string `json:"ProcessName"`
		PID         int    `json:"Id"`
		MemoryBytes uint64 `json:"WorkingSet64"`
	}
	if data[0] == '{' {
		var value struct {
			Name        string `json:"ProcessName"`
			PID         int    `json:"Id"`
			MemoryBytes uint64 `json:"WorkingSet64"`
		}
		if json.Unmarshal(data, &value) == nil {
			values = append(values, value)
		}
	} else {
		_ = json.Unmarshal(data, &values)
	}
	result := make([]processSnapshot, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value.Name) == "" {
			continue
		}
		result = append(result, processSnapshot{Name: value.Name, PID: value.PID, MemoryBytes: value.MemoryBytes})
	}
	return result
}
