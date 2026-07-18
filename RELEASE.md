# Releasing MicroDeviceStatus

This repository publishes install packages for the **server**, **desktop
clients**, and **Android client**, plus a **Docker image** on GHCR.

## What a release contains

| Asset pattern | Contents |
|---------------|----------|
| `mds-server-windows-amd64-*.zip` | Server `.exe` + `INSTALL.txt` |
| `mds-server-linux-*-*.tar.gz` | Server binary + `INSTALL.txt` + systemd unit |
| `mds-server-darwin-*-*.tar.gz` | Server binary + `INSTALL.txt` |
| `mds-server-docker-*.tar.gz` | Compose pull-only bundle + `.env.example` + install notes |
| `mds-desktop-*-*.zip` / `.tar.gz` | Desktop agent + config example + install notes |
| `mds-mobile-android-*.apk` | Unsigned release APK |
| `SHA256SUMS.txt` | Checksums for archives/APK |

Docker image (GHCR):

```text
ghcr.io/sparrived/microdevicestatus:vX.Y.Z
ghcr.io/sparrived/microdevicestatus:latest   # stable (no pre-release suffix) only
```

## How to cut a release

### Option A — tag push (recommended)

```bash
# from a clean main
git checkout main
git pull
git tag -a v0.2.0 -m "MicroDeviceStatus v0.2.0"
git push origin v0.2.0
```

The [Release workflow](.github/workflows/release.yml) runs on `v*` tags and:

1. Builds/tests the server and packages multi-OS server + desktop archives
2. Builds the Android release APK
3. Builds multi-arch (`linux/amd64`, `linux/arm64`) Docker image and pushes to GHCR
4. Creates a GitHub Release with all install assets

### Option B — workflow dispatch

Actions → **Release** → **Run workflow**

- Leave `version` empty for a draft timestamped build
- Set `version` (e.g. `0.2.0`) and `publish=true` to create `v0.2.0`

## Local packaging smoke test

```bash
# requires bash, go, zip/tar
./scripts/package-release.sh 0.0.0-local
ls dist/release
```

Android APK is only produced in CI (or locally with Android SDK):

```bash
cd mds_mobile
./gradlew assembleRelease -Pmds.versionName=0.0.0-local -Pmds.versionCode=1
```

## First-time GHCR note

The package is published under the repository owner. If the package is private
by default, set visibility to Public under GitHub → Packages for anonymous
`docker pull`.

## Version injection

- Server: `-ldflags "-X main.version=..."` (logged at startup)
- Desktop: `-ldflags "-X main.defaultClientVersion=..."`
- Android: Gradle properties `mds.versionName` / `mds.versionCode`
- Docker: build-arg `VERSION`
