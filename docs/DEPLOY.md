# 部署指南

## 二进制部署

### Linux / macOS

```bash
# 从 GitHub Releases 下载对应平台的 zip 包
# （内含 4 个二进制 + .env.example + configs/ + resources/ 骨架目录 + docs/）
wget https://github.com/chihirosyd/img-api/releases/latest/download/img-api-linux-amd64.zip
unzip img-api-linux-amd64.zip
chmod +x img-api build-index sync-redis health-check

# 准备配置（.env.example 已随 zip 提供）
cp .env.example .env

# 图库目录骨架已随 zip 提供，直接往 resources/txt/pc/ 添加 .txt 即可
./img-api
```

> ⚠️ 启动后编辑 `resources/txt/pc/default.txt` 添加图片 URL（一行一个，即时生效）。
> 如需外部 API，编辑 `configs/image.yaml`（重启生效）。
> 本地图片放入新分类目录即时生效；放入已有分类（如 `default/`）需重建索引后生效
> （重启服务或运行 `./build-index`；源码环境用 `go run ./cmd/build-index/`）。
>
> 💡 如果图源还没有图片，访问 `/random` 会返回友好的"开始使用"引导页而非报错。
```

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

sudo systemctl daemon-reload
sudo systemctl enable --now img-api
```

### Windows

```powershell
# 下载 img-api-windows-amd64.zip，解压后得到 4 个可执行文件及配置模板/目录骨架
# 复制 .env.example 为 .env（resources/ 骨架已包含在 zip 中）
.\img-api.exe
```

---

## Docker 部署

> 🐳 compose 默认使用 GitHub Actions 自动构建的镜像
> `ghcr.io/chihirosyd/img-api:latest`（推送 tag 后生成），**用户无需本地编译**。

```bash
# 1. 创建项目目录并下载 compose 文件
mkdir img-api && cd img-api
curl -O https://raw.githubusercontent.com/chihirosyd/img-api/main/docker-compose.yml

# 2. 启动（自动拉取镜像；首次运行会在 ./config/ 自动生成 .env）
docker compose up -d

# 3. 验证
curl http://localhost:8080/health

# 4. 查看/编辑配置（编辑后 restart 生效）
cat config/.env
vim config/.env
docker compose restart
```

> 📁 首次启动会自动创建 `config/`、`configs/`、`resources/`、`storage/` 等挂载目录，
> 并自动生成 `config/.env` 与 `configs/image.yaml`（内置注释示例），直接编辑即可。
> 图库文件直接放在宿主机对应目录即可（如 `resources/txt/pc/default.txt`），
> 外部 API 池直接编辑自动生成的 `configs/image.yaml`（含 picsum / flickr / unsplash 注释模板）。
> 镜像更新：`docker compose pull && docker compose up -d`。

> 🛠️ 开发者如需从源码构建：在 `docker-compose.yml` 中注释掉 `image:` 行、
> 取消 `build:` 段注释，然后 `docker compose build && docker compose up -d`
> （本地无 Go 环境也能借此完成编译验证）。

> ⚠️ 启动后需向 `resources/txt/pc/default.txt` 添加图片 URL，一行一个：
> ```bash
> echo "https://example.com/photo1.jpg" >> resources/txt/pc/default.txt
> echo "https://example.com/photo2.jpg" >> resources/txt/pc/default.txt
> ```
> 如需外部 API 池，编辑 `configs/image.yaml` 并重启。
> 本地图片索引（`storage/index/local.json`）首启自动生成，
> 可通过 `LOCAL_INDEX_REFRESH_MINUTES` 定时刷新。
> 修改 `.env` 配置后执行 `docker compose restart` 生效。

### 启用 Redis（可选）

取消 `docker-compose.yml` 中 redis 段落的注释，并在 `.env` 中设置：

```ini
REDIS_ADDR=redis:6379  #端口须和 `.env`，`docker-compose.yml`中一致
```

```bash
docker compose up -d

# 同步 TXT 图库到 Redis（在容器内执行；修改 TXT 后需重新运行）
docker compose exec img-api /app/sync-redis
```

### 手动构建（不使用远程镜像）

```bash
docker build -t img-api .
docker run -d -p 8080:8080 \
  -v $(pwd)/.env:/app/.env \
  -v $(pwd)/resources/txt:/app/resources/txt:ro \
  --name img-api img-api
```

---

## Nginx 反向代理

```nginx
server {
    listen 80;
    server_name img.yourdomain.com;

    # 限流（可选）
    limit_req_zone $binary_remote_addr zone=img:10m rate=60r/m;
    limit_req zone=img burst=10 nodelay;

    # 防盗链（可选）
    valid_referers none blocked yourdomain.com *.yourdomain.com;
    if ($invalid_referer) { return 403; }

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

Nginx 处理时建议关闭 Go 内置的对应功能：

```ini
CORS_ENABLED=false
RATE_LIMIT_ENABLED=false
REFERER_WHITELIST=
```

> 💡 若想保留 Go 内置限流（`RATE_LIMIT_ENABLED=true`），需配置
> `TRUSTED_PROXIES=172.17.0.0/16`（Nginx 所在网段），限流才会按
> `X-Forwarded-For` 中的真实客户端 IP 统计；未配置时一律按 Nginx 的 IP 计数。

---

## GitHub Actions

推送 tag 自动编译 5 平台二进制并发布 Release：

```bash
git tag v1.0.0
git push origin v1.0.0
```

产物（每个平台一个 zip，内含 4 个二进制：主程序 + 配套工具）：
- `img-api-linux-amd64.zip`
- `img-api-linux-arm64.zip`
- `img-api-windows-amd64.zip`
- `img-api-darwin-amd64.zip`
- `img-api-darwin-arm64.zip`

zip 内容：
- `img-api`（Windows 为 `img-api.exe`）：主程序
- `health-check`：健康检查 CLI
- `build-index`：手动重建本地图片索引
- `sync-redis`：TXT → Redis 同步
- `.env.example`：配置模板（复制为 `.env` 后修改）
- `configs/`：外部 API 池配置、`resources/`：图库目录骨架、`docs/`、`README.md`（如仓库已添加 LICENSE 文件也会一并打包）

---

## 性能参考

| 场景 | QPS | 内存 |
|------|:---:|:---:|
| TXT 源（无缓存），100 个 URL | ~20,000 | ~8MB |
| TXT 源（Redis），10,000 个 URL | ~25,000 | ~12MB |
| 外部 API（受远端限制） | ~500 | ~8MB |

> 参考值：Go 1.22，Intel i7-12700H，wrk 4 线程 100 连接（实际性能因环境而异）
