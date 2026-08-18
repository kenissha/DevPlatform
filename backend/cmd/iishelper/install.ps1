#Requires -RunAsAdministrator
<#
.SYNOPSIS
  One-time setup for the DevPlatform IIS helper service.

.DESCRIPTION
  Registers iishelper.exe as a Windows Service running as LocalSystem
  (the account Windows services run as by default, which already has the
  rights appcmd.exe needs), and prints the exact environment variable to
  set so the helper's named pipe is restricted to the specific account
  devplatform.exe runs as, instead of accepting connections from any
  local account.

  Run this from an elevated (Administrator) PowerShell prompt.

.PARAMETER ExePath
  Full path to the built iishelper.exe.

.PARAMETER DevPlatformAccount
  The Windows account devplatform.exe runs as, e.g. ".\devplatform-svc"
  or "DOMAIN\devplatform-svc". Used only to print the SDDL string below —
  it is not applied automatically, since DEVPLATFORM_IISHELPER_SDDL is an
  environment variable the operator sets on the service, the same way
  every other DEVPLATFORM_* secret/config value already works in this
  project.
#>
param(
    [Parameter(Mandatory = $true)][string]$ExePath,
    [Parameter(Mandatory = $true)][string]$DevPlatformAccount
)

$ErrorActionPreference = 'Stop'

New-Service -Name "DevPlatformIISHelper" `
    -BinaryPathName "$ExePath" `
    -DisplayName "DevPlatform IIS Helper" `
    -Description "Runs appcmd.exe on behalf of devplatform.exe. Do not run devplatform.exe itself elevated - only this service needs Administrator rights." `
    -StartupType Automatic

$sid = (New-Object System.Security.Principal.NTAccount($DevPlatformAccount)).Translate([System.Security.Principal.SecurityIdentifier]).Value

Write-Host ""
Write-Host "Service 'DevPlatformIISHelper' registered (LocalSystem, automatic start)."
Write-Host ""
Write-Host "Before starting it, restrict the named pipe to $DevPlatformAccount by setting this"
Write-Host "environment variable on the DevPlatformIISHelper service (System Properties >"
Write-Host "Environment Variables, or 'setx' for a machine-wide value, then restart the service):"
Write-Host ""
Write-Host "  DEVPLATFORM_IISHELPER_SDDL = D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;GA;;;$sid)"
Write-Host ""
Write-Host "(This grants full access to LocalSystem (SY) and Builtin Administrators (BA) in"
Write-Host "addition to $DevPlatformAccount. LocalSystem must be included: the iishelper"
Write-Host "service itself runs as LocalSystem, and go-winio only applies this security"
Write-Host "descriptor to the pipe's first instance - every later instance, including the one"
Write-Host "created on the first incoming connection, is opened against the existing pipe"
Write-Host "object and access-checked against this same DACL. Without an ACE for SY, that"
Write-Host "check fails, the pipe Accept() errors out, and the service dies moments after"
Write-Host "starting.)"
Write-Host ""
Write-Host "The helper also needs DEVPLATFORM_ALLOWED_SITES_FILE set to a small JSON file"
Write-Host "listing the IIS site names it may ever touch, e.g. [`"Intranet Backend`", `"Intranet Frontend`"]."
Write-Host "This file is deliberately separate from devplatform.exe's deploy-targets store -"
Write-Host "it is edited by hand on this server only, never through the panel, so a"
Write-Host "devplatform.exe compromise can never expand what iishelper is allowed to touch."
Write-Host "If it is empty or unset, iishelper starts with zero allowed sites and rejects"
Write-Host "every deploy request with 'not a configured deploy target site'."
Write-Host ""
Write-Host "IMPORTANT: Windows Services do NOT inherit a logged-in user's environment. Setting"
Write-Host "either DEVPLATFORM_IISHELPER_SDDL or DEVPLATFORM_ALLOWED_SITES_FILE with a plain"
Write-Host "user-scoped 'setx' or in a PowerShell profile will NOT be visible to this service."
Write-Host "Both variables must be set as machine-scoped environment variables - System"
Write-Host "Properties > Environment Variables > System variables (not User variables), or:"
Write-Host ""
Write-Host "  [Environment]::SetEnvironmentVariable('DEVPLATFORM_IISHELPER_SDDL', '<value>', 'Machine')"
Write-Host "  [Environment]::SetEnvironmentVariable('DEVPLATFORM_ALLOWED_SITES_FILE', '<path>', 'Machine')"
Write-Host ""
Write-Host "Then restart the service so it picks up the new values: Start-Service DevPlatformIISHelper"
