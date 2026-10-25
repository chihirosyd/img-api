# 更新日志

本项目遵循[语义化版本](https://semver.org/lang/zh-CN/)，格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/)。

## [1.6.0] - 2026-10-25

### ⚠️ 升级注意（行为变化）

> 以下变化会让"按旧行为配置"的部署或监控看到不同结果，升级前请对照检查。

- `/health` 的 `status=ok` 含义变了：现在只要求**已配置内容**的图源可用（此前要求 txt/local/external 三源齐全，而 external 仓库从不注册，导致 `status` 恒为 `degraded`、Docker 容器恒 `unhealthy`）。升级后健康状态会由 `degraded` 变为 `ok`——若你的监控按 `degraded` 报警，请改为按 `unhealthy` 或具体 `checks` 字段判断。
- `source=local` 的错误码拆分为 `404`（文件不存在）/ `415`（类型不支持）/ `413`（超过 50MB），不再统一返回 `502`。按 502 配置的告警或重试规则需要调整。
- 防盗链对根路径 `/` 的出图分支生效：以前把 `/random` 换成 `/` 即可绕过白名单，现在会被 403。若有页面以 `<img src="https://你的站点/">` 方式嵌入且来源不在白名单，升级后会变成裂图。
- `.env` 解析更严格也更一致：空环境变量视为"未设置"（`APP_HOST=` 不再把服务暴露到 `0.0.0.0`）；成对引号会被剥掉；`image.yaml` 里的未知键会直接报错（此前静默忽略）。
- `sync-redis` 改用**原始文件名**作 Redis 键（`Anime.txt` → `img:pc:Anime`，旧版写入的是小写化的 `img:pc:anime`）：升级后需**重跑一次 `sync-redis`**；重跑前，`Anime` 这类大小写变体分类会退化为文件回退路径（仍能出图，只是不走 Redis 加速）；文件名本来就是全小写的分类键不变、无需任何操作。
- `/health` 的 `stats.circuit_trips` 改名为 `stats.circuit_rejected`（`checks.circuit_trips` 未变）：按旧字段名取数的监控会读不到值
- `sync-redis` 退出码契约变更：**0 全成功 / 1 完全没有同步 / 2 部分成功**（旧版恒为 0）——依赖它判断结果的脚本会开始看到非零
- `?category=` 含 Windows 非法字符（如 `*` `?` `|`）时由 500 改为 **400**（Linux 上原本是 404）：按 500 配置的告警或重试规则需要调整
- 「手动 dispatch 构建镜像」的产出标签变了：`docker.yml` 改为推 `edge` + `sha-xxxx`，**不再推 `latest`**（`latest` 只由 release 工作流在最新 tag 时推进）；若脚本或 compose 依赖手动构建产出的 `latest`，需改指 `edge`
- **源码构建要求 Go 1.27+**：`go.mod` 的 `go` 指令由 1.26.0 升到 1.27.0，镜像基础镜像同步改为 `golang:1.27-alpine`；用 Go 1.26 及更早版本从源码构建会直接报错（二进制与镜像用户不受影响）

### 修复

- GUI 重复启动崩溃：日志初始化失败后再次「启动服务」会空指针 panic（`loggerOnce` 消费后仍用 nil logger）。改为包级初始化状态，失败可重试且始终返回错误
- `X-Request-ID` 截断逻辑修正：此前按**字节**判断长度、按**字符**切片，客户端发 22 个中文字符（66 字节 / 22 rune）即触发 slice 越界 panic（500 + 完整堆栈，且该请求在访问日志与限流之前就崩、不占配额）。改为按 rune 边界截断，零额外分配，并补齐"字节超限但字符不足"与 4 字节字符的回归用例
- 熔断器半开探测遇到客户端取消/超时后不归还探测槽位：每断开一次就永久占掉一个槽，累计 `CIRCUIT_HALF_OPEN_MAX` 次后状态永远停在 `HALF_OPEN`，external 图源只能重启恢复。现在取消/超时路径会归还槽位（仍不计为上游失败）
- `sync-redis` 孤儿清理不再误删「读取失败」的分类：此前一次瞬时 IO 错误（或 `-dir` 指错）会把仍存在的 Redis 集合当成"分类已删除"清掉。现在区分「读取失败 → 保留」与「文件存在但无有效 URL → 删除」，写入改为临时 key + `RENAME` 原子替换（`SAdd` 失败不再丢旧集合），且一个 `.txt` 都没找到时直接跳过清理
- `mode=image` 代理不再把上游错误当成功：非 2xx 响应直接返回 502（此前只校验 `Content-Type`，上游 404/5xx 返回的图片占位图会被当 200 转发）；`io.Copy` 中途断开的错误不再被丢弃（记告警并按失败统计）；超过 50MB 上限改用 `http.ErrAbortHandler` 中止连接（此前依赖 `Hijack`，在 HTTP/2 或非 Hijacker writer 上失败后会正常收尾，客户端拿到被截断却"看似完整"的图片）
- 防盗链不再被根路径旁路：`/` 在 `<img>` 嵌入（`Accept` 以 `image/` 开头）或带 `?mode=` 时会直接出图，此前中间件按路径无条件豁免，盗链方把 `/random` 换成 `/` 即可绕过白名单。现在只豁免"浏览器地址栏访问首页"这一类请求，出图分支照常校验 `Referer`
- `TRUSTED_PROXIES` 下的客户端 IP 改为**从右向左取第一个不可信地址**：此前取 `X-Forwarded-For` 最左段，网关若用 `$proxy_add_x_forwarded_for`（追加语义），最左段就是客户端自带值，每次换一个伪造 IP 即可换一个限流桶（等于无限额），日志也会被写脏。现在逐段跳过可信网段，取最接近真实客户端且无法伪造的那一跳
- SSRF 黑名单补齐 IPv6 转换段：NAT64 `64:ff9b::/96` 与 6to4 `2002::/16` 会先解出内嵌的 IPv4 再按 IPv4 规则判定（`64:ff9b::a9fe:a9fe` 实际就是 `169.254.169.254`），Teredo `2001::/32` 与已废弃的 v4-compatible `::/96` 整段拦截。此前这些地址会绕过黑名单直达内网或云元数据地址
- 限流 429 响应补齐跨域与安全响应头：限流器在 CORS 外层，此前命中限流时 CORS 中间件不会执行，浏览器 fetch 只能看到"CORS 错误"、读不到 429 与 `Retry-After`，前端无法识别是限流并优雅退避；现在 429 分支会补上与正常响应一致的 `Access-Control-*` 与安全头
- 图库与缓存层修正：Redis 不再把"客户端取消/超时"当成 Redis 故障（此前一次刷新就会让全站绕过 Redis 30 秒并打假告警）；本地索引重建串行化并修正临时文件权限；分类判定改为递归，与索引扫描口径一致（图片放在子目录时不再"能出图但分类不存在"）；`external` 指定 `api_name` 时按该 API 的分类白名单过滤；健康检查会拒绝"设备目录是普通文件/不可读"的情况
- 配置解析修正：`.env` 中带引号的值不再被 GUI 原样当成字符串（`RATE_LIMIT_ENABLED="true"` 此前会被误判为 false 并写回关闭限流）；支持 `export` 前缀与 BOM；写回时对含 `#`/`$` 的值加引号；空环境变量视为"未设置"（`APP_HOST=` 不再把服务暴露到 `0.0.0.0`）；非法数字/布尔值、未知 YAML 键、`.env` 解析失败等告警现在会写入日志（GUI 可见）
- 部署与文档修正：手动 `docker run` 不再挂载会被 entrypoint 覆盖的 `.env`；`TRUSTED_PROXIES` 示例去掉中文（此前复制后解析失败、静默忽略）；Nginx 示例把 `limit_req_zone` 移回 `http {}`；CI 工作流权限收窄到各 job（PR 任务只读，PR 构建不再登录 GHCR）；README 补 `git pull --rebase` 与远程部署的目录权限说明；镜像内附 `LICENSE`；`mode=json` 的 `url` 说明更正为相对项目根目录；状态码表补 502/413/415
- 进程生命周期修正：`Shutdown` 改为先等在途请求结束再关缓存/调度器，且 HTTP 超时也会清理后台资源；GUI 启动失败会回收已创建的 Server；redirect 模式只在"上游不支持 HEAD"（405/501/403）时降级 GET，取消/超时不再重发
- `config/image.yaml` 的 `headers` 头名不再被强制转小写：此前配置的 `X-Api-Key` 会变成 `x-api-key`，被大小写敏感的鉴权网关拒绝且无任何线索；`User-Agent` 判定改为大小写不敏感，重复键防护真正生效（此前该分支不可达）
- `image.yaml` 模板示例修正：`default_category` 必须写成列表（`[scenery]`），照抄标量写法会让**整份配置解析失败**、external 图源静默失效
- `build-index` 改为复用主服务的索引写入实现（原子写入 + 落盘 + 重命名）：此前它用固定名 `.tmp` 且不落盘，与主服务行为不一致
- `/health` 不再虚报健康：txt/local 的检查覆盖 `pc`/`pe` 设备子目录（此前只 stat 根目录，而该目录永远存在）；判定只覆盖**已配置内容**的图源（无内容的图源为信息性状态、不计入），`status=ok` 现在意味着"已配置的图源都能取图"——`external` 走 `ExternalPool` 而非仓库，不参与该判定，因此 txt-only 或已配 external 的实例都能正常返回 `ok`（此前 external 键永不出现，导致 `status` 恒为 `degraded`、Docker 容器恒 unhealthy）
- `WriteJSON` 序列化失败时不再返回「200 + 空 body」：改为先序列化再写响应头，失败记告警日志并降级为 500 JSON
- 文档修正：删除 `API.md` 中已不存在的 `local source does not support mode=redirect` 400 示例、统一 local `mode=json` 的 `url` 字段为"相对项目根目录"、404 示例标注仅 Debug 返回；`CONFIG.md` 的 Nginx 示例改为 `$remote_addr` 覆盖 XFF（原 `$proxy_add_x_forwarded_for` 是追加语义，会让伪造 XFF 生效、绕过限流）
- GUI 读取 `.env` 的键匹配改为大小写不敏感，并剥离行内注释：此前 `.env` 里手写 `app_port=9090` 时主服务按 9090 运行、GUI 却显示 8080，点「保存并重启」会把端口改回去
- GUI 的展示地址与「打开首页」跟随 `APP_HOST`（此前硬编码 `127.0.0.1`，改过监听地址后按钮指向无效地址）
- `LOG_LEVEL` 值改为大小写不敏感并校验非法值（此前 `LOG_LEVEL=DEBUG` 会静默回落到 `info`）
- GUI 反复「启动/停止」泄漏后台资源：端口被占用等启动失败场景下 GUI 虽然调了 `Shutdown`，但 `running` 从未置位、函数直接提前返回，限流清理协程、缓存清理协程与本地索引 cron 调度器一个都不释放，每点一次「启动」就漏一套；现在资源清理改由 `sync.Once` 保护并与「是否曾启动」解耦，`Shutdown` 无条件执行清理
- `logger.Init` 重复调用只停后台协程、不关旧文件：GUI 每启停一次泄漏一个日志文件句柄，Windows 下旧日志无法删除或轮转；现在覆盖写入器前先关闭旧文件
- Redis 命中后不再核对分类文件是否仍存在：删除或改名 `.txt` 分类后仍会继续出图（Redis 集合没有过期时间，等于永不失效），且单分类请求与多分类请求会给出相反结论；现在命中后核对文件，文件不存在即按「分类不存在」返回
- 本地图库根目录瞬时不可见（网络盘掉线、`APP_ROOT` 配错）时会用空索引覆盖磁盘上有效的索引：现在区分「根目录不存在」（保留旧索引并限频告警）与「目录存在但无图片」（照常清空），`build-index` 相应改为报错并以非零退出
- `/health` 的同一份响应里给出两个「已运行时长」（handler 创建时刻与统计起点，中间夹着启动预热），数值永久对不上；现在统一取统计起点
- 分类名含 Windows 非法字符或控制字符时返回 500 并刷 ERROR 日志（仅 Windows），与 Linux 的 404 不一致；现在参数校验阶段直接返回 400
- 首页示例 URL 硬编码 `http://`，HTTPS 反代部署下给出的地址粘到博客会被浏览器按混合内容拦掉；现在按 `r.TLS` 输出 `https://`
- 上游图片超过 50MB 主动断连时，该请求既不计成功也不计失败、访问日志也不落盘（`total_requests` 与明细的差额又多一个来源）；现在统计与访问日志都改为 `defer` 收口
- GUI 读取 `.env` 的行内注释规则与 `godotenv` 不一致：值里含无空格前置的 `#`（如 `REDIS_PASSWORD=pa#ss`）会被截断成 `pa`，GUI 保存一次就永久改写密码且无提示；现在与 `godotenv` 同语义（仅空白后的 `#` 视为注释）
- `CIRCUIT_HALF_OPEN_MAX` 的非法值（`<=0`）回退到 1，而默认值与文档都是 3（填 `abc` 反而回退 3）：同一个非法值出现两种结果；现在统一回退 3
- 本地索引的「分类不存在」负缓存每次写入都在全锁下全量扫 map（条目数无上限，key 由请求的 category 决定）；现在改为限频清理 + 容量淘汰，写入恢复为 O(1)
- `/health` 免限流却每次现算健康检查（会 `stat` 目录，空图库时还要遍历整棵树）；现在 checks 结果加 5 秒短缓存

### 变更

- Docker 镜像内置配置改为模板 `/app/.env.default`：容器首次启动用它生成 `config/.env`，之后不再覆盖；`.env.example` 明确标注为「面向有网关部署的推荐值」（`CORS_ENABLED` / `RATE_LIMIT_ENABLED` 为 `false`，而代码内置默认是 `true`）
- 访问日志新增 `query` 字段（超长截断到 256 字节）：`/random` 的语义都在 query 里，此前只记 Path，出现 502 无法从日志判断是哪个 source/category/api
- `.env.example` 的日志项说明移到值上一行（模板不再使用行内注释）
- 分类名的大小写口径统一为「以磁盘原始文件名为准」：此前列分类清单时先把文件名转小写再回探，大小写敏感的文件系统（Linux/Docker）上 `Anime.txt` 能出图却永远不出现在分类清单、多分类请求里也会被过滤掉；Windows/macOS 行为不变
- `stats.circuit_rejected` 的统计口径包含 `HALF_OPEN` 探测槽位耗尽的拒绝，并非每次都代表「刚熔断一次」

### 新增

- **SIGHUP 按需重建本地索引**：`kill -HUP <pid>` 或 `docker compose kill -s HUP img-api` 立即重扫 `resources/local` 并更新索引，取代「exec build-index + 重启服务」两步流程；重建后分类快照立即失效（新分类无需等 30 秒）
- 新增 `CONTRIBUTING.md`：约定「代码 / 注释 / 文档」三者的一致性规则与各文件只放什么（`README.md` 放入口与最常用配置、`docs/` 放对外契约、`private/` 放内部台账），并给出改动行为后同步文档与 `CHANGELOG.md` 的自查流程
- `.env.example` 补充 `APP_ROOT` 与 `IMG_API_OPEN_BROWSER` 两项说明（注明只能作为进程环境变量设置、写进 `.env` 不生效）
- 新增回归测试 20 余个，覆盖上面每条修复（含端口占用后的资源回收、Windows 下日志句柄释放、Redis 命中已删分类、根目录消失不清索引等场景）

### 优化

- `logger` 新增 `Close()`：停止后台清理协程并关闭日志文件（供测试与优雅退出释放文件句柄，Windows 下未关闭的句柄会阻塞文件删除）
- 图片输出恢复 `io.ReaderFrom` 快速路径：中间件包装器（`statusRecorder`/`recoveryWriter`）此前未透传该方法，`mode=image` 的代理与本地文件输出退化为 32KB 用户态缓冲拷贝；本地文件现可走 `sendfile` 零拷贝
- 启动日志新增 `root` 与 `log_dir` 字段：根目录探测错误（发布版最常见的坑）可一眼确认
- `.dockerignore` 补充 `/img-api-gui.exe`、`/setversion*` 与本地构建缓存目录（避免大文件进入构建上下文）
- 仓库首次统一到 `gofmt` 规范：73 个受跟踪 Go 文件全部合规，其中 14 个只有空白对齐与注释缩进调整，无行为变化
- CI 静态检查升级并钉死 `staticcheck@v0.8.2`（原 `v0.7.0` 自身 `go` 指令为 1.25.0，而本模块现要求 1.27.0，兼容余量为 0），并在跑分析前打印实际版本，同时保证 CI 可复现
- `docker.yml` 的 `latest` 标签移出该工作流（改推 `edge` 与 `sha-xxxx`）：此前手动 dispatch 无条件覆盖 `latest`，与 release 工作流的「仅最新 tag 才推进」守卫矛盾，一次误操作就能让所有用 compose 默认值拉取的部署静默降级
- 删除无生产调用方的死代码 `RandomService.CategoryExists`（相关断言改写为 `CategoryExistsFor` 覆盖）；删除 `mode` 分支里与 `redirect` 逐字相同的不可达 `default`
- CI 的 `setup-go` 改为 `1.27.x` + `check-latest: true`：此前默认复用 runner 缓存的 1.26.9，其标准库 `crypto/x509` 存在 GO-2026-6809，`govulncheck` 以退出码 3 让 check 变红；现在工具链、静态检查与镜像基础镜像统一到 Go 1.27

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
