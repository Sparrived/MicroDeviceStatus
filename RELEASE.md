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
| `mds-mobile-android-*.apk` | Installable release APK signed with the default debug key |
| `SHA256SUMS.txt` | Checksums for archives/APK |

Docker image (GHCR):

```text
ghcr.io/sparrived/microdevicestatus:vX.Y.Z
ghcr.io/sparrived/microdevicestatus:latest   # stable (no pre-release suffix) only
```

## How to cut a release

### Component tag push (recommended)

```bash
# from a clean main, choose one component
git checkout main
git pull
git tag -a server-v0.3.0 -m "MicroDeviceStatus Server v0.3.0"
git push origin server-v0.3.0

git tag -a desktop-v0.3.0 -m "MicroDeviceStatus Desktop v0.3.0"
git push origin desktop-v0.3.0

git tag -a mobile-v0.3.0 -m "MicroDeviceStatus Mobile v0.3.0"
git push origin mobile-v0.3.0
```

Each tag starts only its component workflow:

- `server-v*`: server archives, Docker image, and Docker install bundle.
- `desktop-v*`: Windows, Linux, and macOS desktop archives.
- `mobile-v*`: Android release APK.

Component versions are independent. They may use the same version number when
released together, or different version numbers when only one component changed.
The old combined `v*` release flow is retained only by historical releases such
as `v0.2.2`; do not use it for new releases.

### Workflow dispatch

Actions → choose **Release Server**, **Release Desktop**, or **Release Mobile**
→ **Run workflow**.

- Leave `version` empty for a draft timestamped build.
- Set `version` (e.g. `0.3.0`) and `publish=true` to create the component tag
  and GitHub Release.

## Local packaging smoke test

```bash
# requires bash, go, zip/tar; this builds the complete local bundle
./scripts/package-release.sh 0.0.0-local
ls dist/release
```

Android APK is only produced in CI (or locally with Android SDK):

```bash
cd mds_mobile
./gradlew assembleRelease --project-prop=mds.versionName=0.0.0-local --project-prop=mds.versionCode=1
```

The release APK is signed with the default debug key so it can be installed
directly for self-hosted use. Configure a stable production keystore before
publishing to an app store or distributing upgradeable production releases.

## First-time GHCR note

The package is published under the repository owner. If the package is private
by default, set visibility to Public under GitHub → Packages for anonymous
`docker pull`.

## Version injection

- Server: `-ldflags "-X main.version=..."` (logged at startup)
- Desktop: `-ldflags "-X main.defaultClientVersion=..."`
- Android: Gradle properties `mds.versionName` / `mds.versionCode`
- Docker: build-arg `VERSION`
