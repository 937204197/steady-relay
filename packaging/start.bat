@echo off
setlocal
echo Starting Steady Relay...
echo Set UPSTREAM_BASE_URL first, or pass --upstream https://api.example.com/v1
echo Local API base URL after startup: http://127.0.0.1:8080/v1
echo If 8080 is occupied, use the actual port shown in the program log.
if "%~1"=="" if not defined UPSTREAM_BASE_URL (
  echo.
  set /p "UPSTREAM_BASE_URL=Paste your trusted upstream API Base URL (usually ending in /v1), then press Enter: "
)
if not defined UPSTREAM_BASE_URL (
  echo.
  echo No upstream URL was entered. Nothing was started.
  pause
  exit /b 2
)
"%~dp0steady-relay.exe" %*
echo.
echo Steady Relay stopped. Press any key to close this window.
pause >nul
exit /b %ERRORLEVEL%
