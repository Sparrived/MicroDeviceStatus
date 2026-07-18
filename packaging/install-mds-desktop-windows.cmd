@echo off
setlocal
powershell.exe -NoLogo -NoProfile -ExecutionPolicy Bypass -File "%~dp0install-mds-desktop-windows.ps1"
if errorlevel 1 pause
endlocal
