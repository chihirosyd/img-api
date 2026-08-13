#!/bin/sh
# ============================================================
# img-api 容器入口脚本
#
# 执行流程（以 root 运行）：
#   1. 自动创建运行时目录（用户无需手动创建任何文件夹）
#   2. 处理 .env 配置（首次启动自动生成，后续启动用宿主机的覆盖）
#   3. 兜底生成 configs/image.yaml（compose 挂载覆盖后仍保证示例文件存在）
#   4. 修正目录所有权（宿主机挂载的目录默认是 root，降权前改为 appuser）
#   5. 降权以非 root 用户 appuser 运行主程序
#
# 这样既满足"非 root 运行"的安全要求，
# 又满足"用户无需手动创建目录"的易用性要求。
# ============================================================
set -e

# ── 第 1 步：自动创建运行时目录 ──
mkdir -p /app/config
mkdir -p /app/storage/logs
mkdir -p /app/storage/index

# ── 第 2 步：处理 .env 配置 ──
# 首次启动：将默认 .env 复制到宿主机挂载的 /app/config/ 供用户编辑
# 后续启动：用 /app/config/.env 覆盖容器内默认值
if [ -f /app/config/.env ]; then
    cp /app/config/.env /app/.env
else
    cp /app/.env /app/config/.env
fi

# ── 第 3 步：兜底生成 configs/image.yaml ──
# compose 挂载 ./configs 时会覆盖镜像内置的配置目录，首次启动宿主机侧为空目录；
# 这里把内置示例复制出来，保证管理员总能找到 image.yaml 进行编辑
if [ ! -f /app/configs/image.yaml ]; then
    cp /app/configs.default/image.yaml /app/configs/image.yaml 2>/dev/null || true
fi

# ── 第 4 步：修正目录所有权（宿主机挂载目录默认 owner 是 root）──
# 只对需要写入的目录 chown，只读目录（resources/）保持不变
chown -R appuser:appgroup /app/config /app/configs /app/storage 2>/dev/null || true

# ── 第 5 步：降权运行主程序 ──
exec su-exec appuser:appgroup /app/img-api
