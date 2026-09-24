@echo off
rem Double-click launcher for nullmodem-qwk.ps1 -- PowerShell scripts
rem don't run on double-click by default (Windows opens them in an
rem editor instead), so this starts PowerShell with a policy bypass
rem scoped to just this one process and keeps the window open
rem afterward so you can read the output.
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0nullmodem-qwk.ps1"
pause
