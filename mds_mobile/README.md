# mds_mobile

Native Android client for MicroDeviceStatus. It runs a foreground service,
collects memory, storage, battery, network, CPU, and app-process metrics, and
sends the same JSON heartbeat contract as the desktop client.

## Build

Open this directory in Android Studio or run with Gradle 8.14.1+:

```bash
gradle assembleDebug
```

The debug APK is written to `app/build/outputs/apk/debug/app-debug.apk`.
Install it on an emulator with `adb install -r` and use
`http://10.0.2.2:8080` for a server running on the host. A physical device
needs the server's LAN address. The UI accepts the one-time device token
returned by `POST /api/v1/devices`.

The app allows cleartext HTTP for local development. Use an HTTPS endpoint for
real devices and production deployments.

Location reporting is opt-in and disabled by default. Enable `上报当前位置`
in the app and grant Android location permission to add a `location` object to
heartbeats. It contains latitude, longitude, accuracy, provider, and capture
time. Denying the permission leaves ordinary heartbeat reporting enabled.
