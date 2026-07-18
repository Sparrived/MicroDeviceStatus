# MicroDeviceStatus Deployment Guide

This guide covers production deployment of the **server only**. The service is a
single static Go binary plus one SQLite database file. It runs the same way on
Windows and Linux, and can also be packaged with Docker (see section 3).

For architecture, API contract, and authentication details, read
[PROJECT_CONTEXT.md](PROJECT_CONTEXT.md). For local development, see
[README.md](README.md).

## 1. Deployment Model

Recommended shape:

```text
Internet / LAN clients
        |
        v
  Reverse proxy (HTTPS)
  nginx / Caddy / IIS / cloud LB
        |
        v
  microdevicestatus (HTTP on loopback or private port)
        |
        v
  SQLite file on local disk
```

Rules for the MVP:

- Run **one** server process. Sessions are in-memory; multi-instance is not
  supported.
- Put **HTTPS** in front of the Go process. The binary does not terminate TLS.
- Keep the SQLite file on **local disk**, not a network share.
- Treat `MDS_ADMIN_TOKEN`, `MDS_ADMIN_USERNAME`, and `MDS_ADMIN_PASSWORD` as
  secrets. Do not commit them.

## 2. Build Artifacts

### From source (any OS with Go 1.25+)

```powershell
# Windows host
.\scripts\build.ps1
```

```bash
# Linux / macOS host
chmod +x scripts/build.sh
./scripts/build.sh
```

Outputs in `dist/`:

| File | Target |
|------|--------|
| `microdevicestatus-windows-amd64.exe` | Windows x86_64 |
| `microdevicestatus-linux-amd64` | Linux x86_64 |
| `microdevicestatus-linux-arm64` | Linux aarch64 |

No CGO and no shared libraries are required. Copy **only the binary** to the
server. The dashboard HTML is embedded.

### Single-target build

```bash
# Linux host or CI, for Linux amd64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o microdevicestatus .
```

```powershell
# Windows host, for Windows amd64
$env:CGO_ENABLED = "0"
go build -trimpath -ldflags="-s -w" -o microdevicestatus.exe .
```


## 3. Docker Deployment

Recommended when you want a single-host install without installing Go on the
server. The image is multi-stage: build with official Go, run on Alpine as user
`mds` (uid 1000) with SQLite on a volume.

**Rules (same as bare-metal):**

- Run **exactly one** container. Sessions are in-memory; do not scale replicas.
- Put **HTTPS** in front (Caddy / nginx / Traefik / cloud LB). The container only
  speaks plain HTTP.
- Keep the SQLite volume on **local disk**.
- Pass secrets via env / Compose `.env` — never bake them into the image.

### 3.1 Files in this repo

| File | Purpose |
|------|---------|
| `Dockerfile` | Multi-stage build + runtime image |
| `docker-compose.yml` | One-service Compose stack + named volume |
| `.env.example` | Template for required secrets |
| `.dockerignore` | Keeps clients, dist, and secrets out of the build context |
| `scripts/docker-entrypoint.sh` | Fixes volume ownership, then drops root |

### 3.2 Quick start (Docker Compose)

Requirements: Docker Engine + Compose v2 (or Docker Desktop).

```bash
# From the repository root
cp .env.example .env
# Edit .env — set strong MDS_ADMIN_TOKEN and MDS_ADMIN_PASSWORD

docker compose up -d --build
curl -sS http://127.0.0.1:8080/healthz
# expect: {"status":"ok"}
```

Dashboard: `http://127.0.0.1:8080/` with the credentials from `.env`.

Compose binds the published port to **host loopback** (`127.0.0.1:8080:8080`)
so the app is not open to the LAN by default. Change the left side of the port
mapping only if you intentionally expose it (still prefer a reverse proxy).

Useful commands:

```bash
docker compose logs -f mds
docker compose ps
docker compose restart mds
docker compose down          # keeps the named volume
docker compose down -v       # deletes the SQLite volume — data loss
```

### 3.3 Build and run without Compose

```bash
docker build -t microdevicestatus:local .

docker run -d --name microdevicestatus --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e MDS_ADMIN_TOKEN="$(openssl rand -hex 32)" \
  -e MDS_ADMIN_USERNAME=admin \
  -e MDS_ADMIN_PASSWORD="$(openssl rand -base64 24)" \
  -e MDS_ADDR=:8080 \
  -e MDS_DB_PATH=/data/micro-device-status.db \
  -e MDS_COOKIE_SECURE=0 \
  -v mds-data:/data \
  microdevicestatus:local
```

Windows PowerShell equivalent (generate secrets yourself and paste):

```powershell
docker build -t microdevicestatus:local .

docker run -d --name microdevicestatus --restart unless-stopped `
  -p 127.0.0.1:8080:8080 `
  -e MDS_ADMIN_TOKEN="replace-with-a-long-random-token" `
  -e MDS_ADMIN_USERNAME=admin `
  -e MDS_ADMIN_PASSWORD="replace-with-a-long-password" `
  -e MDS_ADDR=:8080 `
  -e MDS_DB_PATH=/data/micro-device-status.db `
  -e MDS_COOKIE_SECURE=0 `
  -v mds-data:/data `
  microdevicestatus:local
```

### 3.4 Environment variables (container)

Same variables as bare-metal. Inside the image defaults are:

| Variable | Container default |
|----------|-------------------|
| `MDS_ADDR` | `:8080` |
| `MDS_DB_PATH` | `/data/micro-device-status.db` |
| `MDS_ADMIN_TOKEN` | **required** |
| `MDS_ADMIN_USERNAME` | **required** (Compose default `admin`) |
| `MDS_ADMIN_PASSWORD` | **required** |
| `MDS_COOKIE_SECURE` | set to `1` behind HTTPS |

### 3.5 Reverse proxy in front of Docker

Point the proxy at the published host port (or the container network alias if
the proxy is another Compose service).

Caddy on the host:

```caddy
status.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

When HTTPS is enabled, set in `.env` / run flags:

```bash
MDS_COOKIE_SECURE=1
```

Then recreate the container so it picks up the new value:

```bash
docker compose up -d
```

Optional: run Caddy (or Traefik) as a sibling Compose service on the same
Docker network and proxy to `http://mds:8080` instead of publishing `8080` on
the host. Keep a **single** `mds` replica.

### 3.6 Data, backup, and upgrade

SQLite path inside the container: `/data/micro-device-status.db` (volume
`mds-data` in Compose).

Backup while the container is stopped (simplest consistent copy):

```bash
docker compose stop mds
docker run --rm -v mds-data:/data -v "$(pwd)/backups:/backups" alpine \
  sh -c 'cp -a /data/. /backups/'
docker compose start mds
```

Or use `sqlite3 ... ".backup ..."` from a one-off container that mounts the
same volume if the CLI is available.

Upgrade:

```bash
git pull
docker compose up -d --build
curl -sS http://127.0.0.1:8080/healthz
```

Notes:

- Rebuild replaces the binary; the named volume keeps devices and reports.
- In-memory dashboard sessions are cleared on container recreate.
- Do not run `docker compose up --scale mds=N` with N > 1.


### 3.8 Published container image (GHCR)

Release tags publish a multi-arch image:

```text
ghcr.io/sparrived/microdevicestatus:vX.Y.Z
```

Pull and run without building from source:

```bash
docker pull ghcr.io/sparrived/microdevicestatus:vX.Y.Z
docker run -d --name microdevicestatus --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -e MDS_ADMIN_TOKEN=... \
  -e MDS_ADMIN_USERNAME=admin \
  -e MDS_ADMIN_PASSWORD=... \
  -e MDS_ADDR=:8080 \
  -e MDS_DB_PATH=/data/micro-device-status.db \
  -v mds-data:/data \
  ghcr.io/sparrived/microdevicestatus:vX.Y.Z
```

GitHub Release assets also include `mds-server-docker-*.tar.gz` (Compose pull
bundle) and platform install archives. See [RELEASE.md](RELEASE.md).

### 3.7 Healthcheck

Compose probes `GET http://127.0.0.1:8080/healthz` inside the container via
BusyBox `wget`. Load balancers should use the same path on the public URL.

## 4. Required Configuration

| Variable | Required | Description |
|----------|----------|-------------|
| `MDS_ADMIN_TOKEN` | Yes | Bearer token for provisioning devices and admin API access |
| `MDS_ADMIN_USERNAME` | Yes | Dashboard login username |
| `MDS_ADMIN_PASSWORD` | Yes | Dashboard login password |
| `MDS_ADDR` | No | Listen address, default `:8080` |
| `MDS_DB_PATH` | No | SQLite path, default `data/micro-device-status.db` |
| `MDS_COOKIE_SECURE` | No | Set to `1` when HTTPS terminates outside the process |

Generate strong values before production:

```bash
# Linux
openssl rand -hex 32   # good for MDS_ADMIN_TOKEN
openssl rand -base64 24
```

```powershell
# Windows
[Convert]::ToBase64String((1..32 | ForEach-Object { Get-Random -Maximum 256 }) -as [byte[]])
```

Never reuse local development credentials.

### Production defaults to set

```bash
export MDS_ADDR="127.0.0.1:8080"          # bind loopback only; proxy handles public traffic
export MDS_DB_PATH="/var/lib/mds/micro-device-status.db"
export MDS_COOKIE_SECURE="1"              # required behind HTTPS
export MDS_ADMIN_TOKEN="..."
export MDS_ADMIN_USERNAME="admin"
export MDS_ADMIN_PASSWORD="..."
```

Windows equivalent paths and env vars are listed in the Windows section below.

## 5. Linux Deployment

### 5.1 Install layout

```bash
sudo useradd --system --home /var/lib/mds --shell /usr/sbin/nologin mds || true
sudo mkdir -p /opt/mds /var/lib/mds /etc/mds
sudo cp dist/microdevicestatus-linux-amd64 /opt/mds/microdevicestatus
sudo chmod 755 /opt/mds/microdevicestatus
sudo chown -R mds:mds /var/lib/mds
```

Create `/etc/mds/mds.env` (mode `600`, owner `root:mds` or `mds:mds`):

```bash
MDS_ADMIN_TOKEN=replace-with-a-long-random-token
MDS_ADMIN_USERNAME=admin
MDS_ADMIN_PASSWORD=replace-with-a-long-password
MDS_ADDR=127.0.0.1:8080
MDS_DB_PATH=/var/lib/mds/micro-device-status.db
MDS_COOKIE_SECURE=1
```

```bash
sudo chmod 600 /etc/mds/mds.env
sudo chown root:mds /etc/mds/mds.env
# or: sudo chown mds:mds /etc/mds/mds.env
```

### 5.2 systemd unit

Create `/etc/systemd/system/microdevicestatus.service`:

```ini
[Unit]
Description=MicroDeviceStatus server
After=network.target
Wants=network-online.target

[Service]
Type=simple
User=mds
Group=mds
EnvironmentFile=/etc/mds/mds.env
WorkingDirectory=/var/lib/mds
ExecStart=/opt/mds/microdevicestatus
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/lib/mds
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
```

Enable and start:

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now microdevicestatus
sudo systemctl status microdevicestatus --no-pager
```

Logs:

```bash
journalctl -u microdevicestatus -f
```

### 5.3 Reverse proxy (Caddy example)

Caddy automatically obtains certificates when DNS points to the host:

```caddy
status.example.com {
    reverse_proxy 127.0.0.1:8080
}
```

### 5.4 Reverse proxy (nginx example)

```nginx
server {
    listen 443 ssl http2;
    server_name status.example.com;

    # ssl_certificate     /path/to/fullchain.pem;
    # ssl_certificate_key /path/to/privkey.pem;

    client_max_body_size 2m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Set `MDS_COOKIE_SECURE=1` when browsers reach the service over HTTPS.

### 5.5 Firewall

Allow only HTTPS (and SSH for admin). Do **not** expose port `8080` publicly if
the proxy is on the same host:

```bash
# example with ufw
sudo ufw allow OpenSSH
sudo ufw allow 443/tcp
sudo ufw enable
```

## 6. Windows Deployment

### 6.1 Install layout

```powershell
New-Item -ItemType Directory -Force -Path C:\mds, C:\mds\data, C:\mds\logs | Out-Null
Copy-Item .\dist\microdevicestatus-windows-amd64.exe C:\mds\microdevicestatus.exe
```

Create `C:\mds\mds.env.ps1` (restrict NTFS permissions to admins / service account):

```powershell
$env:MDS_ADMIN_TOKEN = "replace-with-a-long-random-token"
$env:MDS_ADMIN_USERNAME = "admin"
$env:MDS_ADMIN_PASSWORD = "replace-with-a-long-password"
$env:MDS_ADDR = "127.0.0.1:8080"
$env:MDS_DB_PATH = "C:\mds\data\micro-device-status.db"
$env:MDS_COOKIE_SECURE = "1"
```

Or set machine-level environment variables via System Properties / `setx` /
deployment tooling, then restart the service process so it inherits them.

### 6.2 Run as a Windows service (NSSM)

[NSSM](https://nssm.cc/) is a simple way to wrap the console binary:

```powershell
nssm install MicroDeviceStatus C:\mds\microdevicestatus.exe
nssm set MicroDeviceStatus AppDirectory C:\mds
nssm set MicroDeviceStatus AppEnvironmentExtra `
  MDS_ADMIN_TOKEN=replace-with-a-long-random-token `
  MDS_ADMIN_USERNAME=admin `
  MDS_ADMIN_PASSWORD=replace-with-a-long-password `
  MDS_ADDR=127.0.0.1:8080 `
  MDS_DB_PATH=C:\mds\data\micro-device-status.db `
  MDS_COOKIE_SECURE=1
nssm set MicroDeviceStatus AppStdout C:\mds\logs\stdout.log
nssm set MicroDeviceStatus AppStderr C:\mds\logs\stderr.log
nssm set MicroDeviceStatus AppRotateFiles 1
nssm start MicroDeviceStatus
```

Service control:

```powershell
nssm status MicroDeviceStatus
nssm restart MicroDeviceStatus
nssm stop MicroDeviceStatus
```

### 6.3 Alternative: Scheduled Task at startup

```powershell
$action = New-ScheduledTaskAction `
  -Execute "C:\mds\microdevicestatus.exe" `
  -WorkingDirectory "C:\mds"
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
Register-ScheduledTask -TaskName "MicroDeviceStatus" -Action $action -Trigger $trigger -Principal $principal
```

Environment variables for a scheduled task must be set at the machine level or
injected by a small launcher script that loads `mds.env.ps1` then starts the
binary.

### 6.4 Reverse proxy options on Windows

- **IIS** with Application Request Routing / URL Rewrite to `http://127.0.0.1:8080`
- **Caddy for Windows** with the same Caddyfile as Linux
- **nginx for Windows**, or a cloud load balancer terminating TLS

Always set `MDS_COOKIE_SECURE=1` when clients use HTTPS.

### 6.5 Firewall

```powershell
# Prefer not exposing 8080. If needed only for private LAN testing:
New-NetFirewallRule -DisplayName "MicroDeviceStatus HTTP" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow -Profile Private
```

For production, open only the reverse-proxy HTTPS port (443).

## 7. Verify a Deployment

### Health

```bash
curl -sS http://127.0.0.1:8080/healthz
# expect: {"status":"ok"}
```

Through the public hostname:

```bash
curl -sS https://status.example.com/healthz
```

### Dashboard login

1. Open `https://status.example.com/`
2. Sign in with `MDS_ADMIN_USERNAME` / `MDS_ADMIN_PASSWORD`
3. Confirm the device list loads

### Admin API with bearer token

```bash
curl -sS https://status.example.com/api/v1/devices \
  -H "Authorization: Bearer $MDS_ADMIN_TOKEN"
```

### Provision a device (save the token immediately)

```bash
curl -sS -X POST https://status.example.com/api/v1/devices \
  -H "Authorization: Bearer $MDS_ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"office-pc","platform":"windows"}'
```

The response includes a one-time plaintext `token`. Store it in the client
secret store. The server only keeps the SHA-256 hash.

### Sample heartbeat

```bash
curl -sS -X POST https://status.example.com/api/v1/heartbeats \
  -H "Authorization: Bearer $DEVICE_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"reported_at":"2026-07-18T00:00:00Z","client_version":"0.1.0","metrics":{"cpu_percent":12.5}}'
```

## 8. Database and Backup

- Default file: `data/micro-device-status.db` relative to the process working
  directory, or the absolute path in `MDS_DB_PATH`.
- SQLite runs in WAL mode. For a consistent offline backup while the service is
  running, prefer:

```bash
# Linux, if sqlite3 CLI is installed
sqlite3 /var/lib/mds/micro-device-status.db ".backup '/var/backups/mds/micro-device-status.db'"
```

Or stop the service, copy the database and `-wal` / `-shm` sidecars if present,
then start again:

```bash
sudo systemctl stop microdevicestatus
sudo cp -a /var/lib/mds/micro-device-status.db* /var/backups/mds/
sudo systemctl start microdevicestatus
```

Windows:

```powershell
Stop-Service MicroDeviceStatus   # or: nssm stop MicroDeviceStatus
Copy-Item C:\mds\data\micro-device-status.db* D:\backups\mds\ -Force
Start-Service MicroDeviceStatus
```

There is no built-in retention cleanup yet. Plan disk growth if many devices
send large process lists.

## 9. Upgrade

1. Build or download the new binary.
2. Back up the SQLite database.
3. Replace the binary:
   - Linux: `/opt/mds/microdevicestatus`
   - Windows: `C:\mds\microdevicestatus.exe`
4. Restart the service.
5. Hit `/healthz` and log into the dashboard.

Notes:

- In-memory sessions are cleared on restart; users must log in again.
- Device tokens and report history live in SQLite and survive upgrades when the
  same `MDS_DB_PATH` is kept.
- Keep `MDS_*` secrets unchanged unless intentionally rotating them.

### Rotate admin token or password

1. Update the environment / env file.
2. Restart the service.
3. Update any automation that uses `MDS_ADMIN_TOKEN`.
4. Existing device tokens are independent and stay valid.

## 10. Security Checklist

- [ ] Strong unique `MDS_ADMIN_TOKEN` and dashboard password
- [ ] HTTPS reverse proxy; `MDS_COOKIE_SECURE=1`
- [ ] Process listens on loopback or a private interface only
- [ ] Firewall does not expose the app port publicly
- [ ] Env file / service secrets restricted to the service account
- [ ] Database directory writable only by the service account
- [ ] Device tokens stored only on clients, never in the browser
- [ ] Single process only (no horizontal scale-out with sticky sessions hacks)
- [ ] If using Docker: one container/replica, secrets only via env, volume on local disk

Known MVP limits (do not ignore in production planning):

- No rate limiting or account lockout
- No built-in TLS
- Sessions lost on restart
- No multi-instance session sharing
- No automated retention purge

## 11. Operational Endpoints

| Endpoint | Auth | Purpose |
|----------|------|---------|
| `GET /healthz` | none | Liveness / DB ping for load balancers |
| `GET /` | none (login form) | Embedded dashboard |
| `POST /api/v1/auth/login` | username/password | Create session cookie |
| `GET /api/v1/devices` | session or admin token | List devices |
| `POST /api/v1/devices` | session or admin token | Provision device |
| `POST /api/v1/heartbeats` | device token | Ingest status report |

## 12. Quick Reference

```bash
# Linux start (foreground smoke test)
export MDS_ADMIN_TOKEN=...
export MDS_ADMIN_USERNAME=admin
export MDS_ADMIN_PASSWORD=...
export MDS_ADDR=127.0.0.1:8080
export MDS_DB_PATH=/var/lib/mds/micro-device-status.db
export MDS_COOKIE_SECURE=1
/opt/mds/microdevicestatus
```

```powershell
# Windows start (foreground smoke test)
$env:MDS_ADMIN_TOKEN = "..."
$env:MDS_ADMIN_USERNAME = "admin"
$env:MDS_ADMIN_PASSWORD = "..."
$env:MDS_ADDR = "127.0.0.1:8080"
$env:MDS_DB_PATH = "C:\mds\data\micro-device-status.db"
$env:MDS_COOKIE_SECURE = "1"
C:\mds\microdevicestatus.exe
```

Docker Compose smoke test:

```bash
cp .env.example .env   # set secrets
docker compose up -d --build
curl -sS http://127.0.0.1:8080/healthz
```

Health check:

```text
GET /healthz  ->  {"status":"ok"}
```

If `/healthz` fails after deploy, check:

1. Process is running and bound to the expected `MDS_ADDR`
2. `MDS_DB_PATH` directory is writable
3. Reverse proxy targets the correct upstream
4. Service logs for fatal missing-env startup errors
