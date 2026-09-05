# AI Agent 平台部署指南

## 快速开始（仅数据库）

最快跑起来的方式，先启动数据库，后端和前端本地跑：

```bash
make db-up      # 启动 PostgreSQL + pgvector
make db-ps      # 查看状态
make db-logs    # 查看日志
make db-down    # 停止（保留数据）
```

等价原生命令：`docker compose up -d` / `docker compose ps` / `docker compose logs -f postgres` / `docker compose down`

数据库连接信息：
- 主机：`localhost:5432`
- 数据库：`aiagent`
- 用户名：`postgres`
- 密码：`postgres`（可用 `.env` 里 `DB_PASSWORD` 覆盖）

---

## 完整部署（数据库 + Go 后端 + Nginx 网关）

### 1. 准备环境变量

```bash
make env
# 编辑 .env：至少填 QWEN_API_KEY，并修改 JWT_SECRET（生产必改）
```

### 2. 初始化数据目录

```bash
make init
```

创建 `./data/` 下的全部数据目录，并把 `postgres_data` 属主设为 `999`（容器内 postgres 用户的 uid）。
无 sudo 权限时会提示手动执行 `sudo chown -R 999:999 data/postgres_data`——属主不对，postgres 会启动失败。

### 3. 启动全部服务

```bash
make up        # 构建并启动（等价 docker compose -f docker-compose-full.yml up -d --build）
make up-nc     # 不重建镜像，直接启动
```

### 4. 访问

- 前端：http://localhost（`.env` 里 `WEB_PORT` 默认 80）
- 后端 API：http://localhost:8080/api/health
- 默认账号：admin / admin123

### 5. 查看状态与日志

```bash
make ps
make logs      # 跟踪全部服务
docker compose -f docker-compose-full.yml logs -f server   # 只看后端
```

### 6. 停止 / 清理

```bash
make down      # 停止并移除容器，数据保留在 ./data/
make clean     # 清理构建产物（server/bin、web/dist）
```

> 数据全部在 `./data/` 目录，删除该目录即等于 `down -v` 的清库效果（⚠️ 不可恢复）。

---

## 服务说明

| 服务 | 镜像/端口 | 说明 |
|------|-----------|------|
| postgres | pgvector/pgvector:pg16 / 5432 | PostgreSQL 16 + pgvector 向量扩展 |
| server | 自建 / 8080 | Go 后端 API 服务（含 FFmpeg 视频处理） |
| web | 自建 / 80 | Nginx 网关：前端静态资源 + `/api` 反向代理 + MCP SSE |

架构说明：`web` 是唯一对外入口（Nginx），浏览器只访问 `:80`；Nginx 把 `/api/` 代理到
`server:8080`，并按 `Upgrade` 头自动区分 WebSocket 与普通请求：
- WebSocket（流式对话 `/api/chat/ws`、SSH 终端 `/api/hosts/:id/terminal`、流式命令执行 `/api/hosts/:id/exec`）→ 转发 `Connection: upgrade`
- SSE / MCP 长连接 → 关闭缓冲，流式透传
视频/文件上传上限 200m，见 `web/nginx.conf` 的 `client_max_body_size`。

---

## 配置说明

### 后端配置项（环境变量覆盖 config.yaml）

所有配置项都可以通过环境变量覆盖，前缀 `AIAGENT_`，下划线分隔：

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| AIAGENT_APP_ENV | 运行环境 dev/production | production |
| AIAGENT_DATABASE_DSN | 数据库连接串（compose 内自动指向 postgres 服务） | - |
| AIAGENT_QWEN_APIKEY | 通义千问 API Key | - |
| AIAGENT_QWEN_CHATMODEL | 对话模型 | qwen-plus |
| AIAGENT_QWEN_EMBEDMODEL | 向量模型 | text-embedding-v3 |
| AIAGENT_QWEN_BASEURL | DashScope 兼容模式地址 | dashscope 官方 |
| AIAGENT_SECURITY_JWTSECRET | JWT 密钥 | aiagent-secret-key |
| AIAGENT_SECURITY_TOKENEXPIREHOURS | Token 有效期（小时） | 24 |
| AIAGENT_OBSERVABILITY_LOGEMBEDDING | 是否记录 Embedding 调用日志 | false |
| AIAGENT_SERVER_PORT | 服务端口 | 8080 |

对应 `.env` 键名：`QWEN_API_KEY`、`QWEN_CHAT_MODEL`、`QWEN_EMBED_MODEL`、
`QWEN_BASE_URL`、`JWT_SECRET`、`TOKEN_EXPIRE_HOURS`、`LOG_EMBEDDING`、`APP_ENV` 等，
完整清单见 `.env.example`。

### 数据持久化

所有宿主机侧数据统一放在 `./data/`（`make init` 创建，已 gitignore）：

| 目录 | 容器挂载点 | 说明 |
|------|-----------|------|
| `data/postgres_data` | `/var/lib/postgresql/data` | PostgreSQL 数据，属主必须为 `999` |
| `data/server_uploads` | `/app/uploads` | 上传的视频/文件 |
| `data/server_logs` | `/app/logs` | 后端日志 |
| `data/server_data` | `/app/data` | 后端产物，其中 `/app/data/output` 经 Nginx `/output/*` 对外访问（智能体用 `write_local_file` 生成的 HTML 等） |

**时区**：三个服务统一挂载 `/etc/localtime:/etc/localtime:ro`，跟随宿主机时区。

**健康检查**：`web` 以 `service_healthy` 依赖 `server`，因此 `server` 服务必须在 compose 里定义 `healthcheck`（当前为 `wget /api/health`，`start_period: 30s`）。若删掉该段，启动会报：

```
dependency failed to start: container aiagent-server has no healthcheck configured
```

---

## 视频处理说明

视频处理需要 FFmpeg，server 镜像已内置：

- 抽帧：每 10 秒一帧（短视频每 5 秒）
- 向量化：Qwen text-embedding-v3（1024 维）
- 向量存储：pgvector + ivfflat 索引
- 搜索方式：余弦相似度

---

## MCP 服务器

MCP 端点（经 Nginx 网关统一入口）：
- SSE：`/api/mcp/sse`
- 消息：`/api/mcp/messages`
- 直接调用：`/api/mcp/stream`

内置工具：
- `video_search` - 视频内容语义搜索
- `video_summary` - 获取视频摘要
- `list_agents` - 列出智能体
- `list_videos` - 列出视频
- `generate_report` - 生成报告

> 作为 MCP **客户端**接入第三方服务（如高德地图）时，请使用 `streamable_http` 传输 + `/mcp` 端点；
> `sse` 传输当前为简化实现，对标准 SSE 服务端会返回 404，详见 [README](./README.md#接入第三方-mcp-服务示例高德地图)。

---

## 维护命令

| 场景 | 命令 |
|------|------|
| 改了前端/后端代码后重新部署 | `make up` |
| 只重启服务（改配置） | `make restart` |
| 修改技能库种子提示词后重建 | `make db-reset-skills` |
| 重建向量索引 | `make reembed` |
| 查看全部命令 | `make help` |

### 前端更新须知

`web/Dockerfile` 是多阶段构建，镜像内执行 `npm install` + `npm run build`，**不依赖本地 `web/dist`**（已被 `.dockerignore` 排除）：

- 前端代码改动必须用 `make up`（带 `--build`）才会进镜像，`make up-nc` 不会生效
- 本地 `make build-web` 产物只用于预览，容器用不到
- 若页面内容没更新，先排查浏览器缓存，可执行 `docker compose -f docker-compose-full.yml exec web nginx -s reload`

### 后端产物（output）

智能体通过 `write_local_file` 生成的文件落在容器 `/app/data/output/`，映射到宿主机 `data/server_data/output/`，
经 Nginx 以 `/output/<路径>` 对外提供访问，可直接分享或下载。
