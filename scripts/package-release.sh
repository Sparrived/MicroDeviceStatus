#!/usr/bin/env bash
# Build release artifacts for MicroDeviceStatus (server + clients).
# Usage: scripts/package-release.sh [version]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-${VERSION:-}}"
if [[ -z "${VERSION}" ]]; then
  if git describe --tags --exact-match HEAD >/dev/null 2>&1; then
    VERSION="$(git describe --tags --exact-match HEAD)"
  else
    VERSION="$(git describe --tags --always --dirty 2>/dev/null || echo 0.0.0-dev)"
  fi
fi
VERSION="${VERSION#v}"
VERSION_TAG="v${VERSION}"

DIST="${ROOT}/dist/release"
STAGE="${DIST}/stage"
rm -rf "${DIST}"
mkdir -p "${STAGE}"

export CGO_ENABLED=0
LDFLAGS_SERVER="-s -w -X main.version=${VERSION}"
LDFLAGS_DESKTOP="-s -w -X main.defaultClientVersion=${VERSION}"

echo "==> Version ${VERSION_TAG}"
echo "==> Running server tests"
go test ./...

build_server() {
  local goos="$1" goarch="$2" outname="$3"
  echo "  server ${goos}/${goarch} -> ${outname}"
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="${LDFLAGS_SERVER}" -o "${STAGE}/${outname}" .
}

build_desktop() {
  local goos="$1" goarch="$2" outname="$3"
  local ldflags="${LDFLAGS_DESKTOP}"
  if [[ "${goos}" == "windows" ]]; then
    ldflags+=" -H=windowsgui"
  fi
  echo "  desktop ${goos}/${goarch} -> ${outname}"
  (
    cd mds_desktop
    GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="${ldflags}" -o "${STAGE}/${outname}" .
  )
}

archive_dir() {
  local name="$1"
  local use_zip="$2"
  if [[ "${use_zip}" == "zip" ]]; then
    if command -v zip >/dev/null 2>&1; then
      (cd "${STAGE}" && zip -qr "${DIST}/${name}.zip" "${name}")
    else
      python3 - "${STAGE}" "${name}" "${DIST}/${name}.zip" <<'PY'
import sys, zipfile
from pathlib import Path
stage, name, out = Path(sys.argv[1]), sys.argv[2], Path(sys.argv[3])
root = stage / name
with zipfile.ZipFile(out, "w", zipfile.ZIP_DEFLATED) as zf:
    for path in root.rglob("*"):
        if path.is_file():
            zf.write(path, path.relative_to(stage).as_posix())
print(out)
PY
    fi
  else
    tar -C "${STAGE}" -czf "${DIST}/${name}.tar.gz" "${name}"
  fi
  rm -rf "${STAGE}/${name}"
}

echo "==> Building server binaries"
build_server windows amd64 "microdevicestatus-windows-amd64.exe"
build_server linux   amd64 "microdevicestatus-linux-amd64"
build_server linux   arm64 "microdevicestatus-linux-arm64"
build_server darwin  amd64 "microdevicestatus-darwin-amd64"
build_server darwin  arm64 "microdevicestatus-darwin-arm64"

echo "==> Building desktop client binaries"
build_desktop windows amd64 "mds-desktop-windows-amd64.exe"
build_desktop linux   amd64 "mds-desktop-linux-amd64"
build_desktop linux   arm64 "mds-desktop-linux-arm64"
build_desktop darwin  amd64 "mds-desktop-darwin-amd64"
build_desktop darwin  arm64 "mds-desktop-darwin-arm64"

package_server() {
  local platform="$1" binary="$2" fmt="$3"
  local name="mds-server-${platform}-${VERSION}"
  local dir="${STAGE}/${name}"
  mkdir -p "${dir}"
  cp "${STAGE}/${binary}" "${dir}/${binary}"
  cp packaging/SERVER_INSTALL.txt "${dir}/INSTALL.txt"
  cp packaging/microdevicestatus.service "${dir}/"
  cp .env.example "${dir}/"
  archive_dir "${name}" "${fmt}"
}

package_desktop() {
  local platform="$1" binary="$2" fmt="$3"
  local name="mds-desktop-${platform}-${VERSION}"
  local dir="${STAGE}/${name}"
  mkdir -p "${dir}"
  cp "${STAGE}/${binary}" "${dir}/${binary}"
  cp mds_desktop/config.example.json "${dir}/config.example.json"
  cp packaging/mds-desktop.service packaging/install-mds-desktop-windows.ps1 packaging/install-mds-desktop-windows.cmd "${dir}/"
  cp packaging/DESKTOP_INSTALL.txt "${dir}/INSTALL.txt"
  archive_dir "${name}" "${fmt}"
}

echo "==> Packaging server install archives"
package_server "windows-amd64" "microdevicestatus-windows-amd64.exe" zip
package_server "linux-amd64"   "microdevicestatus-linux-amd64" tar
package_server "linux-arm64"   "microdevicestatus-linux-arm64" tar
package_server "darwin-amd64"  "microdevicestatus-darwin-amd64" tar
package_server "darwin-arm64"  "microdevicestatus-darwin-arm64" tar

echo "==> Packaging desktop client archives"
package_desktop "windows-amd64" "mds-desktop-windows-amd64.exe" zip
package_desktop "linux-amd64"   "mds-desktop-linux-amd64" tar
package_desktop "linux-arm64"   "mds-desktop-linux-arm64" tar
package_desktop "darwin-amd64"  "mds-desktop-darwin-amd64" tar
package_desktop "darwin-arm64"  "mds-desktop-darwin-arm64" tar

echo "==> Packaging Docker install bundle"
DOCKER_NAME="mds-server-docker-${VERSION}"
DOCKER_DIR="${STAGE}/${DOCKER_NAME}"
mkdir -p "${DOCKER_DIR}"
cp docker-compose.yml Dockerfile .env.example scripts/docker-entrypoint.sh packaging/DOCKER_INSTALL.txt "${DOCKER_DIR}/"
cp packaging/DOCKER_INSTALL.txt "${DOCKER_DIR}/INSTALL.txt"
cat > "${DOCKER_DIR}/docker-compose.release.yml" <<EOF
# Pull a published image (preferred for production installs).
# Usage: docker compose -f docker-compose.release.yml --env-file .env up -d
services:
  mds:
    image: \${MDS_IMAGE:-ghcr.io/sparrived/microdevicestatus:${VERSION_TAG}}
    container_name: microdevicestatus
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    environment:
      MDS_ADMIN_TOKEN: \${MDS_ADMIN_TOKEN:?set MDS_ADMIN_TOKEN in .env}
      MDS_ADMIN_USERNAME: \${MDS_ADMIN_USERNAME:-admin}
      MDS_ADMIN_PASSWORD: \${MDS_ADMIN_PASSWORD:?set MDS_ADMIN_PASSWORD in .env}
      MDS_ADDR: ":8080"
      MDS_DB_PATH: /data/micro-device-status.db
      MDS_COOKIE_SECURE: \${MDS_COOKIE_SECURE:-0}
    volumes:
      - mds-data:/data
    healthcheck:
      test: ["CMD", "wget", "-q", "-O", "-", "http://127.0.0.1:8080/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
volumes:
  mds-data:
EOF
tar -C "${STAGE}" -czf "${DIST}/${DOCKER_NAME}.tar.gz" "${DOCKER_NAME}"
rm -rf "${DOCKER_DIR}"

if [[ -n "${ANDROID_APK:-}" && -f "${ANDROID_APK}" ]]; then
  echo "==> Packaging Android APK"
  cp "${ANDROID_APK}" "${DIST}/mds-mobile-android-${VERSION}.apk"
fi

echo "==> Copying loose binaries"
mkdir -p "${DIST}/bin"
cp "${STAGE}/microdevicestatus-"* "${DIST}/bin/" 2>/dev/null || true
cp "${STAGE}/mds-desktop-"* "${DIST}/bin/" 2>/dev/null || true

echo "==> Writing checksums"
(
  cd "${DIST}"
  : > SHA256SUMS.txt
  shopt -s nullglob
  for f in *.zip *.tar.gz *.apk; do
    if command -v sha256sum >/dev/null 2>&1; then
      sha256sum "$f" >> SHA256SUMS.txt
    else
      shasum -a 256 "$f" >> SHA256SUMS.txt
    fi
  done
)

cat > "${DIST}/release-manifest.json" <<EOF
{
  "name": "MicroDeviceStatus",
  "version": "${VERSION}",
  "tag": "${VERSION_TAG}",
  "components": {
    "server": ["windows-amd64", "linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64", "docker"],
    "desktop": ["windows-amd64", "linux-amd64", "linux-arm64", "darwin-amd64", "darwin-arm64"],
    "mobile": ["android"]
  }
}
EOF

echo "==> Artifacts in ${DIST}"
find "${DIST}" -maxdepth 2 -type f | sort
