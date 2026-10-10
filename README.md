# EinoFlow

基于字节 CloudWeGo Eino 内核的高性能可扩展 AI 智能体工作流平台，对标 Dify。

## 技术栈

- 前端：Next.js 14+（App Router）· React 18 · TypeScript · @xyflow/react · Zustand · TailwindCSS + shadcn/ui
- 后端：Go 1.25 · CloudWeGo Hertz · CloudWeGo Eino
- 存储：MySQL（业务）· Milvus（向量）· MinIO（对象）· Redis（缓存 / 会话）
- 可观测：OpenTelemetry + Eino Callback

## 目录结构

```
.
├── backend/     # Go + Hertz + Eino 后端
├── web/         # Next.js 前端
├── docs/        # PRD 与开发计划
└── docker-compose.yml
```

## 快速开始

```bash
# 1. 启动本地依赖（MySQL / Redis / MinIO）
make up

# 2. 准备后端配置（可选，密钥走环境变量）
cp .env.example .env

# 3. 启动后端（默认 http://127.0.0.1:8888）
make backend-run

# 4. 启动前端（待 web/ 初始化后可用）
make web-install && make web-dev
```

后端健康检查：`GET /healthz`、`GET /readyz`。

## 本地依赖说明

| 组件 | 地址 | 说明 |
| --- | --- | --- |
| MySQL | 3306 | 业务数据 |
| Redis | 6379 | 会话 / 缓存 |
| MinIO | 9000（控制台 9001） | 业务对象存储 bucket `einoflow`，Milvus 后端 bucket `milvus` |
| Milvus | 19530（gRPC）、9091（指标） | 向量检索 |
| Attu | 3000 | Milvus 可视化管理台 |

- 全部由根目录 `docker-compose.yml` 编排：`make up` 启动，`make ps` 查看状态，`make milvus-status` 查看 Milvus 侧。
- 前端 dev 端口固定为 **3001**，因为 3000 已被 Attu 占用。
- 本机另有一套独立 Milvus 编排（`/Users/shiqingliang/Documents/docker-app/milvus/`），端口重叠，
  启动本项目前请先在其目录执行 `docker compose down`。

## 公网演示

当前演示地址：**https://einoflow.sql668.com**（Cloudflare 隧道 → 本地完整栈）

用 Cloudflare 隧道把本地完整栈暴露到公网，供演示访问：

```bash
make backend-run     # 终端 1：后端
make web-start       # 终端 2：前端（生产模式）
make tunnel-up       # 终端 3：固定域名隧道（需先配 CF_TUNNEL_TOKEN）
```

配置步骤与注意事项见 [deploy/cloudflared/README.md](deploy/cloudflared/README.md)。

## 开发约定

开发规则见 `AGENTS.md`（项目级）、`web/AGENTS.md`（前端）、`backend/AGENTS.md`（后端）。
所有 AI 编排逻辑必须在后端 `internal/engine`，前端只负责交互与 DSL 序列化。
