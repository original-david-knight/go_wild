# Builds screen-agent.exe and compiles the Windows installer.
# Usage: powershell -File installer\build.ps1 [-Version 0.1.0]
param([string]$Version = "0.1.0")
$ErrorActionPreference = "Stop"

$appDir = Split-Path -Parent $PSScriptRoot
Push-Location $appDir
try {
    go build -o (Join-Path $PSScriptRoot "screen-agent.exe") .
    if ($LASTEXITCODE -ne 0) { throw "go build failed" }
} finally {
    Pop-Location
}

$iscc = $null
$candidates = @(
    "$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe",
    "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe",
    "$env:ProgramFiles\Inno Setup 6\ISCC.exe"
)
foreach ($candidate in $candidates) {
    if (Test-Path $candidate) { $iscc = $candidate; break }
}
if ($null -eq $iscc) {
    $cmd = Get-Command "iscc.exe" -ErrorAction SilentlyContinue
    if ($null -ne $cmd) { $iscc = $cmd.Source }
}
if ($null -eq $iscc) {
    throw "Inno Setup 6 not found; install it with: winget install JRSoftware.InnoSetup"
}

& $iscc "/DMyAppVersion=$Version" (Join-Path $PSScriptRoot "screen-agent.iss")
if ($LASTEXITCODE -ne 0) { throw "ISCC failed" }
Write-Output "Installer written to $PSScriptRoot\output\screen-agent-setup-$Version.exe"
