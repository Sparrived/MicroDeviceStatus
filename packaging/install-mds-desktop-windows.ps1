[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [string] $BinaryPath,

    [Parameter(Mandatory = $true)]
    [string] $ConfigPath,

    [string] $TaskName = "MicroDeviceStatus Desktop Agent"
)

$ErrorActionPreference = "Stop"

$binary = (Resolve-Path -LiteralPath $BinaryPath).Path
$config = (Resolve-Path -LiteralPath $ConfigPath).Path
$workingDirectory = Split-Path -Parent $binary
$action = New-ScheduledTaskAction `
    -Execute $binary `
    -Argument "-config `"$config`"" `
    -WorkingDirectory $workingDirectory
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
$principal = New-ScheduledTaskPrincipal `
    -UserId $env:USERNAME `
    -LogonType Interactive `
    -RunLevel LeastPrivilege

Register-ScheduledTask `
    -TaskName $TaskName `
    -Action $action `
    -Trigger $trigger `
    -Principal $principal `
    -Force | Out-Null

Write-Host "Registered '$TaskName' for $env:USERNAME."
