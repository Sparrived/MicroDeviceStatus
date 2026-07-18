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
the config file and retried in order.

## Build

```powershell
go build -trimpath -o mds_desktop.exe .
```

Build from Linux or macOS with the corresponding `GOOS` and `GOARCH`, for
example `GOOS=windows GOARCH=amd64 go build -trimpath -o mds_desktop.exe .`.
