@echo off
title yuanbao2api
pushd "%~dp0"
echo ========================================
echo  yuanbao2api service launcher (auto-restart loop)
echo  Close this window to stop the service
echo ========================================
:loop
echo [%date% %time%] Starting service...
"%~dp0main.exe"
echo [%date% %time%] Service exited, restarting in 5 seconds...
ping 127.0.0.1 -n 6 >nul
goto loop