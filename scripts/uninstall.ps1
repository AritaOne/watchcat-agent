<#
.SYNOPSIS
  Stop and remove the watchcat-agent Windows service. Run elevated.
#>
[CmdletBinding()]
param([switch]$RemoveConfig)

$ErrorActionPreference = "Stop"

$exePath   = Join-Path $env:ProgramFiles "watchcat-agent\watchcat-agent.exe"
$configDir = Join-Path $env:ProgramData "watchcat-agent"

if (Test-Path $exePath) {
    & $exePath stop 2>$null | Out-Null
    & $exePath uninstall 2>$null | Out-Null
    Remove-Item -Force -Recurse (Split-Path $exePath) -ErrorAction SilentlyContinue
    Write-Host "==> Removed $exePath"
}

Get-NetFirewallRule -DisplayName "watchcat-agent (*" -ErrorAction SilentlyContinue |
    Remove-NetFirewallRule -ErrorAction SilentlyContinue

if ($RemoveConfig -and (Test-Path $configDir)) {
    Remove-Item -Force -Recurse $configDir
    Write-Host "==> Removed $configDir"
} else {
    Write-Host "==> Config kept at $configDir (pass -RemoveConfig to delete)"
}
