@echo off
setlocal
echo Starting Codex Reroute...
echo Set UPSTREAM_BASE_URL first, or pass --upstream https://api.example.com/v1
echo Local API base URL after startup: http://127.0.0.1:8080/v1
echo If 8080 is occupied, use the actual port shown in the program log.
if "%UPSTREAM_BASE_URL%"=="" if "%~1"=="" (
  echo.
  echo UPSTREAM_BASE_URL is required. Example:
  echo   set UPSTREAM_BASE_URL=https://api.example.com/v1
  echo   start.bat
  echo.
  pause
  exit /b 2
)
"%~dp0codex-reroute.exe" %*
echo.
echo Codex Reroute stopped. Press any key to close this window.
pause >nul
exit /b %ERRORLEVEL%
