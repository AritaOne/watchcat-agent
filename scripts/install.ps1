<#
.SYNOPSIS
  Install watchcat-agent as a Windows service.

.DESCRIPTION
  Downloads the release binary (or uses a local one), writes a configuration
  file under ProgramData, registers the Windows service and starts it.
  Run from an elevated PowerShell prompt.

.EXAMPLE
  .\install.ps1 -ListenAddress "0.0.0.0:9800" -BearerToken "s3cret"

.EXAMPLE
  .\install.ps1 -Binary .\dist\watchcat-agent_v0.1.0_windows_amd64.exe
#>
[CmdletBinding()]
param(
    [string]$Version        = "latest",
    [string]$ListenAddress  = "0.0.0.0:9800",
    [string]$BearerToken    = "",
    [string]$Binary         = "",
    [string]$Repo           = "aritaone/watchcat-agent"
)

$ErrorActionPreference = "Stop"

$isAdmin = ([Security.Principal.WindowsPrincipal] `
    [Security.Principal.WindowsIdentity]::GetCurrent()
).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $isAdmin) {
    throw "Run this script from an elevated PowerShell prompt: registering a service requires Administrator."
}

$installDir = Join-Path $env:ProgramFiles "watchcat-agent"
$configDir  = Join-Path $env:ProgramData "watchcat-agent"
$exePath    = Join-Path $installDir "watchcat-agent.exe"
$configPath = Join-Path $configDir  "watchcat-agent.yaml"

New-Item -ItemType Directory -Force -Path $installDir, $configDir | Out-Null

# Match the release asset to this machine's architecture.
$arch = switch ($env:PROCESSOR_ARCHITECTURE) {
    "AMD64" { "amd64" }
    "ARM64" { "arm64" }
    default { throw "Unsupported architecture: $env:PROCESSOR_ARCHITECTURE" }
}

if ($Binary) {
    Write-Host "==> Using local binary $Binary"
    Copy-Item -Force $Binary $exePath
} else {
    $url = if ($Version -eq "latest") {
        "https://github.com/$Repo/releases/latest/download/watchcat-agent_windows_$arch.exe"
    } else {
        "https://github.com/$Repo/releases/download/$Version/watchcat-agent_${Version}_windows_$arch.exe"
    }
    Write-Host "==> Downloading $url"
    Invoke-WebRequest -Uri $url -OutFile $exePath -UseBasicParsing
}

if (Test-Path $configPath) {
    Write-Host "==> Keeping existing config $configPath"
} else {
    @"
server:
  listen_address: "$ListenAddress"
  metrics_path: "/metrics"
  bearer_token: "$BearerToken"
  scrape_timeout: 15s

collectors:
  cpu: true
  memory: true
  filesystem: true
  diskio: true
  network: true
  load: true
  host: true
  process: true
  go: true

labels: {}

log:
  level: info
  file: "$($configDir -replace '\\','\\')\\watchcat-agent.log"
"@ | Set-Content -Path $configPath -Encoding UTF8
    Write-Host "==> Wrote $configPath"
}

# Reinstall so a changed binary path or config path takes effect.
& $exePath uninstall 2>$null | Out-Null
& $exePath install --config $configPath
if ($LASTEXITCODE -ne 0) { throw "Service installation failed with exit code $LASTEXITCODE" }

# Prometheus scrapes over the network, so the port must be open inbound.
$port = $ListenAddress.Split(":")[-1]
$ruleName = "watchcat-agent ($port/tcp)"
if (-not (Get-NetFirewallRule -DisplayName $ruleName -ErrorAction SilentlyContinue)) {
    New-NetFirewallRule -DisplayName $ruleName -Direction Inbound -Protocol TCP `
        -LocalPort $port -Action Allow -Profile Domain,Private | Out-Null
    Write-Host "==> Opened inbound TCP $port for Domain and Private profiles"
}

Write-Host "==> Done. Verify with:"
Write-Host "    Invoke-WebRequest http://127.0.0.1:$port/metrics -UseBasicParsing | Select-Object -Expand Content"
