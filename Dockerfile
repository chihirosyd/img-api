# ── 构建阶段 ──
FROM golang:1.26-alpine AS builder

WORKDIR /build

# 安装 git（go mod 需要）
RUN apk add --no-cache git

# 复制全部源码（含 go.mod），一次性编译所有入口
COPY . .
# 与 release.yml 的 check 任务保持一致：先升级全部依赖再 tidy，
# 保证镜像内的依赖版本与 Release 二进制一致
RUN go get -u ./... && go mod tidy && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o img-api ./cmd/server/ && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o build-index ./cmd/build-index/ && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o sync-redis ./cmd/sync-redis/ && \
    CGO_ENABLED=0 go build -ldflags="-s -w" -o health-check ./cmd/health-check/

# ── 运行阶段（最小镜像） ──
# alpine 3.22：3.19 已于 2025-11 停止维护
FROM alpine:3.22

# ca-certificates: HTTPS 请求所需；tzdata: 时区；su-exec: root→非root 降权
RUN apk add --no-cache ca-certificates tzdata su-exec && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo "Asia/Shanghai" > /etc/timezone && \
    # 创建非 root 运行用户（uid=1000）
    addgroup -g 1000 appgroup && \
    adduser -u 1000 -G appgroup -s /bin/sh -D appuser

WORKDIR /app

# 从构建阶段复制所有二进制文件
COPY --from=builder /build/img-api .
COPY --from=builder /build/build-index .
COPY --from=builder /build/sync-redis .
COPY --from=builder /build/health-check .

# 复制运行时文件 + 入口脚本
COPY docker/entrypoint.sh /app/
COPY .env.example /app/.env
COPY configs/ ./configs/
# 内置示例副本：compose 挂载 ./configs 会覆盖镜像目录，entrypoint 用它兜底生成 image.yaml
COPY configs/ /app/configs.default/
COPY resources/ ./resources/
COPY storage/ ./storage/

# 入口脚本以 root 运行（自动建目录 + chown），内部再降权到 appuser
RUN chmod +x /app/entrypoint.sh && \
    mkdir -p /app/storage/logs /app/storage/index /app/config

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
