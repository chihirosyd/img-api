@echo off
rem 发布脚本：git add + commit + tag + push（commit message 用英文避免 GBK 乱码）
rem 用法：publish.bat <版本号> <message>
cd /d "%~dp0.."
git add -A
git commit -m "release %1: %2"
if errorlevel 1 goto done
git tag %1
git push origin main %1
:done
echo GIT_EXIT=%ERRORLEVEL%
