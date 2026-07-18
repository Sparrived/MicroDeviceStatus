//go:build !windows && !linux && !darwin

package main

import "fmt"

func collectPlatformMetrics() (metricsSnapshot, error) {
	return metricsSnapshot{}, fmt.Errorf("metrics are not implemented for this operating system")
}
