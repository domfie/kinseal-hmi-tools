# The domfie INF binds Microsoft's in-box WinUSB on ARM64.
# It is unsigned. This script does not change Windows signature/security settings.
[CmdletBinding()]
param([switch]$UpdatePackage)
$ErrorActionPreference = 'Stop'
try {
    $admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    if (-not $admin) { throw 'Bitte install-winusb.bat als Administrator ausfuehren.' }
    $devices = @(Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -like 'USB\VID_7365&PID_616C\*' })
    $pending = @($devices | Where-Object {
        (Get-PnpDeviceProperty -InstanceId $_.InstanceId -KeyName 'DEVPKEY_Device_Service' -ErrorAction SilentlyContinue).Data -ine 'WinUSB'
    })
    if ($devices.Count -gt 0 -and $pending.Count -eq 0 -and -not $UpdatePackage) {
        Write-Host 'Das angeschlossene Display verwendet bereits WinUSB. Keine Treiberaenderung erforderlich.'
        exit 0
    }
    $arch = $env:PROCESSOR_ARCHITEW6432
    if (-not $arch) { $arch = $env:PROCESSOR_ARCHITECTURE }
    if ($arch -ine 'ARM64') {
        throw 'Die beigefuegte INF ist nur fuer Windows ARM64. Auf x86/x64 bitte WinUSB mit Zadig zuordnen.'
    }
    $inf = Join-Path $PSScriptRoot 'domfie_HMI_WinUSB_ARM64.inf'
    $expected = 'c2748069f179460dc4b62a4221a9856a8277e81ed4b35a28ba8ac30b1580b218'
    if ((Get-FileHash -LiteralPath $inf -Algorithm SHA256).Hash -ne $expected) {
        throw 'Die beigefuegte INF wurde veraendert. Installation abgebrochen.'
    }
    Write-Host 'Installiere das domfie WinUSB-INF-Paket 1.0.2.0 vom 21.11.2026 (unsigniert).'
    $pnputil = Join-Path $env:WINDIR 'System32\pnputil.exe'
    $native = Join-Path $env:WINDIR 'Sysnative\pnputil.exe'
    if (Test-Path -LiteralPath $native) { $pnputil = $native }
    & $pnputil /add-driver $inf /install
    $result = $LASTEXITCODE
    if ($result -ne 0 -and $result -ne 3010) {
        throw "PnPUtil meldet Fehler $result. Bei Signaturfehlern bitte die dokumentierte Zadig-Alternative verwenden."
    }
    if ($result -eq 3010) {
        Write-Host 'Windows fordert einen Neustart an. Danach Display neu verbinden und Treiberdienst pruefen.'
        exit 3010
    }
    $devices = @(Get-PnpDevice -PresentOnly | Where-Object { $_.InstanceId -like 'USB\VID_7365&PID_616C\*' })
    if ($devices.Count -eq 0) {
        Write-Host 'Paket aufgenommen. Display anschliessen und im Geraete-Manager den WinUSB-Treiberdienst pruefen.'
        exit 0
    }
    foreach ($device in $devices) {
        $service = (Get-PnpDeviceProperty -InstanceId $device.InstanceId -KeyName 'DEVPKEY_Device_Service').Data
        if ($service -ine 'WinUSB') {
            throw 'Windows hat das Display nicht auf WinUSB umgestellt. Bitte Zuordnung mit Zadig pruefen; PnPUtil erzwingt keine niedrigere Treiberprioritaet.'
        }
    }
    foreach ($device in $devices) {
        $provider = (Get-PnpDeviceProperty -InstanceId $device.InstanceId -KeyName 'DEVPKEY_Device_DriverProvider').Data
        if ($provider -ine 'domfie') {
            throw 'WinUSB ist aktiv, aber Windows verwendet weiterhin ein anderes Paket. Anbieteranzeige im Geraete-Manager pruefen; das domfie-Paket wurde nicht als aktiv bestaetigt.'
        }
    }
    Write-Host 'WinUSB-Zuordnung und Anbieter domfie bestaetigt. Display einmal abziehen und wieder anschliessen.' 
    exit 0
} catch {
    Write-Host ('Fehler: ' + $_.Exception.Message)
    exit 1
}
