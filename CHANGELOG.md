# 更新日志

本项目遵循[语义化版本](https://semver.org/lang/zh-CN/)，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [1.4.2] - 2026-08-24

### 修复

- 首页内容溢出被裁剪：长 URL 撑破三列网格（列宽无上限 + 卡片 `overflow: hidden`）；列宽改用 `minmax(0,1fr)`、长文本强制换行、新增 700px 中屏断点

## [1.4.1] - 2026-08-24

### 移除

- 删除仓库内 LICENSE 文件（许可证由仓库所有者在 GitHub 界面选择创建）

## [1.4.0] - 2026-08-24

### 新增

- **Windows 图形控制面板**（`img-api-gui.exe`）：启动/停止/重启服务、核心设置编辑（保存后自动重启）、开机自启（当前用户级注册表）、后台运行（关闭窗口最小化到托盘）；命令行版 `img-api.exe` 完全不变
- Windows 双击命令行版自动打开浏览器到教程首页，控制台打印友好启动横幅（`IMG_API_OPEN_BROWSER=0` 可关闭）
- 首页美化：hero 区 + GitHub 仓库按钮、三步上手卡片化、常用参数表格化、页脚（GitHub / 更新日志链接）
- GUI 美化：品牌蓝主题（与网页一致）、卡片式分区、状态指示点、按钮配色分级、深色模式开关、窗口/托盘/exe 图标（`cmd/genicon` 图标生成器）
- exe 文件图标（`cmd/gui/icon.ico` + `icon_windows.syso`，仅 Windows 构建生效）

### 修复

- GUI `--background`（开机自启）模式下进程立即退出导致服务停止：隐藏窗口后仍进入事件循环，托盘与进程保持存活
- Docker healthcheck 参数错误：`health-check -url` 接收基地址并自动追加 `/health`，compose 传入完整路径导致请求 `/health/health` 恒 404、容器被标记 unhealthy；改为传入 `http://localhost:8080`
- 访问日志与防盗链拦截日志的 IP 统一尊重 `TRUSTED_PROXIES`（此前重构后不信任任何转发头，反代部署日志只见代理 IP）

### 优化

- **Web 框架去除 Gin，改用标准库 `net/http`**（Go 1.22+ 路由）；**配置读取去除 Viper**（改用 godotenv + yaml.v3）：服务器 exe 体积 17.5MB → 9.5MB，依赖数量大幅减少，全部测试通过；行为差异：非 GET 方法由 404 变为 405、HEAD 由 GET 路由匹配、访问日志与限流统一尊重 `TRUSTED_PROXIES`

## [1.3.0] - 2026-08-23

### 移除

- Token 鉴权（`AUTH_ENABLED` / `AUTH_TOKEN`）与健康检查密钥（`HEALTH_SECRET`）：公开图 API 场景下 token 无法在 `<img>` 嵌入时携带（出现在公开网页等于无保护），移除后配置更简单；`/health` 恒返回完整内部状态，首页仪表盘恒完整展示

## [1.2.1] - 2026-08-23

### 修复

- 浏览器地址栏访问 `/` 被误判为图片请求（现代浏览器导航的 Accept 头含 `image/avif` 等条目），导致首页显示"图片源未配置"占位图而非教程页；`acceptsImage` 改为按 Accept **前缀**判断，`/random` 的 404/503 提示页 HTML/SVG 协商同步修正

### 文档

- `DEPLOY.md` 新增「更新升级」章节（Docker / 二进制升级与回滚、跨版本迁移提示）

## [1.2.0] - 2026-08-23

### 新增

- **首页运行状态仪表盘**：浏览器访问 `/` 展示图源健康徽章、请求统计卡片、运行时长（分类清单仅 Debug 展示；配置 `HEALTH_SECRET` 后仅极简状态，与公开 `/health` 语义一致）
- **首页 UI 美化**：渐变背景与顶部彩条、状态呼吸灯、健康状态徽章、统计数字卡片、深色模式自动适配、移动端响应式

### 文档

- `DEPLOY.md` 补全 Windows 完整教程（双击运行 / 静默后台 / 开机自启 / 防火墙放行）与 Linux 后台运行、systemd 排障命令

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
