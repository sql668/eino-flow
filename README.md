# EinoFlow

基于字节 CloudWeGo Eino 内核的高性能可扩展 AI 智能体工作流平台，对标 Dify。

## 技术栈

- 前端：Next.js 14+（App Router）· React 18 · TypeScript · @xyflow/react · Zustand · TailwindCSS + shadcn/ui
- 后端：Go 1.25 · CloudWeGo Hertz · CloudWeGo Eino
- 存储：PostgreSQL（业务）· Milvus（向量）· MinIO（对象）· Redis（缓存 / 会话）
- 可观测：OpenTelemetry + Eino Callback

## 目录结构

```
.
├── backend/     # Go + Hertz + Eino 后端
├── web/         # Next.js 前端
├── deploy/      # 本机重排服务、模型缓存等部署辅助
└── docker-compose.yml
```

> 需求文档、开发计划与进度台账放在本机的 `docs/` 目录（暂未纳入版本管理），
> 因此 clone 下来的仓库里没有这个目录。

## 快速开始

```bash
# 1. 启动本地依赖（PostgreSQL / Redis / MinIO）
make up

# 2. 准备后端配置（可选，密钥走环境变量）
cp .env.example .env

# 3. 启动后端（默认 http://127.0.0.1:8888）
make backend-run

# 4. 启动前端（待 web/ 初始化后可用）
make web-install && make web-dev
```

后端健康检查：`GET /healthz`、`GET /readyz`。

### 模型服务（RAG 必需）

后端启动时会校验向量化模型与向量集合，**模型不可用会直接启动失败**（避免「看起来在跑但检索总是空」）。
本地默认用 Ollama + `bge-m3`（1024 维）：

```bash
ollama pull bge-m3:latest          # 模型必须在本地
curl http://127.0.0.1:11434/api/tags   # 确认 Ollama 在跑
```

模型地址 / 模型名 / 维度来源有两条路径，**数据库配置优先，配置文件兜底**：

1. 数据库配置（推荐）：前端「模型配置」页（`/settings/models`）保存厂商凭证并绑定模型；
2. 配置文件兜底：`backend/configs/config.yaml` 的 `embedding` 一节，用于「还没配置任何模型」时的启动。

维度不在配置里写死，而是启动时向服务端探测，并与已有 Milvus 集合比对，不一致直接报错。
换 embedding 模型后必须重建集合（删除 `einoflow_segments` 后重启后端会自动重建）。

模型凭证以 AES-256-GCM 加密存储，**必须配置加密密钥**，否则后端拒绝启动：

```bash
# 生成并写入 .env（.env 已被 gitignore，不会进仓库）
printf 'EINOFLOW_SECRET_KEY=%s\n' "$(openssl rand -base64 32)" >> .env
```

注意：换掉这个密钥，已保存的厂商凭证将无法解密，需要在界面上重新填写。

### 重排（可选，两阶段检索的第二阶段）

检索默认是「向量召回 20 条 → 重排取前 5 条」。重排需要一个 cross-encoder 模型
（Ollama 不提供 rerank 接口，见 D5）。**两条路协议一致，按你的机器选一条**：

| 你的环境 | 怎么做 | 说明 |
| --- | --- | --- |
| Linux x86_64（含云服务器） | `docker compose --profile rerank up -d tei` | 官方 TEI 镜像，原生执行，无需装 Python |
| macOS（Apple Silicon / Intel） | `deploy/rerank-local/bootstrap.sh && deploy/rerank-local/run.sh start` | PyTorch 直接调用本机 GPU（Metal / CUDA），也可退到 CPU |
| 都不想装 | 不启动，并设 `EINOFLOW_RETRIEVAL_RERANK=off` | 检索照常可用，只是少一层精排 |

**Apple Silicon 上不要用容器里的 TEI**：该镜像只发布 linux/amd64，容器里也拿不到 AVX2，
只能走标量内核。实测同一模型、同样 2 条候选：模拟环境 **216 秒**，本机 MPS 服务 **0.52 秒**。

另外重排开销与候选数近似线性，机器弱就调小 `retrieval.candidate_top_k`（默认 20），
或换更小的模型（如 `BAAI/bge-reranker-base`）。请求里可以用 `"rerank": true / false`
单独控制某一次检索是否重排，返回同时带 `vectorScore` 与 `rerankScore` 便于对比两阶段效果。

## 本地依赖说明

| 组件 | 地址 | 说明 |
| --- | --- | --- |
| PostgreSQL | 5432 | 业务数据 |
| Redis | 6379 | 会话 / 缓存 |
| MinIO | 9000（控制台 9001） | 业务对象存储 bucket `einoflow`，Milvus 后端 bucket `milvus` |
| Milvus | 19530（gRPC）、9091（指标） | 向量检索 |
| Attu | 3000 | Milvus 可视化管理台 |
| MinerU | 8000（V1 API，健康检查 `/v1/health`） | 文档解析（版面分析 + OCR），在 `rag` profile 下 |

- 全部由根目录 `docker-compose.yml` 编排：`make up` 启动，`make ps` 查看状态，`make milvus-status` 查看 Milvus 侧。
- 文档解析服务 MinerU 较重（首次需构建镜像，约 1GB），放在 `rag` profile：`make up-rag` 一并启动。
- 前端 dev 端口固定为 **3001**，因为 3000 已被 Attu 占用。
- 如果你本机已有别的 Milvus 编排在用 `19530 / 9091 / 3000` 端口，先到那套编排的目录执行
  `docker compose down`，再启动本项目。

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
