# 部署指南

| 章节 | 内容 |
|---|---|
| [二进制部署](#二进制部署) | 单文件部署、开机自启、目录结构 |
| [Docker 部署](#docker-部署) | compose、手动 `docker run`、配置来源 |
| [Nginx 反向代理](#nginx-反向代理) | 反代、限流、防盗链示例 |
| [更新升级](#更新升级) | 版本升级与回滚 |
| [GitHub Actions](#github-actions) | 发布流程与 CI |
| [性能参考](#性能参考) | 压测数据与资源占用 |
| [常见现象排查](#常见现象排查) | 现象 → 原因 → 处理 |

> 🧭 **按场景阅读**：
> - **第一次部署** → 看[二进制部署](#二进制部署)或 [Docker 部署](#docker-部署)，再配 [Nginx 反向代理](#nginx-反向代理)；
> - **服务已在跑，要升级/回滚** → 直接看[更新升级](#更新升级)；
> - **遇到问题** → 先看文末[常见现象排查](#常见现象排查)；
> - **维护者发布新版** → 看 [GitHub Actions](#github-actions)。

## 二进制部署

### Linux / macOS

```bash
# 1. 从 GitHub Releases 下载对应平台的 zip 包
# （内含二进制程序 + .env.example + config/ + resources/ 骨架目录 + docs/）
# （Windows 版另含图形控制面板 img-api-gui.exe）
wget https://github.com/chihirosyd/img-api/releases/latest/download/img-api-linux-amd64.zip
unzip img-api-linux-amd64.zip
cd img-api
chmod +x img-api build-index sync-redis health-check

# 2. 准备配置（.env.example 已随 zip 提供）
cp .env.example .env

# 3. 启动服务（前台运行，Ctrl+C 停止）
# 图库目录骨架已随 zip 提供，直接往 resources/txt/pc/ 添加 .txt 即可
./img-api

# 4. 另开一个终端验证
curl http://localhost:8080/health
# 浏览器打开 http://localhost:8080/ 可见教程首页，/random 随机出图
```

后台运行（可选，关闭终端不中断）：

```bash
nohup ./img-api > img-api.log 2>&1 &

tail -f img-api.log        # 查看日志
pkill img-api              # 停止服务
```

> 🍎 macOS 同理：下载 darwin 对应架构的 zip，其余步骤一致；
> 长期运行推荐下方 systemd（Linux）/ launchd（macOS）。

> ⚠️ 启动后编辑 `resources/txt/pc/default.txt` 添加图片 URL（一行一个，即时生效）。
> 如需外部 API，编辑 `config/image.yaml`（重启生效）。
> 本地图片放入新分类目录即时生效；放入已有分类（如 `default/`）需重建索引并让服务加载：
> 运行 `./build-index`（源码 `go run ./cmd/build-index/`）后**重启**，或**发 `SIGHUP` 免重启**
> （`kill -HUP <pid>`；容器：`docker compose kill -s HUP img-api`），也可删除
> `storage/index/local.json` 后重启自动重建。
>
> 💡 如果图源还没有图片，访问 `/random` 会返回友好的"开始使用"引导页而非报错。

### systemd（开机自启）

```bash
sudo tee /etc/systemd/system/img-api.service << 'EOF'
[Unit]
Description=Random Image API
After=network.target

[Service]
Type=simple
User=www-data
WorkingDirectory=/opt/img-api
ExecStart=/opt/img-api/img-api
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# ⚠️ 关键步骤：授予运行用户目录权限。
# 服务启动时会创建 storage/logs（日志）与 storage/index/local.json（本地图片索引），
# /opt/img-api 默认为 root 所有，www-data 无写权限会导致服务启动失败（exit 1）。
# 替代方案：只授权 storage/ 目录（sudo chown -R www-data:www-data /opt/img-api/storage），
# 或把上面 User= 改成你自己的用户名运行。
sudo chown -R www-data:www-data /opt/img-api

sudo systemctl daemon-reload
sudo systemctl enable --now img-api
```

排障与查看：

```bash
systemctl status img-api          # 状态
journalctl -u img-api -f          # 实时日志
systemctl restart img-api         # 修改 .env 后重启
```

### Windows

**方式一：图形控制面板（推荐小白用户）**

```powershell
# 双击 img-api-gui.exe 打开控制面板：
#   - 启动 / 停止 / 重启服务
#   - 编辑核心设置（保存后自动重启生效）
#   - 开机自启（当前用户级，无需管理员）
#   - 后台运行（关闭窗口时最小化到托盘，服务继续运行）
```

> ⚠️ 若直接删除程序目录而未先在控制面板**关闭「开机自启」开关**，注册表会残留
> 指向不存在 exe 的开机自启项。可手动清理：删除注册表
> `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 下的 `img-api-gui` 值
> （或在删除程序前先关闭自启开关）。

**方式二：命令行（服务器/脚本场景）**

```powershell
# 1. 从 GitHub Releases 下载 img-api-windows-amd64.zip
# 2. 解压到目录（如 D:\img-api），exe 必须与 .env、config/、resources/ 同目录
cd D:\img-api

# 3. 复制配置模板（可选，不改则使用默认配置）
copy .env.example .env

# 4. 双击 img-api.exe 运行：控制台窗口显示友好启动横幅，
#    并自动打开默认浏览器到教程首页（含运行状态仪表盘），
#    即可直观确认服务是否正常；关闭控制台窗口即停止服务
#    不想自动打开浏览器：设置环境变量 IMG_API_OPEN_BROWSER=0 后运行
.\img-api.exe

# 5. 浏览器验证（若未自动打开，手动访问）
start http://localhost:8080/
```

> 📁 图库编辑方式与其他平台一致：改 `resources\txt\pc\default.txt` 即时生效，
> 改 `.env` 后重新运行 exe 生效。

#### 后台静默运行（可选，无黑色窗口）

```powershell
# 启动（隐藏窗口，关闭当前终端不影响服务）
Start-Process .\img-api.exe -WindowStyle Hidden

# 停止服务
Get-Process img-api | Stop-Process
```

#### 开机自启（任务计划程序，无窗口）

管理员 PowerShell 执行一次（路径改成你的解压目录）：

```powershell
$action  = New-ScheduledTaskAction -Execute "D:\img-api\img-api.exe" -WorkingDirectory "D:\img-api"
$trigger = New-ScheduledTaskTrigger -AtStartup
Register-ScheduledTask -TaskName "img-api" -Action $action -Trigger $trigger -RunLevel Highest -Force
```

管理：任务计划程序库中找到 `img-api`；移除自启：`Unregister-ScheduledTask -TaskName img-api -Confirm:$false`。

#### 防火墙放行（局域网/公网访问时）

管理员 PowerShell 执行一次（仅本机访问可跳过）：

```powershell
New-NetFirewallRule -DisplayName "img-api 8080" -Direction Inbound -Protocol TCP -LocalPort 8080 -Action Allow
# 若改过监听端口（APP_PORT），把上面两处 8080 同步为新端口
```

> 公网直接暴露更推荐 Docker 部署或前方反代（见下方 Nginx 章节）。

---

## Docker 部署

> 🐳 compose 默认使用 GitHub Actions 自动构建的镜像
> `ghcr.io/chihirosyd/img-api:latest`（推送 tag 后生成），**用户无需本地编译**。

```bash
# 1. 创建项目目录并下载 compose 文件
mkdir img-api && cd img-api
curl -O https://raw.githubusercontent.com/chihirosyd/img-api/main/docker-compose.yml

# 2. 启动（自动拉取镜像；首次运行会在 ./config/ 自动生成 .env）
#    注意：以下所有 docker compose 命令都必须在 docker-compose.yml 所在目录执行
#    （报 no configuration file provided 时先 cd 到项目目录）
docker compose up -d

# 3. 验证
curl http://localhost:8080/health

# 4. 查看/编辑配置（编辑后 restart 生效）
cat config/.env
vim config/.env
docker compose restart
```

> 🔌 换宿主机端口（8080 被占用时）：把 compose 里 `ports` 的**左侧**改成新端口
> （如 `"9090:8080"`），再 `docker compose up -d` 重建即可；容器内仍监听 8080，
> `.env` 无需改动。改后记得同步三处：外部访问用新端口、Nginx 反代的
> `proxy_pass` 指向新端口、防火墙放行规则改为新端口。

> 📁 首次启动会自动创建 `config/`、`resources/`、`storage/` 等挂载目录，
> 并自动生成 `config/.env`、`config/image.yaml`（内置注释示例）以及图库骨架：
> `resources/txt/{pc,pe}/default.txt`、`resources/local/{pc,pe}/default/` 目录，直接编辑即可。
> 图库文件直接放在宿主机对应目录即可（如 `resources/txt/pc/default.txt`），
> 外部 API 池直接编辑自动生成的 `config/image.yaml`（含 picsum / flickr / unsplash 注释模板）。
> 镜像更新：`docker compose pull && docker compose up -d`。

> 🛠️ 开发者如需从源码构建：在 `docker-compose.yml` 中注释掉 `image:` 行、
> 取消 `build:` 段注释，然后 `docker compose build && docker compose up -d`
> （本地无 Go 环境也能借此完成编译验证）。

> ⚠️ 启动后需向 `resources/txt/pc/default.txt` 添加图片 URL，一行一个：
> ```bash
> echo "https://example.com/photo1.jpg" >> resources/txt/pc/default.txt
> echo "https://example.com/photo2.jpg" >> resources/txt/pc/default.txt
> ```
> 如需外部 API 池，编辑 `config/image.yaml` 并重启。
> 本地图片索引（`storage/index/local.json`）首启自动生成，
> 可通过 `LOCAL_INDEX_REFRESH` 计划表自动刷新（Go duration / @daily 等描述符 / 5 字段 cron）。
> 修改 `.env` 配置后执行 `docker compose restart` 生效。

### 启用 Redis（可选）

取消 `docker-compose.yml` 中 redis 段落的注释，并在 `.env` 中设置：

```ini
REDIS_ADDR=redis:6379  #端口须和 `.env`，`docker-compose.yml`中一致
```

```bash
docker compose up -d

# 同步 TXT 图库到 Redis（在容器内执行；修改 TXT 后需重新运行）
# 注意：img-api 是 compose 服务名（非容器名），可用 docker compose ps --services 查看实际服务名并替换
docker compose exec img-api /app/sync-redis
```

### 手动构建（不使用远程镜像）

```bash
docker build -t img-api .
# 左侧 8080 可换成宿主机实际端口；右侧为容器内部端口，须与 .env 的 APP_PORT 一致
docker run -d -p 8080:8080 \
  -v $(pwd)/config:/app/config \
  -v $(pwd)/resources/txt:/app/resources/txt \
  --name img-api img-api
```

> ⚠️ 不要再单独挂 `-v $(pwd)/.env:/app/.env`：entrypoint 启动时会把 `config/.env`
> 覆盖到 `/app/.env`，单独挂载的宿主机文件会被替换（配置看似改了、实际不生效）。
> 配置一律放 `config/.env`。
> ⚠️ `resources/txt` 挂载**不能加 `:ro`**：容器首次启动需在其中创建 `pc/`、`pe/`
> 骨架与示例 `default.txt`（只读挂载会导致 entrypoint 失败、容器反复重启）。
> 如需加固，可先在宿主机建好 `resources/txt/pc`、`resources/txt/pe` 骨架后再改回只读。

> 📌 关于配置来源：挂载 `./config` 时，容器首次启动会用镜像内置的模板
> `/app/.env.default` 生成 `config/.env`（之后不再覆盖），因此宿主机侧目录为空也不会
> 启动失败。注意该模板的 `CORS_ENABLED` / `RATE_LIMIT_ENABLED` 是 `false`
> （面向有网关的部署）——直接暴露端口时请改为 `true`，详见 [CONFIG.md](CONFIG.md)。

---

## Nginx 反向代理

```nginx
# limit_req_zone 只能写在 http 上下文（不能放进 server {}），否则 nginx -t 会报
# "directive is not allowed here"。把它放到 nginx.conf 的 http {} 里：
http {
    limit_req_zone $binary_remote_addr zone=img:10m rate=60r/m;
    # ... 其余 http 级配置（include 等）
}

server {
    listen 80;
    server_name img.yourdomain.com;

    # 限流（可选，zone 已在 http 块定义）
    limit_req zone=img burst=10 nodelay;

    # 防盗链（可选）
    valid_referers none blocked yourdomain.com *.yourdomain.com;
    if ($invalid_referer) { return 403; }

    location / {
        # 若 compose 宿主机端口改过，这里同步为新端口（默认 8080）
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        # 覆盖客户端伪造的 XFF（不要用 $proxy_add_x_forwarded_for 透传：
        # 应用从右向左跳过 TRUSTED_PROXIES 后取第一个不可信地址，
        # 透传虽不能绕过限流，但会把伪造 IP 留在链里污染日志）
        proxy_set_header X-Forwarded-For $remote_addr;
    }
}
```

Nginx 处理时建议关闭 Go 内置的对应功能（`<img>` 标签嵌图不需要跨域，
此处关闭 CORS 不影响博客引用图片）：

```ini
CORS_ENABLED=false
RATE_LIMIT_ENABLED=false
REFERER_WHITELIST=
TRUSTED_PROXIES=172.17.0.0/16   # 可选但推荐：填写后应用自带日志能按真实访客 IP 记录（Nginx 日志不受影响）
```

> 💡 若想保留 Go 内置限流（`RATE_LIMIT_ENABLED=true`），则必须配置
> `TRUSTED_PROXIES=172.17.0.0/16`（Nginx 所在网段）（示例，请按照实际填写），限流才会按
> `X-Forwarded-For` 中的真实客户端 IP 统计；未配置时一律按 Nginx 的 IP 计数。
> 注意：`TRUSTED_PROXIES` 需配合上方 `proxy_set_header X-Forwarded-For` 才生效，
> 且必须**覆盖**（而非透传）客户端 XFF；网段尽量填窄（能覆盖网关即可）。

---

## 更新升级

### Docker（compose）部署

```bash
# 1. （可选）更新 compose 文件（仓库有变动时）
curl -O https://raw.githubusercontent.com/chihirosyd/img-api/main/docker-compose.yml

# 2. 拉取新镜像并重建容器
docker compose pull && docker compose up -d

# 3. 验证版本（看 version 字段）
curl http://localhost:8080/health
```

> 📌 升级不会动你的数据：`.env`、`config/`、`resources/`、`storage/` 均为宿主机挂载，
> 重建容器后配置与图库原样保留。
>
> 📌 固定版本升级：把 compose 里 `image: ghcr.io/chihirosyd/img-api:latest`
> 改为 `:1.2.0`（替换版本号即可；注意镜像 tag 不带 `v` 前缀）；`latest` 则始终跟随最新发布。

### 二进制部署（Linux / macOS / Windows）

> ⚠️ 升级**只覆盖二进制文件**：`.env`、`config/`、`resources/`、`storage/` 一律保留。
> 先把新 zip 解压到临时目录，再只把二进制复制到原部署目录覆盖，
> 不要在原目录直接 `unzip -o`（会覆盖你的配置与图库文件）。

```bash
# 1. 下载新版本 zip
wget https://github.com/chihirosyd/img-api/releases/latest/download/img-api-linux-amd64.zip

# 2. 解压到临时目录（zip 内文件位于 img-api/ 子目录下）
unzip -q -o img-api-linux-amd64.zip -d /tmp/img-api-new

# 3. 只复制二进制覆盖（把「/你的/实际部署目录」替换成你的部署路径，例如 /opt/img-api）
cd /你的/实际部署目录
cp /tmp/img-api-new/img-api/img-api \
   /tmp/img-api-new/img-api/build-index \
   /tmp/img-api-new/img-api/sync-redis \
   /tmp/img-api-new/img-api/health-check .
chmod +x img-api build-index sync-redis health-check

# 4. 重启服务
./img-api
```

Windows：把新 zip 解压到临时目录，将 `img-api.exe`、`img-api-gui.exe`、
`build-index.exe`、`sync-redis.exe`、`health-check.exe` 复制到原部署目录覆盖
（`.env` 与图库不动），重新双击 `img-api.exe`。

### 回滚

- Docker：把 compose 里镜像版本号改回上一版（如 `:1.1.0`，不带 `v`）→ `docker compose up -d`；
- 二进制：重新下载旧版本 zip，解压到临时目录后复制二进制覆盖即可。

### 跨版本注意

- 每次升级前建议查看 [CHANGELOG.md](../CHANGELOG.md) 了解变更内容。

---

## GitHub Actions

推送 tag 自动编译 5 平台二进制并发布 Release。依赖以仓库锁定的
`go.mod` / `go.sum` 为准：Release 二进制、Docker 镜像与 CI 检查使用同一套版本
（依赖升级只在本地或 `Update go.sum` 兜底工作流中进行，构建环节不再现场升级）。

### 发布流程

```mermaid
flowchart TD
    A[本地开发完成] --> B[更新 CHANGELOG<br/>go run ./cmd/setversion<br/>go get -u ./... && go mod tidy]
    B --> C[git push origin main]
    C --> D{Update go.sum 工作流}
    D -->|本地已升级：通常空跑，无新提交| E[git pull --rebase]
    D -->|兜底：依赖有更新则自动提交 go.mod / go.sum| E
    E --> F[git tag vX.Y.Z<br/>git push origin vX.Y.Z]
    F --> G[release.yml：check 全绿后编译 5 平台并发布 Release]
    F --> H[release.yml docker job：check 全绿后构建多架构镜像推送 GHCR]
    G -.依赖按 tag 锁定.-> F
    H -.依赖按 tag 锁定.-> F
```

### 发布前本地检查清单（防止 CI 红灯）

```bash
# 单测与静态检查（CI check 同款）
go test ./...
go vet ./...
# 依赖升级并整理（产物与仓库锁定依赖保持一致；之后 gosum 通常空跑）
go get -u ./... && go mod tidy
```

> 💡 Windows 上 `go vet ./...` / `go test ./...` **不需要 CGO**：`cmd/gui` 的构建标记是
> `//go:build windows && cgo`，CGO 关闭时会自动跳过 GUI。
> 只有**编译 GUI 本身**（`go build ./cmd/gui`）才需要 `CGO_ENABLED=1` 且已安装 C 编译器
> （如 MSYS2 的 mingw-w64 gcc）；发布流程与 CI 的 `gui-check` 都已显式开启 CGO。
> CI 在 Linux 上无此问题：`cmd/gui` 带 `//go:build windows` 标记，Linux 下不参与编译。

### 发布步骤

```bash
# 注：下面命令中的 vX.Y.Z 是版本占位符，请替换为实际版本号（如 v1.5.0）
# 1. 更新 CHANGELOG.md（[未发布] 段改名为 ## [x.y.z] - 日期）
# 2. 同步版本号到代码内置默认值（唯一来源是 CHANGELOG）
go run ./cmd/setversion
# 3. 提交（依赖已在发布前升级）
git add -A && git commit -m "release vX.Y.Z"
# 3.5 核对行为变更注记：docs/ 里「🕒 自 v… 起」必须与实际版本号一致（当前标的是 v1.6.0）
git grep -n "🕒"
# 4. 先推 main：Update go.sum 工作流会兜底检查 go.mod/go.sum（通常空跑无新提交）
git push origin main
# 5. 等 Update go.sum 跑完后拉取，再打 tag 触发发布
git pull --rebase
git tag vX.Y.Z
git push origin vX.Y.Z
```

> 💡 `git pull --rebase` 的作用：把远端 `Update go.sum` 可能产生的自动提交
> 拉到本地，并让本地提交重放到它之上（历史保持直线，不产生 merge 提交），
> 这样 tag 才会打在最新依赖之上。若 gosum 空跑（无新提交），该命令就是空操作。

> ⚠️ 不要跳过第 4、5 步（把 main 和 tag 一起推）：tag 应落在
> `Update go.sum` 兜底提交之后，否则本次 Release 的依赖可能比仓库落后一个版本。

产物（每个平台一个 zip：Linux/macOS 含 4 个二进制，Windows 另含图形控制面板）：
- `img-api-linux-amd64.zip`
- `img-api-linux-arm64.zip`
- `img-api-windows-amd64.zip`
- `img-api-darwin-amd64.zip`
- `img-api-darwin-arm64.zip`

zip 内容：
- `img-api`（Windows 为 `img-api.exe`）：主程序（命令行）
- `img-api-gui.exe`（仅 Windows）：图形控制面板（启动/停止/设置/自启/托盘后台）
- `health-check`：健康检查 CLI
- `build-index`：手动重建本地图片索引
- `sync-redis`：TXT → Redis 同步
- `.env.example`：配置模板（复制为 `.env` 后修改）
- `config/`：外部 API 池配置、`resources/`：图库目录骨架、`docs/`、`README.md`、`CHANGELOG.md`（如仓库已添加 LICENSE 文件也会一并打包）

---

## 性能参考

| 场景 | QPS | 内存 |
|------|:---:|:---:|
| TXT 源（无缓存），100 个 URL | ~20,000 | ~8MB |
| TXT 源（Redis），10,000 个 URL | ~25,000 | ~12MB |
| 外部 API（受远端限制） | ~500 | ~8MB |

> 参考值：Go 1.26，Intel i7-12700H，wrk 4 线程 100 连接（实际性能因环境而异）

---

## 常见现象排查

> 先看"现象"，再看这一列；每条都给了下一步该查什么。
> 状态码本身的含义见 [API 参考 → 常见状态码速查](API.md#常见状态码速查)。

| 现象 | 最可能原因 | 处理 |
|---|---|---|
| 容器一直 `unhealthy` | `/health` 返回 `degraded`：某个**已配置**的图源取不到图 | `curl localhost:8080/health` 看 `checks` 里哪项不是 `healthy` |
| `checks.local` = `unhealthy: 设备目录缺失: pe` | 只有 `pc` 没有 `pe`，或挂载没生效 | 补齐 `resources/local/pe/`（`txt` 同理）；检查 compose `volumes` |
| 请求图片返回 `404` | 分类名/设备目录不对，或索引仍指向已删除的文件 | 确认 `resources/local/{pc,pe}/{category}/` 有图；发 `SIGHUP` 重建索引 |
| 日志反复出现 `config problem` | `.env` 里有非法数字/布尔值、未知的 `image.yaml` 键，或 `.env` 解析失败 | 按日志里的变量名修正；值含 `$`/`#` 用单引号（见 CONFIG.md「取值规则」）。注意：**空值不会告警**（空环境变量按"未设置"处理） |
| 日志出现 `config/image.yaml problem` | YAML 缩进/未知键写错，或 `default_category` 写成了标量 | 按报错修正；`default_category` 必须是列表 `[scenery]` |
| 启动失败 `listen tcp :8080: bind: address already in use` | 端口被其他实例/程序占用 | 换 `APP_PORT`，并同步 compose `ports` 右侧与 healthcheck 的 `-url` |
| `429 Too Many Requests` 突然变多 | 内置限流阈值偏小；或未配 `TRUSTED_PROXIES`，所有访客共用网关 IP 一个桶 | 配 `TRUSTED_PROXIES`，或关闭内置限流交给网关 `limit_req` |
| `502` 反复出现 | 外部图床不可用、返回非图片内容、或图片超过 50MB | 查日志 `upstream image fetch failed` 的 `status`；换 API 或调小图 |
| Docker 里放不进图片（`Permission denied`） | `resources/` 是 Docker 以 root 创建的 | `sudo chown -R $(id -u):$(id -g) resources/`（详见 [二进制部署](#二进制部署) 的目录说明） |
| 改了 `.env` 但不生效 | 没有重启服务 | `docker compose restart`；只有 TXT/图片内容类改动不需要重启（见 CONFIG.md「变更生效时机」） |
