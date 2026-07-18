//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

var linuxCPUState struct {
	sync.Mutex
	total uint64
	idle  uint64
}

func collectPlatformMetrics() (metricsSnapshot, error) {
	result := metricsSnapshot{}
	var collectionErrors []string
	if value, err := linuxCPUPercent(); err != nil {
		collectionErrors = append(collectionErrors, err.Error())
	} else {
		result.CPUPercent = value
	}
	if total, available, err := linuxMemory(); err != nil {
		collectionErrors = append(collectionErrors, err.Error())
	} else {
		result.MemoryTotalBytes = total
		result.MemoryUsedBytes = total - minUint64(available, total)
		if total > 0 {
			result.MemoryPercent = float64(result.MemoryUsedBytes) / float64(total) * 100
		}
	}
	if total, free, err := linuxDisk(); err != nil {
		collectionErrors = append(collectionErrors, err.Error())
	} else {
		result.DiskTotalBytes = total
		result.DiskFreeBytes = free
		if total > 0 {
			result.DiskUsedPercent = float64(total-free) / float64(total) * 100
		}
	}
	result.Processes = linuxProcesses()
	if len(collectionErrors) > 0 {
		return result, fmt.Errorf("%s", strings.Join(collectionErrors, "; "))
	}
	return result, nil
}

func linuxCPUPercent() (float64, error) {
	file, err := os.Open("/proc/stat")
	if err != nil {
		return 0, fmt.Errorf("read CPU: %w", err)
	}
	defer file.Close()
	line, err := bufio.NewReader(file).ReadString('\n')
	if err != nil {
		return 0, fmt.Errorf("read CPU: %w", err)
	}
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, fmt.Errorf("parse CPU: invalid /proc/stat")
	}
	var values []uint64
	for _, field := range fields[1:] {
		value, parseErr := strconv.ParseUint(field, 10, 64)
		if parseErr != nil {
			return 0, fmt.Errorf("parse CPU: %w", parseErr)
		}
		values = append(values, value)
	}
	var total, idle uint64
	for index, value := range values {
		total += value
		if index < 2 {
			idle += value
		}
	}
	linuxCPUState.Lock()
	defer linuxCPUState.Unlock()
	if linuxCPUState.total == 0 {
		linuxCPUState.total = total
		linuxCPUState.idle = idle
		return 0, nil
	}
	deltaTotal := total - linuxCPUState.total
	deltaIdle := idle - linuxCPUState.idle
	linuxCPUState.total = total
	linuxCPUState.idle = idle
	if deltaTotal == 0 {
		return 0, nil
	}
	return float64(deltaTotal-deltaIdle) / float64(deltaTotal) * 100, nil
}

func linuxMemory() (uint64, uint64, error) {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("read memory: %w", err)
	}
	defer file.Close()
	values := make(map[string]uint64)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		value, parseErr := strconv.ParseUint(fields[1], 10, 64)
		if parseErr == nil {
			values[strings.TrimSuffix(fields[0], ":")] = value * 1024
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, 0, fmt.Errorf("read memory: %w", err)
	}
	return values["MemTotal"], values["MemAvailable"], nil
}

func linuxDisk() (uint64, uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs("/", &stat); err != nil {
		return 0, 0, fmt.Errorf("read disk: %w", err)
	}
	return stat.Blocks * uint64(stat.Bsize), stat.Bavail * uint64(stat.Bsize), nil
}

func linuxProcesses() []processSnapshot {
	ctx, cancel := requestContext()
	defer cancel()
	output, err := exec.CommandContext(ctx, "ps", "-eo", "pid=,comm=,%cpu=,rss=", "--sort=-%cpu").Output()
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
	sort.SliceStable(result, func(left, right int) bool { return result[left].CPUPercent > result[right].CPUPercent })
	return result
}

func init() {
	if runtime.NumCPU() < 1 {
		panic("runtime reports no CPUs")
	}
}
