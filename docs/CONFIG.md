# 配置参考

所有配置通过项目根目录的 `.env` 文件管理（外部 API 池配置在 `config/image.yaml`，
见下方章节）。首次使用时复制 `.env.example` 为 `.env` 并修改。

> ⚠️ 修改 `.env` 后需**重启服务**生效（Docker 部署执行 `docker compose restart`，
> 需在 `docker-compose.yml` 所在目录执行，否则报 `no configuration file provided`）。

> 🔑 **.env 特殊字符陷阱**：解析器会对值做 `$` 变量展开，并把 `#` 当作注释。
> 值含 `$` 或 `#` 时（常见于 Redis 密码等），请用**单引号**包裹：
> `REDIS_PASSWORD='pa$sw0rd'`。双引号不能阻止 `$` 展开，不写引号则会直接出错。

---

## 应用设置

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `APP_DEBUG` | bool | `false` | 调试模式（仅开发排查用）：`true` 显示详细错误 + 调用位置 + "分类不存在"页附可用分类列表，会暴露图库分类清单，公网部署保持 `false` |
| `APP_NAME` | string | `img-api` | 应用名称（日志标识） |
| `APP_HOST` | string | `0.0.0.0` | 监听地址（`0.0.0.0` 监听所有网卡） |
| `APP_PORT` | int | `8080` | 应用监听端口（Docker 部署时指容器内端口，改端口需同步 compose 端口映射右侧） |

---

## 安全设置

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `RATE_LIMIT_ENABLED` | bool | `true` | 是否开启内置 IP 限流（纯内存实现：重启清零、多副本各自计数）。前方有 Nginx/OpenResty 时建议关闭并改用网关的 `limit_req`，否则需配合 `TRUSTED_PROXIES` 才能按真实访客统计 |
| `RATE_LIMIT_MAX` | int | `60` | 每分钟每 IP 最大请求数（`RATE_LIMIT_ENABLED=true` 时生效） |
| `REFERER_WHITELIST` | string | — | 防盗链域名白名单，逗号分隔（如 `mysite.com,blog.mysite.com`）。空 Referer 默认放行；留空 = 不限制。经 Nginx 转发后依然生效 |
| `TRUSTED_PROXIES` | string | — | 可信反代网段（逗号分隔 CIDR，如 `172.17.0.0/16`）。仅当请求来自这些网段时，限流器与应用自带访问日志（`storage/logs`）才信任 `X-Forwarded-For`/`X-Real-IP`；留空 = 一律按 TCP 对端 IP（防伪造）。限流交给网关后仍建议填写，应用自带日志才能记录真实访客 IP（网关自身的日志不受影响，本就能记录真实 IP） |
| `CORS_ENABLED` | bool | `true` | 是否添加跨域响应头。`<img>` 标签嵌图不需要跨域；只有其他网站的 JS 调用本 API 才需要。Nginx 已处理时可关闭（仅一侧开启，勿两边同开） |

---

## 图片来源

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
> 已有分类中增删图片需重建索引并重启加载（`build-index` 后重启、删除索引文件后重启，
> 或配置 `LOCAL_INDEX_REFRESH` 计划刷新）——仅重启服务不会重新扫描目录，
> 重建前删除的图片可能随机返回 502，新增的图片不会被选中。
>
> 🔧 **手动重建 local.json（步骤）**：
>
> 1. Docker 部署先确认服务名：运行 `docker compose ps --services` 查看实际服务名，
>    把下面步骤中的 `img-api` 替换成它（默认服务名就是 `img-api`）；
>    二进制包 / 源码部署跳过此步。
> 2. 重建索引（按部署方式三选一）：
>    - 二进制包：`./build-index`
>    - 源码：`go run ./cmd/build-index/`
>    - Docker：`docker compose exec img-api /app/build-index`
> 3. 让服务加载新索引（二选一）：
>    - 重启服务（Docker：`docker compose restart`）
>    - 等 `LOCAL_INDEX_REFRESH` 计划刷新（需已配置）
>
> 也可以直接删除 `storage/index/local.json` 后重启，启动时会自动重建。
>
> 💡 `img-api` 是 compose **服务名**（不是容器名）。`docker compose ps --services`
> 可直接列出实际服务名；本文档其他 `docker compose exec img-api ...` 命令同理替换。

---

## Redis 缓存

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

## 外部 API 池

编辑 `config/image.yaml` 配置。每项支持以下字段：

> 💡 键名**大小写不敏感**（`external_apis` 与 `EXTERNAL_APIS` 等价），推荐与下表一致地全小写。

| 字段 | 类型 | 必填 | 说明 |
|------|------|:--:|------|
| `name` | string | 是 | API 标识（`?api=xxx` 匹配此值） |
| `url` | string | 是 | 请求模板，支持 `{width}` `{height}` `{category}` 占位符 |
| `response_type` | string | — | `redirect`（默认）/ `json`：描述**上游 API 怎么返回图片**（302 重定向 / JSON 里嵌 URL）。注意与请求参数 `mode`（最终怎么把图给访客）无关——`mode=image`（服务端代理直出）在两种 `response_type` 下都可用 |
| `url_field` | string | — | JSON 模式时提取 URL 的字段路径（如 `urls.raw`） |
| `categories` | []string | — | 支持的分类列表，空=匹配所有 |
| `default_category` | []string | — | 默认分类（多值随机选一）；空=不传分类（`{category}` 占位符置空、不加 `category_param`，避免上游收到字面 `default`） |
| `category_param` | string | — | 分类对应的 query 参数名（如 `query`） |
| `headers` | map | — | 自定义请求头（如 `Authorization`） |

> URL 占位符：`{width}` `{height}` 自动替换为设备对应尺寸，`{category}` 替换为实际分类。不需要的占位符直接不写即可。

> 📱 设备自适应：上游请求会**按设备类型自动携带对应的 User-Agent**（手机 UA / 桌面 UA），
> 因此按 UA 自适应横竖屏的 API 无需占位符也能返回对应版本图片；
> 若在 `headers` 中显式配置了 `User-Agent`，则以配置值为准。

> 📌 请求的分类不受支持时（指定 `api` 时检查该项的 `categories` 白名单，
> 随机模式检查池内任一 API 是否支持）返回 404"分类不存在"提示页，
> 且不计入熔断器失败次数。

> ⚠️ 如果 `external_apis` 列表为空（未配置任何端点），
> 访问 `source=external` 会返回 HTTP 503 的"开始使用"引导页（`mode=json` 时返回 JSON，
> 博客 `<img>` 嵌入时返回 SVG 提示图），指导管理员在 `config/image.yaml` 中添加端点。

---

## 熔断器

仅对 `source=external` 生效。

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `CIRCUIT_FAILURE_THRESHOLD` | int | `5` | 连续失败多少次后断路 |
| `CIRCUIT_TIMEOUT_SECONDS` | int | `30` | 断路后多少秒尝试半开探测 |
| `CIRCUIT_HALF_OPEN_MAX` | int | `3` | 半开状态最多放行多少个探测请求 |

状态机：
```
CLOSED（正常）──连续失败 5 次──→ OPEN（断路 30 秒）
OPEN ──超时后──→ HALF_OPEN（放行 3 个请求探测）
HALF_OPEN ──任一成功──→ CLOSED
HALF_OPEN ──任一失败──→ OPEN
```

---

## 日志

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `LOG_LEVEL` | string | `info` | `debug` / `info` / `warn` / `error` |
| `LOG_DIR` | string | `storage/logs` | 日志目录（相对于项目根路径） |
| `LOG_MAX_AGE` | int | `30` | 保留天数（0=永久） |
| `LOG_MAX_SIZE` | int | `0` | 单文件最大 MB（0=不限制）。运行期每 5 分钟检查一次，超限自动切分为 `.1/.2 ...`；跨天自动写入新日期文件 |

---

## 版本

> 💡 版本号由代码内置（随发布自动更新，`cmd/setversion` 与 CHANGELOG 同步），
> 显示于首页与 `/health`；如需临时覆盖可设置环境变量 `APP_VERSION`。

---

## 变更生效时机

| 变更 | 生效方式 |
|------|---------|
| TXT 增删 URL（未启用 Redis） | 自动感知，即时生效（mtime 快照校验） |
| TXT 增删 URL（已同步 Redis） | 需重新运行 sync-redis（源码 `go run ./cmd/sync-redis/`，二进制 `./sync-redis`） |
| local 新增分类（新目录） | 即时 |
| local 已有分类中增删图片 | 重建索引后重启加载：二进制 `./build-index` / 源码 `go run ./cmd/build-index/` / Docker `docker compose exec img-api /app/build-index`，再重启服务；或删除 `storage/index/local.json` 后重启自动重建；或配置 `LOCAL_INDEX_REFRESH` 计划刷新。仅重启服务不会重扫目录；重建前删除的图片可能随机返回 502，新增的图片不会被选中 |
| 分类清单（提示页可用列表 / 多分类筛选） | 30 秒快照缓存：新建分类最迟 30 秒纳入清单；单分类直接取图不受影响、即时生效 |
| `config/image.yaml`（外部 API） | 重启服务 |
| `.env` 配置 | 重启服务 |

---

## Nginx 反代场景

如果前方有 Nginx/OpenResty，限流与跨域建议只开一侧，推荐交给网关处理：

```nginx
# .env —— 方案 A：限流/跨域交给网关（推荐）
CORS_ENABLED=false              # 跨域交给网关（<img> 嵌图本来就不需要跨域）
RATE_LIMIT_ENABLED=false        # 限流交给网关的 limit_req
TRUSTED_PROXIES=172.17.0.0/16（示例，请按照实际填写）   # 可选但推荐：应用自带日志按真实访客 IP 记录（网关日志不受影响）

# .env —— 方案 B：保留 Go 内置限流
CORS_ENABLED=false              # 跨域交给网关（或保留 true，二选一，勿两边同开）
RATE_LIMIT_ENABLED=true         # Go 限流
TRUSTED_PROXIES=172.17.0.0/16（示例，请按照实际填写）      # 必填：填写网关所在网段，限流才按真实客户端 IP 统计

# nginx.conf（两种方案均建议）
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header Host $host;
```

直连容器（ip+端口）可通过调整配置文件使用项目自带的配置。
