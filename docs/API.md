# API 参考文档

## 三步上手：从 0 到第一张图

> 第一次使用这个服务？先看这一节。熟悉之后可直接跳到 [请求参数](#请求参数)。

### 第 1 步：确认服务地址

部署完成后，在浏览器打开下面的地址，能看到随机图片就说明服务通了：

```
http://<服务器IP>:8080/random      # 示例：http://1.2.3.4:8080/random
```

绑定域名或反代后换成你的域名，如 `https://img.example.com/random`。
下文把这一串统称为 `{服务地址}`——使用示例时请把它替换成你自己的地址（花括号一并替换）。

### 第 2 步：URL 拼接规则

一个完整请求的结构：

```
{服务地址}/random?参数1=值1&参数2=值2
```

| 规则 | 说明 |
|------|------|
| `?` | 只写一次，跟在 `/random` 后面 |
| `名字=值` | 一个参数一个等式，如 `category=anime` |
| `&` | 连接多个参数 |
| 不写 = 用默认值 | 所有参数都是可选的 |
| 顺序 | 随意，不影响结果 |

对照示例拆解：

```
https://img.example.com/random?source=txt&category=anime&type=pe
└────────── 服务地址 ──────────┘└─接口─┘└────────── 参数部分 ──────────┘
```

> 💡 中文、空格等特殊字符需要 URL 编码；在浏览器地址栏直接输入时会自动处理。

### 第 3 步：复制即用

把 `{服务地址}` 替换成你的地址即可直接使用：

| 需求 | 写法 |
|------|------|
| 访问服务首页（教程页） | `{服务地址}/` |
| 拿一张随机图 | `{服务地址}/random` |
| 嵌入博客/网页 | `<img src="{服务地址}/random">` |
| 指定分类 | `{服务地址}/random?category=风景` |
| 多分类随机选一 | `{服务地址}/random?category=风景,动漫` |
| 只要电脑横屏图 | `{服务地址}/random?type=pc` |
| 只要手机竖屏图 | `{服务地址}/random?type=pe` |
| 要 JSON 数据（前端调用） | `{服务地址}/random?mode=json` |
| 要图片二进制（隐藏来源） | `{服务地址}/random?mode=image` |
| 用服务器上的本地图片 | `{服务地址}/random?source=local&mode=image` |
| 指定外部 API | `{服务地址}/random?source=external&api=picsum` |

> 💡 `source=external` 需要先在 `config/image.yaml` 配置 API 池，未配置时返回 503 引导页
> （按页面提示配置即可）；`api=picsum` 只是示例名，实际以你的配置为准。

> 💡 没看到图？页面会返回带文字的提示内容，对照 [常见状态码速查](#常见状态码速查) 就能找到原因。

---

## 获取随机图片

> 🎲 每次请求都会独立随机选取一张图片（服务端不缓存选中的结果），
> 同一分类的连续请求通常返回不同图片。

```
# TXT 图库（默认）
GET /random
或
GET /random?source=txt

# 本地图片（local 不支持默认的 redirect 模式，需带 mode=image）
GET /random?source=local&mode=image

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

### 参数详解

#### type — 设备类型

| 值 | 说明 |
|------|------|
| `auto` | 根据 User-Agent 自动判断设备（默认） |
| `pc` | 强制返回 PC 端（横屏）图片 |
| `pe` | 强制返回手机端（竖屏）图片 |

> 📌 平板说明：iPad（iOS 13+）的 User-Agent 是桌面模式、不含移动关键词，
> 会被识别为 `pc`；需要竖屏图时显式传 `type=pe`。

#### source — 图片来源

| 值 | 说明 | 配置方式 |
|------|------|---------|
| `txt` | TXT 图库（推荐） | 新建 `resources/txt/{pc\|pe}/{分类名}.txt` 即时生效；向已有分类增删 URL 即时生效 |
| `local` | 本地图片文件 | 新建 `resources/local/{pc\|pe}/{分类名}/` 文件夹并放入图片，即时生效；已有分类中增删图片需重建索引并重启加载（详见 [CONFIG.md](CONFIG.md)） |
| `external` | 外部 API 池 | 编辑 `config/image.yaml` 配置多个外部 API，再用 `api` 参数指定用哪个 |

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

> API 名称对应 `config/image.yaml` 中 `name` 字段。未配置分类时自动使用 `default_category`（多个随机选），都没有则回退 `"default"`。

> 📌 指定的 API 名称在池中不存在时返回 404 的"API 不存在"提示页（`mode=json` 为 JSON，
> Debug 模式下附 `available` 可用列表），而不是 500。

#### category — 分类

- 单分类：`?category=anime`
- 多分类随机：`?category=anime,scenery,beauty`
- 留空使用默认分类：`default`
- 支持中文：`?category=风景`（浏览器自动 URL 编码）

> 💡 多分类行为：从请求的分类中筛选出**实际存在**的再随机选取，
> 不会因某个分类不存在而报错；仅当全部分类都不存在时返回 404 提示页。
> 分类清单有 30 秒快照缓存：新建的分类最迟 30 秒后才会被纳入多分类筛选
> 与提示页可用列表；单分类直接取图不受影响，即时生效。

> 📌 分类只在另一设备目录存在时（如仅 `pc/` 配置了该分类），当前设备类型的请求
> 同样返回"分类不存在"提示页（404），而不是 500。

> 📌 外部 API 渠道（`source=external`）下，请求的分类不在任何 API 的 `categories`
> 白名单中时（指定 `api` 时检查该项的白名单），同样返回"分类不存在"提示页（404），
> 且不计入熔断器失败次数。

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

```
GET /health
```

返回完整内部状态：

```json
{
  "status": "ok",
  "version": "1.3.0",
  "uptime": "2h 5m 30s",
  "checks": {
    "txt": "healthy",
    "local": "healthy",
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

### 状态码

| status | 含义 |
|--------|------|
| `ok` | 图源仓库健康。`local` / `txt` 仓库在启动时即初始化，`/health` 直接检查；`external` 渠道经 `external_pool` 信息性展示 |
| `degraded` | 部分图源仓库不可用（服务仍可处理其他来源） |

> `external_pool` / `circuit_breaker` / `cache` 为信息性状态，不参与健康判定。
> `version` 取自 `APP_VERSION` 配置（未设置时用代码内置默认值，与 CHANGELOG 保持一致）。

---

## 路由一览

| 路由 | 说明 |
|------|------|
| `/random` | 获取随机图片 |
| `/` | 首页：浏览器直接访问返回教程页 + 运行状态仪表盘；`<img>` 嵌入或带 `mode` 参数时等价于 `/random` |
| `/health` | 健康检查（完整内部状态与运行时统计） |

---

## 常见状态码速查

> 打开接口但没看到预期结果时，先查这张表。

| 状态码 | 意味着 | 常见原因 | 处理办法 |
|--------|--------|----------|----------|
| `302` | ✅ 成功 | 默认模式 `redirect` 正在跳转到真实图片 | 无需处理，浏览器和 `<img>` 标签会自动跟随 |
| `200` | ✅ 成功 | `mode=json` 或 `mode=image` | 无需处理 |
| `400` | 参数写错 | 参数值不在允许范围，或 `source=local` 没配 `mode=image` | 对照 [请求参数](#请求参数) 表检查拼写 |
| `403` | 被拒绝 | Referer 不在防盗链白名单 | 联系站长确认白名单 |
| `404` | 分类或 API 不存在 | 分类名与 txt 文件名不一致、多分类全都不存在、external 池无此 API | 检查拼写；Debug 模式下响应会附可用列表 |
| `405` | 方法不允许 | 用了非 GET 方法（如 POST） | 本 API 仅支持 GET / OPTIONS |
| `429` | 请求太频繁 | 触发限流 | 放慢频率，或联系站长调整限流配置 |
| `500` | 服务端处理失败 | 图源临时故障等 | 查看服务日志排查 |
| `503` | 图源还没有内容 | 请求的 source 还没配置任何图片 | 页面本身是"开始使用"引导，按提示添加图片 |
