[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string]$StudioPath,
    [string]$Destination = (Join-Path $env:LOCALAPPDATA 'KinSealStudio5.1-WinUSB')
)
$ErrorActionPreference = 'Stop'
$manifest = Get-Content -LiteralPath (Join-Path $PSScriptRoot 'BUILD-INFO.json') -Raw | ConvertFrom-Json
function Assert-Hash([string]$Path, [string]$Expected) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Datei fehlt: $Path" }
    if ((Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash -ne $Expected) {
        throw "Unbekannter oder geaenderter Dateistand: $Path. Keine DLL wurde ausgetauscht."
    }
}
$source = (Resolve-Path -LiteralPath $StudioPath).Path
Assert-Hash (Join-Path $source 'KinSealStudio5.1.exe') $manifest.vendor_studio_sha256
Assert-Hash (Join-Path $source 'AMZUsb.dll') $manifest.vendor_dll_sha256
Assert-Hash (Join-Path $PSScriptRoot 'AMZUsb.dll') $manifest.bridge_dll_sha256
if (Test-Path -LiteralPath $Destination) { throw "Ziel existiert bereits: $Destination. Bitte ein neues Ziel angeben." }
$destinationFull = [IO.Path]::GetFullPath($Destination)
if ($destinationFull.StartsWith($source.TrimEnd('\') + '\', [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Das Ziel darf nicht innerhalb des Studio-Quellordners liegen.'
}
New-Item -ItemType Directory -Path $destinationFull | Out-Null
Get-ChildItem -LiteralPath $source -Force | Copy-Item -Destination $destinationFull -Recurse -Force
Assert-Hash (Join-Path $destinationFull 'AMZUsb.dll') $manifest.vendor_dll_sha256
Assert-Hash (Join-Path $destinationFull 'KinSealStudio5.1.exe') $manifest.vendor_studio_sha256
Move-Item -LiteralPath (Join-Path $destinationFull 'AMZUsb.dll') -Destination (Join-Path $destinationFull 'AMZUsb.vendor.dll')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'AMZUsb.dll') -Destination (Join-Path $destinationFull 'AMZUsb.dll')
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Restore-StudioBridge.ps1') -Destination $destinationFull
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'BUILD-INFO.json') -Destination (Join-Path $destinationFull 'StudioBridge-BUILD-INFO.json')
Write-Host "Studio-Kopie vorbereitet: $destinationFull"
Write-Host 'WinUSB am Display beibehalten. In dieser Kopie KinSealStudio5.1.exe starten und den normalen USB-Download verwenden.'
Write-Host 'Hardwaretest der Studio-Integration steht noch aus. Log: %TEMP%\kinseal-studio-winusb.log'
