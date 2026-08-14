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

New-Service -Name "DevPlatformIISHelper" `
    -BinaryPathName $ExePath `
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
Write-Host "  DEVPLATFORM_IISHELPER_SDDL = D:P(A;;GA;;;$sid)"
Write-Host ""
Write-Host "Then: Start-Service DevPlatformIISHelper"
