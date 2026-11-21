[CmdletBinding()]
param([string]$StudioPath = $PSScriptRoot)
$ErrorActionPreference = 'Stop'
$manifest = Get-Content -LiteralPath (Join-Path $StudioPath 'StudioBridge-BUILD-INFO.json') -Raw | ConvertFrom-Json
$dll = Join-Path $StudioPath 'AMZUsb.dll'
$backup = Join-Path $StudioPath 'AMZUsb.vendor.dll'
if ((Get-FileHash -LiteralPath $dll -Algorithm SHA256).Hash -ne $manifest.bridge_dll_sha256) {
    throw 'Die aktive DLL wurde inzwischen geaendert. Wiederherstellung abgebrochen.'
}
if ((Get-FileHash -LiteralPath $backup -Algorithm SHA256).Hash -ne $manifest.vendor_dll_sha256) {
    throw 'Die Original-DLL fehlt oder wurde veraendert. Wiederherstellung abgebrochen.'
}
Copy-Item -LiteralPath $backup -Destination $dll -Force
Write-Host 'Original-AMZUsb.dll in der Studio-Kopie wiederhergestellt. Diese benoetigt wieder den libusb0-Treiber.'
