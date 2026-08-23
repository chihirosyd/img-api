# 更新日志

本项目遵循[语义化版本](https://semver.org/lang/zh-CN/)，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [未发布]

## [1.1.0] - 2026-08-23

### 新增

- **根路径教程首页**：浏览器访问 `/` 展示三步上手与参数速查页；`<img>` 直接嵌入 `/` 仍返回随机图片，兼容不变
- **孤儿 Set 清理**：`Cache` 接口新增 `ScanKeys`，sync-redis 同步时自动删除已移除分类的 Redis 残留
- **redirect 型外部 API 支持自定义请求头**（如 Authorization）
- 测试：RequestID 单测、首页三分流用例、参数大小写回归

### 变更

- **配置目录合并**：`configs/` 与 `config/` 统一为 `config/`（`.env` 与 `image.yaml` 同居一目录），旧部署的 `configs/image.yaml` 启动时自动迁移
- 请求参数 `source` / `mode` / `type` 大小写不敏感
- `fetchRedirect` 返回实际分类（不再固定为 `external`）
- Docker 容器日志轮转（`max-size: 10m / max-file: 3`，不锁定 driver）

### 修复

- **entrypoint 兜底补齐图库骨架**：远程镜像部署（只下载 compose 文件）时，挂载空目录会覆盖镜像内置的 `default.txt` 与目录骨架，现首启自动补齐
- **`.dockerignore` 修复**：不再整体排除 `config/`（会导致镜像构建失败），仅排除敏感 `config/.env`
- **local 索引生效时机文档修正**：仅重启服务不会重扫目录，需 `build-index` 后重启加载（或定时刷新）
- RequestID 超长输入按 rune 截断，不再切断多字节 UTF-8
- `netx` 拦截全部组播地址（224.0.0.0/4 与 ff00::/8）
- CORS 预检放行 `X-Health-Secret` 请求头
- Windows 下 viper 对配置文件缺失的误报警告
- config 测试与执行环境隔离（不再被终端残留的 `APP_*` 环境变量干扰）

### 安全

- `sync-redis` 的 `-dir` 支持绝对路径
- `MemoryCache.Get` 返回数据拷贝，防调用方污染缓存
- 配置 `$` 陷阱告警扩展到全部字符串配置项

## [1.0.0] - 2026-08-22

- 首个发布版本：TXT / 本地 / 外部 API 三种图源、设备自适应、Redis 缓存降级、熔断保护、Token 鉴权、IP 限流、Referer 防盗链、健康检查双模式
