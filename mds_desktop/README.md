# mds_desktop

Cross-platform device agent for Windows, Linux, and macOS. It collects a
small system snapshot and sends `POST /api/v1/heartbeats` with the provisioned
device token.

## Configure

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

On Windows, register the agent in the interactive user session so foreground
window collection works:

```powershell
$action = New-ScheduledTaskAction `
  -Execute "C:\mds\mds_desktop.exe" `
  -Argument "-config C:\mds\mds-desktop.json" `
  -WorkingDirectory "C:\mds"
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$principal = New-ScheduledTaskPrincipal `
  -UserId $env:USERNAME `
  -LogonType Interactive `
  -RunLevel LeastPrivilege
Register-ScheduledTask `
  -TaskName "MicroDeviceStatus Desktop Agent" `
  -Action $action `
  -Trigger $trigger `
  -Principal $principal
```

Windows heartbeats include the foreground display name, executable process
name, and capture time. The raw window title remains private to the management
API and is excluded from the public blog snapshot.

## Build

```powershell
go build -trimpath -o mds_desktop.exe .
```

Build from Linux or macOS with the corresponding `GOOS` and `GOARCH`, for
example `GOOS=windows GOARCH=amd64 go build -trimpath -o mds_desktop.exe .`.
