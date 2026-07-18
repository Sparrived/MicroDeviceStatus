# Build MicroDeviceStatus for Windows and Linux (CGO-free pure Go).
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $PSScriptRoot
Set-Location $Root

$env:CGO_ENABLED = "0"
$targets = @(
  @{ GOOS = "windows"; GOARCH = "amd64"; Out = "dist/microdevicestatus-windows-amd64.exe" },
  @{ GOOS = "linux";   GOARCH = "amd64"; Out = "dist/microdevicestatus-linux-amd64" },
  @{ GOOS = "linux";   GOARCH = "arm64"; Out = "dist/microdevicestatus-linux-arm64" }
)

New-Item -ItemType Directory -Force -Path "dist" | Out-Null

foreach ($t in $targets) {
  $env:GOOS = $t.GOOS
  $env:GOARCH = $t.GOARCH
  Write-Host "Building $($t.Out) ..."
  & go build -trimpath "-ldflags=-s -w" -o $t.Out .
  if ($LASTEXITCODE -ne 0) {
    throw "go build failed for $($t.Out) with exit $LASTEXITCODE"
  }
}

Remove-Item Env:GOOS, Env:GOARCH -ErrorAction SilentlyContinue
Write-Host "Done."
Get-ChildItem dist | Format-Table Name, Length -AutoSize
