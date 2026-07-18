package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const defaultClientVersion = "0.1.0"

type config struct {
	Endpoint        string `json:"endpoint"`
	Token           string `json:"token"`
	IntervalSeconds int    `json:"interval_seconds"`
	ClientVersion   string `json:"client_version"`
}

type heartbeat struct {
	ReportedAt    string             `json:"reported_at"`
	ClientVersion string             `json:"client_version"`
	Platform      string             `json:"platform"`
	Hostname      string             `json:"hostname"`
	Metrics       metricsSnapshot    `json:"metrics"`
	ForegroundApp *appSnapshot       `json:"foreground_app,omitempty"`
	Processes     []processSnapshot  `json:"processes,omitempty"`
}

type appSnapshot struct {
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
}

type processSnapshot struct {
	Name        string  `json:"name"`
	PID         int     `json:"pid"`
	CPUPercent  float64 `json:"cpu_percent,omitempty"`
	MemoryBytes uint64  `json:"memory_bytes,omitempty"`
}

type agent struct {
	config     config
	queuePath  string
	httpClient *http.Client
}

func main() {
	configPath := flag.String("config", "mds-desktop.json", "path to the client configuration file")
	endpoint := flag.String("endpoint", "", "server URL, for example https://mds.example.com")
	token := flag.String("token", "", "device bearer token")
	interval := flag.Int("interval", 0, "heartbeat interval in seconds")
	version := flag.String("version", "", "client version included in heartbeats")
	once := flag.Bool("once", false, "send one heartbeat and exit")
	flag.Parse()

	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if value := strings.TrimSpace(*endpoint); value != "" {
		cfg.Endpoint = value
	}
	if value := strings.TrimSpace(*token); value != "" {
		cfg.Token = value
	}
	if *interval > 0 {
		cfg.IntervalSeconds = *interval
	}
	if value := strings.TrimSpace(*version); value != "" {
		cfg.ClientVersion = value
	}
	if value := strings.TrimSpace(os.Getenv("MDS_ENDPOINT")); cfg.Endpoint == "" && value != "" {
		cfg.Endpoint = value
	}
	if value := strings.TrimSpace(os.Getenv("MDS_DEVICE_TOKEN")); cfg.Token == "" && value != "" {
		cfg.Token = value
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://127.0.0.1:8080"
	}
	if cfg.Token == "" {
		log.Fatal("device token is required; set token in the config file or MDS_DEVICE_TOKEN")
	}
	if cfg.IntervalSeconds < 5 {
		cfg.IntervalSeconds = 60
	}
	if cfg.ClientVersion == "" {
		cfg.ClientVersion = defaultClientVersion
	}

	queuePath := strings.TrimSuffix(*configPath, filepath.Ext(*configPath)) + ".queue.jsonl"
	a := &agent{
		config:    cfg,
		queuePath: queuePath,
		httpClient: &http.Client{Timeout: 15 * time.Second},
	}

	if *once {
		if err := a.runCycle(); err != nil {
			log.Fatal(err)
		}
		return
	}

	log.Printf("mds_desktop sending to %s every %s", strings.TrimRight(cfg.Endpoint, "/"), time.Duration(cfg.IntervalSeconds)*time.Second)
	for {
		if err := a.runCycle(); err != nil {
			log.Printf("heartbeat deferred: %v", err)
		}
		time.Sleep(time.Duration(cfg.IntervalSeconds) * time.Second)
	}
}

func loadConfig(path string) (config, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config{}, nil
	}
	if err != nil {
		return config{}, fmt.Errorf("read config: %w", err)
	}
	var cfg config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return config{}, fmt.Errorf("decode config: %w", err)
	}
	return cfg, nil
}

func (a *agent) runCycle() error {
	report, err := buildHeartbeat(a.config.ClientVersion)
	if err != nil {
		return err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return fmt.Errorf("encode heartbeat: %w", err)
	}
	if err := a.sendWithQueue(payload); err != nil {
		return err
	}
	log.Printf("heartbeat accepted at %s", report.ReportedAt)
	return nil
}

func buildHeartbeat(clientVersion string) (heartbeat, error) {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	metrics, metricsErr := collectPlatformMetrics()
	result := heartbeat{
		ReportedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		ClientVersion: clientVersion,
		Platform:      platformName(),
		Hostname:      hostname,
		Metrics:       metrics,
		ForegroundApp: metrics.ForegroundApp,
		Processes:     metrics.Processes,
	}
	if metricsErr != nil {
		result.Metrics.CollectionErrors = []string{metricsErr.Error()}
	}
	return result, nil
}

func (a *agent) sendWithQueue(payload []byte) error {
	pending, err := readQueue(a.queuePath)
	if err != nil {
		return err
	}
	pending = append(pending, append([]byte(nil), payload...))
	if len(pending) > 1000 {
		pending = pending[len(pending)-1000:]
	}
	for index, item := range pending {
		if err := a.post(item); err != nil {
			if queueErr := writeQueue(a.queuePath, pending[index:]); queueErr != nil {
				return fmt.Errorf("send heartbeat: %v; save offline queue: %w", err, queueErr)
			}
			return fmt.Errorf("send heartbeat: %w", err)
		}
	}
	return writeQueue(a.queuePath, nil)
}

func (a *agent) post(payload []byte) error {
	endpoint := strings.TrimRight(a.config.Endpoint, "/") + "/api/v1/heartbeats"
	request, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+a.config.Token)
	request.Header.Set("Content-Type", "application/json")
	response, err := a.httpClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("server returned %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func readQueue(path string) ([][]byte, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open offline queue: %w", err)
	}
	defer file.Close()
	var items [][]byte
	decoder := json.NewDecoder(file)
	for {
		var item json.RawMessage
		if err := decoder.Decode(&item); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("decode offline queue: %w", err)
		}
		items = append(items, append([]byte(nil), item...))
	}
	return items, nil
}

func writeQueue(path string, items [][]byte) error {
	if len(items) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("clear offline queue: %w", err)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create queue directory: %w", err)
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create offline queue: %w", err)
	}
	encoder := json.NewEncoder(file)
	for _, item := range items {
		if err := encoder.Encode(json.RawMessage(item)); err != nil {
			file.Close()
			return fmt.Errorf("write offline queue: %w", err)
		}
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close offline queue: %w", err)
	}
	if err := os.Rename(temporary, path); err != nil {
		return fmt.Errorf("replace offline queue: %w", err)
	}
	return nil
}

func platformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	default:
		return runtime.GOOS
	}
}

func requestContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 3*time.Second)
}
