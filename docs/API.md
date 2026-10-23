# API 参考文档

| 章节 | 内容 |
|---|---|
| [三步上手](#三步上手从-0-到第一张图) | 最简用法、可用参数、返回值 |
| [获取随机图片](#获取随机图片) | 全部 query 参数与各模式差异 |
| [健康检查](#健康检查) | `/health` 字段与 `ok` / `degraded` 判定 |
| [路由一览](#路由一览) | 全部路径与方法 |
| [常见状态码速查](#常见状态码速查) | 状态码 → 原因 → 处理 |

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

# 本地图片（redirect 自动降级为 image，也可显式传 mode=image）
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
| `category` | string | — | `default` | 图片分类，逗号分隔多选（如 `anime,scenery`），最长 128 字节。默认分类内置为 `default`（大小写不敏感），txt/local 渠道可通过 `TXT_DEFAULT_CATEGORY` / `LOCAL_DEFAULT_CATEGORY` 配置修改 |
| `api` | string | — | 随机 | `source=external` 时指定使用哪个外部 API（不传则随机选一个），最长 128 字节 |

### 参数详解

#### type — 设备类型

| 值 | 说明 |
|------|------|
| `auto` | 根据 User-Agent 自动判断设备（默认） |
| `pc` | 强制返回 PC 端（横屏）图片 |
| `pe` | 强制返回手机端（竖屏）图片 |

> 📌 平板说明：iPad 的 User-Agent 含 "iPad" 关键词，会被识别为 `pe`（竖屏）；
> 需要横屏图时显式传 `type=pc`。

#### source — 图片来源

| 值 | 说明 | 配置方式 |
|------|------|---------|
| `txt` | TXT 图库（推荐） | 新建 `resources/txt/{pc\|pe}/{分类名}.txt`（文件名中的**英文字母请使用小写**，如 `anime.txt`；中文分类名如 `风景.txt` 直接使用即可）即时生效；向已有分类增删 URL 即时生效 |
| `local` | 本地图片文件 | 新建 `resources/local/{pc\|pe}/{分类名}/` 文件夹并放入图片，即时生效；已有分类中增删图片需重建索引并让服务加载（重启或发 `SIGHUP`，详见 [CONFIG.md](CONFIG.md)） |
| `external` | 外部 API 池 | 编辑 `config/image.yaml` 配置多个外部 API，再用 `api` 参数指定用哪个 |

> ⚠️ `source=external` 只是选择“外部 API 池”这个图源。池里具体用哪个 API 由 `api` 参数决定，不传则随机。

#### mode — 返回模式

| 值 | 行为 | 适用场景 |
|------|------|---------|
| `redirect` | HTTP 302 重定向到图片 URL（默认） | `<img>` 标签嵌入 |
| `json` | 返回 JSON 对象，包含 URL 和元信息 | 前端 AJAX 调用 |
| `image` | 服务端代理下载图片后直接输出二进制 | 需要隐藏真实图片来源 |

> ⚠️ `source=local` 不支持 302 跳转（图源返回的是文件系统路径）：请求 `mode=redirect`
>（含默认值）时**自动降级为 `mode=image`**——服务端直接输出图片，无需显式传参；
> 需要元数据时使用 `mode=json`。

> 📌 `source=local&mode=json` 返回的 `url` 是**相对项目根目录的路径**
> （如 `resources/local/pc/default/photo.jpg`，原为服务器绝对路径），不暴露
> 服务器绝对路径，仅供调试参考，不是可公开访问的 URL。

#### api — 指定使用哪个外部 API

前提：`source=external`。两个参数的关系：

- `source=external` → 选择图源为“外部 API 池”
- `api=flickr` → 从池中指定使用名为 `flickr` 的 API（不传则随机选一个）

| 完整请求 | 行为 |
|------|------|
| `?source=external` | 从池中**随机**选一个 API |
| `?source=external&api=flickr` | **指定**使用 `flickr`（大小写不敏感） |
| `?source=external&api=flickr&category=cat` | 指定 API + 指定分类 |

> API 名称对应 `config/image.yaml` 中 `name` 字段。未配置分类时自动使用 `default_category`（多个随机选）；都没有则不传分类（`{category}` 占位符置空、不加 `category_param`）。

> 📌 指定的 API 名称在池中不存在时返回 404 的"API 不存在"提示页（`mode=json` 为 JSON，
> Debug 模式下附 `available` 可用列表），而不是 500。

#### category — 分类

- 单分类：`?category=anime`
- 多分类随机：`?category=anime,scenery,beauty`
- 留空或传 `default`（大小写不敏感）：使用默认分类（内置 `default`；txt/local 可通过
  `TXT_DEFAULT_CATEGORY` / `LOCAL_DEFAULT_CATEGORY` 修改，external 使用各 API 的 `default_category`）
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
| `X-Request-ID` | 本次请求的唯一追踪 ID（客户端传入的合法值会被复用，非法值自动重新生成），日志中同名字段与之对应 |
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
  "message": "invalid category: must not contain path separators or '..'"
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
  "available": ["default", "scenery"]   // 仅 APP_DEBUG=true 时返回
}
```

```json
{
  "code": 404,
  "message": "external api not found: flickr",
  "available": ["picsum", "unsplash"]   // 仅 APP_DEBUG=true 时返回
}
```

> 📌 `available` 字段仅在 `APP_DEBUG=true` 时返回，生产环境不暴露分类清单（上面的两个示例为 Debug 模式下的响应）。

> 💡 **友好提示页**：请求的图源未配置任何内容时，返回 HTTP 503 的"开始使用"引导页（HTML），
> 介绍三种添加图片的方式。图源有内容但请求的分类不存在时，返回 HTTP 404 的"分类不存在"
> 提示页（可用分类列表仅在 Debug 模式下展示，生产环境不暴露）。`mode=json` 时返回 JSON；
> 博客 `<img>` 标签嵌入场景（`Accept` 以 `image/` 开头或 `mode=image`）返回内嵌文字的 SVG 占位图，
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
  "version": "x.y.z",
  "uptime": "2h 5m",
  "checks": {
    "txt": "healthy",
    "local": "healthy",
    "external_pool": "healthy (1 APIs)",
    "circuit_breaker": "CLOSED",
    "circuit_trips": "0",
    "cache": "redis"
  },
  "stats": {
    "total_requests": 1234,
    "total_success": 1200,
    "total_fail": 34,
    "circuit_trips": 0,
    "uptime_seconds": 7530,
    "today_requests": 567,
    "today_success": 550,
    "today_fail": 17,
    "today_trips": 0
  }
}
```

### `status` 字段

> 这里说的是响应体里的 `status` 字段；HTTP 状态码统一见 [常见状态码速查](#常见状态码速查)。

> 🕒 **自 v1.6.0 起**：只判定**已配置内容**的图源（此前要求 txt/local/external 三源齐全，
> external 仓库从不注册，导致 `status` 恒为 `degraded`、Docker 容器恒 unhealthy）。

| status | 含义 |
|--------|------|
| `ok` | **已配置内容**的图源都能取图 |
| `degraded` | 某个已配置的图源不可用 |

> 🔍 判定范围：只判定"已配置内容"的图源（有 TXT 条目 / 有本地图片 / `image.yaml` 配了 API）。
> - txt / local 的检查覆盖「根目录 + `pc`/`pe` 设备子目录」，子目录缺失会报 `unhealthy`
> - 没有任何内容的图源（例如只跑 txt 时的 local、未配 `image.yaml` 时的 external）**不参与判定**，
>   因此 txt-only 的实例同样是 `ok`
> - `external` 不进入 `status`：它走 `ExternalPool` 而非仓库，可用性看 `checks.external_pool`
>   与 `checks.circuit_breaker`
> - Docker 探针：compose 使用 `health-check -strict`，`degraded` 会让容器显示 unhealthy

> `external_pool` / `circuit_breaker` / `circuit_trips` / `cache` 为信息性状态，不参与健康判定。
> `circuit_trips`（checks 内，字符串）为熔断器累计进入 OPEN 的次数，与 `stats.circuit_trips`（请求被熔断拦截的次数）含义不同。
> `version` 为代码内置版本号（随发布自动更新，与 CHANGELOG 一致）；
> 可用环境变量 `APP_VERSION` 临时覆盖。
> 统计字段两组：`total_*` 自服务启动累计（重启归零）；`today_*` 当日累计，按本地时间每日 0 点自动归零。
>
> 📌 **`fail` 的含义是"没有成功返回图片"，与 HTTP 状态码不是一一对应**：
> 引导页/提示页返回 **200**（SVG 占位图必须用 200，否则 `<img>` 只会显示裂图），
> 但因为没有真正给出图片而计入 `fail`；参数校验失败、限流拦截等也只记 `fail`。
> 因此 `total_requests - total_success - total_fail` **不恒为 0**，这是有意的口径差异；
> 计算成功率请用 `total_success / total_requests`。

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
| `400` | 参数写错 | 参数值不在允许范围（非法字符、超长等） | 对照 [请求参数](#请求参数) 表检查拼写 |
| `403` | 被拒绝 | Referer 不在防盗链白名单 | 联系站长确认白名单 |
| `404` | 资源不存在 | 分类名与 txt 文件名不一致、多分类全都不存在、external 池无此 API、`source=local` 的文件已被删除、访问了不存在的路径（返回 JSON 而非纯文本） | 检查拼写；Debug 模式下响应会附可用列表 |
| `405` | 方法不允许 | 用了非 GET 方法（如 POST） | 本 API 仅支持 GET / OPTIONS |
| `413` | 图片太大 | `source=local` 且文件超过 50MB | 换更小的图，或压缩后重试 |
| `415` | 类型不支持 | `source=local` 且扩展名不在 jpg/png/gif/webp/bmp/svg 之列 | 换成受支持的图片格式 |
| `429` | 请求太频繁 | 触发限流（响应附 `Retry-After: 60`，客户端可据此自动退避） | 放慢频率，或联系站长调整限流配置 |
| `500` | 服务端处理失败 | 图源临时故障、本地文件读不出等 | 查看服务日志排查 |
| `502` | 外部图源取图失败 | `mode=image` 时代理上游返回非 2xx / 非图片内容 / 超过 50MB | 多为上游图床问题；换一张图或稍后重试 |
| `503` | 图源还没有内容 | 请求的 source 还没配置任何图片 | 页面本身是"开始使用"引导，按提示添加图片 |

> 🕒 **自 v1.6.0 起**：
> - `source=local` 的错误码由统一的 `502` 拆分为 `404`（文件不存在）/ `415`（类型不支持）/ `413`（超过 50MB）；
> - `429` 额外携带 `Access-Control-*` 与安全响应头，浏览器 `fetch` 可直接识别限流并读 `Retry-After`；
> - 未知路径返回 JSON `404`（此前是纯文本 `404 page not found`）；
> - `mode=image` 代理不再把上游非 2xx 当成功（此前只校验 `Content-Type`）。
