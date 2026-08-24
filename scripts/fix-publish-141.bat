@echo off
rem 修复 v1.4.1 发布：删除错误 tag → 重新提交 → 重新打 tag 推送
cd /d "%~dp0.."
git tag -d v1.4.1
git push origin :v1.4.1
git add -A
git commit -m "release v1.4.1: remove LICENSE file, add publish script args"
if errorlevel 1 goto done
git tag v1.4.1
git push origin main v1.4.1
:done
echo GIT_EXIT=%ERRORLEVEL%
