# MicroDeviceStatus

Small HTTP service for receiving periodic device status reports and storing
them in SQLite.

For the full architecture, API contract, authentication model, and future
development notes, read [PROJECT_CONTEXT.md](PROJECT_CONTEXT.md) first.

For production install, reverse proxy, systemd/Windows service, backup, and
upgrade steps, read [DEPLOY.md](DEPLOY.md).

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
go run .
```

### Linux / macOS (bash)

```bash
export MDS_ADMIN_TOKEN="replace-with-a-long-random-token"
export MDS_ADMIN_USERNAME="admin"
export MDS_ADMIN_PASSWORD="replace-with-a-long-password"
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
```

Dashboard management APIs accept either the admin bearer token or the
`HttpOnly` session cookie created by login. Heartbeats require the device
bearer token. Put the service behind HTTPS before connecting real devices.
Device tokens are stored as
SHA-256 hashes; the plaintext token is returned only during provisioning.
