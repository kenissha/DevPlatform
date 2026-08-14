#Requires -RunAsAdministrator
<#
.SYNOPSIS
  One-time setup for the DevPlatform backend as a Windows Service.

.DESCRIPTION
  Registers devplatform.exe with the Service Control Manager and writes
  its configuration into the service's own environment, so the process
  stays up independently of IIS.

  Running as a service rather than under IIS's httpPlatformHandler is
  deliberate: this process does work that no incoming request drives —
  the nightly repository backup runs on a timer, and an approved deploy
  keeps running for minutes after its HTTP request was answered. A
  request-lifecycle host is free to idle out or recycle the process
  between requests, which silently skips backups and truncates deploys.

  IIS is still involved: the frontend site's web.config reverse-proxies
  /api, /healthz and /git to this service's port. That part does not
  change — only what starts and supervises the backend process does.

  Config goes into the service's own registry Environment value rather
  than machine-wide environment variables, so the JWT secret and git
  password are not readable from every process on the box, and so
  nothing else on this server can be affected by a name collision.

.PARAMETER ExePath
  Full path to devplatform.exe.

.PARAMETER DataDir
  Directory for all persisted state (git repos, tasks, users, audit log).
  Must not live under a user profile — see docs/DURUM.md's IIS lessons.

.PARAMETER FrontendDir
  Optional. Built frontend (frontend/dist) to serve from this process.
  Leave empty when IIS serves the frontend as its own site, which is the
  arrangement install.ps1's own reverse-proxy notes assume.

.PARAMETER ListenAddr
  Address to listen on, e.g. ":8081". The frontend's web.config rewrite
  rules must point at this same port.

.PARAMETER JwtSecret
  HMAC secret shared with whatever system issues login tokens.

.PARAMETER GitUsername
.PARAMETER GitPassword
  Credentials git clients present when pushing/pulling over HTTP.

.EXAMPLE
  .\install.ps1 -ExePath D:\inetpub\wwwroot\DevPlatform\Backend\devplatform.exe `
                -DataDir D:\inetpub\wwwroot\DevPlatform\data `
                -ListenAddr ":8081" `
                -JwtSecret "..." -GitUsername devplatform -GitPassword "..."
#>
param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [Parameter(Mandatory = $true)][string]$DataDir,
    [Parameter(Mandatory = $true)][string]$ListenAddr,
    [Parameter(Mandatory = $true)][string]$JwtSecret,
    [Parameter(Mandatory = $true)][string]$GitUsername,
    [Parameter(Mandatory = $true)][string]$GitPassword,
    [string]$FrontendDir = ""
)

$ErrorActionPreference = 'Stop'

$serviceName = "DevPlatform"

if (-not (Test-Path $ExePath)) {
    throw "devplatform.exe not found at: $ExePath"
}

if (Get-Service -Name $serviceName -ErrorAction SilentlyContinue) {
    Write-Host "Service '$serviceName' already exists - stopping and removing it first."
    Stop-Service -Name $serviceName -Force -ErrorAction SilentlyContinue
    # sc.exe rather than Remove-Service, which only exists on PowerShell 6+.
    & sc.exe delete $serviceName | Out-Null
    Start-Sleep -Seconds 2
}

New-Service -Name $serviceName `
    -BinaryPathName "$ExePath" `
    -DisplayName "DevPlatform" `
    -Description "DevPlatform backend: git hosting, task board, and deploy approvals. Stays running independently of IIS so nightly backups and in-progress deploys are not interrupted." `
    -StartupType Automatic | Out-Null

# The service reads its config from its own environment. REG_MULTI_SZ at
# HKLM\SYSTEM\CurrentControlSet\Services\<name>\Environment is the
# documented way to give one service its own variables without touching
# machine-wide state.
$env_lines = @(
    "DEVPLATFORM_DATA_DIR=$DataDir",
    "DEVPLATFORM_LISTEN_ADDR=$ListenAddr",
    "DEVPLATFORM_JWT_SECRET=$JwtSecret",
    "DEVPLATFORM_GIT_USERNAME=$GitUsername",
    "DEVPLATFORM_GIT_PASSWORD=$GitPassword"
)
if ($FrontendDir -ne "") {
    $env_lines += "DEVPLATFORM_FRONTEND_DIR=$FrontendDir"
}

$service_key = "HKLM:\SYSTEM\CurrentControlSet\Services\$serviceName"
New-ItemProperty -Path $service_key -Name "Environment" `
    -PropertyType MultiString -Value $env_lines -Force | Out-Null

# Restart on failure: first two failures retry after 5s, then every 60s.
# Without this, a crash leaves the platform down until someone notices.
& sc.exe failure $serviceName reset= 86400 actions= restart/5000/restart/5000/restart/60000 | Out-Null

Start-Service -Name $serviceName

Write-Host ""
Write-Host "Service '$serviceName' installed and started."
Write-Host ""
Write-Host "  Listening on : $ListenAddr"
Write-Host "  Data dir     : $DataDir"
Write-Host ""
Write-Host "Verify it is up:"
Write-Host "  Invoke-RestMethod http://127.0.0.1$ListenAddr/healthz"
Write-Host ""
Write-Host "The IIS backend site (the one using httpPlatformHandler to launch this exe)"
Write-Host "is now redundant and should be stopped/removed - two copies of this process"
Write-Host "would fight over the same port and the same data directory."
Write-Host ""
Write-Host "The IIS FRONTEND site stays as-is: its web.config reverse-proxies /api,"
Write-Host "/healthz and /git to $ListenAddr, which this service now serves."
