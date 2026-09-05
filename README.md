# AI Agent 平台

面向运维与业务场景的 **智能体（Agent）平台**：把大模型、工具（MCP / 技能 / 主机命令）、知识库与多模态数据（文档、视频、摄像头事件）组装成可发布、可交付、可观测的智能体，并通过 **MCP 端点**对外提供 Agent-as-a-Service 能力。

一个智能体 = 一套模型配置 + 一组工具/技能 + 一批绑定数据 + 一个对外发布版本 + 一套客户凭据。

---

## 核心能力

| 业务域 | 说明 | 关键入口 |
|--------|------|----------|
| **智能体** | 智能体 CRUD、启停、API Key、预设问题、版本发布与回滚、多模型绑定、资源绑定（知识库/视频源/摄像头隔离） | `server/internal/handler/agent.go`、`agent_delivery.go`、`agent_model.go`、`agent_resource.go` |
| **Agent 运行时** | 多轮工具调用、ReAct 式执行、流式事件（thinking / tool_call / tool_result / text）、危险操作人工确认、多租户作用域隔离 | `server/internal/agent/`、`service/agent_core.go`、`service/agent_runtime.go`、`internal/approval/` |
| **对话与流式** | SSE 流式对话、WebSocket 流式对话（支持中途 stop）、会话/消息管理、作用域化记忆（会话摘要 / 用户档案 / 可检索事件） | `handler/chat.go`、`/api/chat/ws`、`internal/memory/` |
| **运维主机** | 主机与主机组管理、SSH Web 终端（xterm）、流式命令执行、远程文件管理、命令记录与操作审计 | `handler/host.go`、`host_file.go`、`pkg/sshx/` |
| **能力市场** | MCP 注册表、技能库、工具库、Agent 模板、提示词库、知识库 | `handler/market.go`、`handler/knowledge.go` |
| **运行观测** | 调用观测（LLM/向量调用日志与汇总）、模型目录与单价、模型路由规则 | `handler/market.go`（`/api/ops/call-logs`、`/api/market/models`、`/api/market/routing`） |
| **数据与检索** | 文档/视频/摄像头数据的向量索引与混合检索；Agent `doc_search` 走检索增强链路（查询改写→聚合压缩带出处→可选重排） | `handler/file.go`、`video.go`、`camera.go`、`service/indexer.go`、`service/ffmpeg.go`、`internal/knowledge/retriever.go` |
| **对外交付** | 客户管理（客户即平台用户）、版本发布、访问凭据（API Key）、产品套餐订阅、用量计量 | `handler/agent_delivery.go`、`handler/tenant.go` |
| **MCP 对外服务** | 平台级 MCP 端点 + 智能体级 MCP 端点（客户 API Key 鉴权，按快照暴露工具） | `internal/mcp/`、`handler/mcp.go`、`handler/agent_mcp.go` |
| **权限与系统设置** | RBAC（用户/角色/权限点/菜单/按钮/受管接口 + 自研 Casbin 策略）、大模型配置、菜单管理、接口管理、索引维护 | `pkg/rbac/`、`pkg/casbin/`、`handler/{user,role,menu,api}.go`、`handler/model_config.go`、`handler/reindex.go` |

---

## 技术栈

**后端（Go 1.25）**
- Web：Gin、gorilla/websocket、JWT（`golang-jwt/jwt/v5`）
- 数据：GORM + PostgreSQL / **pgvector**（向量检索，ivfflat 索引）
- 模型编排：CloudWeGo **Eino** + eino-ext OpenAI 兼容组件（默认对接阿里 DashScope / Qwen）
- 其他：viper（配置，支持热更新与 `AIAGENT_*` 环境变量覆盖）、zap + lumberjack（日志）、FFmpeg（视频抽帧）、SSH（运维主机）

**前端（Vue 3）**
- Vite 6 + Vue 3.5 + Vue Router（hash 模式）+ Pinia
- Element Plus + `@element-plus/icons-vue`
- xterm（Web 终端）、marked + highlight.js（Markdown 渲染）

**前端路由**：后端菜单驱动的动态路由（`web/src/router/index.js` 按登录用户菜单树注册路由），侧边栏与按钮权限同源。

---

## 目录结构

```
aiagent/
├── server/                  # Go 后端
│   ├── cmd/main.go          # 入口
│   ├── internal/
│   │   ├── handler/         # HTTP Handler（路由注册与请求处理）
│   │   ├── service/         # 业务逻辑（Agent 运行时、向量、视频、模型、运维工具…）
│   │   ├── store/           # 数据仓库（GORM 持久化 + 权限种子数据）
│   │   ├── model/           # 数据模型与权限常量
│   │   ├── middleware/      # 鉴权、Casbin、链路追踪、客户端凭据
│   │   ├── agent/           # Agent 运行时（编排、作用域）
│   │   ├── toolkit/         # 工具注册与人工确认
│   │   ├── knowledge/ memory/ approval/ document/ mcp/ mcpclient/
│   ├── pkg/                 # 可复用基础包（app / auth / casbin / database / ilog / rbac / sshx / tracex / shutdown）
│   ├── conf.d/config.yaml   # 配置文件
│   ├── tests/               # 测试与核对脚本（Python）
│   └── Dockerfile
├── web/                     # Vue3 前端
│   ├── src/{api,views,components,stores,router,layouts,directives,utils}/
│   ├── nginx.conf           # 部署用 Nginx 网关配置
│   └── Dockerfile
├── docker-compose.yml       # 仅数据库（本地开发用）
├── docker-compose-full.yml  # 完整部署：pgvector + Go 后端 + Nginx 网关
├── Makefile                 # 开发 / 构建 / 部署命令入口（make help 查看全部）
├── data/                    # 部署数据目录（make init 创建，已 gitignore）
├── docs/                    # 文档（培训教材、排障手册）
├── DEPLOY.md                # 部署指南
└── README.md
```

---

## 快速开始

### 1. Docker 完整部署（推荐）

```bash
make env      # 首次：由 .env.example 生成 .env（至少填 QWEN_API_KEY，务必改 JWT_SECRET）
make init     # 首次：创建数据目录 ./data/，并为 postgres_data 设好属主
make up       # 构建并启动全部服务
```

等价原生命令：`cp .env.example .env && docker compose -f docker-compose-full.yml up -d --build`

- 前端入口：http://localhost（`.env` 里 `WEB_PORT` 默认 80）
- 后端健康检查：http://localhost:8080/api/health
- 默认账号：`admin` / `admin123`（首次启动自动创建，登录后请立即改密）

架构：`web`（Nginx，唯一对外入口）→ `/api` 代理到 `server:8080`，并按 `Upgrade` 头自动区分 WebSocket 与 SSE；数据落在 pgvector。

### 2. 本地开发

```bash
make db-up     # 仅启动数据库（PostgreSQL + pgvector）
make server    # 后端（工作目录必须是 server/，配置按相对路径 conf.d/config.yaml 读取）
make web       # 前端（默认 5173，已配置 /api 代理到 8080，含 WebSocket）
```

等价原生命令：`docker compose up -d` / `cd server && go run ./cmd/main.go` / `cd web && npm run dev`

### 3. 前端构建

```bash
make build-web   # 产物 web/dist（容器构建在镜像内完成，本地 dist 不进镜像）
```

---

## 常用命令

`make help` 列出全部命令，常用项：

| 命令 | 说明 |
|------|------|
| `make env` | 由 `.env.example` 生成 `.env`（已存在则跳过） |
| `make init` | 创建数据目录并修正 `postgres_data` 属主 |
| `make up` / `make up-nc` | 完整部署（构建镜像）/ 不重建镜像直接启动 |
| `make down` / `make restart` | 停止（保留数据）/ 重启 |
| `make ps` / `make logs` | 查看状态 / 跟踪日志 |
| `make db-reset-skills` | 清空技能库种子数据，后端重启后按最新 seed 重写 |
| `make db-up` / `make db-down` | 本地开发用数据库启停 |
| `make server` / `make web` | 本地运行后端 / 前端 |
| `make build` / `make build-web` / `make build-all` | 编译后端 / 构建前端 / 两者 |
| `make fmt` / `make vet` / `make test` | 格式化 / 静态检查 / 测试 |
| `make reembed` | 重建向量索引 |
| `make clean` | 清理 `server/bin`、`web/dist` |

> 改了前端或后端代码后要用 `make up`（带 `--build`），`make up-nc` 不会重建镜像。

---

## 数据目录

部署数据统一放在 `./data/`（由 `make init` 创建，已加入 `.gitignore`）：

| 目录 | 容器挂载点 | 说明 |
|------|-----------|------|
| `data/postgres_data` | `/var/lib/postgresql/data` | PostgreSQL 数据，属主需为 `999`（容器内 postgres 用户） |
| `data/server_uploads` | `/app/uploads` | 上传的文档与视频 |
| `data/server_logs` | `/app/logs` | 后端日志 |
| `data/server_data` | `/app/data` | 后端产物（如 `output/` 下生成的 HTML，经 `/output/*` 对外访问） |

本地运行（`make server`）时，`server/` 下的 `data/`、`uploads/`、`logs/` 由程序自动创建，与部署数据隔离。

时区统一跟随宿主机（compose 挂载 `/etc/localtime`），不再固定 `Asia/Shanghai`。

---

## 配置

主配置 `server/conf.d/config.yaml`，全部支持 `AIAGENT_` 前缀环境变量覆盖（下划线分隔层级），常用项：

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| `AIAGENT_DATABASE_DSN` | PostgreSQL 连接串 | 见 config.yaml |
| `AIAGENT_QWEN_APIKEY` | DashScope API Key | 空 |
| `AIAGENT_QWEN_CHATMODEL` | 对话模型 | qwen-plus |
| `AIAGENT_QWEN_EMBEDMODEL` | 向量模型 | text-embedding-v3 |
| `AIAGENT_QWEN_BASEURL` | 模型服务地址 | DashScope 兼容模式 |
| `AIAGENT_SECURITY_JWTSECRET` | JWT 密钥 | aiagent-secret-key |
| `AIAGENT_SECURITY_TOKENEXPIREHOURS` | Token 有效期（小时） | 24 |
| `AIAGENT_OBSERVABILITY_LOGEMBEDDING` | 记录向量调用日志 | false |
| `AIAGENT_SERVER_PORT` | 后端端口 | 8080 |

完整 `.env` 示例见 `.env.example`，部署细节见 [DEPLOY.md](./DEPLOY.md)。

---

## 权限体系

- **权限点**（部分）：`task:*`（智能体）、`node:view/manage`（运维主机）、`host:exec`（命令执行）、`host:file`（主机文件）、`market:view/manage`（能力市场）、`ops:view`（运行观测）、`user:manage`、`role:manage`、`system:admin`
- **内置角色**：`admin`（全量）、`operator`（业务操作，排除系统管理类）、`viewer`（只读）
- **校验层次**：JWT 鉴权 → Casbin 受管接口 → Handler 级 `RequirePerm` → 前端菜单/按钮权限码
- **菜单**：后端菜单树下发（`GET /api/menus/my`），内置目录为「智能体 / 能力市场 / 运行观测 / 系统设置」；运维主机不进平台侧边栏，在运维型智能体工作台的主机面板中维护

---

## 知识库检索增强

Agent 通过 `doc_search` 工具检索授权知识库时，走一条检索增强链路（`server/internal/knowledge/retriever.go` + `service/agent_runtime.go`），缓解「召回片段直接堆砌、回答笼统模糊」的问题：

1. **查询改写**：用对话模型把用户口语化/模糊的问题改写成利于向量召回的精准 query（LLM 调用失败则回落原 query）。
2. **混合检索**：向量召回 + 全文检索加权排序（`store.HybridSearchInKBs`），命中片段经 `CleanSearchResults` 去噪去重。
3. **重排（可选）**：已预留 `WithRerank` 接口与 `rerank` 模型角色；接入 rerank 模型 client 后按交叉编码重排召回结果（当前默认走加权排序）。
4. **聚合压缩**：用对话模型把命中片段提炼成**带出处的要点**（「来源：文件名 页码/行号」），仅依据所给片段、不编造片段外内容。

`doc_search` 的观察结果在 `observationToText` 中**绕过 facts 压缩、直接透传**（截断上限 1400 字），避免 300 字截断丢失细节与引用。改写与聚合每次检索会多 2 次对话模型调用（已落调用观测日志），任一环节失败均回落到「原文截断 + 文件名」的朴素链路，不会中断检索。

> 接入真实 rerank：实现 rerank 模型 client 后，在 `agent_runtime.go` 的 `builtinToolHandlers` 里 `knowledge.WithRerank(...)` 注入即可，检索逻辑无需改动。

---

## MCP 对外服务

| 端点 | 鉴权 | 说明 |
|------|------|------|
| `GET /api/mcp/sse`、`POST /api/mcp/messages`、`POST /api/mcp/stream` | 无（平台级） | 平台内置工具 |
| **`POST /api/mcp?key=xxx`**、**`GET /api/mcp?key=xxx`**、**`POST /api/mcp/rpc?key=xxx`** | 客户 API Key | **推荐**，按发布快照暴露该智能体的工具 |
| `GET /api/mcp/agents/:slug/sse`、`POST /api/mcp/agents/:slug/messages`、`/stream` | 客户 API Key | 旧式按 slug 寻址，保留以兼容已接入客户 |

API Key 可通过 **`?key=`**（与高德 MCP 一致，推荐）、`Authorization: Bearer`、`X-Api-Key` 或 `?api_key=` 传入。

对外接入统一为「单端点 + URL 携带 key」的形态，与高德 MCP 一致，一条 URL 即可接入、无需自定义请求头：

```bash
curl -X POST "https://你的域名/api/mcp?key=你的API-Key" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
```

```json
{
  "mcpServers": {
    "我的智能体": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "https://你的域名/api/mcp?key=你的API-Key"]
    }
  }
}
```

key 即凭据，凭据本身已绑定智能体与钉住的版本，因此 URL 里无需再写 slug。

---

## 接入第三方 MCP 服务（示例：高德地图）

平台既可作为 MCP Server 对外提供能力，也可作为 **MCP 客户端**连接外部 MCP 服务器（如高德地图），把外部工具注册进智能体。配置层（`model.AgentMCPServer`）+ 运行期转发（`service/agent_runtime.go` 的 `RegisterTools`）已打通：`internal/mcpclient` 支持 `streamable_http` / `sse` 两种传输，并自动完成 `initialize` 握手与会话（`Mcp-Session-Id`）管理，兼容有状态与无状态服务端。

以高德 MCP 为例：

1. 在高德开放平台创建应用，拿到 Key；
2. 在「智能体 → MCP 配置」新建一条 `AgentMCPServer`：
   - Transport：`streamable_http`
   - URL：`https://mcp.amap.com/mcp?key=你的Key`（Key 也可放 Headers 的 `Authorization`）
   - Enabled：true
3. 运行期会自动 `ListTools`，把高德工具（天气 / 路线规划 / 打车 / POI 搜索 / 专属地图绘制等）注册为 `mcp_amap_xxx` 并转发调用，智能体即可在对话中编排使用。

验证连通性（无需落库，直接打高德端点）：

```bash
export AMAP_MCP_KEY=你的Key
cd server && go run ./mcp_probe   # 打印高德暴露的工具清单
```

> 注意：外部 MCP 工具默认标记「有副作用、需人工确认」，只读类工具（天气 / POI）也会弹确认。若要让旅行规划类场景全自动运行，需为只读 / 可信 MCP server 放开审批（见审批策略）。

**已知限制（`sse` 传输）**：`sse` 目前是简化实现——直接对同一端点 POST JSON-RPC（见 `internal/mcpclient/client.go` 的 `doRequest`），未按 MCP 规范先 `GET /sse` 建立事件流、再从 `event: endpoint` 取消息端点后 POST。对高德这类标准 SSE 服务端，`/sse` 只接受 GET，POST 会返回 404：

```
MCP 返回 404: {"path":"/sse","status":404,"error":"Not Found",...}
```

因此接入高德等标准服务端时，请按上表使用 **`streamable_http` + `/mcp` 端点**，不要填 `/sse`（两者是不同端点，不是同一个 URL 换传输类型）。

---

## 内置提示词技能（能力市场）

首次启动会把以下提示词写入技能库（`store/seedSkillLibrary`），智能体在「技能」中引用即可；`Summary` 字段决定该技能的触发时机。

| 技能 | 分类 | 触发场景 | 要点 |
|------|------|---------|------|
| 出行规划助手 | 出行 | 用户提出旅行 / 出游 / 行程安排需求 | 补齐必要信息 → 结合时节与气候排程（雨天/晴天双方案）→ 按天时间轴表格（交通方式、耗时、门票预约）→ 预算明细 → 需要时用 `write_local_file` 生成可视化 HTML 并返回 `/output/` 链接 |
| 运维助手 | 运维 | 排查主机/服务故障、查看 CPU/内存/磁盘/网络/进程/服务状态、执行运维命令 | 只读优先（`host_*` 系列工具）、最小变更、可回滚；危险命令先说明影响并等待确认；结论先行 + 证据 + 分步命令标注风险 |
| 文件检索助手 | 检索 | 针对知识库文档提问、需要出处 | 拆解问题多角度召回、关键结论交叉验证；每条结论标注来源文档名；检索不到明确说明，严禁编造 |

工具类技能（非提示词）：视频搜索工具集、知识库检索工具集。

> 种子数据只在表为空时写入。修改 `seedSkillLibrary` 后执行 `make db-reset-skills` 清空重建（会重建 ID，已引用旧技能的智能体需重新挂载）。

---

## 测试与脚本

| 脚本 | 用途 |
|------|------|
| `server/tests/mcp_smoke_test.py` | MCP 端到端冒烟：登录→挑已发布智能体→建临时凭据→跑 initialize/tools/list/tools/call 与 401 校验→清理 |
| `server/tests/mcp_stdio_bridge.py` | stdio ↔ HTTP 桥接，供只支持 stdio 的客户端（Claude Desktop / Cursor）接入 |
| `server/tests/tenant_user_check.py` | 核对「客户 ↔ 系统用户」对齐情况 |

---

## 相关文档

- [DEPLOY.md](./DEPLOY.md) — 部署、数据目录、Nginx 网关与 WebSocket/SSE 配置
- [docs/AI-Agent高级开发培训.md](./docs/AI-Agent高级开发培训.md) — 基于真实代码的部门培训教材（架构 / 工具 / MCP / Skills / 记忆 / 安全治理）
- [docs/排障手册.md](./docs/排障手册.md) — 常见问题与排查步骤
