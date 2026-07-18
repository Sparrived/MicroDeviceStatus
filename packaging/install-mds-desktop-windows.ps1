[CmdletBinding()]
param(
    [string] $BinaryPath,
    [string] $ConfigPath,
    [string] $TaskName = "MicroDeviceStatus Desktop Agent"
)

$ErrorActionPreference = "Stop"
$interactive = [string]::IsNullOrWhiteSpace($BinaryPath) -or [string]::IsNullOrWhiteSpace($ConfigPath)
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path

function Find-FirstExistingPath {
    param([string[]] $Candidates)

    foreach ($candidate in $Candidates) {
        if (-not [string]::IsNullOrWhiteSpace($candidate) -and (Test-Path -LiteralPath $candidate -PathType Leaf)) {
            return (Resolve-Path -LiteralPath $candidate).Path
        }
    }
    return $null
}

function Read-ExistingPath {
    param(
        [string] $Label,
        [string] $DefaultPath
    )

    while ($true) {
        $suffix = if ($DefaultPath) { " [$DefaultPath]" } else { "" }
        $value = Read-Host "$Label$suffix"
        if ([string]::IsNullOrWhiteSpace($value)) {
            $value = $DefaultPath
        }
        if ([string]::IsNullOrWhiteSpace($value)) {
            Write-Warning "Enter a path."
            continue
        }
        $value = [Environment]::ExpandEnvironmentVariables($value.Trim().Trim('"'))
        if (-not (Test-Path -LiteralPath $value -PathType Leaf)) {
            Write-Warning "File not found: $value"
            continue
        }
        return (Resolve-Path -LiteralPath $value).Path
    }
}

function Read-Endpoint {
    param([string] $DefaultEndpoint)

    while ($true) {
        $suffix = if ($DefaultEndpoint) { " [$DefaultEndpoint]" } else { "" }
        $value = Read-Host "Server URL (for example http://127.0.0.1:8080)$suffix"
        if ([string]::IsNullOrWhiteSpace($value)) {
            $value = $DefaultEndpoint
        }
        try {
            $uri = [Uri] $value
            if ($uri.Scheme -notin @("http", "https") -or [string]::IsNullOrWhiteSpace($uri.Host)) {
                throw "The server URL must start with http:// or https://."
            }
            return $value.TrimEnd('/')
        } catch {
            Write-Warning $_.Exception.Message
        }
    }
}

function Read-Secret {
    param([bool] $KeepExisting)

    while ($true) {
        $secure = Read-Host "Device token (hidden input; leave blank to keep the existing token)" -AsSecureString
        $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
        try {
            $value = [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
        } finally {
            [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
        }
        if ([string]::IsNullOrWhiteSpace($value) -and $KeepExisting) {
            return $null
        }
        if (-not [string]::IsNullOrWhiteSpace($value)) {
            return $value
        }
        Write-Warning "The device token cannot be empty."
    }
}

function Read-Interval {
    param([int] $DefaultInterval)

    while ($true) {
        $value = Read-Host "Heartbeat interval in seconds (minimum 5) [$DefaultInterval]"
        if ([string]::IsNullOrWhiteSpace($value)) {
            return $DefaultInterval
        }
        $parsed = 0
        if ([int]::TryParse($value, [ref] $parsed) -and $parsed -ge 5 -and $parsed -le 86400) {
            return $parsed
        }
        Write-Warning "Enter an integer from 5 to 86400."
    }
}

function Pause-Interactive {
    if ($interactive) {
        Read-Host "Press Enter to close this window" | Out-Null
    }
}

try {
    if ($interactive) {
        Write-Host ""
        Write-Host "MicroDeviceStatus desktop agent setup" -ForegroundColor Cyan
        Write-Host "The agent will run in the background after the current user logs in."
        Write-Host ""

        $defaultBinary = Find-FirstExistingPath @(
            (Join-Path $scriptRoot "mds-desktop-windows-amd64.exe"),
            (Join-Path $scriptRoot "desktop.exe"),
            (Join-Path $scriptRoot "mds_desktop.exe"),
            (Join-Path $scriptRoot "..\mds_desktop\desktop.exe"),
            (Join-Path $scriptRoot "..\mds_desktop\mds-desktop-windows-amd64.exe")
        )
        $BinaryPath = Read-ExistingPath "Desktop agent exe path" $defaultBinary

        $defaultConfig = Join-Path (Split-Path -Parent $BinaryPath) "mds-desktop.json"
        if ([string]::IsNullOrWhiteSpace($ConfigPath)) {
            $ConfigPath = $defaultConfig
        } else {
            $ConfigPath = [Environment]::ExpandEnvironmentVariables($ConfigPath.Trim().Trim('"'))
            if (-not [IO.Path]::IsPathRooted($ConfigPath)) {
                $ConfigPath = Join-Path (Get-Location) $ConfigPath
            }
            $ConfigPath = [IO.Path]::GetFullPath($ConfigPath)
        }
    }

    if (-not (Test-Path -LiteralPath $BinaryPath -PathType Leaf)) {
        throw "Desktop agent exe not found: $BinaryPath"
    }
    $BinaryPath = (Resolve-Path -LiteralPath $BinaryPath).Path
    $workingDirectory = Split-Path -Parent $BinaryPath

    if ($interactive) {
        $templatePath = Find-FirstExistingPath @(
            (Join-Path $workingDirectory "config.example.json"),
            (Join-Path $scriptRoot "..\mds_desktop\config.example.json")
        )
        if (Test-Path -LiteralPath $ConfigPath -PathType Leaf) {
            $configObject = Get-Content -LiteralPath $ConfigPath -Raw | ConvertFrom-Json
        } elseif ($templatePath) {
            $configObject = Get-Content -LiteralPath $templatePath -Raw | ConvertFrom-Json
        } else {
            $configObject = [pscustomobject] @{
                endpoint = ""
                token = ""
                interval_seconds = 60
                client_version = "0.1.0"
            }
        }

        $defaultEndpoint = [string] $configObject.endpoint
        if ($defaultEndpoint -eq "https://mds.example.com") {
            $defaultEndpoint = "http://127.0.0.1:8080"
        }
        $endpoint = Read-Endpoint $defaultEndpoint
        $existingToken = [string] $configObject.token
        $hasExistingToken = -not [string]::IsNullOrWhiteSpace($existingToken) -and $existingToken -notlike "paste-*"
        $newToken = Read-Secret $hasExistingToken
        if ($null -ne $newToken) {
            $configObject.token = $newToken
        }
        if (-not $hasExistingToken -and [string]::IsNullOrWhiteSpace([string] $configObject.token)) {
            throw "The device token cannot be empty."
        }
        $configObject.endpoint = $endpoint
        $defaultInterval = [int] $configObject.interval_seconds
        if ($defaultInterval -lt 5) {
            $defaultInterval = 60
        }
        $configObject.interval_seconds = Read-Interval $defaultInterval

        $configDirectory = Split-Path -Parent $ConfigPath
        if ($configDirectory) {
            New-Item -ItemType Directory -Path $configDirectory -Force | Out-Null
        }
        $json = $configObject | ConvertTo-Json -Depth 4
        [IO.File]::WriteAllText($ConfigPath, $json, [Text.UTF8Encoding]::new($false))
        $ConfigPath = (Resolve-Path -LiteralPath $ConfigPath).Path

        $currentUser = [Security.Principal.WindowsIdentity]::GetCurrent().Name
        & icacls.exe $ConfigPath /inheritance:r /grant:r ("{0}:F" -f $currentUser) | Out-Null
        if ($LASTEXITCODE -ne 0) {
            Write-Warning "Could not restrict config permissions automatically: $ConfigPath"
        }
    } else {
        $ConfigPath = (Resolve-Path -LiteralPath $ConfigPath).Path
    }

    $action = New-ScheduledTaskAction `
        -Execute $BinaryPath `
        -Argument ('-config "{0}"' -f $ConfigPath) `
        -WorkingDirectory $workingDirectory
    $trigger = New-ScheduledTaskTrigger -AtLogOn -User $env:USERNAME
    $principal = New-ScheduledTaskPrincipal `
        -UserId $env:USERNAME `
        -LogonType Interactive `
        -RunLevel Limited

    Register-ScheduledTask `
        -TaskName $TaskName `
        -Action $action `
        -Trigger $trigger `
        -Principal $principal `
        -Force | Out-Null

    Write-Host "Registered login startup task: $TaskName" -ForegroundColor Green
    Write-Host "Config file: $ConfigPath"

    if ($interactive) {
        $testNow = Read-Host "Test one heartbeat now? [Y/n]"
        if ([string]::IsNullOrWhiteSpace($testNow) -or $testNow -match "^(y|yes)$") {
            Write-Host "Testing heartbeat..."
            & $BinaryPath -config $ConfigPath -once
            if ($LASTEXITCODE -ne 0) {
                Write-Warning "Heartbeat test failed; the offline queue will keep the report and the startup task is still registered."
            }
        }

        $startNow = Read-Host "Start the background agent now? [Y/n]"
        if ([string]::IsNullOrWhiteSpace($startNow) -or $startNow -match "^(y|yes)$") {
            Start-ScheduledTask -TaskName $TaskName
            Write-Host "Background agent started."
        }
    }
} catch {
    Write-Error $_.Exception.Message
    if ($interactive) {
        Pause-Interactive
    }
    exit 1
}

Pause-Interactive
