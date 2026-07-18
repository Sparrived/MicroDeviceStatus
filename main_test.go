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
