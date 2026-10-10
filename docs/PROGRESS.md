# EinoFlow 开发进度与模块台账

> **本文档是后续开发的入口**。记录模块划分、完成情况、未完成事项、已定技术决策与已知限制。
> 每个开发阶段结束时必须更新，避免「代码在跑但没人知道还差什么」。
>
> 更新约定：
> - 新增 / 完成 / 放弃一个模块，更新第 3 节状态表；
> - 做出影响架构的决定（选型、协议、目录约定），追加到第 5 节并写明理由；
> - 发现限制或债务，追加到第 6 节，不要口头说说过就算；
> - 状态标记：✅ 完成 · 🚧 进行中 · ⬜ 未开始 · ⏸️ 挂起（附原因）。

最后更新：2026-10-10（阶段 1 完成）

## 1. 当前阶段

**V0.1 极速 Demo（进行中，约完成 75%）**

V0.1 的目标是「表单式配置跑通：创建应用 → 上传知识库 → ReAct/RAG 问答 → 流式对话」。
按 PRD 的阶段门禁，**不提前开发拖拽画布、插件市场、MCP、多智能体、完整观测面板**。

当前卡点：RAG 链路（解析 → 切片 → 向量化 → 检索 → 溯源）已端到端跑通并实测通过，
剩下的是模型配置模块（阶段 2）、重排（阶段 3）、检索追踪可视化（阶段 4）与 Eino 内核接入 + SSE 对话。

## 2. 规模现状

| 项 | 数值 |
| --- | --- |
| 后端 Go 代码 | 约 8100 行 / 69 个文件 |
| 前端 TS/TSX | 372 行（仅骨架） |
| 已实现 HTTP 路由 | 20 条 |
| 单元与集成测试 | 62 个用例，6 个测试包全绿 |
| 数据库迁移 | 0001_init、0002_segment_metadata、0003_retrieval_trace |

## 3. 模块清单与状态

### 3.1 工程与基础设施

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 项目规则 | ✅ | `AGENTS.md` · `web/AGENTS.md` · `backend/AGENTS.md` | 三层规则就近生效 |
| 一键启停脚本 | ✅ | `scripts/einoflow.sh` | start / stop / restart / status / logs / tunnel |
| 本地依赖编排 | ✅ | `docker-compose.yml` | PostgreSQL · Redis · MinIO · etcd · Milvus · Attu ·（rag profile）MinerU |
| 公网演示 | ✅ | `deploy/cloudflared/` | Cloudflare 隧道，https://einoflow.sql668.com |
| MinerU 服务镜像 | ✅ | `deploy/mineru/Dockerfile` | 自建 CPU 版（官方镜像强制 NVIDIA GPU） |

### 3.2 后端基础层

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 配置加载 | ✅ | `internal/config` | YAML + 环境变量覆盖 + 校验 |
| 数据库迁移器 | ✅ | `internal/repository/postgres/migrate.go` | 按语句拆分、单事务执行、版本记录 |
| 数据访问分层 | ✅ | `internal/repository` | 接口在 repository，GORM 仅在 `repository/postgres` |
| 事务支持 | ✅ | `internal/repository/tx.go` | `TxManager` + context 透传，跨仓储事务无需传参 |
| 对象存储 | ✅ | `internal/repository/minio` | S3 兼容协议，启动自动建 bucket |
| 健康检查 | ✅ | `internal/handler/health.go` | `/healthz` 不查依赖；`/readyz` 查真实依赖 |

### 3.3 业务 API

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 应用管理 | ✅ | `handler/app.go` + 四层 | CRUD、软删除、过滤、分页 |
| 知识库管理 | ✅ | `handler/dataset.go` + 四层 | CRUD、软删除、关键词过滤 |
| 会话与消息 | ✅ | `handler/conversation.go` + 四层 | 会话硬删除 + 消息级联删除（事务） |
| 文档上传与查询 | ✅ | `handler/document.go` + 四层 | multipart 上传、异步入库、分段查询 |

### 3.4 RAG 管线

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 文档解析抽象（IR） | ✅ | `engine/rag/loader/document.go` | `ParsedDocument` / `Block` / `Inline` / `DocumentMeta` |
| 解析器注册表 | ✅ | `engine/rag/loader/loader.go` | 按文件类型选择，前序优先 |
| MinerU 解析器 | ✅ | `engine/rag/loader/mineru` | HTTP 客户端 + middle_json 映射 + 协议版本校验 |
| 纯文本解析器 | ✅ | `engine/rag/loader/plaintext` | txt / md，兼作降级路径 |
| 切片器 | ✅ | `engine/rag/chunker` | 标题开新片、表格整块、噪声丢弃、标题面包屑 |
| 入库流水线 | ✅ | `engine/rag/pipeline.go` | 取文件 → 解析 → 切分 → 落库，含状态流转 |
| 向量化 | ✅ | `engine/model` · `engine/model/ollama` | `Embedder` 接口 + Ollama 实现（批量 /api/embed、维度一致性校验） |
| 模型解析（阶段 1 形态） | ✅ | `engine/model/resolver` | 配置驱动，启动即探测维度；阶段 2 换成数据库驱动，调用方不变 |
| Milvus 向量存储 | ✅ | `repository/milvus` | 单集合 + dataset_id 过滤；HNSW(COSINE) + 标量倒排索引；启动校验维度 |
| 检索器 | ✅ | `engine/rag/retriever.go` | 查询向量化 → 召回 → 回表还原片段（含溯源字段） |
| 检索接口 | ✅ | `POST /api/v1/datasets/:id/search` | 返回命中片段 + page / locator / kind / titlePath |
| 检索留痕 | ✅ | `repository/postgres/retrieval.go` | `retrieval_runs` + `retrieval_hits` 已写入（可视化见阶段 4） |

### 3.5 模型与编排

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| **模型配置模块** | ⬜ | — | 方案已定（见 D3），未开始 |
| **Rerank 服务** | ⬜ | — | 方案已定（TEI 边车，见 D5），未开始 |
| **Eino 内核接入** | ⬜ | `internal/engine` | **当前 RAG 直连 MinerU / Ollama，尚未经过 Eino 编排** |
| **流式对话接口（SSE）** | ⬜ | — | 未开始 |
| 检索追踪记录 | ⬜ | — | 表结构已设计（见 D6），未开始 |

### 3.6 前端

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 工程骨架 | ✅ | `web/` | Next.js 14 + TS 严格 + Tailwind + Zustand |
| 请求层与 SSE 封装 | ✅ | `web/lib/http.ts` | 同源转发，统一错误处理 |
| 类型契约 | ✅ | `web/types/api.ts` | 与后端响应结构对齐 |
| **基础页面** | ⬜ | — | 登录 / 应用列表 / 知识库 / 对话调试 均未开始 |
| **检索溯源展示** | ⬜ | — | 需求已确认（列为可选功能），未开始 |

## 4. 未完成事项（附阻塞原因）

| # | 事项 | 阻塞原因 |
| --- | --- | --- |
| 1 | 向量化 + Milvus + 检索 | 依赖模型配置落地；**Embedding 模型已实测选定 bge-m3** |
| 2 | 模型配置模块 | 方案已定，待开发（阶段 2） |
| 3 | Rerank | 待开发（阶段 3）；**已实测 Ollama 无法提供 rerank**（见第 6 节） |
| 4 | 检索追踪与页面展示 | 待开发（阶段 4） |
| 5 | Eino 内核接入与 SSE 对话 | 未开始；V0.1 的核心目标之一 |
| 6 | 前端基础页面 | 未开始 |
| 7 | 图片实体提取 | 未开始；MinerU 的 `image_body` 内容为空，需改用 `--format zip` 取图 |
| 8 | 检索相似度阈值过滤 | 未开始；当前只按 top_k 截断，bge-m3 对无关文本仍会给 0.45 左右的分，需要阈值剔除 |
| 9 | 知识库级模型绑定 | `datasets.embedding_model` 列目前不参与实际选型（始终用全局配置），阶段 2 接入 |

## 5. 已确定的技术决策

> 每条都写明理由，避免后来者反复推翻。有争议时**追加新条目**而不是删除旧条目。

**D1 · 数据库用 PostgreSQL**

理由：`timestamptz` 从类型层面消除时区偏差（MySQL 的 naive DATETIME 已在本项目踩坑两次）；
JSONB 提供「将来要按 DSL 内部字段检索」的期权；与参考项目 Dify 一致；切换成本在表少时最低。

**D2 · 数据访问用 GORM，但限制在 `repository/postgres` 内**

理由：减少 CRUD 模板代码；同时用接口把 ORM 关在实现层内，service 及以上不感知。
配套纪律：表结构以 SQL 迁移脚本为准，**不使用 AutoMigrate**；
`Updates(struct)` 会跳过零值，需用 `Select` 显式指定列。

**D3 · 模型配置采用「内置厂商 + 启用配置」（A 方案）**

理由：提供与 Dify 等价的配置体验，成本可控。
**真正的动态插件加载（下载 → 加载 → 沙箱 → 版本管理）留给 V2.0 插件市场。**

已定细节：

- 厂商是代码（`internal/engine/model/providers/*`），凭证与模型选择是数据；
- 密钥 AES-256-GCM 加密存储，加密密钥来自环境变量；API **只回掩码，绝不回明文**；
- **支持应用级覆盖全局模型配置**（同 Dify）。

**D4 · 文档解析统一走 MinerU，不自研 OOXML / PDF 解析**

理由：MinerU 4.0 已覆盖 PDF / 图片 / Office 等格式且质量好（Apache-2.0 + 高门槛附加条款，商用无碍）；
自研版面分析与 OCR 是数月量级投入，ROI 极低。

配套纪律：解析结果映射进**项目自己的 IR**，切片逻辑不直接依赖 MinerU 字段；
解析器可插拔，但**当前只实现 MinerU + 纯文本兜底**。

**D5 · Embedding 用 bge-m3，Rerank 用 TEI 边车**

理由（均为实测数据）：bge-m3 中文区分度 0.5156，`nomic-embed-text` 为 **−0.1454**
（对无关中文给出 0.86 高分，中文场景失效）；Ollama 无 rerank 接口，其 rerank 模型调用即崩溃。

已定细节：

- 向量维度 **1024**，Milvus collection 须按此建；**启动时校验维度，不一致直接报错**（静默写入会产生大量噪声且难排查）；
- 边车需暴露 `/info` 报告自己加载的模型，与配置的模型名不一致时**明确报错**，避免「界面显示 A、实际跑 B」。

**D6 · 检索可追溯，记录每次检索与命中**

两张表：`retrieval_runs`（查询、top_k、耗时）+ `retrieval_hits`（命中片段、排名、召回分数、rerank 分数、阶段）。

理由：沉淀为差异化亮点（Dify 检索可见性弱）；且记录不留则事后无法补，
因此**即使 rerank 尚未落地也要先把表建好**。

**D7 · 分段向量与文本分开存，向量主键即分段主键**

Milvus 只存三样东西：主键、两个过滤字段（dataset_id / document_id）、向量本身；
片段文本与溯源信息留在 PostgreSQL。

已定细节：

- **所有知识库共用一个集合**，用 `dataset_id == n` 过滤。一个知识库一个集合会产生大量小集合，
  每个都要单独建索引与加载，运维成本远高于多存一列；
- **向量主键直接用 `document_segments.id`**：省一套 ID 映射，也不会出现两边对不上。
  V0.1 的文档重新解析是「先删旧分段再写新分段」，主键会全部重建，因此入库前必须
  **先按 document_id 删旧向量再写新向量**（顺序反了会把刚写的删掉）；
- 检索用 **Strong 一致性**：默认的 Bounded 有秒级可见性延迟，会表现为「刚上传完搜不到」，
  演示与测试都无法接受；
- 写入用 **Upsert** 而不是 Insert：重试或重复入库时不会因主键重复直接失败；
- `DropCollection` **刻意不放进 `VectorStore` 接口**：删集合是运维动作（换模型重建索引），
  放进接口会诱使业务代码去调它；
- 维度校验分三处：建集合时、启动时（与已有集合比对）、写入时（逐条比对）。
  三处都返回 `model.ErrDimensionMismatch`，**一律失败而不是静默写入**。

## 6. 已知限制与技术债

| # | 限制 | 影响 | 处理计划 |
| --- | --- | --- | --- |
| 1 | 入库队列是**进程内**的 | 重启丢失排队任务；队列满只记日志，文档停在 `pending` | 引入持久队列（Redis）时替换 |
| 2 | `token_count` 按**字符数**估算 | 切片长度控制不精确 | 接入 embedding 后换真实分词器 |
| 3 | 图片实体未提取 | 切片里只有「这里有图」，没有图 | 改用 MinerU `--format zip` 取图 |
| 4 | Eino 内核尚未接入 | 当前 AI 调用是直连，未经过编排层 | V0.1 剩余工作 |
| 5 | Ollama 的 rerank 模型不可用 | 实测 `/api/embed` 与 `/api/generate` 均 GGML_ASSERT 崩溃，且无 `/rerank` 端点 | 用 TEI 边车替代 |
| 6 | `nomic-embed-text` 中文失效 | 实测排除，勿再考虑用于中文 | — |
| 7 | MinerU 只验证过 PDF | Office / HTML 等格式未经真实文档测试 | 遇到时补充验证 |
| 8 | 解析产物未保留 | 只留切分结果，原始 middle_json 未落库 | 需要排查解析质量时得重新解析 |
| 9 | Milvus 客户端依赖较重 | 引入 milvus/client v2 后 go.mod 多了约 130 个间接依赖（含 etcd、sentry、otel） | 已实测可接受：这些包不在我们的调用链上，不进最终二进制；若将来构建体积成为问题再评估改走 REST |
| 10 | 向量与分段的一致性靠流程保证 | PostgreSQL 与 Milvus 无跨库事务，写入中途失败会留下短暂不一致 | 检索侧已按「文档已就绪」过滤，不一致窗口内不会被召回；后续可加对账任务 |

## 7. 下一步（阶段划分，已确认）

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 1 | 模型解析接口（配置驱动）+ Embedding(bge-m3) + Milvus + 检索接口 + 溯源字段返回 | ✅ 已完成（2026-10-10 实测通过） |
| 2 | 模型配置模块（表 + 加密 + API + 前端页面 + 连通性测试） | ⬜ |
| 3 | Rerank 边车（TEI）+ provider 路由 + 两阶段分数落库 | ⬜ |
| 4 | 检索追踪页面（命中片段可视化） | ⬜ |

阶段 1 实测结论（2026-10-10，本机全链路）：

- 上传《万用表RS-12的使用.pdf》→ 23 个分段，全部回填 `vector_id`；
- 检索「如何测量直流电压」→ 首位命中即第 4 页「直流电压测量」章节（相似度 0.7786），
  第 4 位命中规格表格（`kind=table`），**页码、标题路径、片段类型都可溯源**；
- `retrieval_runs` / `retrieval_hits` 各写入 1 / 4 条，检索留痕链路可用；
- 集成测试覆盖：集合往返（写入 → 跨知识库过滤 → 覆盖写 → 按文档删除 → 按主键删除）、
  维度不一致报错、非法维度直接拒绝。

复现命令：

```bash
# 向量库集成测试（需本机 Milvus 已启动）
cd backend && MILVUS_TEST_ADDRESS=127.0.0.1:19530 MILVUS_TEST_DATABASE=einoflow \
  go test ./internal/repository/milvus/ -v -run TestStore

# 检索接口（需后端已启动）
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"query":"如何测量直流电压","topK":4}' \
  http://127.0.0.1:8888/api/v1/datasets/2/search
```

阶段 1 刻意先做**接口 + 配置驱动实现**，而不是完整配置模块：
RAG 只依赖「embedding 地址 + 模型名 + 维度」三项信息，先跑通检索这个真正的验证点；
阶段 2 换成数据库驱动时调用方不用改（该模式已在换 GORM、换 PostgreSQL 时验证过两次）。

## 8. 参考与约定

- 需求以 `docs/` 下的 PRD 与任务计划书为准；
- 本地 Dify 源码可作设计参考（`/Users/shiqingliang/Documents/Projects/PublicProjects/dify`），
  **禁止直接复制代码**，理由与许可证说明见根 `AGENTS.md` 第 11 节；
- 解析样本在 `docs/rag-test/`（其中含个人简历，注意个人信息）；
- 后端细节规则见 `backend/AGENTS.md`，前端见 `web/AGENTS.md`。
