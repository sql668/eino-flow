# EinoFlow 项目级命令入口（见根 AGENTS.md）。
# 后端：Go 1.25 + Hertz + Eino；前端：Next.js + pnpm。

BACKEND_DIR := backend
WEB_DIR     := web

# 载入 .env（存在时），供 tunnel-* 等目标读取 CF_TUNNEL_TOKEN。
ifneq (,$(wildcard .env))
include .env
export
endif

.PHONY: help
help: ## 显示所有可用命令
	@# 只扫本 Makefile：include .env 后 MAKEFILE_LIST 会有多个文件，grep 会加上文件名前缀导致输出错乱。
	@grep -E '^[a-zA-Z0-9_-]+:.*?## .*$$' $(firstword $(MAKEFILE_LIST)) | awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

# ---------- 后端 ----------
.PHONY: backend-run
backend-run: ## 启动后端服务
	cd $(BACKEND_DIR) && go run ./cmd/server

.PHONY: backend-build
backend-build: ## 编译后端二进制到 backend/bin/server
	cd $(BACKEND_DIR) && go build -o bin/server ./cmd/server

.PHONY: backend-tidy
backend-tidy: ## 整理后端依赖
	cd $(BACKEND_DIR) && go mod tidy

.PHONY: backend-test
backend-test: ## 运行后端测试
	cd $(BACKEND_DIR) && go test ./...

.PHONY: backend-check
backend-check: ## 后端自检：格式化 + vet + 构建 + 测试
	cd $(BACKEND_DIR) && gofmt -l . && go vet ./... && go build ./... && go test ./...

# ---------- 前端 ----------
.PHONY: web-install
web-install: ## 安装前端依赖（pnpm）
	cd $(WEB_DIR) && pnpm install

.PHONY: web-dev
web-dev: ## 启动前端开发服务器
	cd $(WEB_DIR) && pnpm dev

.PHONY: web-build
web-build: ## 构建前端
	cd $(WEB_DIR) && pnpm build

.PHONY: web-start
web-start: ## 生产模式启动前端（先构建后运行，演示更稳）
	cd $(WEB_DIR) && pnpm build && pnpm exec next start -p 3001

.PHONY: web-check
web-check: ## 前端自检：lint + 类型检查 + 构建
	cd $(WEB_DIR) && pnpm lint && pnpm exec tsc --noEmit && pnpm build

# ---------- 本地基础设施 ----------
.PHONY: up
up: ## 启动全部本地依赖（PostgreSQL / Redis / MinIO / etcd / Milvus / Attu）
	docker compose up -d

.PHONY: up-rag
up-rag: ## 启动完整栈（额外含 MinerU 文档解析服务，首次需构建镜像）
	docker compose --profile rag up -d

.PHONY: ps
ps: ## 查看本项目容器状态
	docker compose ps

.PHONY: milvus-status
milvus-status: ## 查看 Milvus 相关容器状态（Milvus / Attu）
	docker ps --filter name=milvus --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'

.PHONY: down
down: ## 停止本地依赖
	docker compose down

.PHONY: clean
clean: ## 清理构建产物
	rm -rf $(BACKEND_DIR)/bin

# ---------- 公网演示（Cloudflare 隧道，详见 deploy/cloudflared/README.md） ----------
.PHONY: start stop restart status logs
start: ## 一键启动（仅本地）：依赖容器 + 后端 + 前端
	./scripts/einoflow.sh start

.PHONY: start-tunnel
start-tunnel: ## 一键启动并开启公网隧道
	./scripts/einoflow.sh start --tunnel

stop: ## 一键停止：前端 + 后端 + 隧道 + 依赖容器
	./scripts/einoflow.sh stop

restart: ## 一键重启
	./scripts/einoflow.sh restart

status: ## 查看整体运行状态
	./scripts/einoflow.sh status

logs: ## 查看日志：make logs ARG=backend
	./scripts/einoflow.sh logs $(ARG)

.PHONY: tunnel-quick
tunnel-quick: ## 启动临时隧道（随机 trycloudflare 地址，免账号）
	@docker rm -f einoflow-quick-tunnel >/dev/null 2>&1 || true
	docker run -d --name einoflow-quick-tunnel cloudflare/cloudflared:latest tunnel --url http://host.docker.internal:3001
	@sleep 10 && echo "公网地址：" && docker logs einoflow-quick-tunnel 2>&1 | grep -oE 'https://[a-z0-9-]+\.trycloudflare\.com' | head -1

.PHONY: tunnel-up
tunnel-up: ## 启动固定域名隧道（需在 .env 配置 CF_TUNNEL_TOKEN）
	@test -n "$(CF_TUNNEL_TOKEN)" || { echo "错误：请先在 .env 配置 CF_TUNNEL_TOKEN，步骤见 deploy/cloudflared/README.md"; exit 1; }
	@docker rm -f einoflow-tunnel >/dev/null 2>&1 || true
	@# 加 @ 前缀是为了不回显 token，避免它留在终端历史里。
	@docker run -d --name einoflow-tunnel --restart unless-stopped cloudflare/cloudflared:latest tunnel --no-autoupdate run --token $(CF_TUNNEL_TOKEN) >/dev/null
	@echo "隧道已启动，用 make tunnel-logs 查看连接状态"

.PHONY: tunnel-logs
tunnel-logs: ## 查看隧道日志
	docker logs --tail 50 einoflow-tunnel

.PHONY: tunnel-down
tunnel-down: ## 停止隧道
	@docker rm -f einoflow-tunnel >/dev/null 2>&1 || true
	@docker rm -f einoflow-quick-tunnel >/dev/null 2>&1 || true
	@echo "隧道已停止"
