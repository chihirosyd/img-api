@echo off
rem 构建 Windows 图形控制面板（正式版：压缩体积 + 无控制台窗口 + exe 图标）
rem 需要 go 在 PATH 中（本地 gcc 供 CGO，Windows 下可用 MSYS2 ucrt64）
cd /d "%~dp0.."
go build -ldflags="-s -w -H windowsgui" -o img-api-gui.exe ./cmd/gui/
if errorlevel 1 exit /b 1
echo OK: img-api-gui.exe
