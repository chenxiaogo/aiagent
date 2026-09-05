# AI Agent 平台：开发 / 构建 / 部署命令
# 用法：make help
#
# 主线：完整部署（docker-compose-full.yml，pgvector + 后端 + Nginx 网关）
# 辅助：仅数据库（docker-compose.yml，本地开发用）
# 数据目录（make init 自动创建）
#   data/postgres_data    - 数据库数据（属主 999 = 容器内 postgres 用户）
#   data/server_uploads   - 上传文件    → 容器 /app/uploads
#   data/server_logs      - 应用日志    → 容器 /app/logs
#   data/server_data      - output 产物 → 容器 /app/data

.DEFAULT_GOAL := help

SHELL    := /bin/bash
GO       := go
COMPOSE  := sudo docker compose
FULL     := docker-compose-full.yml   # 完整部署：pgvector + 后端 + Nginx 网关
BASIC    := docker-compose.yml        # 仅数据库（本地开发用）
ENV_FILE := .env

.PHONY: help env init \
        up up-nc down restart ps logs db-reset-skills \
        db-up db-down db-logs db-ps \
        server web web-install \
        build build-web build-all \
        fmt vet test \
        reembed \
        clean

help: ## 列出所有命令
	@echo "AI Agent 平台命令："
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) | awk 'BEGIN{FS=":.*?## "}{printf "  %-14s %s\n", $$1, $$2}'

# ---------- 环境 / 初始化 ----------
env: ## 生成 .env（从 .env.example 复制，不存在时才生成）
	@if [ -f $(ENV_FILE) ]; then echo "$(ENV_FILE) 已存在，跳过"; else cp .env.example $(ENV_FILE) && echo "已生成 $(ENV_FILE)，请按需修改（务必改 JWT_SECRET）"; fi

init: ## 初始化数据目录（./data/，本地运行时 server/ 下目录由程序自动创建）
	@mkdir -p data/postgres_data data/server_uploads data/server_logs data/server_data
	@# pgvector 镜像内 postgres 用户 uid=999，属主不对时才 chown
	@[ "$$(stat -c %u data/postgres_data)" = "999" ] || sudo chown -R 999:999 data/postgres_data 2>/dev/null || echo "请手动：sudo chown -R 999:999 data/postgres_data"
	@echo "数据目录已就绪"

# ---------- 完整部署（docker-compose-full.yml，主线） ----------
up: ## 完整部署并构建镜像（首次先 make env && make init）
	$(COMPOSE) -f $(FULL) up -d --build

up-nc: ## 完整部署（不重建镜像）
	$(COMPOSE) -f $(FULL) up -d

down: ## 停止完整部署（保留数据卷）
	$(COMPOSE) -f $(FULL) down

restart: ## 重启完整部署（改配置后生效）
	$(COMPOSE) -f $(FULL) restart

ps: ## 查看完整部署状态
	$(COMPOSE) -f $(FULL) ps

logs: ## 跟踪完整部署日志
	$(COMPOSE) -f $(FULL) logs -f --tail=100

db-reset-skills: ## 清空技能库种子数据（后端重启后按最新 seed 重新写入）
	$(COMPOSE) -f $(FULL) exec -T postgres psql -U postgres -d aiagent -c "DELETE FROM skill_libraries;"
	$(COMPOSE) -f $(FULL) restart server
	@echo "技能库已清空，后端重启后重新写入种子提示词"

# ---------- 本地开发（仅数据库） ----------
db-up: ## 启动本地开发数据库（PostgreSQL + pgvector）
	$(COMPOSE) -f $(BASIC) up -d

db-down: ## 停止本地开发数据库（保留数据卷）
	$(COMPOSE) -f $(BASIC) down

db-logs: ## 查看数据库日志
	$(COMPOSE) -f $(BASIC) logs -f

db-ps: ## 查看数据库状态
	$(COMPOSE) -f $(BASIC) ps

server: ## 本地运行后端（先 make db-up；配置按 server/conf.d/config.yaml 相对路径读取）
	cd server && $(GO) run ./cmd/main.go

web-install: ## 安装前端依赖
	cd web && npm install

web: ## 本地运行前端（Vite dev，默认 5173，已代理 /api 到 8080）
	cd web && npm run dev

# ---------- 构建 ----------
build: ## 编译后端二进制（输出 server/bin/aiagent）
	cd server && $(GO) build -o bin/aiagent ./cmd/main.go

build-web: ## 构建前端产物（输出 web/dist）
	cd web && npm run build

build-all: build build-web ## 编译后端 + 构建前端

# ---------- 代码质量 ----------
fmt: ## gofmt 格式化后端代码
	cd server && $(GO) fmt ./...

vet: ## go vet 静态检查后端代码
	cd server && $(GO) vet ./...

test: ## 运行后端测试
	cd server && $(GO) test ./...

# ---------- 辅助 ----------
reembed: ## 重建向量索引（全量 re-embed，需先启动数据库）
	cd server && $(GO) run ./cmd/reembed/main.go

clean: ## 清理构建产物（server/bin、web/dist）
	rm -rf server/bin web/dist
