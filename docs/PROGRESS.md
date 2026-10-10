# EinoFlow 开发进度与模块台账

> **本文档是后续开发的入口**。记录模块划分、完成情况、未完成事项、已定技术决策与已知限制。
> 每个开发阶段结束时必须更新，避免「代码在跑但没人知道还差什么」。
>
> 更新约定：
> - 新增 / 完成 / 放弃一个模块，更新第 3 节状态表；
> - 做出影响架构的决定（选型、协议、目录约定），追加到第 5 节并写明理由；
> - 发现限制或债务，追加到第 6 节，不要口头说说过就算；
> - 状态标记：✅ 完成 · 🚧 进行中 · ⬜ 未开始 · ⏸️ 挂起（附原因）。

最后更新：2026-10-10（阶段 3 完成）

## 1. 当前阶段

**V0.1 极速 Demo（进行中，约完成 92%）**

V0.1 的目标是「表单式配置跑通：创建应用 → 上传知识库 → ReAct/RAG 问答 → 流式对话」。
按 PRD 的阶段门禁，**不提前开发拖拽画布、插件市场、MCP、多智能体、完整观测面板**。

RAG 全链路（解析 → 切片 → 向量化 → 两阶段检索 → 溯源留痕）、模型配置（凭证 / 模型 / 三级绑定）
与重排（TEI 边车）均已落地。剩下的是检索追踪可视化（阶段 4）与
**Eino 内核接入 + SSE 对话**——后者是 V0.1 收尾前最关键的缺口。

## 2. 规模现状

| 项 | 数值 |
| --- | --- |
| 后端 Go 代码 | 约 11000 行 / 88 个文件 |
| 前端 TS/TSX | 约 2100 行（控制台外壳 + 模型供应商页） |
| 已实现 HTTP 路由 | 33 条 |
| 单元与集成测试 | 90 个测试函数 / 84 个执行用例，13 个测试包全绿 |
| 数据库迁移 | 0001_init、0002_segment_metadata、0003_retrieval_trace、0004_model_config、0005_retrieval_rerank |

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
| 健康检查 | ✅ | `internal/handler/health.go` | `/healthz` 不查依赖；`/readyz` 逐项探 PostgreSQL / MinIO / Milvus，失败一次列全 |

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
| 凭证加密 | ✅ | `internal/security` | AES-256-GCM；密钥只从环境变量注入；接口与前端只接触掩码 |
| 厂商注册表（内置厂商） | ✅ | `engine/model/providers` | ollama / openai_compatible / tei；声明凭证字段与能力；含连通性探测 |
| OpenAI 兼容向量化 | ✅ | `engine/model/openai` | `/v1/embeddings`，按 index 还原顺序，维度一致性校验 |
| 数据库驱动模型解析 | ✅ | `engine/model/resolver/database.go` | 知识库 > 应用 > 全局 三级覆盖 + 配置兜底 + 缓存与写后失效 |
| 模型配置 API | ✅ | `handler/modelconfig.go` | 厂商 4 条 / 模型 5 条 / 绑定 4 条，共 13 条路由 |
| 模型配置页 | ✅ | `web/app/settings/models` | 厂商凭证 + 模型清单 + 「当前生效模型」横幅 |
| Rerank 客户端 | ✅ | `engine/model/tei` | `/rerank` 调用：按 index 归位分数、分批、漏返回即报错 |
| 重排边车（TEI） | ✅ | `docker-compose.yml`（profile `rerank`） | 一次性下载器 + 本地路径加载（绕开镜像站无 ETag 的问题） |
| 两阶段检索 | ✅ | `engine/rag/retriever.go` | 召回 candidateTopK → 重排 → 取 topK；可逐请求开关；不静默降级 |
| 两阶段留痕 | ✅ | `retrieval_runs` / `retrieval_hits` | vector 阶段记全部候选、rerank 阶段记最终结果与两个分数 |
| 模型配置页（前端） | ✅ | `web/app/(console)/settings/models` | 对齐 Dify「模型供应商」：已配置 / 待配置 / 显示模型折叠区 |
| 控制台外壳（前端） | ✅ | `web/components/layout` | 全局导航 + 设置二级导航，三栏结构对齐 Dify |
| **LLM 调用** | ⬜ | — | 厂商已声明 llm 能力，实现随 SSE 对话一起落地 |
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
| 1 | Rerank | 待开发（阶段 3）；**已实测 Ollama 无法提供 rerank**（见第 6 节） |
| 2 | 检索追踪页面 | 后端留痕数据已在写（`retrieval_runs` / `retrieval_hits`），可视化属阶段 4 |
| 3 | Eino 内核接入与 SSE 对话 | 未开始；V0.1 的核心目标之一（当前 RAG 是直连调用，未经过 Eino 编排） |
| 4 | 前端基础页面 | 只有模型配置页；应用列表 / 知识库 / 对话调试均未开始 |
| 5 | 图片实体提取 | 未开始；MinerU 的 `image_body` 内容为空，需改用 `--format zip` 取图 |
| 6 | 检索相似度阈值过滤 | 未开始；当前只按 top_k 截断，bge-m3 对无关文本仍会给 0.45 左右的分，需要阈值剔除 |
| 7 | 应用级 / 知识库级模型覆盖的前端入口 | 后端的绑定接口已支持三级覆盖，前端目前只做了「全局默认」 |
| 8 | LLM 模型的实际调用 | 模型配置里可以添加 llm 模型，但目前没有任何代码消费它 |
| 9 | `datasets.embedding_model` 列 | 该列仍不参与选型（选型改由 model_bindings 决定），后续要么接上要么删列 |

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

**D8 · 模型配置：厂商是代码，凭证是数据；凭证只存密文、只回掩码**

落地 D3 的具体形态（表结构见 `0004_model_config.sql`）：

| 层 | 存什么 | 位置 |
| --- | --- | --- |
| 厂商（代码） | 厂商标识、展示名、默认地址、支持的模型类型、需要的凭证字段、连通性探测方式、如何造出实现 | `engine/model/providers` |
| 厂商配置（数据） | 用户填的 base_url + 加密凭证 + 启用状态（一个厂商一条） | `model_providers` |
| 模型（数据） | 模型名 + 能力类型（llm / embedding / rerank）+ 启用状态 | `model_instances` |
| 绑定（数据） | 作用域（global / app / dataset）+ 类型 → 模型 | `model_bindings` |

已定细节：

- **密钥只来自环境变量**（`EINOFLOW_SECRET_KEY`，32 字节，支持 base64 / hex / 原始串）。
  缺失时**拒绝启动**：宁可起不来，也不要「凭证存进库里却等于没加密」。
  换密钥后旧凭证解不开，会明确报错要求重填，**不做静默降级**；
- **凭证永不回明文**：接口只回掩码（前 3 位 + `***` + 后 4 位，且不泄漏长度），
  服务层视图里根本没有存放原始凭证的字段；
- **三级覆盖用一张表表达**（scope + app_id + dataset_id），避免「取生效模型」退化成三段 if-else；
  未使用的作用域 ID 一律存 0 而不是 NULL——唯一索引里 NULL 不参与比较，会让「同一作用域只能绑一个」失效；
- **只有完全没有绑定时才回落到配置文件**；绑定了但配置有问题（凭证缺失、类型不匹配、已停用）
  一律显式报错。静默回落会造成「以为配置生效了、实际跑的是另一套」；
- **解析结果缓存 60s**，且所有写操作后立即 `Invalidate`：既避免每次检索都探测一次维度，
  又不出现「改了配置还在用旧模型」；
- `DeleteModel` **在同一事务内级联删除绑定**；删除仍有模型的厂商**直接拒绝**并提示先删模型；
- 绑定校验会验证应用 / 知识库是否存在：scope=app 却不给 appId 这种「静默落空」的绑定必须当场拒绝。

**D9 · 重排：独立边车 + 两阶段检索 + 两阶段留痕**

理由与做法（承接 D5「Rerank 用 TEI 边车」）：

- **边车不参与向量化**：TEI 只承载重排，向量化仍走 Ollama / OpenAI 兼容接口。
  一个服务只做一件事，出问题时能立刻判断是哪一段；
- **候选数与最终条数分开**：`retrieval.candidate_top_k`（默认 20）召回 → 重排 → `retrieval.top_k`（默认 5）。
  不分开的话，重排只能在最终条数里挑，等于没有挑选空间；
- **逐请求可开关**：`rerank` 三态（不传=按配置 / true / false），
  这样页面与命令行都能直接做「开重排 vs 关重排」的 A/B 对比；
- **未配置 ≠ 失败**：解析器返回 `ErrNotConfigured` 时跳过重排并**说明原因**（`rerankSkipped`），
  而调用失败、分数条数不符则一律报错——**不静默降级**，
  否则「重排没跑」会被误读成「重排没效果」；
- **两阶段都留痕**：vector 阶段记全部候选（含向量分数），rerank 阶段记最终结果（含两个分数），
  `retrieval_runs` 补 `candidate_count` 与 `rerank_model`。
  只记最终结果的话，「重排把哪条提上来」无从回溯；
- **分数按 index 归位**：TEI 按分数降序返回，返回值里的 `index` 才是候选下标；
  直接按返回顺序当输入顺序会把分数配错片段——这类错误不报错，只让结果悄悄变差；
- **边车模型来自本地路径**：TEI 自带下载器要求响应带 ETag，hf-mirror 不返回该头，
  因此用一次性容器先把模型拉到卷里，再让边车按本地路径加载。
  模型名的比对容忍路径形式（`/data/models/BAAI/xxx` 视为与 `BAAI/xxx` 同一个模型）。

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
| 11 | 模型配置缓存是**进程内**的 | 多实例部署时，A 实例改了配置，B 实例最长 60s 后才生效 | 单实例演示无影响；多实例时改为 Redis 失效广播 |
| 12 | `model_instances` 没有参数列 | 温度、top_p、rerank top_n 等模型参数暂无处可配 | 对话 / 重排落地时加 `params` JSONB 列 |
| 13 | 应用级 / 知识库级覆盖没有界面 | 只能用接口绑定，配置页只做了全局默认 | 阶段 4 做检索追踪页时一并补上 |
| 14 | **本机（Apple Silicon）跑 TEI 极慢** | CPU 版镜像只有 linux/amd64，容器内无 AVX2，走标量路径：实测 2 条候选 216 秒、1 条候选超 120 秒超时 | 本地 `.env` 默认 `EINOFLOW_RETRIEVAL_RERANK=off`（检索本身不受影响）；部署到 CentOS x86_64 后改回 `auto` 即为原生执行。需要本地看效果时把 `TEI_MODEL_ID` 换成更小的模型（如 cross-encoder/ms-marco-MiniLM-L-6-v2） |
| 15 | **2 核 2G 服务器装不下全部组件** | Milvus + MinerU + TEI 三者叠加超出 2C2G | 演示只起核心（Postgres / Milvus / MinIO / 后端 / 前端），解析与重排按需启动或放到其它机器 |
| 16 | 重排超时是写死的 120 秒 | 远程或模拟环境下可能需要更长，也可能希望更短 | 接入配置项（`rerank.timeout`）而不是改常量 |

## 7. 下一步（阶段划分，已确认）

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 1 | 模型解析接口（配置驱动）+ Embedding(bge-m3) + Milvus + 检索接口 + 溯源字段返回 | ✅ 已完成（2026-10-10 实测通过） |
| 2 | 模型配置模块（表 + 加密 + API + 前端页面 + 连通性测试） | ✅ 已完成（2026-10-10 实测通过） |
| 3 | Rerank 边车（TEI）+ provider 路由 + 两阶段分数落库 | ✅ 已完成（2026-10-10，代码链路全部通过测试；本机模拟环境的性能见技术债 14） |
| 4 | 检索追踪页面（命中片段可视化） | ⬜ 下一步 |
| 5（新增） | Eino 内核接入 + ReAct/RAG 问答 + SSE 流式对话 | ⬜ V0.1 收尾的关键一步 |

阶段 1 实测结论（2026-10-10，本机全链路）：

- 上传《万用表RS-12的使用.pdf》→ 23 个分段，全部回填 `vector_id`；
- 检索「如何测量直流电压」→ 首位命中即第 4 页「直流电压测量」章节（相似度 0.7786），
  第 4 位命中规格表格（`kind=table`），**页码、标题路径、片段类型都可溯源**；
- `retrieval_runs` / `retrieval_hits` 各写入 1 / 4 条，检索留痕链路可用；
- 集成测试覆盖：集合往返（写入 → 跨知识库过滤 → 覆盖写 → 按文档删除 → 按主键删除）、
  维度不一致报错、非法维度直接拒绝。

阶段 2 实测结论（2026-10-10，本机全链路）：

- `GET /model/providers` 返回三个内置厂商与配置状态；`POST /model/providers/ollama/test`
  实际拉到本机 Ollama 的 6 个模型并确认 `bge-m3:latest` 可用（13 ms）；
- 添加模型 → `POST /model/instances/1/test` 返回「模型可用，向量维度 1024」；
- 绑定全局默认后 `GET /model/effective` 显示 `scope=global`、`fromConfig=false`，
  说明解析已从配置文件切到数据库；随后检索接口仍然命中「交流电压测量」章节（0.7244）；
- 凭证安全：写入假密钥后接口只回 `sk-***0001`，数据库里 `credentials LIKE '%明文%'` 为 false；
- 重启后端时若 `EINOFLOW_SECRET_KEY` 缺失会直接启动失败（刻意行为）。

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

阶段 3 实测记录（2026-10-10）：

- 边车镜像 `ghcr.io/huggingface/text-embeddings-inference:cpu-1.6`（amd64-only）拉取成功；
  模型 `BAAI/bge-reranker-v2-m3`（2.1GB）经 hf-mirror 预下载到 `deploy/tei/models`；
  容器 `/info` 返回 `model_id=/data/models/BAAI/bge-reranker-v2-m3`、`max_input_length=8192`；
- 配置链路：保存 TEI 厂商 → 探测通过（「与配置一致」）→ 添加 rerank 模型 →
  绑定全局 → `/model/effective?type=rerank` 返回 `scope=global`；
- **关重排**的检索实测正常：召回 10 条 → 返回 3 条，`rerankSkipped=重排已被关闭`，
  首位命中「直流电压测量」（向量分 0.7700），耗时 628ms；
- **开重排**在真机上因模拟无 AVX2 触发超时（见技术债 14），
  重排链路的正确性由单测覆盖：分数按 index 归位、分批、漏返回报错、
  候选与最终结果分离、两阶段留痕、未配置时跳过并给出原因。

## 8. 参考与约定

- 需求以 `docs/` 下的 PRD 与任务计划书为准；
- 本地 Dify 源码可作设计参考（`/Users/shiqingliang/Documents/Projects/PublicProjects/dify`），
  **禁止直接复制代码**，理由与许可证说明见根 `AGENTS.md` 第 11 节；
- 解析样本在 `docs/rag-test/`（其中含个人简历，注意个人信息）；
- 后端细节规则见 `backend/AGENTS.md`，前端见 `web/AGENTS.md`。
