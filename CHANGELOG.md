# 更新日志

本项目遵循[语义化版本](https://semver.org/lang/zh-CN/)，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [未发布]

## [1.5.0] - 2026-09-14

本次为全面代码审查后的加固版本（审查覆盖发布链路、核心逻辑、一致性与测试共 84 项），重点变更如下。

### 变更

- `source=local` + `mode=redirect`（含默认值）自动降级为 `mode=image`：服务端直接输出图片，不再返回 400 破图；`DEFAULT_SOURCE=local` 时首页嵌图开箱即用
- `source=local&mode=json` 的 `url` 字段改为**相对图库根目录**的路径（如 `pc/default/a.jpg`，原为服务器绝对路径）
- TXT 分类文件名统一**小写约定**：大写 `.TXT` 文件不再被识别（提示页/取图/sync-redis 三处一致）；中文分类名不受影响
- txt 图库与外部 API 仅接受 `http/https` 图片地址：其他协议（`ftp:`、`javascript:` 等）与含控制字符的行被跳过/拒绝
- 429 限流响应新增 `Retry-After: 60`；`/health` 探针不占限流配额；OPTIONS 预检同样计入限流
- `health-check` 新增 `-strict` 参数（degraded 状态退出码 1），compose healthcheck 已启用：服务半故障时容器显示 unhealthy 而非恒 healthy
- `/health` 新增 `circuit_trips` 熔断次数

### 修复

**发布/部署链路（高危）**

- GUI 开机自启根目录探测改用 `resources/` 目录标记（发布包不含 go.mod，此前自启从错误目录启动、服务启动失败）
- 二进制升级指引重写为「临时目录解压、只覆盖二进制」：不再覆盖用户的 `config/image.yaml` 与图库 `default.txt`，并修复 zip 嵌套目录导致"升级后仍是旧版本"的问题
- 文档镜像 tag 去 `v` 前缀（镜像实际 tag 为 `1.4.9`，此前文档写 `:v1.2.0` 会拉取失败）
- Release 拆独立 publish job：修复 5 平台并行创建 Release 的 422 竞态（间歇性缺 zip）
- 镜像构建并入 release.yml 并依赖检查全绿：测试红不再推送镜像/挪动 latest；仅最新 tag 可更新 latest（防旧 tag 回退）
- systemd 示例补 `chown` 权限步骤；手动 docker run 示例去掉 `:ro`（修复容器循环重启）

**缓存与存储**

- 内存缓存过期删除竞态修复：不再误删刚写入的新值、计数不再漂移（淘汰上限失效问题），新增优雅关闭
- 本地索引扫描失败不再用空索引覆盖磁盘上有效的 `local.json`
- 索引原子写入（同目录随机临时文件 + 落盘 + 重命名）
- Redis 运行期故障 30 秒冷却直走文件（不再每个请求等待超时）并输出限频告警

**安全**

- SSRF 拦截补全保留网段（TEST-NET、`240.0.0.0/4`、`198.18.0.0/15` 等）
- `X-Forwarded-For`/`X-Real-IP` 首段合法性校验：伪造值回退对端 IP（防无限限流桶）
- 防盗链豁免首页与 `/health`（外链访问首页不再 403）；文档写明 `referrerpolicy=no-referrer` 可绕过边界
- query 参数长度上限 128 字节（防日志膨胀/响应放大）；X-Request-ID 控制字符清洗

**接口正确性**

- JSON 型外部 API 返回实际分类（不再固定 `external`，与 redirect 型一致）
- 上游 Content-Type 大小写不敏感判定（`Image/Png` 不再误拒 502）
- IO 故障不再误报「分类不存在」404；`mode=image` 代理失败计入失败统计
- `default` 分类判定大小写不敏感；超长行处理两条路径结论一致
- panic 恢复输出统一 JSON 契约；端口占用错误同步返回（先 Listen 再 Serve）
- `APP_PORT` 非法值回退默认并输出告警（此前静默用 8080/绑随机端口）

**GUI 与配置**

- GUI 保存 `.env` 键匹配大小写不敏感（不再追加重复键）且保留原行尾；重启失败提示修正；启动前即显示配置端口
- GUI 反复重启的后台协程泄漏修复（缓存/限流器/索引调度器随服务优雅关闭）
- 配置的默认分类值增加路径校验（非法回退内置 default 并告警）

### CI/CD 与 Docker

- 新增 Windows（windows-latest）GUI 构建验证 job：`cmd/gui` 不再零覆盖"盲发"
- 版本一致性门禁：tag 必须与 CHANGELOG、config.go 三方一致；`go mod tidy` 后校验 diff
- Release 资产新增 SHA256SUMS 与 SPDX SBOM；setup-go 启用依赖缓存；staticcheck（v0.7.0）/ govulncheck（v1.5.0）固定版本
- gosum 自动升级依赖提交前先跑测试；Dockerfile 依赖层缓存（源码变更不重下依赖）+ 时区 ARG（构建期可覆盖）
- `setversion` 防呆：CHANGELOG 版本低于代码版本时报错、相等时幂等空操作

### 优化

- 熔断器：客户端取消/超时不计入失败；时钟可注入（超时转移可测试）；新增 Trips 计数
- 分类清单快照/图源空置检查重建合并（缓存过期瞬间只扫描一次）；Health 检查不再持锁 IO
- local 索引未命中且目录不存在时负缓存 30 秒（轻量 Stat 复核，新增分类仍即时可用）
- netx DNS 解析缓存 30 秒 + 拨号器支持注入解析器
- `math/rand` → `math/rand/v2`；`writeJSON` 抽共享包（handler/middleware 同一实现）
- 测试大幅补强：熔断并发半开上限、外部 API httptest 端到端、拨号拦截集成断言、缓存淘汰、符号链接等 60+ 断言

### 文档

- API/README/CONFIG/DEPLOY 全面同步：local 自动降级与相对路径说明、分类名小写约定、防盗链边界与首页豁免、XFF 覆盖要求、升级指引重写、开机自启残留清理；`/health` 新增字段（`circuit_trips`）与 `-strict` 用法、429 的 `Retry-After`、参数长度上限、`.env.example` 与 `image.yaml` 注释同步更新

## [1.4.9] - 2026-09-06

### 变更

- Release 发布流程：Release 标题使用 tag 名、正文使用 CHANGELOG.md 内容（此前轻量 tag 的正文会回退为 commit message）

### 其他

- 仓库新增 LICENSE 文件（MIT），README 许可证徽章链接指向该文件

## [1.4.8] - 2026-08-31

### 修复

- 外部 API 上游请求按设备类型自动携带对应的 User-Agent（手机 UA / 桌面 UA），使按 UA 自适应横竖屏的 API 能返回对应版本图片（此前使用 Go 默认 UA，恒被上游识别为电脑端返回横图）；`headers` 中显式配置的 `User-Agent` 优先

### 文档

- 端口相关说明完善：`APP_PORT`/`APP_HOST` 注释、compose 端口映射注释、DEPLOY 换宿主机端口提示、Nginx `proxy_pass` 与防火墙示例的同步提醒

## [1.4.7] - 2026-08-30

### 新增

- 首页仪表盘与 `/health` 增加"今日统计"：`today_*` 字段按本地时间每日 0 点自动归零，`total_*` 保持自启动累计（重启归零）

## [1.4.6] - 2026-08-30

### 新增

- local 索引自动刷新升级为计划表（`LOCAL_INDEX_REFRESH`）：支持 Go duration（如 `30s`/`30m`/`24h`/`168h`）、`@hourly`/`@daily`/`@weekly`/`@monthly`/`@yearly` 描述符与标准 5 字段 cron（如 `0 3 * * *` 每天 03:00），底层改用 robfig/cron 调度

### 变更

- external 渠道未配置 `default_category` 时不再向上游发送字面 `default`：`{category}` 占位符置空、不追加 `category_param`，避免与上游 API 的分类参数冲突
- 移除旧键 `LOCAL_INDEX_REFRESH_MINUTES`（整数分钟），改由 `LOCAL_INDEX_REFRESH` 统一承载（分钟写 `30m`）；项目尚无存量部署，不做兼容迁移

## [1.4.5] - 2026-08-30

### 优化

- 统一 CI、Docker 构建与发布流程的依赖来源：依赖以仓库锁定的 `go.mod` / `go.sum` 为准（Dockerfile 不再现场 `go get -u`，release.yml check 只 `go mod tidy`），Release 二进制、镜像与检查三者同源
- 发布流程改为"先推 main、等 Update go.sum 兜底跑完、再打 tag"；两个工作流增加同 ref 并发串行保护
- docs/DEPLOY.md 发布章节补充流程图、发布前本地检查清单；README 新增"发布新版本"指引

## [1.4.4] - 2026-08-30

### 新增

- txt / local 渠道支持自定义默认分类（`TXT_DEFAULT_CATEGORY` / `LOCAL_DEFAULT_CATEGORY`）：请求不带 `category`（或传 `default`）时使用，留空保持内置 `default`；"分类不存在"提示页同步显示实际使用的分类

### 其他

- 升级 golang.org/x/image v0.24.0 → v0.45.0（修复 4 个 tiff 解码相关漏洞，govulncheck 全绿）
- GUI 主题改用 theme.DefaultTheme 并显式按开关选择亮/暗变体，移除已弃用的 LightTheme/DarkTheme 调用

## [1.4.3] - 2026-08-26

### 优化

- 运行时长格式化按量级自动升级单位（秒 → 分 → 时 → 天 → 月 → 年，保留两位有效单位）：首页"已运行"与 `/health` 的 `uptime` 由 `45h 15m` 变为 `1d 21h`，长时间运行更直观

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
