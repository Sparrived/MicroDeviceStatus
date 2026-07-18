#!/usr/bin/env bash
# Build MicroDeviceStatus for Windows and Linux (CGO-free pure Go).
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
export CGO_ENABLED=0
mkdir -p dist

build() {
  local goos="$1" goarch="$2" out="$3"
  echo "Building ${out} ..."
  GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w" -o "$out" .
}

build windows amd64 dist/microdevicestatus-windows-amd64.exe
build linux   amd64 dist/microdevicestatus-linux-amd64
build linux   arm64 dist/microdevicestatus-linux-arm64
echo "Done."
ls -la dist
