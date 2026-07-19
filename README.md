# MicroDeviceStatus

Small HTTP service for receiving periodic device status reports and storing
them in SQLite.

For the full architecture, API contract, authentication model, and future
development notes, read [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) first.

For production install (including Docker), reverse proxy, systemd/Windows
service, backup, and upgrade steps, read [DEPLOY.md](DEPLOY.md).

For Next.js blog integration, read [BLOG_DEVICE_STATUS_GUIDE.md](BLOG_DEVICE_STATUS_GUIDE.md).

## Requirements

- Go 1.25 or newer
- No CGO required (`modernc.org/sqlite` is pure Go)

The same source builds and runs on Windows and Linux.

## Run

### Windows (PowerShell)

```powershell
$env:MDS_ADMIN_TOKEN = "replace-with-a-long-random-token"
$env:MDS_ADMIN_USERNAME = "admin"
$env:MDS_ADMIN_PASSWORD = "replace-with-a-long-password"
$env:MDS_PUBLIC_STATUS_TOKEN = "replace-with-a-separate-read-only-token"
$env:MDS_PUBLIC_DEVICE_IDS = "computer-device-id,phone-device-id"
go run .
```

### Linux / macOS (bash)

```bash
export MDS_ADMIN_TOKEN="replace-with-a-long-random-token"
export MDS_ADMIN_USERNAME="admin"
export MDS_ADMIN_PASSWORD="replace-with-a-long-password"
export MDS_PUBLIC_STATUS_TOKEN="replace-with-a-separate-read-only-token"
export MDS_PUBLIC_DEVICE_IDS="computer-device-id,phone-device-id"
go run .
```

The default listen address is `:8080` and the default database is
`data/micro-device-status.db`.

Open `http://127.0.0.1:8080/` for the built-in dashboard and log in with
`MDS_ADMIN_USERNAME` and `MDS_ADMIN_PASSWORD`.

Environment variables:

- `MDS_ADMIN_TOKEN`: token for device provisioning and query APIs.
- `MDS_ADMIN_USERNAME`: dashboard login username.
- `MDS_ADMIN_PASSWORD`: dashboard login password.
- `MDS_PUBLIC_STATUS_TOKEN`: independent read-only token for the public snapshot; server-side only.
- `MDS_PUBLIC_DEVICE_IDS`: comma-separated allowlist of device IDs for the public snapshot.
- `MDS_STATUS_ONLINE_SECONDS`: online threshold, default `300`.
- `MDS_STATUS_STALE_SECONDS`: stale threshold, default `1800`.
- `MDS_REPORT_RETENTION_DAYS`: report retention, default `30`; `0` disables cleanup.
- `MDS_COOKIE_SECURE`: set to `1` when HTTPS terminates outside the Go process.
- `MDS_ADDR`: listen address, default `:8080`.
- `MDS_DB_PATH`: SQLite path, default `data/micro-device-status.db`.

## Build

Local binary for the current OS:

```bash
# Linux / macOS
go build -trimpath -o microdevicestatus .

# Windows (PowerShell)
go build -trimpath -o microdevicestatus.exe .
```

Cross-compile Windows and Linux binaries (`CGO_ENABLED=0`):

```powershell
# Windows
.\scripts\build.ps1
```

```bash
# Linux / macOS
chmod +x scripts/build.sh
./scripts/build.sh
```

Outputs land in `dist/`:

- `microdevicestatus-windows-amd64.exe`
- `microdevicestatus-linux-amd64`
- `microdevicestatus-linux-arm64`

## Docker

Quick local/production-style run (requires Docker + Compose):

```bash
cp .env.example .env
# set MDS_ADMIN_TOKEN and MDS_ADMIN_PASSWORD in .env
docker compose up -d --build
curl -sS http://127.0.0.1:8080/healthz
```

Open `http://127.0.0.1:8080/`. Full image layout, reverse proxy, backup, and
upgrade notes are in [DEPLOY.md](DEPLOY.md#3-docker-deployment).

## API

Create a device token with the admin token:

### PowerShell

```powershell
$headers = @{ Authorization = "Bearer $env:MDS_ADMIN_TOKEN" }
$body = @{ name = "my-phone"; platform = "android" } | ConvertTo-Json
$device = Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/api/v1/devices -Headers $headers -Body $body -ContentType application/json
$device
```

### curl

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/devices \
  -H "Authorization: Bearer $MDS_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"my-phone","platform":"android"}'
```

The returned device token is shown only on creation. Store it in the client.

Send a heartbeat:

### PowerShell

```powershell
$headers = @{ Authorization = "Bearer $($device.token)" }
$body = @{
  reported_at = (Get-Date).ToUniversalTime().ToString("o")
  client_version = "0.1.0"
  metrics = @{ cpu_percent = 12.5; memory_percent = 43.2 }
  foreground_app = @{ name = "example" }
  processes = @()
} | ConvertTo-Json -Depth 6
Invoke-RestMethod -Method Post -Uri http://127.0.0.1:8080/api/v1/heartbeats -Headers $headers -Body $body -ContentType application/json
```

### curl

```bash
curl -sS -X POST http://127.0.0.1:8080/api/v1/heartbeats \
  -H "Authorization: Bearer $DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reported_at":"2026-07-18T00:00:00Z","client_version":"0.1.0","metrics":{"cpu_percent":12.5,"memory_percent":43.2},"foreground_app":{"name":"example"},"processes":[]}'
```

Query devices and reports with the admin token:

```text
GET /healthz
GET /api/v1/devices
GET /api/v1/devices/{id}
GET /api/v1/devices/{id}/latest
GET /api/v1/devices/{id}/reports?limit=50&from=2026-07-18T00:00:00Z&to=2026-07-19T00:00:00Z
POST /api/v1/auth/login
POST /api/v1/auth/logout
GET /api/v1/auth/me
GET /api/v1/public/snapshot
```

Dashboard management APIs accept either the admin bearer token or the
`HttpOnly` session cookie created by login. Heartbeats require the device
bearer token. Put the service behind HTTPS before connecting real devices.
Device tokens are stored as
SHA-256 hashes; the plaintext token is returned only during provisioning.

The public snapshot requires `Authorization: Bearer <MDS_PUBLIC_STATUS_TOKEN>`
and returns only allowlisted devices. It derives status from the server receive
time and excludes raw coordinates, process lists, full window titles, and
tokens. A Next.js blog should call it from a server-side route rather than from
the browser.

## Clients

`mds_desktop/` is a standalone Go agent for Windows, Linux, and macOS. It
collects host metrics, sends heartbeats on a configurable interval, and keeps
failed reports in a private JSONL retry queue. See
[mds_desktop/README.md](mds_desktop/README.md). Desktop release packages also
include Windows and Linux user-session startup configurations.

`mds_mobile/` is a native Android client. It uses a foreground service for
periodic reporting, stores failed heartbeats locally for retry, and supports
the same device token returned by `POST /api/v1/devices`. See
[mds_mobile/README.md](mds_mobile/README.md).

Mobile location reporting is opt-in. After enabling it and granting Android
location permission, heartbeats contain only best-effort country, province,
city, and district names. No coordinates are transmitted.

## Releases

GitHub Actions publishes the server, desktop, and Android client independently.
See [RELEASE.md](RELEASE.md) for the component tag formats and workflow names.

```bash
git tag -a server-v0.3.0 -m "MicroDeviceStatus Server v0.3.0"
git push origin server-v0.3.0
```

See [RELEASE.md](RELEASE.md) for asset names, GHCR image tags, and local packaging.
