# 配置参考

| 分组 | 章节 |
|---|---|
| **一、配置参考**<br>（按变量查默认值与含义） | [应用设置](#应用设置) · [安全设置](#安全设置) · [图片来源](#图片来源) · [Redis 缓存](#redis-缓存) · [外部 API 池](#外部-api-池) · [熔断器](#熔断器) · [日志](#日志) · [版本](#版本) |
| **二、生效与排障**<br>（改完什么时候生效） | [变更生效时机](#变更生效时机) |
| **三、网关部署** | [Nginx 反代场景](#nginx-反代场景) |

所有配置通过项目根目录的 `.env` 文件管理（外部 API 池配置在 `config/image.yaml`，
见下方章节）。首次使用时复制 `.env.example` 为 `.env` 并修改。

> 🐳 **Docker 例外**：容器里的程序同样读 `/app/.env`，但那个文件不由用户维护——
> `docker/entrypoint.sh` 每次启动都用 `/app/config/.env` 覆盖它；若 `config/.env` 不存在，
> 首次启动会先用镜像内置模板 `/app/.env.default` 生成一份。所以**容器部署要改的是宿主机
> 挂载目录里的 `config/.env`**（别再把 `.env` 单独挂到 `/app/.env`，会被覆盖而"改了不生效"），
> 只有二进制部署才直接用项目根的 `.env`。详见 docs/DEPLOY.md 的「手动构建（不使用远程镜像）」
> 及其下方的配置来源说明。

> ⚠️ 修改 `.env` 后需**重启服务**生效（Docker 部署执行 `docker compose restart`，
> 需在 `docker-compose.yml` 所在目录执行，否则报 `no configuration file provided`）。

> 🔑 **.env 特殊字符陷阱**：解析器会对值做 `$` 变量展开，并把 `#` 当作注释。
> 值含 `$` 或 `#` 时（常见于 Redis 密码等），请用**单引号**包裹：
> `REDIS_PASSWORD='pa$sw0rd'`。双引号不能阻止 `$` 展开，不写引号则会直接出错。
>
> 📌 **取值规则（环境变量 > `.env` > 默认值，键名不分大小写）**：
> - 成对引号会被剥掉：`APP_PORT="8080"` 等价于 `APP_PORT=8080`；
> - 支持 shell 风格的 `export APP_PORT=8080`（`export ` 前缀被忽略）；
> - 文件开头的 UTF-8 BOM 会被忽略；
> - **空值 = 未设置**：`APP_HOST=` 不会覆盖 `.env` 里的值（Docker Compose 里写 `APP_HOST:` 就是这种情形）；
> - 非法数字/布尔值回退默认并写告警到日志；`image.yaml` 里的未知键直接报错（不再静默忽略）。
>
> 🕒 **自 v1.6.0 起**：引号剥离、"空值 = 未设置"、`export` 前缀与 BOM 兼容、未知 YAML 键报错
> 才开始生效；更早版本会把 `"true"` 当成带引号的字符串、把 `APP_HOST=` 当成有效覆盖。

---

## 一、配置参考（按变量查）

### 应用设置

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `APP_DEBUG` | bool | `false` | 调试模式（仅开发排查用）：`true` 显示详细错误 + 调用位置 + "分类不存在"页附可用分类列表，会暴露图库分类清单，公网部署保持 `false` |
| `APP_NAME` | string | `img-api` | 应用名称（日志标识） |
| `APP_HOST` | string | `0.0.0.0` | 监听地址（`0.0.0.0` 监听所有网卡） |
| `APP_PORT` | int | `8080` | 应用监听端口（Docker 部署时指容器内端口，改端口需同步 compose 端口映射右侧） |

> 📁 **`APP_ROOT`（项目根目录，全平台生效，只能作为进程环境变量）**：决定 `.env`、`resources/`、`storage/` 的定位
> （读配置、扫图库、写日志与本地索引都相对它）。优先级：`APP_ROOT` → 可执行文件所在目录（该目录下含
> `resources/` 才认作根目录）→ 当前工作目录。它**不能写进 `.env`**：`.env` 自身就在 `APP_ROOT/.env`，
> 先有根目录才谈读配置。Docker 部署已由 `docker-compose.yml` 设置 `APP_ROOT=/app`；二进制部署仅在
> "exe 与 `config/`、`resources/` 不同目录"或"从其他工作目录启动（systemd、开机自启）"时需显式设置。

> 🖥️ **`IMG_API_OPEN_BROWSER`（仅 Windows 生效，只能作为进程环境变量）**：双击 `img-api.exe` 启动时是否自动
> 打开默认浏览器到首页（方便普通用户直观确认服务是否正常）。`0` = 不打开；不设置或其它值 = 打开。
> Linux / macOS / Docker 下该变量**无任何作用** —— 服务器模式下只有 Windows 会执行打开浏览器的逻辑。
> 设置方式：Windows 的 `set` / `setx` 或「系统属性 → 环境变量」面板（写进 `.env` 不生效）。
>
> 📌 打开浏览器是**异步且静默失败**的：走 Windows 的系统默认 http 处理器（`rundll32 url.dll,FileProtocolHandler`），
> 所以装 Edge / Chrome / Firefox 都行，程序不关心具体是哪个。**万一机器上没有可用浏览器、或 http 协议没有
> 注册处理器**，结果只是不弹窗口（个别情况下由 Windows 自己弹一个「没有可打开此链接的应用」提示）——
> **服务照常运行**，手动访问 `http://localhost:<端口>/` 即可；该失败仅记一条 Debug 日志，`LOG_LEVEL=info` 时看不到。

---

### 安全设置

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `RATE_LIMIT_ENABLED` | bool | `true` | 是否开启内置 IP 限流（纯内存实现：重启清零、多副本各自计数）。429 响应附 `Retry-After: 60` 与 CORS/安全响应头（前端 fetch 能识别是限流并退避）；`/health` 探针不计入限流；OPTIONS 预检计入。前方有 Nginx/OpenResty 时建议关闭并改用网关的 `limit_req`，否则需配合 `TRUSTED_PROXIES` 才能按真实访客统计 |
| `RATE_LIMIT_MAX` | int | `60` | 每分钟每 IP 最大请求数（`RATE_LIMIT_ENABLED=true` 时生效） |
| `REFERER_WHITELIST` | string | — | 防盗链域名白名单，逗号分隔（如 `mysite.com,blog.mysite.com`）。仅作用于图片接口（`/health` 不拦截；`/` 仅在浏览器地址栏访问首页时豁免，以 `<img>` 嵌入或带 `?mode=` 参数同样受保护）；空 Referer 默认放行（盗链站可用 `referrerpolicy=no-referrer` 绕过，仅拦截普通盗链）；留空 = 不限制。经 Nginx 转发后依然生效 |
| `TRUSTED_PROXIES` | string | — | 可信反代网段（逗号分隔 CIDR，如 `172.17.0.0/16`）。仅当请求来自这些网段时，限流器与应用自带访问日志（`storage/logs`）才信任 `X-Forwarded-For`/`X-Real-IP`（且值必须是合法 IP，否则按对端 IP）；留空 = 一律按 TCP 对端 IP（防伪造）。网段务必填窄（覆盖网关即可）。应用会从右向左跳过可信网段、取第一个不可信地址，因此网关**覆盖**客户端 XFF（`proxy_set_header X-Forwarded-For $remote_addr;`）是最佳实践；透传 `$http_x_forwarded_for` 已不会让限流失效，但会把伪造 IP 留在日志里。限流交给网关后仍建议填写，应用自带日志才能记录真实访客 IP（网关自身的日志不受影响，本就能记录真实 IP） |
| `CORS_ENABLED` | bool | `true` | 是否添加跨域响应头。`<img>` 标签嵌图不需要跨域；只有其他网站的 JS 调用本 API 才需要。Nginx 已处理时可关闭（仅一侧开启，勿两边同开） |

> ⚠️ **`.env.example` 的默认值与代码默认值不同，这是有意的**：
> 模板里 `CORS_ENABLED=false`、`RATE_LIMIT_ENABLED=false`，是**面向"前方有 Nginx/OpenResty 网关"的推荐值**
> （跨域与限流交给网关，见本文档 [Nginx 反代场景](#nginx-反代场景)）；
> 而代码内置默认值是 `true`（即**不建 `.env`** 时两项都开启）。
> - 直接暴露端口、无网关：请显式写 `true`，或不要从模板复制这两项
> - 有网关且已配置限流/跨域：保持模板的 `false`
> - Docker 部署：首次启动会用镜像内置模板生成 `config/.env`，同样是 `false`，按上面的规则调整

> 💡 **Docker 部署的配置来源**：镜像内置的是配置模板 `/app/.env.default`，
> 容器首次启动时若 `config/.env` 不存在才会用它生成；之后一律以 `config/.env` 为准
> （不会覆盖你已改好的配置）。

> 🕒 **自 v1.6.0 起**：`REFERER_WHITELIST` 对根路径 `/` 的出图分支同样生效
> （`<img>` 嵌入或带 `?mode=` 时受白名单约束），只有浏览器地址栏访问首页才豁免。

---

### 图片来源

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DEFAULT_SOURCE` | string | `txt` | 默认图片来源：`txt` / `local` / `external` |
| `TXT_DEFAULT_CATEGORY` | string | — | txt 渠道默认分类：请求不带 `category`（或显式传 `default`）时使用。留空 = 内置 `default`（`default.txt`） |
| `LOCAL_DEFAULT_CATEGORY` | string | — | local 渠道默认分类：请求不带 `category`（或显式传 `default`）时使用。留空 = 内置 `default`（`default` 目录） |
| `LOCAL_INDEX_REFRESH` | string | — | 本地图片索引（`storage/index/local.json`）自动刷新计划，`0`/空=仅首次启动生成（三种写法见下方速查表） |

> 💡 `LOCAL_INDEX_REFRESH` 三种写法**任选其一**（每次只填一个值）：
>
> | 写法 | 示例值 | 说明 |
> |------|------|------|
> | Go duration | `30s` / `30m` / `2h` / `24h` / `168h` | 单位仅 `ns`/`µs`/`ms`/`s`/`m`/`h`；天写 `24h`、周写 `168h` |
> | 描述符 | `@hourly` / `@daily` / `@weekly` / `@monthly` / `@yearly` | 按自然周期，服务器本地时区 |
> | 5 字段 cron | `0 3 * * *` / `0 2 1 * *` | 每天 03:00 / 每月 1 号 02:00 |
>
> 示例：`LOCAL_INDEX_REFRESH=@daily`、`LOCAL_INDEX_REFRESH=0 3 * * *`

> 📌 本地图片索引说明：首次启动时若 `storage/index/local.json` 不存在会自动生成。
> 之后按 `LOCAL_INDEX_REFRESH` 计划表重新扫描刷新。
> 新增分类（新目录）即时可用（索引未命中时直接扫描目录兜底）；
> 已有分类中增删图片需重建索引，并让服务加载新索引（重启、发 `SIGHUP`，或等计划刷新）
> ——仅重启服务不会重新扫描目录，重建前删除的图片可能随机返回 404（索引仍指向
> 已不存在的文件），新增的图片不会被选中。
>
> 🔧 **手动重建 local.json（步骤）**：
>
> **最快方式（免 exec、免重启）**：向服务进程发 `SIGHUP`
> —— Linux/二进制：`kill -HUP <pid>`；Docker：`docker compose kill -s HUP img-api`（服务名即可）。
> 服务收到后立即重扫 `resources/local` 并写回索引，同时让分类清单快照立即失效。
> ⚠️ Windows 不支持 `SIGHUP`，请用下面第 2 步或重启服务。
>
> 其余方式（需要重新扫描时可任选）：
>
> 1. Docker 部署先确认服务名：运行 `docker compose ps --services` 查看实际服务名，
>    把下面步骤中的 `img-api` 替换成它（默认服务名就是 `img-api`）；
>    二进制包 / 源码部署跳过此步。
> 2. 重建索引（按部署方式三选一）：
>    - 二进制包：`./build-index`
>    - 源码：`go run ./cmd/build-index/`
>    - Docker：`docker compose exec img-api /app/build-index`
> 3. 让服务加载新索引（三选一）：
>    - 发 `SIGHUP`（推荐，免重启，见上文命令）
>    - 重启服务（Docker：`docker compose restart`）
>    - 等 `LOCAL_INDEX_REFRESH` 计划刷新（需已配置）
>
> 也可以直接删除 `storage/index/local.json` 后重启，启动时会自动重建。
>
> 💡 `img-api` 是 compose **服务名**（不是容器名）。`docker compose ps --services`
> 可直接列出实际服务名；本文档其他 `docker compose exec img-api ...` 命令同理替换。

---

### Redis 缓存

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `REDIS_ADDR` | string | — | Redis 地址（留空不使用 Redis，直读文件/目录） |
| `REDIS_PASSWORD` | string | — | Redis 密码 |
| `REDIS_DB` | int | `0` | Redis 数据库编号（0~15） |

> 启用 Redis 后运行 sync-redis 将 TXT 内容同步到 Redis Set：
> 源码环境 `go run ./cmd/sync-redis/`，二进制包 `./sync-redis`，
> Docker `docker compose exec img-api /app/sync-redis`。
> Redis 仅用于加速 TXT 图库的**集合级随机选取**（`SRandMember` O(1)），
> 不缓存选中的结果——每次请求依然随机返回。
> ⚠️ 同步是一次性操作：之后修改 TXT 文件不会自动更新 Redis，需重新运行同步命令。

---

### 外部 API 池

编辑 `config/image.yaml` 配置。每项支持以下字段：

> 💡 键名**大小写不敏感**（`external_apis` 与 `EXTERNAL_APIS` 等价），推荐与下表一致地全小写。

| 字段 | 类型 | 必填 | 说明 |
|------|------|:--:|------|
| `name` | string | 是 | API 标识（`?api=xxx` 匹配此值） |
| `url` | string | 是 | 请求模板，支持 `{width}` `{height}` `{category}` 占位符 |
| `response_type` | string | — | `redirect`（默认）/ `json`：描述**上游 API 怎么返回图片**（302 重定向 / JSON 里嵌 URL）。注意与请求参数 `mode`（最终怎么把图给访客）无关——`mode=image`（服务端代理直出）在两种 `response_type` 下都可用 |
| `url_field` | string | — | JSON 模式时提取 URL 的字段路径（如 `urls.raw`） |
| `categories` | []string | — | 支持的分类列表，空或显式写 `["all"]` 都表示匹配所有分类（`config/image.yaml` 的示例用的就是 `["all"]`） |
| `default_category` | []string | — | 默认分类（多值随机选一）；空=不传分类（`{category}` 占位符置空、不加 `category_param`，避免上游收到字面 `default`） |
| `category_param` | string | — | 分类对应的 query 参数名（如 `query`） |
| `headers` | map | — | 自定义请求头（如 `Authorization`）。**头名按你写的原样使用**（不会被转成小写，兼容大小写敏感的鉴权网关）；未显式配置 `User-Agent` 时按设备类型自动注入 |

> URL 占位符：`{width}` `{height}` 自动替换为设备对应尺寸，`{category}` 替换为实际分类。不需要的占位符直接不写即可。

> 📱 设备自适应：上游请求会**按设备类型自动携带对应的 User-Agent**（手机 UA / 桌面 UA），
> 因此按 UA 自适应横竖屏的 API 无需占位符也能返回对应版本图片；
> 若在 `headers` 中显式配置了 `User-Agent`，则以配置值为准。

> 📌 请求的分类不受支持时（指定 `api` 时检查该项的 `categories` 白名单，
> 随机模式检查池内任一 API 是否支持）返回 404"分类不存在"提示页，
> 且不计入熔断器失败次数。

> ⚠️ 如果 `external_apis` 列表为空（未配置任何端点），
> 访问 `source=external` 会返回"开始使用"引导页：浏览器直接访问为 HTTP 503 的 HTML 页，
> `mode=json` 时返回 503 JSON，博客 `<img>` 嵌入时返回 **HTTP 200** 的 SVG 提示图
> （`<img>` 对非 2xx 响应不渲染内容、只会显示裂图，故 SVG 分支刻意用 200），
> 指导管理员在 `config/image.yaml` 中添加端点。

---

### 熔断器

仅对 `source=external` 生效。

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `CIRCUIT_FAILURE_THRESHOLD` | int | `5` | 连续失败多少次后断路 |
| `CIRCUIT_TIMEOUT_SECONDS` | int | `30` | 断路后多少秒尝试半开探测 |
| `CIRCUIT_HALF_OPEN_MAX` | int | `3` | 半开状态最多放行多少个探测请求 |

状态机：
```
CLOSED（正常）──连续失败 N 次（默认 5）──→ OPEN（默认断路 30 秒）
OPEN ──超时后──→ HALF_OPEN（默认放行 3 个请求探测）
HALF_OPEN ──任一成功──→ CLOSED
HALF_OPEN ──任一失败──→ OPEN
```

> 📌 客户端取消/超时的请求不计入失败次数；熔断器累计进入 OPEN 的次数可在
> `/health` 的 `checks.circuit_trips` 查看。

---

### 日志

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `LOG_LEVEL` | string | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_DIR` | string | `storage/logs` | 日志目录：相对路径按项目根解析，**绝对路径（如 `/var/log/img-api`）直接使用** |
| `LOG_MAX_AGE` | int | `30` | 保留天数（0=永久） |
| `LOG_MAX_SIZE` | int | `0` | 单文件最大 MB（0=不限制）。运行期每 5 分钟检查一次，超限自动切分为 `.1/.2 ...`；跨天自动写入新日期文件 |

---

### 版本

> 💡 版本号由代码内置（随发布自动更新，`cmd/setversion` 与 CHANGELOG 同步），
> 显示于首页与 `/health`；如需临时覆盖可设置环境变量 `APP_VERSION`。

---

## 二、生效与排障（"我改了为什么没生效"）

### 变更生效时机

| 变更 | 生效方式 |
|------|---------|
| TXT 增删 URL（未启用 Redis） | 自动感知，即时生效（mtime 快照校验） |
| TXT 增删 URL（已同步 Redis） | 需重新运行 sync-redis（源码 `go run ./cmd/sync-redis/`，二进制 `./sync-redis`） |
| local 新增分类（新目录） | 即时 |
| local 已有分类中增删图片 | 重建索引后让服务加载新索引：重启服务，**或发 `SIGHUP`（免重启，推荐）**，或等 `LOCAL_INDEX_REFRESH` 计划刷新。重建命令：二进制 `./build-index` / 源码 `go run ./cmd/build-index/` / Docker `docker compose exec img-api /app/build-index`；也可删除 `storage/index/local.json` 后重启自动重建。**仅重启服务不会重扫目录**；重建前删除的图片可能随机返回 404（索引仍指向已不存在的文件），新增的图片不会被选中 |
| 分类清单（提示页可用列表 / 多分类筛选） | 30 秒快照缓存：什么都不做时新建分类最迟 30 秒纳入清单；**重建索引或发 `SIGHUP` 会立即让快照失效**，无需等 30 秒。单分类直接取图不受影响、即时生效 |
| `config/image.yaml`（外部 API） | 重启服务 |
| `.env` 配置 | 重启服务 |

---

## 三、网关部署示例

### Nginx 反代场景

如果前方有 Nginx/OpenResty，限流与跨域建议只开一侧，推荐交给网关处理：

```nginx
# .env —— 方案 A：限流/跨域交给网关（推荐）
CORS_ENABLED=false              # 跨域交给网关（<img> 嵌图本来就不需要跨域）
RATE_LIMIT_ENABLED=false        # 限流交给网关的 limit_req
TRUSTED_PROXIES=172.17.0.0/16   # 可选但推荐：应用自带日志按真实访客 IP 记录（网关日志不受影响）

# .env —— 方案 B：保留 Go 内置限流
CORS_ENABLED=false              # 跨域交给网关（或保留 true，二选一，勿两边同开）
RATE_LIMIT_ENABLED=true         # Go 限流
TRUSTED_PROXIES=172.17.0.0/16      # 必填：填写网关所在网段，限流才按真实客户端 IP 统计

# nginx.conf（两种方案均建议）
# ⚠️ 建议**覆盖**而非透传客户端 XFF：应用从右向左跳过 TRUSTED_PROXIES
#    网段内的地址、取第一个不可信地址；$proxy_add_x_forwarded_for 会把
#    客户端伪造值留在左侧，虽不再能绕过限流，但仍会污染日志与限流桶
proxy_set_header X-Forwarded-For $remote_addr;
proxy_set_header Host $host;
```

直连容器（ip+端口）可通过调整配置文件使用项目自带的配置。
