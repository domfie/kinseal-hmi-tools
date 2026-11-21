@echo off
setlocal
if /I "%~1"=="/update" (
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Install-WinUSB.ps1" -UpdatePackage
) else (
  powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0Install-WinUSB.ps1"
)
set "RESULT=%ERRORLEVEL%"
echo.
pause
exit /b %RESULT%
