package main

import (
	"io"
	"net/http"
	"net/http/httptest"
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

func TestLoadConfigAcceptsUTF8BOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := append([]byte{0xef, 0xbb, 0xbf}, []byte(`{"endpoint":"http://example.test","token":"device-token","interval_seconds":30}`)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Endpoint != "http://example.test" || cfg.Token != "device-token" || cfg.IntervalSeconds != 30 {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestSendWithQueuePostsHeartbeat(t *testing.T) {
	var received []byte
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/heartbeats" {
			t.Errorf("path = %s", r.URL.Path)
		}
		authorization = r.Header.Get("Authorization")
		received, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	agent := &agent{
		config:     config{Endpoint: server.URL, Token: "device-token"},
		queuePath:  filepath.Join(t.TempDir(), "queue.jsonl"),
		httpClient: server.Client(),
	}
	payload := []byte(`{"reported_at":"2026-07-18T00:00:00Z"}`)
	if err := agent.sendWithQueue(payload); err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer device-token" {
		t.Fatalf("authorization = %q", authorization)
	}
	if string(received) != string(payload) {
		t.Fatalf("payload = %s, want %s", received, payload)
	}
}
