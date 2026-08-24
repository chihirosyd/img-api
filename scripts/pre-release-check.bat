@echo off
rem 发布前检查：git 状态、LICENSE、gitignore、残留文件
cd /d "%~dp0.."
echo === git status ===
git status --short
echo === LICENSE ===
if exist LICENSE (echo LICENSE EXISTS) else (echo NO LICENSE FILE)
echo === stray files ===
if exist check-report.txt (echo check-report.txt PRESENT) else (echo no check-report.txt)
echo === .gitignore ===
type .gitignore
echo === icon resources ===
dir /b cmd\gui\icon.* cmd\gui\icon_windows.syso 2>nul
echo === DONE ===
