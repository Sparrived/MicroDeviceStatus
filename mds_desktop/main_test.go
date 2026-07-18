package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOfflineQueueRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "offline.queue.jsonl")
	items := [][]byte{[]byte(`{"reported_at":"one"}`), []byte(`{"reported_at":"two"}`)}
	if err := writeQueue(path, items); err != nil {
		 t.Fatal(err)
	}
	read, err := readQueue(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(read) != len(items) || string(read[1]) != string(items[1]) {
		t.Fatalf("queue = %q, want %q", read, items)
	}
	if err := writeQueue(path, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("queue still exists, stat error = %v", err)
	}
}

func TestPlatformName(t *testing.T) {
	if platformName() == "" {
		t.Fatal("platform name is empty")
	}
}
