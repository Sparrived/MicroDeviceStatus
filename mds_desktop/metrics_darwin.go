//go:build darwin

package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
)

func collectPlatformMetrics() (metricsSnapshot, error) {
	result := metricsSnapshot{}
	var collectionErrors []string
	if value, err := darwinCPUPercent(); err != nil {
		collectionErrors = append(collectionErrors, err.Error())
	} else {
		result.CPUPercent = value
	}
	if total, available, err := darwinMemory(); err != nil {
		collectionErrors = append(collectionErrors, err.Error())
	} else {
		result.MemoryTotalBytes = total
		result.MemoryUsedBytes = total - minUint64(available, total)
		if total > 0 {
			result.MemoryPercent = float64(result.MemoryUsedBytes) / float64(total) * 100
		}
	}
	if total, free, err := darwinDisk(); err != nil {
		collectionErrors = append(collectionErrors, err.Error())
	} else {
		result.DiskTotalBytes = total
		result.DiskFreeBytes = free
		if total > 0 {
			result.DiskUsedPercent = float64(total-free) / float64(total) * 100
		}
	}
	result.ForegroundApp = darwinForegroundApp()
	result.Processes = darwinProcesses()
	if len(collectionErrors) > 0 {
		return result, fmt.Errorf("%s", strings.Join(collectionErrors, "; "))
	}
	return result, nil
}

func darwinForegroundApp() *appSnapshot {
	output, err := exec.Command("osascript", "-e", "tell application \"System Events\" to get name of first application process whose frontmost is true").Output()
	if err != nil {
		return nil
	}
	name := strings.TrimSpace(string(output))
	if name == "" {
		return nil
	}
	return &appSnapshot{Name: name}
}

func darwinCPUPercent() (float64, error) {
	output, err := exec.Command("ps", "-A", "-o", "%cpu=").Output()
	if err != nil {
		return 0, fmt.Errorf("read CPU: %w", err)
	}
	var total float64
	for _, value := range strings.Fields(string(output)) {
		parsed, parseErr := strconv.ParseFloat(value, 64)
		if parseErr == nil {
			total += parsed
		}
	}
	cores := runtime.NumCPU()
	if cores < 1 {
		cores = 1
	}
	return total / float64(cores), nil
}

func darwinMemory() (uint64, uint64, error) {
	totalOutput, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0, 0, fmt.Errorf("read memory: %w", err)
	}
	total, err := strconv.ParseUint(strings.TrimSpace(string(totalOutput)), 10, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("parse memory: %w", err)
	}
	output, err := exec.Command("vm_stat").Output()
	if err != nil {
		return total, 0, fmt.Errorf("read memory pages: %w", err)
	}
	pageSize := uint64(4096)
	freePages := uint64(0)
	for _, line := range strings.Split(string(output), "\n") {
		if !strings.Contains(line, "Pages free") && !strings.Contains(line, "Pages inactive") && !strings.Contains(line, "Pages speculative") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		value, parseErr := strconv.ParseUint(strings.Trim(fields[2], "."), 10, 64)
		if parseErr == nil {
			freePages += value
		}
	}
	return total, freePages * pageSize, nil
}

func darwinDisk() (uint64, uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0, fmt.Errorf("read disk: %w", err)
	}
	return stat.Blocks * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize), nil
}

func darwinProcesses() []processSnapshot {
	output, err := exec.Command("ps", "-Ao", "pid=,comm=,%cpu=,rss=", "-r").Output()
	if err != nil {
		return nil
	}
	result := make([]processSnapshot, 0, 8)
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		cpu, cpuErr := strconv.ParseFloat(fields[len(fields)-2], 64)
		memory, memoryErr := strconv.ParseUint(fields[len(fields)-1], 10, 64)
		if pidErr != nil || cpuErr != nil || memoryErr != nil {
			continue
		}
		result = append(result, processSnapshot{Name: strings.Join(fields[1:len(fields)-2], " "), PID: pid, CPUPercent: cpu, MemoryBytes: memory * 1024})
		if len(result) == 8 {
			break
		}
	}
	return result
}
