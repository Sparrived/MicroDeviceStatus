//go:build windows

package main

import "testing"

func TestWindowsActivityState(t *testing.T) {
	if got := windowsActivityState(0); got != "busy" {
		t.Fatalf("zero idle time = %q, want busy", got)
	}
	if got := windowsActivityState(windowsIdleAfter.Seconds() - 1); got != "busy" {
		t.Fatalf("below threshold = %q, want busy", got)
	}
	if got := windowsActivityState(windowsIdleAfter.Seconds()); got != "idle" {
		t.Fatalf("at threshold = %q, want idle", got)
	}
}

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
	if metrics.ActivityState != "busy" && metrics.ActivityState != "idle" {
		t.Fatalf("activity state = %q", metrics.ActivityState)
	}
}
