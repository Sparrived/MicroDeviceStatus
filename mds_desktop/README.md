# mds_desktop

Cross-platform device agent for Windows, Linux, and macOS. It collects a
small system snapshot and sends `POST /api/v1/heartbeats` with the provisioned
device token.

## Configure

On Windows release packages, double-click `install-mds-desktop-windows.cmd`
to open the interactive setup wizard. It creates the config file, registers
current-user login startup, and can test/start the agent immediately.

Copy `config.example.json` to `mds-desktop.json`, replace the endpoint and
token, then run:

```powershell
go run .
```

The same executable also accepts `-endpoint`, `-token`, `-interval`, and
`-once`. `MDS_ENDPOINT` and `MDS_DEVICE_TOKEN` can be used instead of storing
the token in a file. Failed reports are kept in a private JSONL queue beside
the config file and retried in order. Use an HTTPS endpoint and keep the config
file readable only by the Windows user running the agent.

The running agent checks the config file about once per second and reloads valid
changes without restarting. If a file is temporarily invalid while editing, it
keeps the last valid configuration and retries after the next file change.

On Windows, register the agent in the interactive user session so foreground
window collection works:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\install-mds-desktop-windows.ps1 `
  -BinaryPath "C:\mds\mds-desktop-windows-amd64.exe" `
  -ConfigPath "C:\mds\mds-desktop.json"
```

On Linux, install the user service after copying the binary and config to
their permanent paths:

```bash
install -Dm755 ./mds-desktop-linux-amd64 ~/.local/bin/mds-desktop
install -Dm600 ./mds-desktop.json ~/.config/mds-desktop/mds-desktop.json
install -Dm644 ./mds-desktop.service ~/.config/systemd/user/mds-desktop.service
systemctl --user daemon-reload
systemctl --user enable --now mds-desktop.service
```

Both startup configurations run after the user logs in. Keep the agent in the
interactive user session instead of running it as SYSTEM/root so foreground
application collection remains available.

Windows heartbeats include the foreground display name, executable process
name, and capture time. They also include `metrics.activity_state` as `busy` or
`idle`; five minutes without keyboard or mouse input is considered idle, and
`metrics.idle_seconds` reports the measured idle duration. The raw window title
remains private to the management API and is excluded from the public blog
snapshot.

## Build

```powershell
go build -trimpath -ldflags="-H=windowsgui" -o mds_desktop.exe .
```

The `windowsgui` linker mode keeps the background agent from opening a console
window when it is launched by Task Scheduler.

Build from Linux or macOS with the corresponding `GOOS` and `GOARCH`, for
example `GOOS=windows GOARCH=amd64 go build -trimpath -o mds_desktop.exe .`.
