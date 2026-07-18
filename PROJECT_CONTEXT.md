# MicroDeviceStatus Project Context

This document is the project memory for future development sessions. Read it
before changing the server, API, authentication, database schema, or dashboard.

## Purpose

MicroDeviceStatus receives periodic status reports from phone and computer
clients, stores the reports, and exposes them through an HTTP API and a small
built-in dashboard.

The project contains the server plus first-party desktop and Android client
agents. Client collection remains platform-specific and intentionally small.

## Current Stack

- Go 1.25 or newer. The development machine currently has Go 1.26.5.
- Go standard library `net/http` with method/path routing.
- SQLite through `modernc.org/sqlite`.
- HTML, CSS, and JavaScript embedded into the Go binary with `go:embed`.
- No frontend framework, HTTP framework, message queue, or microservice layer.
- No CGO and no OS-specific system calls in the server process. Paths use
  `path/filepath`; the binary is portable across Windows and Linux.

The intentional design goal is one small deployable binary plus one SQLite
database file that runs the same way on Windows and Linux.

## Project Layout

```text
main.go                 HTTP server, authentication, API, database setup
web.go                  Embedded dashboard loader and GET /
web/index.html          Login page and device dashboard
mds_desktop/            Cross-platform Go device client
mds_mobile/             Native Android device client
main_test.go            Database, API, token, and session tests
README.md               Quick start and API examples
DEPLOY.md               Production deployment (bare metal + Docker)
PROJECT_CONTEXT.md      This document
scripts/build.ps1       Cross-compile Windows/Linux binaries (PowerShell)
scripts/build.sh        Cross-compile Windows/Linux binaries (bash)
go.mod / go.sum         Go module metadata
data/                   Runtime SQLite data, ignored by Git
dist/                   Build outputs, ignored by Git
```

## Running Locally

The server is pure Go with `modernc.org/sqlite` (no CGO). The same source
runs on Windows and Linux.

Windows PowerShell:

```powershell
$env:MDS_ADMIN_TOKEN = "replace-with-a-long-device-api-token"
$env:MDS_ADMIN_USERNAME = "admin"
$env:MDS_ADMIN_PASSWORD = "replace-with-a-long-password"
$env:MDS_PUBLIC_STATUS_TOKEN = "replace-with-a-separate-read-only-token"
$env:MDS_PUBLIC_DEVICE_IDS = "computer-device-id,phone-device-id"
go run .
```

Linux / macOS:

```bash
export MDS_ADMIN_TOKEN="replace-with-a-long-device-api-token"
export MDS_ADMIN_USERNAME="admin"
export MDS_ADMIN_PASSWORD="replace-with-a-long-password"
export MDS_PUBLIC_STATUS_TOKEN="replace-with-a-separate-read-only-token"
export MDS_PUBLIC_DEVICE_IDS="computer-device-id,phone-device-id"
go run .
```

Defaults:

- Dashboard: `http://127.0.0.1:8080/`
- Health check: `GET /healthz`
- Database: `data/micro-device-status.db`
- Listen address: `:8080`

Environment variables:

- `MDS_ADMIN_TOKEN`: bearer token for administrative API access and device
  provisioning. This is separate from the dashboard password.
- `MDS_ADMIN_USERNAME`: dashboard login username.
- `MDS_ADMIN_PASSWORD`: dashboard login password.
- `MDS_PUBLIC_STATUS_TOKEN`: separate bearer token for the allowlisted public snapshot; never expose it to browsers.
- `MDS_PUBLIC_DEVICE_IDS`: comma-separated device IDs allowed in the public snapshot.
- `MDS_STATUS_ONLINE_SECONDS`: online threshold, default 300.
- `MDS_STATUS_STALE_SECONDS`: stale threshold, default 1800.
- `MDS_REPORT_RETENTION_DAYS`: raw report retention, default 30; set to 0 to disable cleanup.
- `MDS_COOKIE_SECURE=1`: use when HTTPS terminates outside the Go process.
- `MDS_ADDR`: HTTP listen address.
- `MDS_DB_PATH`: SQLite database path.

Never reuse local development credentials outside local testing.

## Deployment

Production install (bare metal or Docker), reverse proxy, systemd and Windows
service setup, backup, and upgrade steps are documented in [DEPLOY.md](DEPLOY.md).

Deploy as a single process behind HTTPS. Keep SQLite on local disk. Set
`MDS_COOKIE_SECURE=1` when TLS terminates outside the Go process.

## Authentication Model

There are two independent credential types.

### Dashboard and management APIs

The browser logs in with `MDS_ADMIN_USERNAME` and `MDS_ADMIN_PASSWORD`:

```text
POST /api/v1/auth/login
GET  /api/v1/auth/me
POST /api/v1/auth/logout
```

Successful login creates an in-memory session and an `HttpOnly`,
`SameSite=Lax` cookie named `mds_session`. The session lifetime is 12 hours.
Sessions are lost when the server restarts. This is intentional for the
single-process MVP; move sessions to a persistent/shared store only if the
service becomes multi-instance.

Management APIs accept either this browser session cookie or the admin bearer
token:

```text
Authorization: Bearer <MDS_ADMIN_TOKEN>
```

### Device clients

Each device is provisioned through `POST /api/v1/devices`. The response
contains a random device token. The plaintext token is returned only at
provisioning time; the database stores only its SHA-256 hash.

Device clients send heartbeats with:

```text
Authorization: Bearer <device-token>
```

The dashboard never needs to know device tokens.

## API Contract

### Public

```text
GET /healthz
GET /api/v1/public/snapshot
```

Returns `{"status":"ok"}` when the database is reachable.

The snapshot requires `Authorization: Bearer <MDS_PUBLIC_STATUS_TOKEN>` and
only includes `MDS_PUBLIC_DEVICE_IDS`. It returns server-derived
`never_seen`, `online`, `stale`, or `offline` status and a fixed projection of
metrics, foreground app, and district-level location. It never returns device
tokens, process lists, window titles, hostnames, or raw latitude/longitude.
The endpoint sends `Cache-Control: no-store`; a blog server may cache its own
proxy response.

### Admin management

```text
POST /api/v1/devices
GET  /api/v1/devices
GET  /api/v1/devices/{id}
GET  /api/v1/devices/{id}/latest
GET  /api/v1/devices/{id}/reports?limit=50&from=<RFC3339>&to=<RFC3339>
```

Create device body:

```json
{
  "name": "office-pc",
  "platform": "windows"
}
```

### Device ingestion

```text
POST /api/v1/heartbeats
```

The heartbeat body is intentionally flexible JSON. The server accepts an
optional `reported_at` RFC3339 string and preserves the remaining payload.
Typical fields are `client_version`, `metrics`, `foreground_app`, `processes`,
and an opt-in `location` object from mobile clients.

The server adds `received_at` in the report record. Report payloads are capped
at 1 MiB. A daily cleanup deletes reports older than
`MDS_REPORT_RETENTION_DAYS` (30 days by default).

## Database Model

SQLite is initialized automatically by `openDB`.

### `devices`

- `id`: random server-generated device ID
- `name`: display name
- `platform`: client platform string
- `token_hash`: unique SHA-256 hash of the device bearer token
- `created_at`: RFC3339 timestamp
- `last_seen_at`: server receive timestamp of the latest heartbeat
- `disabled`: integer boolean reserved for device disabling

### `reports`

- `id`: auto-increment report ID
- `device_id`: foreign key to `devices`
- `reported_at`: client timestamp, normalized to UTC
- `received_at`: server timestamp
- `payload`: original normalized JSON payload

SQLite uses WAL mode, a busy timeout, foreign keys, one connection, and an
index on `(device_id, reported_at)`.

The MVP stores raw report JSON. Do not normalize every possible metric until
there is a real query or retention requirement that needs it.

## Dashboard Behavior

The page at `/` is embedded in the binary and does not need a separate static
file server or npm build.

After login it provides:

- device list and search
- recent, delayed, and offline status
- latest payload inspection
- recent report history
- device provisioning and one-time token display
- 30-second automatic refresh

The page uses same-origin session cookies. Do not reintroduce admin tokens into
browser local storage unless the authentication model is deliberately changed.

## Development Checks

Run from the repository root:

```powershell
# Windows
gofmt -w main.go main_test.go web.go
go test ./...
go vet ./...
go build -trimpath -o microdevicestatus.exe .
.\scripts\build.ps1
```

```bash
# Linux / macOS
gofmt -w main.go main_test.go web.go
go test ./...
go vet ./...
go build -trimpath -o microdevicestatus .
./scripts/build.sh
```

Cross-builds must keep `CGO_ENABLED=0`. The service uses pure-Go SQLite so
Windows and Linux binaries can be produced from either host OS.

The tests cover database initialization, device provisioning, heartbeat
persistence, bearer token separation, successful login, invalid password
rejection, session access to management APIs, and logout invalidation.

When changing the API or authentication, update `main_test.go` and perform one
real HTTP check against `/healthz`, login, a management endpoint, and logout.

## Constraints and Next Steps

Known constraints:

- Sessions are in memory and single-process only.
- Login credentials come from environment variables; there is no user table or
  password reset flow.
- There is no HTTPS termination in the Go process. Use a reverse proxy or a
  platform TLS endpoint before exposing the service.
- There is no rate limiter or account lockout yet.
- Desktop and Android clients keep a bounded local retry queue for outages.

Recommended next implementation order:

1. Add device disable/revoke management.
2. Add automated database backups before long-running deployment; report
   retention cleanup is now built in and controlled by `MDS_REPORT_RETENTION_DAYS`.
3. Improve foreground-window collection where each desktop environment allows it.
4. Add iOS only with platform-specific collection limits.

Keep the server API HTTP/JSON and outbound-client initiated. Do not add a
broker or WebSocket layer unless a real real-time control requirement appears.
