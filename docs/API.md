# API 参考文档

## 获取随机图片

> 🎲 每次请求都会独立随机选取一张图片（服务端不缓存选中的结果），
> 同一分类的连续请求通常返回不同图片。

```
# TXT 图库（默认）
GET /random
或
GET /random?source=txt

# 本地图片
GET /random?source=local

# 外部 API 池
GET /random?source=external
```

### 请求参数

| 参数 | 类型 | 必填 | 默认值 | 说明 |
|------|------|:--:|--------|------|
| `type` | string | — | `auto` | 设备类型：`auto`（自动检测）/ `pc` / `pe` |
| `source` | string | — | `txt` | 图片来源：`txt` / `local` / `external`（外部 API 池）。默认值可通过 `DEFAULT_SOURCE` 配置修改 |
| `mode` | string | — | `redirect` | 返回模式：`redirect` / `json` / `image` |
| `category` | string | — | `default` | 图片分类，逗号分隔多选（如 `anime,scenery`） |
| `api` | string | — | 随机 | `source=external` 时指定使用哪个外部 API（不传则随机选一个） |
| `token` | string | 视配置 | — | 鉴权 Token（`AUTH_ENABLED=true` 时必填） |

### 参数详解

#### type — 设备类型

| 值 | 说明 |
|------|------|
| `auto` | 根据 User-Agent 自动判断设备（默认） |
| `pc` | 强制返回 PC 端（横屏）图片 |
| `pe` | 强制返回手机端（竖屏）图片 |

#### source — 图片来源

| 值 | 说明 | 配置方式 |
|------|------|---------|
| `txt` | TXT 图库（推荐） | 新建 `resources/txt/{pc\|pe}/{分类名}.txt` 即时生效；向已有分类增删 URL 即时生效 |
| `local` | 本地图片文件 | 新建 `resources/local/{pc\|pe}/{分类名}/` 文件夹并放入图片，即时生效；已有分类中增删图片需重建索引 |
| `external` | 外部 API 池 | 编辑 `configs/image.yaml` 配置多个外部 API，再用 `api` 参数指定用哪个 |

> ⚠️ `source=external` 只是选择“外部 API 池”这个图源。池里具体用哪个 API 由 `api` 参数决定，不传则随机。

#### mode — 返回模式

| 值 | 行为 | 适用场景 |
|------|------|---------|
| `redirect` | HTTP 302 重定向到图片 URL（默认） | `<img>` 标签嵌入 |
| `json` | 返回 JSON 对象，包含 URL 和元信息 | 前端 AJAX 调用 |
| `image` | 服务端代理下载图片后直接输出二进制 | 需要隐藏真实图片来源 |

> ⚠️ `source=local` 不支持 `mode=redirect`（返回 400）：本地图源返回的是文件系统路径，
> 无法通过 302 让浏览器访问。请使用 `mode=image`（服务端直接输出）或 `mode=json`。

> 📌 `source=local&mode=json` 返回的 `url` 是服务器本地文件路径（如 `/app/resources/local/...`），
> 仅供调试参考，不是可公开访问的 URL。

#### api — 指定使用哪个外部 API

前提：`source=external`。两个参数的关系：

- `source=external` → 选择图源为“外部 API 池”
- `api=flickr` → 从池中指定使用名为 `flickr` 的 API（不传则随机选一个）

| 完整请求 | 行为 |
|------|------|
| `?source=external` | 从池中**随机**选一个 API |
| `?source=external&api=flickr` | **指定**使用 `flickr`（大小写不敏感） |
| `?source=external&api=flickr&category=cat` | 指定 API + 指定分类 |

> API 名称对应 `configs/image.yaml` 中 `name` 字段。未配置分类时自动使用 `default_category`（多个随机选），都没有则回退 `"default"`。

> 📌 指定的 API 名称在池中不存在时返回 404 的"API 不存在"提示页（`mode=json` 为 JSON，
> Debug 模式下附 `available` 可用列表），而不是 500。

#### category — 分类

- 单分类：`?category=anime`
- 多分类随机：`?category=anime,scenery,beauty`
- 留空使用默认分类：`default`
- 支持中文：`?category=风景`（浏览器自动 URL 编码）

> 💡 多分类行为：从请求的分类中筛选出**实际存在**的再随机选取，
> 不会因某个分类不存在而报错；仅当全部分类都不存在时返回 404 提示页。

> 📌 分类只在另一设备目录存在时（如仅 `pc/` 配置了该分类），当前设备类型的请求
> 同样返回"分类不存在"提示页（404），而不是 500。

### 通用响应头

所有响应均包含以下响应头：

| 响应头 | 说明 |
|--------|------|
| `X-Request-ID` | 本次请求的唯一追踪 ID（客户端传入的会被复用），日志中同名字段与之对应 |
| `X-Content-Type-Options` | `nosniff` |
| `X-Frame-Options` | `DENY` |
| `Referrer-Policy` | `strict-origin-when-cross-origin` |
| `Access-Control-Allow-Origin` | `*`（仅 `CORS_ENABLED=true` 时） |

### 响应示例

**mode=redirect（默认）**
```http
HTTP/1.1 302 Found
Location: https://images.unsplash.com/photo-1506905925346-21bda4d32df4?w=1920
```

**mode=json**
```json
{
  "code": 200,
  "message": "success",
  "data": {
    "url": "https://images.unsplash.com/photo-1506905925346-21bda4d32df4?w=1920",
    "category": "default",
    "width": 0,
    "height": 0
  }
}
```

**mode=image**
```
HTTP/1.1 200 OK
Content-Type: image/jpeg
Cache-Control: public, max-age=300

<binary image data>
```

### 错误响应

```json
{
  "code": 400,
  "message": "invalid source, must be: txt / local / external"
}
```

```json
{
  "code": 400,
  "message": "local source does not support mode=redirect, use mode=image or mode=json"
}
```

```json
{
  "code": 429,
  "message": "too many requests, please try again later"
}
```

```json
{
  "code": 401,
  "message": "invalid or missing token"
}
```

```json
{
  "code": 403,
  "message": "access denied: referer not allowed"
}
```

```json
{
  "code": 500,
  "message": "failed to get random image: ..."
}
```

```json
{
  "code": 404,
  "message": "category not found: anime",
  "available": ["default", "scenery"]
}
```

```json
{
  "code": 404,
  "message": "external api not found: flickr",
  "available": ["picsum", "unsplash"]
}
```

> 📌 `available` 字段仅在 `APP_DEBUG=true` 时返回，生产环境不暴露分类清单。

> 💡 **友好提示页**：请求的图源未配置任何内容时，返回 HTTP 503 的"开始使用"引导页（HTML），
> 介绍三种添加图片的方式。图源有内容但请求的分类不存在时，返回 HTTP 404 的"分类不存在"
> 提示页（可用分类列表仅在 Debug 模式下展示，生产环境不暴露）。`mode=json` 时返回 JSON；
> 博客 `<img>` 标签嵌入场景（`Accept` 含 `image/` 或 `mode=image`）返回内嵌文字的 SVG 占位图，
> 提示不会因裂图而消失。

---

## 健康检查

### 公开模式

```
GET /health
```

未配置 `HEALTH_SECRET` 时返回完整内部状态：

```json
{
  "status": "ok",
  "version": "1.0.0",
  "uptime": "2h 5m 30s",
  "checks": {
    "txt": "healthy",
    "external_pool": "healthy (1 APIs)",
    "circuit_breaker": "CLOSED",
    "cache": "redis"
  },
  "stats": {
    "total_requests": 1234,
    "total_success": 1200,
    "total_fail": 34,
    "circuit_trips": 0,
    "uptime_seconds": 7530
  }
}
```

配置 `HEALTH_SECRET=mykey` 后公开模式仅返回极简信息：

```json
{
  "status": "ok",
  "version": "1.0.0"
}
```

### 私有模式

```
GET /health-mykey
```

需要与 `HEALTH_SECRET` 匹配的密钥，返回完整内部状态（同上）。

> 💡 推荐用请求头传密钥，避免密钥出现在 URL/代理日志中：
> ```
> GET /health
> X-Health-Secret: mykey
> ```
> 注意：未配置 `HEALTH_SECRET` 时，`/health-{任意值}` 等同于公开模式（返回完整状态）。

密钥不匹配时返回：
```json
{
  "code": 403,
  "message": "invalid health secret"
}
```

### 状态码

| status | 含义 |
|--------|------|
| `ok` | 所有**已初始化**的图源仓库健康（只有被请求过的图源才会出现在检查结果中） |
| `degraded` | 部分图源仓库不可用（服务仍可处理其他来源） |

> `external_pool` / `circuit_breaker` / `cache` 为信息性状态，不参与健康判定。

---

## 鉴权

当 `AUTH_ENABLED=true` 时，以下方式传递 Token（优先级从高到低）：

1. **X-Token 头**：`X-Token: your-token`
2. **Bearer 头**：`Authorization: Bearer your-token`
3. **URL 参数**：`?token=your-token`（不推荐，会出现在访问日志中）

Token 不匹配时返回：
```json
{
  "code": 401,
  "message": "invalid or missing token"
}
```

---

## 路由一览

| 路由 | 说明 |
|------|------|
| `/random` | 获取随机图片 |
| `/` | 同 `/random` |
| `/health` | 健康检查（公开模式） |
| `/health-{secret}` | 健康检查（私有模式，需匹配 HEALTH_SECRET） |
