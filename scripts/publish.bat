@echo off
rem 发布 v1.4.0：提交全部改动、打 tag、推送 main 与 tag（触发 CI 构建 Release 与镜像）
cd /d "%~dp0.."
git add -A
git commit -m "release v1.4.0: GUI 控制面板与美化、首页 hero 化、去 gin/viper 精简、exe 图标、配置键名大小写不敏感、修复 compose healthcheck 与后台模式"
if errorlevel 1 goto done
git tag v1.4.0
git push origin main v1.4.0
:done
echo GIT_EXIT=%ERRORLEVEL%
