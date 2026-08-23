#!/bin/sh
# ============================================================
# img-api 容器入口脚本
#
# 执行流程（以 root 运行）：
#   1. 自动创建运行时目录（用户无需手动创建任何文件夹）
#   2. 处理 .env 配置（首次启动自动生成，后续启动用宿主机的覆盖）
#   3. 兜底生成 config/image.yaml（compose 挂载覆盖后仍保证示例文件存在）
#   4. 兜底补齐 resources 图库骨架（compose 挂载覆盖后仍保证 default.txt 与目录存在）
#   5. 修正目录所有权（宿主机挂载的目录默认是 root，降权前改为 appuser）
#   6. 降权以非 root 用户 appuser 运行主程序
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

# ── 第 3 步：兜底生成 config/image.yaml ──
# compose 挂载 ./config 时会覆盖镜像内置的配置目录，首次启动宿主机侧为空目录；
# 这里把内置示例复制出来，保证管理员总能找到 image.yaml 进行编辑
# 兼容旧版本：老部署的 configs/image.yaml（旧挂载）自动复制迁移到 config/image.yaml
if [ -f /app/configs/image.yaml ] && [ ! -f /app/config/image.yaml ]; then
    cp /app/configs/image.yaml /app/config/image.yaml 2>/dev/null || true
fi
if [ ! -f /app/config/image.yaml ]; then
    cp /app/config.default/image.yaml /app/config/image.yaml 2>/dev/null || true
fi

# ── 第 4 步：兜底补齐 resources 图库骨架 ──
# compose 挂载 ./resources/** 时会覆盖镜像内置骨架，首次启动宿主机侧可能是空目录；
# 这里补齐目录与示例 default.txt（仅当不存在时创建，不覆盖用户已有内容）。
# 注意：因此 resources 挂载不能加 :ro（compose 中已保持可写）
mkdir -p /app/resources/txt/pc /app/resources/txt/pe
mkdir -p /app/resources/local/pc/default /app/resources/local/pe/default
[ -f /app/resources/txt/pc/default.txt ] || cp /app/resources.default/txt/pc/default.txt /app/resources/txt/pc/default.txt 2>/dev/null || true
[ -f /app/resources/txt/pe/default.txt ] || cp /app/resources.default/txt/pe/default.txt /app/resources/txt/pe/default.txt 2>/dev/null || true

# ── 第 5 步：修正目录所有权（宿主机挂载目录默认 owner 是 root）──
# 只对需要写入的目录 chown；resources 由宿主机侧管理，保持其原有所有权
chown -R appuser:appgroup /app/config /app/storage 2>/dev/null || true

# ── 第 6 步：降权运行主程序 ──
exec su-exec appuser:appgroup /app/img-api
