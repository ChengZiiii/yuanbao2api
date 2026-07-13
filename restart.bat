@echo off
title yuanbao2api
echo ========================================
echo  元宝2API 服务启动器（自动重启循环）
echo  关闭此窗口即可停止服务
echo ========================================
:loop
echo [%date% %time%] 启动服务...
.\main.exe
echo [%date% %time%] 服务已退出，5 秒后重启...
timeout /t 5 >nul
goto loop
