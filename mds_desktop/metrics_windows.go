//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

func collectPlatformMetrics() (metricsSnapshot, error) {
	ctx, cancel := requestContext()
	defer cancel()
	script := `$ErrorActionPreference = "Stop"
$os = Get-CimInstance Win32_OperatingSystem
$disk = Get-CimInstance Win32_LogicalDisk -Filter "DeviceID='$env:SystemDrive'"
$cpu = 0
try { $cpu = (Get-Counter '\Processor(_Total)\% Processor Time').CounterSamples[0].CookedValue } catch {}
$processes = @(Get-Process | Sort-Object WorkingSet64 -Descending | Select-Object -First 8 ProcessName,Id,WorkingSet64)
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class MdsForegroundWindow {
  [DllImport("user32.dll")] static extern IntPtr GetForegroundWindow();
  [DllImport("user32.dll", CharSet = CharSet.Unicode)] static extern int GetWindowText(IntPtr handle, StringBuilder text, int count);
  public static object Read() {
    var handle = GetForegroundWindow();
    if (handle == IntPtr.Zero) return null;
    var title = new StringBuilder(512);
    GetWindowText(handle, title, title.Capacity);
    return new { name = title.ToString(), title = title.ToString() };
  }
}
'@
$foreground = [MdsForegroundWindow]::Read()
[pscustomobject]@{
  cpu_percent = [double]$cpu
  memory_total_bytes = [uint64]($os.TotalVisibleMemorySize * 1024)
  memory_free_bytes = [uint64]($os.FreePhysicalMemory * 1024)
  disk_total_bytes = [uint64]$disk.Size
  disk_free_bytes = [uint64]$disk.FreeSpace
  foreground_app = $foreground
  processes = $processes
} | ConvertTo-Json -Compress -Depth 4`
	output, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script).Output()
	if err != nil {
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
