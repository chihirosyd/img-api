# 参与开发与维护约定

> 本文只约定**如何保持代码、注释、文档三者一致**，不涉及代码风格。
> 目标：任何一次行为变更后，读者在 `README.md` 与 `docs/` 里看到的都是**当前真实行为**。

**速览（30 秒版）**：改行为 → 按 §2 / §3 找到所有"副本"并同步 → `CHANGELOG.md` 的 `[未发布]` 加一条 → 若改变了旧行为则加 `🕒 自 vX.Y.Z 起` → 跑 §8 自检。

适用范围：修改 `internal/`、`cmd/`、`docker/`、`config/`、`docs/`、`README.md`、`.env.example` 的任何提交。

---

## 1. 文档职责边界（内容不要放错文件）

| 文件 | 只放什么 | 不要放什么 |
|---|---|---|
| `README.md` | 入口：卖点、最快上手、最常用 6 项配置、发布三步 | 完整配置表、完整 API 契约（链接过去） |
| `docs/API.md` | 对外契约：参数、响应、状态码、`/health` 字段 | 部署步骤、内部实现细节 |
| `docs/CONFIG.md` | 每个环境变量的默认值与含义、生效时机、网关示例 | 部署流程（链接 DEPLOY） |
| `docs/DEPLOY.md` | 部署方式、升级回滚、排障表 | 变量含义（链接 CONFIG） |
| `CHANGELOG.md` | 版本史（**版本号的唯一来源**） | 运维手册 |
| `private/` | 内部审查台账（gitignored） | —— |

> ⚠️ 维护者内容**不要**放进 `docs/`：`release.yml` 会 `cp -r docs` 打进 Release 包，直接发给终端用户。

---

## 2. 单一事实来源（SSOT）

每个事实只以一处为准，其余位置是"副本"，**改动时必须同步**：

| 事实 | 以什么为准 | 必须同步的位置 |
|---|---|---|
| 配置项默认值 | `internal/config/config.go` 的 `defaults` 表 | `docs/CONFIG.md` 表格、`.env.example`、`README.md` 核心配置 |
| 参数 / 响应 / 状态码 | `internal/handler/api.go` | `docs/API.md`、`docs/DEPLOY.md` 排障表 |
| 路由与方法 | `internal/app/server.go` | `docs/API.md` 路由一览 |
| `/health` 判定规则 | `internal/handler/health.go` | `docs/API.md`、`docs/DEPLOY.md` 排障表、`README.md` 特性行 |
| 索引何时生效 | `internal/repository/local_repo.go` + `cmd/server`（SIGHUP） | `README.md`、`docs/CONFIG.md`（两处）、`docs/DEPLOY.md`、`docs/API.md` |
| CLI 参数 | `cmd/*/main.go` 的 `flag.*` | `docs/DEPLOY.md`、`docs/CONFIG.md` |
| 部署链路（`.env.default` / entrypoint / compose） | `docker/entrypoint.sh`、`Dockerfile`、`docker-compose.yml` | `docs/DEPLOY.md`、`docs/CONFIG.md`、`README.md` |
| 版本号 | `CHANGELOG.md` | `cmd/setversion` 写入 `config.go`；git tag；`docs/` 里的 🕒 注记 |

> CI 会校验 `git tag` = `CHANGELOG.md` = `config.go` 三方版本一致（`release.yml` 的 Verify 步骤）。

---

## 3. 常见改动对照（按改动类型查）

§2 是按"事实"组织的；实际动手时用这张表反查更直接：

| 你要做的事 | 必须同步的位置 | 顺带检查 |
|---|---|---|
| 加 / 改一个环境变量 | `config.go` 的 `defaults` → `docs/CONFIG.md` 表格 → `.env.example` →（常用项才进 README） | 其它文档里引用它的示例（如 DEPLOY 的示例 `.env`） |
| 改某项默认值 | 同上四处 | `.env.example` 的注释是否还准确 |
| 加 / 改状态码 | `handler/api.go` → `docs/API.md` 状态码表 → `docs/DEPLOY.md` 排障表 | `/health` 的 `status` 是否被误当成 HTTP 状态码 |
| 改路由 | `app/server.go` → `docs/API.md` 路由一览 | 中间件豁免路径（`/health`、`/`）是否仍成立 |
| 改索引生效方式 | `repository/local_repo.go` + `cmd/server` → README、CONFIG（两处）、DEPLOY、API | 是否还有文档在说"必须重启" |
| 改 Docker 链路 | `docker/entrypoint.sh` / `Dockerfile` / `docker-compose.yml` → DEPLOY、CONFIG、README | `.dockerignore` 是否漏了新文件 |
| 改 CLI 参数 | `cmd/*/main.go` 的 `flag` → DEPLOY、CONFIG | 示例命令能否直接复制执行 |
| 发版 | `CHANGELOG.md` → `go run ./cmd/setversion` → 核对所有 🕒 注记 | 三方版本一致（tag / CHANGELOG / config.go） |

---

## 4. 行为变更的五步清单

1. **改代码**（含同文件内与新行为矛盾的注释）；
2. 按 §2 / §3 **找到所有副本**并同步；
3. `CHANGELOG.md` 的 `[未发布]` 加一条，写用户视角的"什么变了 / 为什么"；
4. 如果**改变了旧行为** → 在对应段落加 `🕒 自 vX.Y.Z 起：…` 注记；
5. 跑一遍 §8 的自检命令。

---

## 5. 版本注记规则

- **只对"会改变用户既有预期"的变更加**：判定规则、状态码、默认值、错误码、行为边界；
- **新增功能不加**（读者默认它就是新的）；
- 格式统一成一行：`> 🕒 **自 vX.Y.Z 起**：…`；
- 发版前把 `vX.Y.Z` 换成真实版本号，并 `git grep -n "🕒"` 逐条核对。

---

## 6. 术语与格式约定

- 字段名、命令、状态值一律用反引号：`status`、`health-check -strict`、`degraded`；
- 术语固定：**图源**（txt / local / external）、**分类**（category）、**索引**（`local.json`）、**重建**（重扫并写回索引）；
- 中文用全角标点；英文单词与数字两侧不加空格；
- 命令示例必须能直接复制执行，不写 `...` 之类的省略；
- 同一概念在四份文档里同名：统一叫「更新升级」，不要混用「升级 / 更新」。

---

## 7. 禁止事项

- ❌ **手改 `config.go` 里的版本号** —— 必须 `go run ./cmd/setversion`，CI 会校验 `tag = CHANGELOG = config.go`；
- ❌ **把维护者内容放进 `docs/`** —— `release.yml` 会 `cp -r docs` 打进 Release 发给用户；
- ❌ **把同一句话复制到三处** —— 能留一处 + 链接就留一处（曾因复制导致四份文档四种说法）。

---

## 8. 提交前自检

```bash
go vet ./... && go test ./... -count=1
git grep -n "🕒"                            # 版本注记是否与实际发版号一致
git grep -nE "重启加载|需重启|随机返回 502"  # 旧措辞残留（按上次修正替换关键词）
```

再按本次改动类型补一条，例如：

- 把 `502` 改成 `404` → `git grep -nE "502"`，确认只剩"代理上游失败"那几处；
- 改了默认值 → `git grep -nE "默认"`，核对 `CONFIG.md` 与 `.env.example`；
- 新增 / 删除文档章节 → `git grep -n "](#"`，检查目录锚点是否都还有对应标题。

---

## 9. 两个真实踩过的坑

1. **表格行末尾没有句号**
   批量替换时 `old_string` 带了 `。`，而表格里那一行结尾是 `|` → 该处被静默跳过，于是同一份文档里出现了"404"和"502"两种说法。
   → 改表格内容时逐行核对，**不要只信"全部替换成功"的提示**。

2. **同一事实写三处**
   "索引重建后怎么生效"曾在 `README` / `CONFIG` / `DEPLOY` / `API` 四处各写一遍，结果只改了一处，另外三处还写着"必须重启"。
   → 能留一处 + 链接，就不要复制。

---

## 10. 文档改动同样要验证

- Markdown 表格**列数必须与表头一致**（3 列表头下不能放 4 列）；
- 目录锚点遵循 GitHub slug 规则（中文保留、空格转 `-`、标点删除）；
- 新增 / 删除章节后，同步更新文首的目录表；
- 涉及命令的，实际跑一次（或标注"未在本环境验证"）。
