@echo off
setlocal
set "KINSEAL_BRIDGE_DIR=%~dp0"
set "KINSEAL_STUDIO_DIR=%ProgramFiles(x86)%\KinSealStudio5.1"
if "%ProgramFiles(x86)%"=="" set "KINSEAL_STUDIO_DIR=%ProgramFiles%\KinSealStudio5.1"
set "KINSEAL_BRIDGE_ACTION=install"
if /I "%~1"=="/restore" (
  set "KINSEAL_BRIDGE_ACTION=restore"
  if not "%~2"=="" set "KINSEAL_STUDIO_DIR=%~2"
) else (
  if not "%~1"=="" set "KINSEAL_STUDIO_DIR=%~1"
)
powershell.exe -NoProfile -ExecutionPolicy Bypass -Command ^
 "$ErrorActionPreference='Stop'; $staged=$null; try {" ^
 "if(-not ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)){throw 'Bitte die Batchdatei als Administrator ausfuehren.'};" ^
 "if(Get-Process -Name 'KinSealStudio5.1' -ErrorAction SilentlyContinue){throw 'Bitte zuerst KinSeal Studio schliessen.'};" ^
 "$studio=$env:KINSEAL_STUDIO_DIR; $target=Join-Path $studio 'AMZUsb.dll'; $backup=Join-Path $studio 'AMZUsb.vendor.dll';" ^
 "$original='af71f4c1f9c1f9965d503339befad7ae54d545c3b927b3f29e7a3064f9e02d14'; $bridge='285707fd0d23c54676d106485a3c74f753485ee1a8e8fad6c03f420505d54d77';" ^
 "function Hash($p){(Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash};" ^
 "if((Hash (Join-Path $studio 'KinSealStudio5.1.exe')) -ne '7e962ae88df814da649c0914deed76ed7331a4bea815d1f3e89e7dbdd309428a'){throw 'Unbekannte Studio-Version. Keine Aenderung.'};" ^
 "$current=Hash $target; if($env:KINSEAL_BRIDGE_ACTION -eq 'restore'){" ^
 "if($current -eq $original){Write-Host 'Original-DLL ist bereits aktiv.'; exit 0}; if($current -ne $bridge){throw 'Unbekannte aktive DLL. Keine Aenderung.'}; if((Hash $backup) -ne $original){throw 'Original-Sicherung ungueltig.'}; $source=$backup; $expected=$original" ^
 "}else{" ^
 "$source=Join-Path $env:KINSEAL_BRIDGE_DIR 'AMZUsb.dll'; $expected=$bridge; if((Hash $source) -ne $bridge){throw 'Bridge-DLL ungueltig.'};" ^
 "if(Test-Path -LiteralPath $backup){if((Hash $backup) -ne $original){throw 'Vorhandene Sicherung ist unbekannt. Keine Aenderung.'}};" ^
 "if($current -eq $bridge){if(-not (Test-Path -LiteralPath $backup)){throw 'Bridge aktiv, aber Original-Sicherung fehlt.'}; Write-Host 'WinUSB-Bridge ist bereits installiert.'; exit 0};" ^
 "if($current -ne $original){throw 'Unbekannte aktive DLL. Keine Aenderung.'}; if(-not (Test-Path -LiteralPath $backup)){Copy-Item -LiteralPath $target -Destination $backup}" ^
 "}; $staged=Join-Path $studio ('AMZUsb.'+[guid]::NewGuid().ToString('N')+'.tmp'); Copy-Item -LiteralPath $source -Destination $staged; if((Hash $staged) -ne $expected){throw 'Kopierpruefung fehlgeschlagen.'};" ^
 "[IO.File]::Replace($staged,$target,$null); $staged=$null; if((Hash $target) -ne $expected){throw 'Zielpruefung fehlgeschlagen.'}; Write-Host ('Fertig: '+$target);" ^
 "}catch{Write-Host ('Fehler: '+$_.Exception.Message); exit 1}finally{if($staged -and (Test-Path -LiteralPath $staged)){Remove-Item -LiteralPath $staged}}"
set "RESULT=%ERRORLEVEL%"
echo.
if "%RESULT%"=="0" (
  if /I "%KINSEAL_BRIDGE_ACTION%"=="restore" (echo Original-DLL aktiv. Dafuer wieder den libusb0-Treiber verwenden.) else (echo WinUSB beibehalten. Studio kann jetzt gestartet werden.)
)
pause
exit /b %RESULT%
