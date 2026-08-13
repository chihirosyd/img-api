# 配置参考

所有配置通过项目根目录的 `.env` 文件管理。首次使用时复制 `.env.example` 为 `.env` 并修改。

> ⚠️ 修改 `.env` 后需**重启服务**生效（Docker 部署执行 `docker compose restart`）。

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
| `APP_PORT` | int | `8080` | 监听端口 |

---

## 安全设置

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `AUTH_ENABLED` | bool | `false` | 是否开启 Token 鉴权 |
| `AUTH_TOKEN` | string | — | 鉴权密钥，客户端需携带相同 token |
| `RATE_LIMIT_ENABLED` | bool | `true` | 是否开启 IP 限流 |
| `RATE_LIMIT_MAX` | int | `60` | 每分钟每 IP 最大请求数 |
| `REFERER_WHITELIST` | string | — | 防盗链域名白名单，逗号分隔（如 `mysite.com,blog.mysite.com`）。空 Referer 默认放行 |
| `TRUSTED_PROXIES` | string | — | 可信反代网段（逗号分隔 CIDR，如 `172.17.0.0/16`）。仅当请求来自这些网段时限流器才信任 `X-Forwarded-For`/`X-Real-IP`；留空 = 直接按对端 IP 限流（防伪造） |
| `CORS_ENABLED` | bool | `true` | 是否添加 CORS 头（Nginx 反代时可关闭） |

---

## 图片来源

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `DEFAULT_SOURCE` | string | `txt` | 默认图片来源：`txt` / `local` / `external` |
| `LOCAL_INDEX_REFRESH_MINUTES` | int | `0` | 本地图片索引（`storage/index/local.json`）自动刷新间隔（分钟）。`0`=仅在首次启动时生成一次 |

> 📌 本地图片索引说明：首次启动时若 `storage/index/local.json` 不存在会自动生成。
> 之后按 `LOCAL_INDEX_REFRESH_MINUTES` 定时重新扫描刷新。
> 新增分类（新目录）即时可用；已有分类中增删图片需重建索引后生效
> （重启 / `build-index` / 定时刷新），重建前删除图片可能随机返回 502。

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

编辑 `configs/image.yaml` 配置。每项支持以下字段：

| 字段 | 类型 | 必填 | 说明 |
|------|------|:--:|------|
| `name` | string | 是 | API 标识（`?api=xxx` 匹配此值） |
| `url` | string | 是 | 请求模板，支持 `{width}` `{height}` `{category}` 占位符 |
| `response_type` | string | — | `redirect`（默认）/ `json` |
| `url_field` | string | — | JSON 模式时提取 URL 的字段路径（如 `urls.raw`） |
| `categories` | []string | — | 支持的分类列表，空=匹配所有 |
| `default_category` | []string | — | 默认分类（多值随机选一）；空=无分类 |
| `category_param` | string | — | 分类对应的 query 参数名（如 `query`） |
| `headers` | map | — | 自定义请求头（如 `Authorization`） |

> URL 占位符：`{width}` `{height}` 自动替换为设备对应尺寸，`{category}` 替换为实际分类。不需要的占位符直接不写即可。

> ⚠️ 如果 `external_apis` 列表为空（未配置任何端点），
> 访问 `source=external` 会返回 HTTP 503 的"开始使用"引导页（`mode=json` 时返回 JSON，
> 博客 `<img>` 嵌入时返回 SVG 提示图），指导管理员在 `configs/image.yaml` 中添加端点。

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

## 健康检查

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `HEALTH_SECRET` | string | — | 健康检查密钥。非空时 `/health` 仅返回极简状态，`/health-{secret}` 或 `X-Health-Secret` 请求头返回完整状态 |

> ⚠️ 建议不要把 `HEALTH_SECRET` 与 `AUTH_TOKEN` 设为相同值：`health-check` CLI 会把密钥
> 拼进 URL（`/health-{secret}`），可能被反向代理/访问日志记录，两者相同时等同于泄露鉴权 Token。

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

| 变量 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| `APP_VERSION` | string | `1.0.0` | 语义版本号，影响 `/health` 返回 |

---

## 变更生效时机

| 变更 | 生效方式 |
|------|---------|
| TXT 增删 URL（未启用 Redis） | 自动感知，即时生效（mtime 快照校验） |
| TXT 增删 URL（已同步 Redis） | 需重新运行 sync-redis（源码 `go run ./cmd/sync-redis/`，二进制 `./sync-redis`） |
| local 新增分类（新目录） | 即时 |
| local 已有分类中增删图片 | 重启服务 / `./build-index`（源码 `go run ./cmd/build-index/`）/ 定时刷新后生效；重建前删除图片可能随机返回 502 |
| `configs/image.yaml`（外部 API） | 重启服务 |
| `.env` 配置 | 重启服务 |

---

## Nginx 反代场景

如果前方有 Nginx，建议配置：

```nginx
# .env
CORS_ENABLED=false              # Nginx 处理 CORS
RATE_LIMIT_ENABLED=true         # Go 仍可限流
TRUSTED_PROXIES=10.0.0.0/8      # 填写 Nginx 所在网段，限流才按真实客户端 IP 统计

# nginx.conf
proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
proxy_set_header Host $host;
```

直连容器（ip+端口）可通过调整配置文件使用项目自带的配置。
