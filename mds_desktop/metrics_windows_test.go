//go:build windows

package main

import "testing"

func TestCollectPlatformMetrics(t *testing.T) {
	metrics, err := collectPlatformMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if metrics.MemoryTotalBytes == 0 {
		t.Fatal("memory total is empty")
	}
	if metrics.DiskTotalBytes == 0 {
		t.Fatal("disk total is empty")
	}
}
