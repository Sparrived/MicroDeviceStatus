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
heartbeats. It contains latitude, longitude, accuracy, provider, capture time,
and a best-effort district name. The MDS public snapshot never exposes the raw
coordinates.

Enable `开机自动恢复上报` to let the `BOOT_COMPLETED` receiver restart the
foreground service after a reboot. Android still requires the user to allow
notifications, background operation, and battery use without restriction; some
manufacturers also require enabling the app's auto-start setting manually.

The app can optionally request Usage Access in system settings. With that
permission it reports the most recent non-MDS foreground application as
`name`, `package_name`, and `captured_at`. Without it, every heartbeat sends
`foreground_app: null` and does not misidentify MDS itself as the active app.
