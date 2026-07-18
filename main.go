package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const (
	maxJSONBody = 1 << 20
	defaultAddr = ":8080"
	defaultDB   = "data/micro-device-status.db"
	sessionName = "mds_session"
	sessionTTL  = 12 * time.Hour
)

// version is set at build time via -ldflags "-X main.version=...".
var version = "dev"

type server struct {
	db            *sql.DB
	adminSecret   string
	adminUsername string
	adminPassword string
	sessions      map[string]time.Time
	sessionsMu    sync.Mutex
}

type device struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Platform   string  `json:"platform"`
	CreatedAt  string  `json:"created_at"`
	LastSeenAt *string `json:"last_seen_at,omitempty"`
	Disabled   bool    `json:"disabled"`
}

type report struct {
	ID         int64           `json:"id"`
	ReportedAt string          `json:"reported_at"`
	ReceivedAt string          `json:"received_at"`
	Payload    json.RawMessage `json:"payload"`
}

func main() {
	addr := flag.String("addr", envOr("MDS_ADDR", defaultAddr), "HTTP listen address")
	dbPath := flag.String("db", envOr("MDS_DB_PATH", defaultDB), "SQLite database path")
	adminSecret := envOr("MDS_ADMIN_TOKEN", "")
	adminUsername := envOr("MDS_ADMIN_USERNAME", "")
	adminPassword := envOr("MDS_ADMIN_PASSWORD", "")
	flag.Parse()

	if adminSecret == "" {
		log.Fatal("MDS_ADMIN_TOKEN must be set")
	}
	if adminUsername == "" || adminPassword == "" {
		log.Fatal("MDS_ADMIN_USERNAME and MDS_ADMIN_PASSWORD must be set")
	}

	db, err := openDB(*dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	s := &server{
		db:            db,
		adminSecret:   adminSecret,
		adminUsername: adminUsername,
		adminPassword: adminPassword,
		sessions:      make(map[string]time.Time),
	}
	log.Printf("micro-device-status %s listening on %s", version, *addr)
	if err := http.ListenAndServe(*addr, s.routes()); err != nil {
		log.Fatal(err)
	}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.dashboard)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("POST /api/v1/auth/login", s.login)
	mux.HandleFunc("POST /api/v1/auth/logout", s.logout)
	mux.HandleFunc("GET /api/v1/auth/me", s.authMe)
	mux.HandleFunc("POST /api/v1/devices", s.createDevice)
	mux.HandleFunc("GET /api/v1/devices", s.listDevices)
	mux.HandleFunc("GET /api/v1/devices/{id}", s.getDevice)
	mux.HandleFunc("GET /api/v1/devices/{id}/latest", s.getLatest)
	mux.HandleFunc("GET /api/v1/devices/{id}/reports", s.listReports)
	mux.HandleFunc("POST /api/v1/heartbeats", s.receiveHeartbeat)
	return logging(mux)
}

func (s *server) healthz(w http.ResponseWriter, _ *http.Request) {
	if err := s.db.Ping(); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if !secureEqual(strings.TrimSpace(input.Username), s.adminUsername) || !secureEqual(input.Password, s.adminPassword) {
		writeError(w, http.StatusUnauthorized, "invalid username or password")
		return
	}

	token, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	expiresAt := time.Now().Add(sessionTTL)
	s.sessionsMu.Lock()
	for existing, expires := range s.sessions {
		if !expires.After(time.Now()) {
			delete(s.sessions, existing)
		}
	}
	s.sessions[token] = expiresAt
	s.sessionsMu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     sessionName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || os.Getenv("MDS_COOKIE_SECURE") == "1",
		SameSite: http.SameSiteLaxMode,
		Expires:  expiresAt,
		MaxAge:   int(sessionTTL / time.Second),
	})
	writeJSON(w, http.StatusOK, map[string]string{"username": s.adminUsername})
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionName); err == nil {
		s.sessionsMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionsMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   r.TLS != nil || os.Getenv("MDS_COOKIE_SECURE") == "1",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) authMe(w http.ResponseWriter, r *http.Request) {
	if !s.sessionValid(r) {
		writeError(w, http.StatusUnauthorized, "login required")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": s.adminUsername})
}

func (s *server) sessionValid(r *http.Request) bool {
	cookie, err := r.Cookie(sessionName)
	if err != nil || cookie.Value == "" {
		return false
	}
	s.sessionsMu.Lock()
	expiresAt, exists := s.sessions[cookie.Value]
	if exists && !expiresAt.After(time.Now()) {
		delete(s.sessions, cookie.Value)
		exists = false
	}
	s.sessionsMu.Unlock()
	return exists
}

func (s *server) createDevice(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	var input struct {
		Name     string `json:"name"`
		Platform string `json:"platform"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	input.Platform = strings.TrimSpace(input.Platform)
	if input.Name == "" || input.Platform == "" {
		writeError(w, http.StatusBadRequest, "name and platform are required")
		return
	}

	id, err := randomHex(16)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create device id")
		return
	}
	token, err := randomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create device token")
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`
		INSERT INTO devices (id, name, platform, token_hash, created_at)
		VALUES (?, ?, ?, ?, ?)
	`, id, input.Name, input.Platform, hashToken(token), now)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save device")
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"device": device{ID: id, Name: input.Name, Platform: input.Platform, CreatedAt: now},
		"token":  token,
	})
}

func (s *server) listDevices(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}

	rows, err := s.db.Query(`
		SELECT id, name, platform, created_at, last_seen_at, disabled
		FROM devices
		ORDER BY COALESCE(last_seen_at, created_at) DESC
	`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list devices")
		return
	}
	defer rows.Close()

	devices := make([]device, 0)
	for rows.Next() {
		var d device
		var lastSeen sql.NullString
		var disabled int
		if err := rows.Scan(&d.ID, &d.Name, &d.Platform, &d.CreatedAt, &lastSeen, &disabled); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read devices")
			return
		}
		if lastSeen.Valid {
			d.LastSeenAt = &lastSeen.String
		}
		d.Disabled = disabled != 0
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read devices")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

func (s *server) getDevice(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	d, err := s.findDevice(r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read device")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *server) getLatest(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	deviceID := r.PathValue("id")
	if _, err := s.findDevice(deviceID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read device")
		return
	}

	var item report
	if err := s.db.QueryRow(`
		SELECT id, reported_at, received_at, payload
		FROM reports WHERE device_id = ?
		ORDER BY id DESC LIMIT 1
	`, deviceID).Scan(&item.ID, &item.ReportedAt, &item.ReceivedAt, &item.Payload); errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"report": nil})
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read latest report")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"report": item})
}

func (s *server) listReports(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	deviceID := r.PathValue("id")
	if _, err := s.findDevice(deviceID); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "device not found")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read device")
		return
	}

	limit := 50
	if value := r.URL.Query().Get("limit"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 1 || parsed > 200 {
			writeError(w, http.StatusBadRequest, "limit must be between 1 and 200")
			return
		}
		limit = parsed
	}
	query := `
		SELECT id, reported_at, received_at, payload
		FROM reports WHERE device_id = ?
	`
	args := []any{deviceID}
	if from := r.URL.Query().Get("from"); from != "" {
		query += " AND reported_at >= ?"
		args = append(args, from)
	}
	if to := r.URL.Query().Get("to"); to != "" {
		query += " AND reported_at <= ?"
		args = append(args, to)
	}
	query += " ORDER BY id DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list reports")
		return
	}
	defer rows.Close()
	reports := make([]report, 0)
	for rows.Next() {
		var item report
		if err := rows.Scan(&item.ID, &item.ReportedAt, &item.ReceivedAt, &item.Payload); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read reports")
			return
		}
		reports = append(reports, item)
	}
	if err := rows.Err(); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read reports")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"reports": reports})
}

func (s *server) receiveHeartbeat(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		writeError(w, http.StatusUnauthorized, "bearer token required")
		return
	}

	var deviceID string
	var disabled int
	if err := s.db.QueryRow(`SELECT id, disabled FROM devices WHERE token_hash = ?`, hashToken(token)).Scan(&deviceID, &disabled); errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusUnauthorized, "invalid device token")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to authenticate device")
		return
	}
	if disabled != 0 {
		writeError(w, http.StatusForbidden, "device is disabled")
		return
	}

	payload, reportedAt, ok := decodeHeartbeat(w, r)
	if !ok {
		return
	}
	receivedAt := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.Exec(`
		INSERT INTO reports (device_id, reported_at, received_at, payload)
		VALUES (?, ?, ?, ?)
	`, deviceID, reportedAt, receivedAt, payload)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save heartbeat")
		return
	}
	if _, err := s.db.Exec(`UPDATE devices SET last_seen_at = ? WHERE id = ?`, receivedAt, deviceID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update device status")
		return
	}
	reportID, _ := result.LastInsertId()
	writeJSON(w, http.StatusAccepted, map[string]any{
		"device_id":   deviceID,
		"report_id":   reportID,
		"received_at": receivedAt,
	})
}

func (s *server) findDevice(id string) (device, error) {
	var d device
	var lastSeen sql.NullString
	var disabled int
	err := s.db.QueryRow(`
		SELECT id, name, platform, created_at, last_seen_at, disabled
		FROM devices WHERE id = ?
	`, id).Scan(&d.ID, &d.Name, &d.Platform, &d.CreatedAt, &lastSeen, &disabled)
	if lastSeen.Valid {
		d.LastSeenAt = &lastSeen.String
	}
	d.Disabled = disabled != 0
	return d, err
}

func (s *server) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.sessionValid(r) {
		return true
	}
	token, ok := bearerToken(r)
	if !ok || !secureEqual(token, s.adminSecret) {
		writeError(w, http.StatusUnauthorized, "login required")
		return false
	}
	return true
}

func openDB(path string) (*sql.DB, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	for _, statement := range []string{
		`PRAGMA journal_mode = WAL`,
		`PRAGMA busy_timeout = 5000`,
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS devices (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			platform TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			created_at TEXT NOT NULL,
			last_seen_at TEXT,
			disabled INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS reports (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id TEXT NOT NULL REFERENCES devices(id),
			reported_at TEXT NOT NULL,
			received_at TEXT NOT NULL,
			payload TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS reports_device_time ON reports(device_id, reported_at DESC)`,
	} {
		if _, err := db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize database: %w", err)
		}
	}
	return db, nil
}

func decodeHeartbeat(w http.ResponseWriter, r *http.Request) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	var payload map[string]json.RawMessage
	if err := decoder.Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, "", false
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return nil, "", false
	}
	reportedAt := time.Now().UTC().Format(time.RFC3339Nano)
	if raw, exists := payload["reported_at"]; exists {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			writeError(w, http.StatusBadRequest, "reported_at must be an RFC3339 string")
			return nil, "", false
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			writeError(w, http.StatusBadRequest, "reported_at must be an RFC3339 string")
			return nil, "", false
		}
		reportedAt = parsed.UTC().Format(time.RFC3339Nano)
	}
	normalized, err := json.Marshal(payload)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return nil, "", false
	}
	return normalized, reportedAt, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	if err := ensureEOF(decoder); err != nil {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("extra JSON value")
		}
		return err
	}
	return nil
}

func bearerToken(r *http.Request) (string, bool) {
	value := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func secureEqual(left, right string) bool {
	leftSum := sha256.Sum256([]byte(left))
	rightSum := sha256.Sum256([]byte(right))
	return subtle.ConstantTimeCompare(leftSum[:], rightSum[:]) == 1
}

func randomHex(size int) (string, error) {
	data := make([]byte, size)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return hex.EncodeToString(data), nil
}

func envOr(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(started).Round(time.Millisecond))
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
