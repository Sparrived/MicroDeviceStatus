package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestHeartbeatRoundTrip(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &server{db: db, adminSecret: "admin-secret"}

	createBody := bytes.NewBufferString(`{"name":"test-phone","platform":"android"}`)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/devices", createBody)
	createReq.Header.Set("Authorization", "Bearer admin-secret")
	createResp := httptest.NewRecorder()
	s.createDevice(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResp.Code, createResp.Body.String())
	}
	var created struct {
		Device device `json:"device"`
		Token  string `json:"token"`
	}
	if err := json.Unmarshal(createResp.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Device.ID == "" || created.Token == "" {
		t.Fatal("create response did not include device id and token")
	}

	heartbeatBody := bytes.NewBufferString(`{"reported_at":"2026-07-18T00:00:00Z","metrics":{"cpu_percent":12.5},"location":{"latitude":31.2304,"longitude":121.4737,"accuracy_meters":20},"processes":[]}`)
	heartbeatReq := httptest.NewRequest(http.MethodPost, "/api/v1/heartbeats", heartbeatBody)
	heartbeatReq.Header.Set("Authorization", "Bearer "+created.Token)
	heartbeatResp := httptest.NewRecorder()
	s.receiveHeartbeat(heartbeatResp, heartbeatReq)
	if heartbeatResp.Code != http.StatusAccepted {
		t.Fatalf("heartbeat status = %d, body = %s", heartbeatResp.Code, heartbeatResp.Body.String())
	}

	latestReq := httptest.NewRequest(http.MethodGet, "/api/v1/devices/"+created.Device.ID+"/latest", nil)
	latestReq.SetPathValue("id", created.Device.ID)
	latestReq.Header.Set("Authorization", "Bearer admin-secret")
	latestResp := httptest.NewRecorder()
	s.getLatest(latestResp, latestReq)
	if latestResp.Code != http.StatusOK {
		t.Fatalf("latest status = %d, body = %s", latestResp.Code, latestResp.Body.String())
	}
	if !bytes.Contains(latestResp.Body.Bytes(), []byte(`"cpu_percent":12.5`)) {
		t.Fatalf("latest response did not contain heartbeat payload: %s", latestResp.Body.String())
	}
	if !bytes.Contains(latestResp.Body.Bytes(), []byte(`"latitude":31.2304`)) {
		t.Fatalf("latest response did not contain location payload: %s", latestResp.Body.String())
	}
}

func TestAdminAndDeviceTokensAreSeparated(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &server{db: db, adminSecret: "admin-secret"}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	resp := httptest.NewRecorder()
	s.listDevices(resp, req)
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusUnauthorized)
	}
}

func TestPublicSnapshotAuthAndProjection(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	if _, err := db.Exec(`
		INSERT INTO devices (id, name, platform, token_hash, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, ?), (?, ?, ?, ?, ?, NULL)
	`, "public", "公开设备", "android", hashToken("public-device"), now.Add(-time.Hour).Format(time.RFC3339Nano), now.Add(-10*time.Second).Format(time.RFC3339Nano),
		"hidden", "不公开设备", "windows", hashToken("hidden-device"), now.Add(-time.Hour).Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
		"never", "未上线设备", "android", hashToken("never-device"), now.Format(time.RFC3339Nano),
	); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO reports (device_id, reported_at, received_at, payload)
		VALUES (?, ?, ?, ?)
	`, "public", now.Add(-11*time.Second).Format(time.RFC3339Nano), now.Add(-10*time.Second).Format(time.RFC3339Nano), []byte(`{"reported_at":"2026-07-18T00:00:00Z","hostname":"secret-host","metrics":{"cpu_percent":0,"battery_percent":76,"network_connected":true,"memory_percent":43.2,"disk_used_percent":68.1},"foreground_app":{"name":"微信","package_name":"com.tencent.mm","captured_at":"2026-07-18T00:00:00Z","title":"私人文档.docx - 编辑器"},"location":{"country":"中国","province":"江苏省","city":"无锡市","district":"滨湖区","latitude":31.49,"longitude":120.31,"accuracy_meters":80,"captured_at":"2026-07-18T00:00:00Z"},"processes":[{"name":"secret.exe","pid":123}]}`))
	if err != nil {
		t.Fatal(err)
	}

	s := &server{
		db:           db,
		publicSecret: "public-secret",
		publicIDs:    map[string]struct{}{"public": {}, "never": {}},
		onlineAfter:  5 * time.Minute,
		staleAfter:   30 * time.Minute,
	}

	for _, token := range []string{"", "admin-secret", "wrong-secret"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/public/snapshot", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp := httptest.NewRecorder()
		s.publicSnapshot(resp, req)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("token %q status = %d, want %d", token, resp.Code, http.StatusUnauthorized)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/public/snapshot", nil)
	req.Header.Set("Authorization", "Bearer public-secret")
	resp := httptest.NewRecorder()
	s.publicSnapshot(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("public snapshot status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if resp.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control = %q", resp.Header().Get("Cache-Control"))
	}
	var snapshot struct {
		Devices []publicDevice `json:"devices"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Devices) != 2 {
		t.Fatalf("public devices = %d, want 2", len(snapshot.Devices))
	}
	if snapshot.Devices[0].ID != "never" || snapshot.Devices[0].Status != "never_seen" {
		t.Fatalf("unexpected first public device: %+v", snapshot.Devices[0])
	}
	public := snapshot.Devices[1]
	if public.ID != "public" || public.Status != "online" || public.HeartbeatAge == nil {
		t.Fatalf("unexpected public device status: %+v", public)
	}
	if public.Metrics.CPUPercent == nil || *public.Metrics.CPUPercent != 0 || public.Metrics.BatteryPercent == nil || *public.Metrics.BatteryPercent != 76 {
		t.Fatalf("metrics were not projected: %+v", public.Metrics)
	}
	if public.ForegroundApp == nil || public.ForegroundApp.PackageName == nil || *public.ForegroundApp.PackageName != "com.tencent.mm" {
		t.Fatalf("foreground app was not projected: %+v", public.ForegroundApp)
	}
	if public.Location == nil || public.Location.District == nil || *public.Location.District != "滨湖区" {
		t.Fatalf("location was not projected: %+v", public.Location)
	}
	if public.Location.Country == nil || *public.Location.Country != "中国" || public.Location.Province == nil || *public.Location.Province != "江苏省" || public.Location.City == nil || *public.Location.City != "无锡市" {
		t.Fatalf("location hierarchy was not projected: %+v", public.Location)
	}
	for _, forbidden := range []string{"latitude", "longitude", "processes", "secret.exe", "私人文档", "secret-host", "public-device"} {
		if bytes.Contains(resp.Body.Bytes(), []byte(forbidden)) {
			t.Fatalf("public response contains forbidden field/value %q: %s", forbidden, resp.Body.String())
		}
	}
}

func TestPublicStatusBoundaries(t *testing.T) {
	s := &server{onlineAfter: 5 * time.Minute, staleAfter: 30 * time.Minute}
	now := time.Date(2026, 7, 18, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		age    time.Duration
		status string
	}{
		{age: 299 * time.Second, status: "online"},
		{age: 300 * time.Second, status: "stale"},
		{age: 1799 * time.Second, status: "stale"},
		{age: 1800 * time.Second, status: "offline"},
	} {
		seen := now.Add(-test.age).Format(time.RFC3339Nano)
		status, age := s.statusForDevice(&seen, now)
		if status != test.status || age == nil || *age != int64(test.age/time.Second) {
			t.Fatalf("age %s => status %q, heartbeat age %v", test.age, status, age)
		}
	}
	status, age := s.statusForDevice(nil, now)
	if status != "never_seen" || age != nil {
		t.Fatalf("never seen => status %q, age %v", status, age)
	}
}

func TestReportRetention(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO devices (id, name, platform, token_hash, created_at) VALUES (?, ?, ?, ?, ?)`, "retained", "Retained", "android", hashToken("retained"), now.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, reportedAt := range []time.Time{now.AddDate(0, 0, -31), now.AddDate(0, 0, -29)} {
		value := reportedAt.Format(time.RFC3339Nano)
		if _, err := db.Exec(`INSERT INTO reports (device_id, reported_at, received_at, payload) VALUES (?, ?, ?, ?)`, "retained", value, value, `{}`); err != nil {
			t.Fatal(err)
		}
	}
	if err := (&server{db: db, retentionDays: 30}).cleanupReports(now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM reports`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("retained report count = %d, want 1", count)
	}
}

func TestLoginSession(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &server{
		db:            db,
		adminSecret:   "admin-token",
		adminUsername: "admin",
		adminPassword: "password",
		sessions:      make(map[string]time.Time),
	}

	badLoginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"wrong"}`))
	badLoginResp := httptest.NewRecorder()
	s.login(badLoginResp, badLoginReq)
	if badLoginResp.Code != http.StatusUnauthorized {
		t.Fatalf("invalid login status = %d, want %d", badLoginResp.Code, http.StatusUnauthorized)
	}

	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewBufferString(`{"username":"admin","password":"password"}`))
	loginResp := httptest.NewRecorder()
	s.login(loginResp, loginReq)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResp.Code, loginResp.Body.String())
	}
	cookies := loginResp.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != sessionName {
		t.Fatalf("login did not set the session cookie")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meReq.AddCookie(cookies[0])
	meResp := httptest.NewRecorder()
	s.authMe(meResp, meReq)
	if meResp.Code != http.StatusOK {
		t.Fatalf("auth/me status = %d, want %d", meResp.Code, http.StatusOK)
	}

	devicesReq := httptest.NewRequest(http.MethodGet, "/api/v1/devices", nil)
	devicesReq.AddCookie(cookies[0])
	devicesResp := httptest.NewRecorder()
	s.listDevices(devicesResp, devicesReq)
	if devicesResp.Code != http.StatusOK {
		t.Fatalf("session did not authorize management API: status = %d", devicesResp.Code)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	logoutReq.AddCookie(cookies[0])
	logoutResp := httptest.NewRecorder()
	s.logout(logoutResp, logoutReq)
	if logoutResp.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want %d", logoutResp.Code, http.StatusNoContent)
	}
	meAfterLogoutReq := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	meAfterLogoutReq.AddCookie(cookies[0])
	meAfterLogoutResp := httptest.NewRecorder()
	s.authMe(meAfterLogoutResp, meAfterLogoutReq)
	if meAfterLogoutResp.Code != http.StatusUnauthorized {
		t.Fatalf("auth/me after logout status = %d, want %d", meAfterLogoutResp.Code, http.StatusUnauthorized)
	}
}
