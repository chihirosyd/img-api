# 🎲 img-api — 高性能随机图片 API

Go 语言实现的高性能随机图片 API 服务，轻量、零数据库依赖，专为博客和个人网站设计。

> ⚠️ **重要声明**：本项目由 AI（DeepSeek）生成，尚未经过完整的运行测试，
> Releases 中自动构建的二进制包与 Docker 镜像同样未经人工验证，
> 正式投入使用前请务必先自行测试；如发现任何问题，欢迎提交 Issue。

[![Go Version](https://img.shields.io/badge/Go-%3E%3D1.26-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)
[![Docker](https://img.shields.io/badge/docker-ready-2496ED?logo=docker)](https://www.docker.com/)

---

## ✨ 特性

- 🚀 **高性能** — Go 原生编译，goroutine 高并发，内存占用低（参考值见 docs/DEPLOY.md）
- 🎲 **每次请求随机** — 不缓存选中结果，同一分类连续访问通常返回不同图片
- 📱 **设备自适应** — 自动识别 PC/手机 User-Agent，返回对应横竖屏图片
- 🔀 **三种图源** — TXT 图库、本地文件、外部 API 池，可按名称和分类筛选
- 💾 **Redis 缓存** — SRandMember O(1) 随机 + 内存自动降级
- ⚡ **熔断保护** — 外部 API 异常三态熔断，保障服务稳定
- 🛡️ **安全防护** — IP 限流 + Referer 防盗链 + SSRF 防护 + 安全响应头
- 🔑 **健康检查** — `/health` 返回完整内部状态与运行时统计
- 💡 **友好提示页** — 图源未配置 / 分类或 API 不存在时返回引导页（HTML / JSON / SVG 占位图）
- 🏠 **根路径首页** — 浏览器访问 `/` 展示教程页 + 运行状态仪表盘（状态/统计/图源健康，分类清单仅 Debug 展示），`<img>` 嵌入时仍直接出图
- 🗂️ **本地图片索引** — `local.json` 首启自动生成，支持定时自动刷新，无索引时自动扫目录兜底
- 📦 **免依赖部署** — 静态编译为独立二进制，无需 Go 环境、数据库或任何运行时；`build-index` / `sync-redis` / `health-check` 三个配套工具随 Release 包提供
- 🖥️ **Windows 图形控制面板**（`img-api-gui.exe`）— 启动/停止服务、核心设置编辑、开机自启、后台托盘运行，小白双击即用
- 🐳 **Docker 支持** — Alpine 多阶段构建、非 root 运行，镜像精简
- 🔄 **CI/CD** — GitHub Actions 自动编译 5 平台二进制并发布 Release
- 📊 **访问日志** — 每个请求记录 request_id、状态码与耗时，便于排障（健康检查探测不记录）

---

## 🚀 快速开始

### 本地运行（Go 1.26+）

```bash
git clone https://github.com/chihirosyd/img-api.git && cd img-api
cp .env.example .env
go run ./cmd/server/
curl http://localhost:8080/health
```

> 💡 国内网络无法访问 proxy.golang.org 时，先执行
> `go env -w GOPROXY=https://goproxy.cn,direct` 再构建。

> 💡 本地没有 Go 环境？`docker compose build` 可在容器内完成编译验证
> （仅开发者需要，终端用户直接使用镜像即可）。

### Docker 部署

```bash
# 直接使用 GitHub 自动构建的镜像，无需本地编译
# 首次启动会自动生成 ./config/.env 与 ./config/image.yaml 示例
# （编辑后 docker compose restart 生效）
docker compose up -d
curl http://localhost:8080/health
```

> ⚠️ `docker compose` 命令需在 `docker-compose.yml` 所在目录执行；如果报
> `no configuration file provided`，先 `cd` 到项目目录再执行。

> 🐳 compose 默认使用 `ghcr.io/chihirosyd/img-api:latest` 镜像（推送 tag 后自动构建）。
> 开发者如需从源码构建：取消 `docker-compose.yml` 中 `build:` 段的注释即可。
> 版本更新/回滚方法见 [docs/DEPLOY.md](docs/DEPLOY.md) 的「更新升级」章节。

### 二进制部署

从 [Releases](https://github.com/chihirosyd/img-api/releases) 下载对应平台的 zip 包
（内含可执行文件 + `.env.example` + `config/` + `resources/` 目录骨架 + 文档；
Windows 版另含图形控制面板 `img-api-gui.exe`）：

```bash
# Linux / macOS 示例
unzip img-api-linux-amd64.zip
chmod +x img-api build-index sync-redis health-check
cp .env.example .env
./img-api
```

Windows 用户解压后复制 `.env.example` 为 `.env`，直接运行 `img-api.exe` 即可
（`.env` 与 `resources/` 骨架已随 zip 提供，详见 [docs/DEPLOY.md](docs/DEPLOY.md)）。

---

## 📖 核心 API

### 获取随机图片

```
GET /random?type=auto&source=txt&mode=redirect&category=default
```

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| `type` | string | `auto` | `auto` / `pc` / `pe` |
| `source` | string | `txt` | `txt` / `local` / `external` |
| `mode` | string | `redirect` | `redirect` / `json` / `image` |
| `category` | string | `default` | 逗号分隔多选（如 `anime,scenery`） |
| `api` | string | — | 指定池中具体的外部 API（需配合 `source=external`；值为 `image.yaml` 中该项的 `name`，不传则随机选一个） |

> ⚠️ `source=local` 不支持 `mode=redirect`（返回 400）：本地图源返回的是文件系统路径，
> 无法通过 302 让浏览器访问，请使用 `mode=image` 或 `mode=json`。

> 💡 `source` 与 `api` 是两层选择：`source=external` 先进入"外部 API 池"渠道，
> `api=flickr` 再指定渠道内具体哪个 API（值对应 `image.yaml` 中某项的 `name`）；
> 不传 `api` 则从池中随机选一个。

### 使用示例

```html
<!-- 博客嵌入：自动适配设备 -->
<img src="https://your-api.com/random" alt="随机图片">

<!-- 外部 API 池：指定 flickr + 动漫分类 -->
https://your-api.com/random?source=external&api=flickr&category=anime

<!-- 外部 API 池：随机选取 -->
https://your-api.com/random?source=external

<!-- 多分类随机 -->
https://your-api.com/random?category=anime,scenery,beauty

<!-- 手机竖屏 + JSON 返回 -->
https://your-api.com/random?type=pe&category=anime&mode=json
```

### 健康检查

```
GET /health              → 完整状态与运行时统计
```

> 📚 完整 API 文档见 [docs/API.md](docs/API.md)
> 🗒️ 版本更新内容见 [CHANGELOG.md](CHANGELOG.md)

---

## 🔧 核心配置

| 配置项 | 默认值 | 说明 |
|--------|--------|------|
| `APP_PORT` | `8080` | 监听端口 |
| `RATE_LIMIT_MAX` | `60` | 每分钟每 IP 最大请求 |
| `REFERER_WHITELIST` | — | 防盗链域名（逗号分隔），Nginx 转发后依然生效 |
| `TRUSTED_PROXIES` | — | 可信反代网段（CIDR，逗号分隔，可选），填写后限流与应用自带日志按真实访客 IP 统计（网关自身日志不受影响） |
| `REDIS_ADDR` | — | Redis 地址（留空禁用） |
| `LOCAL_INDEX_REFRESH_MINUTES` | `0` | 本地图片索引自动刷新间隔（分钟，0=仅首次启动生成） |
| `TXT_DEFAULT_CATEGORY` | — | txt 渠道默认分类（请求不带 `category` 时使用，留空=`default`） |
| `LOCAL_DEFAULT_CATEGORY` | — | local 渠道默认分类（同上，留空=`default`） |
| `CIRCUIT_FAILURE_THRESHOLD` | `5` | 熔断失败阈值 |
| `CORS_ENABLED` | `true` | 跨域响应头；`<img>` 嵌图无需跨域，Nginx 已处理时可关闭 |

> 📚 完整配置参考见 [docs/CONFIG.md](docs/CONFIG.md)

---

## 🏗️ 项目结构

```
img-api/
├── cmd/                        # 入口程序
│   ├── server/                 # 主服务（命令行）
│   ├── gui/                    # Windows 图形控制面板（img-api-gui.exe）
│   ├── build-index/            # 本地图片索引（手动重建，通常无需运行）
│   ├── health-check/           # 健康检查 CLI
│   ├── sync-redis/             # TXT → Redis 同步
│   ├── genicon/                # exe/托盘图标生成器
│   └── setversion/             # CHANGELOG → config.go / .env.example 版本同步
├── internal/                   # 核心源码
│   ├── config/                 # 配置中心
│   ├── model/                  # 数据模型
│   ├── handler/                # HTTP 处理器
│   ├── service/                # 业务编排
│   ├── repository/             # 数据仓库
│   ├── cache/                  # Redis + 内存缓存
│   ├── circuit/                # 熔断器
│   ├── netx/                   # 安全出站 HTTP（防 SSRF/DNS rebinding）
│   ├── middleware/              # 中间件链
│   ├── device/                 # 设备检测
│   ├── logger/                 # 日志
│   └── app/                    # 工具
├── resources/                  # 图库目录（txt/ 和 local/）
├── config/image.yaml          # 外部 API 池配置
├── docs/                       # 文档
├── Dockerfile
└── .env.example
```

---

## 📝 添加图片和分类

### TXT 图库（source=txt，推荐）

```
resources/txt/
├── pc/                          ← PC 端
│   ├── default.txt              ← 默认分类
│   └── anime.txt                ← 新建分类 = 新建 .txt 文件
└── pe/                          ← 手机端
    └── default.txt
```

每行一个图片 URL，`#` 开头为注释：

```
# 自然风光
https://images.unsplash.com/photo-1506905925346-21bda4d32df4?w=1920
https://images.unsplash.com/photo-1441974231531-c6227db76b6e?w=1920
```

**默认分类**：`default.txt` 已随项目提供（Docker 远程镜像部署时首次启动自动生成，含注释示例）。
**新增分类**：在 `resources/txt/pc/` 下新建 `分类名.txt`，填好 URL，即时生效。
向已有分类中增删 URL：自动感知文件变化，即时生效。

> ⚠️ 若启用了 Redis 并运行过 sync-redis，TXT 文件的改动不会自动同步到 Redis，
> 需重新同步后生效：源码环境 `go run ./cmd/sync-redis/`、二进制包 `./sync-redis`、
> Docker `docker compose exec img-api /app/sync-redis`（`img-api` 为 compose 服务名，可用 `docker compose ps --services` 查看实际名称）。

### 本地图片（source=local）

```
resources/local/
├── pc/
│   └── default/                 ← 新建分类 = 新建文件夹 + 放图片
│       ├── img001.jpg
│       └── img002.png
└── pe/
    └── default/
        └── photo.jpg
```

支持 jpg/png/gif/webp/bmp/svg。

> 📁 源码/二进制包部署时，`pc/default/` 与 `pe/default/` 目录骨架已随项目提供；
> Docker 远程镜像部署（只下载 compose 文件）时，首次启动会自动补齐这些目录。
> 直接把图片放进去即可。

> ⚠️ 生效时机：**新增分类**（新目录）即时生效；**已有分类中增删图片**需重建索引并重启加载：
> 先运行 `./build-index`（源码 `go run ./cmd/build-index/`，Docker `docker compose exec img-api /app/build-index`），
> 再重启服务；或删除 `storage/index/local.json` 后重启自动重建；或等 `LOCAL_INDEX_REFRESH_MINUTES` 定时刷新。
> 索引重建前删除的图片可能随机返回 502，新增的图片不会被选中。
>
> 💡 Docker 命令中的 `img-api` 是 compose 服务名（非容器名），
> 运行 `docker compose ps --services` 查看实际服务名并替换。

> 💡 本地图片请使用 `mode=image`（服务端直接输出）或 `mode=json` 访问，
> 不支持 `mode=redirect`。

> 🗂️ 索引机制：首次启动自动扫描生成 `storage/index/local.json`，
> 可通过 `LOCAL_INDEX_REFRESH_MINUTES` 定时自动刷新；
> 索引中不存在的分类会直接扫目录兜底（仅新增分类即时可用，见上方生效时机说明）。

### 外部 API（source=external）

编辑 `config/image.yaml` 配置 API 池，`?source=external` 调用；`?api=名称` 指定池中某个 API（不传则随机选一个）。

> 💬 如果访问的图源还没有配置图片，API 会返回友好的"开始使用"引导页
> （浏览器直接访问为 HTML 教程、博客 `<img>` 嵌入为 SVG 提示图、`mode=json` 为 JSON），
> 而非生硬的错误码。详见 [docs/API.md](docs/API.md)。

---

## 🚢 部署指南

- 📦 [二进制部署](docs/DEPLOY.md#二进制部署)
- 🐳 [Docker 部署](docs/DEPLOY.md#docker-部署)
- 🔄 [Nginx 反代](docs/DEPLOY.md#nginx-反向代理)

---

## 📄 License

MIT © 2026
