# EinoFlow 开发进度与模块台账

> **本文档是后续开发的入口**。记录模块划分、完成情况、未完成事项、已定技术决策与已知限制。
> 每个开发阶段结束时必须更新，避免「代码在跑但没人知道还差什么」。
>
> 更新约定：
> - 新增 / 完成 / 放弃一个模块，更新第 3 节状态表；
> - 做出影响架构的决定（选型、协议、目录约定），追加到第 5 节并写明理由；
> - 发现限制或债务，追加到第 6 节，不要口头说说过就算；
> - 状态标记：✅ 完成 · 🚧 进行中 · ⬜ 未开始 · ⏸️ 挂起（附原因）。

最后更新：2026-10-18（`add-react-agent-chat` / `add-retrieval-trace-console` /
`add-dictionary-keyword-tokenizer` 均已完成并归档；**下次接手先看第 9.10 节**）

## 1. 当前阶段

**V0.1 极速 Demo（✅ 核心目标已达成，2026-10-18）**

V0.1 的目标是「表单式配置跑通：创建应用 → 上传知识库 → ReAct/RAG 问答 → 流式对话」。
按 PRD 的阶段门禁，**不提前开发拖拽画布、插件市场、MCP、多智能体、完整观测面板**。

RAG 全链路（解析 → 切片 → 向量化 → 三种检索方式 → 两阶段分数留痕）、模型配置（凭证 / 模型 / 三级绑定）、
重排（TEI 边车）与**流式对话**（Eino Graph 编排 + SSE + 前端打字机 / 思考过程）均已落地并实测。

V0.1 的「创建应用 → 上传知识库 → ReAct/RAG 问答 → 流式对话」已全部跑通：
ReAct / 智能体问答已于 2026-10-18 补齐并端到端实测（见第 9.8 节）。
**检索追踪 / 召回测试页面**仍属于后续可视化工作（change `add-retrieval-trace-console`，
不影响 V0.1 的核心目标）。

## 2. 规模现状

| 项 | 数值 |
| --- | --- |
| 后端 Go 代码 | 约 19200 行 / 91 个文件（不含测试） |
| 后端测试 | **31 个测试文件 / 193 个测试函数**，全部包绿（含 Milvus 真机集成、全文 / 混合检索、PostgreSQL 建库往返）；无外部依赖时全量单测约 10 秒 |
| 前端 TS/TSX | 约 8000 行 / 56 个文件（含 9 个页面、共享组件与 hooks） |
| 已实现 HTTP 路由 | 52 条 |
| 数据库迁移 | 0001_init … 0011_message_tool_calls、0012_retrieval_run_config |

## 3. 模块清单与状态

### 3.1 工程与基础设施

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 项目规则 | ✅ | `AGENTS.md` · `web/AGENTS.md` · `backend/AGENTS.md` | 三层规则就近生效 |
| AI Coding 流程 | ✅ | `docs/ai-coding/` · 根 `AGENTS.md` 第 13 节 | harness / OpenSpec / Superpowers 三层分工、端到端流水线、每步门禁 |
| 规格与变更（OpenSpec） | ✅ | `openspec/` | CLI 1.14.1；6 个 capability 规格；4 个 change 全部归档（见第 9.7~9.10 节）；Codex 侧技能在 `.agents/skills/`（12 个：核心 6 + 扩展 6） |
| 执行方法论（Superpowers） | ✅ | Codex 插件 `superpowers@superpowers-dev` 7.0.0 | brainstorm / TDD / 计划 / 复查 / worktree；**不落项目文件** |
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
| 入库流水线 | ✅ | `engine/rag/pipeline.go` | **两段式**：解析（产物落对象存储，文档进入待确认）→ 清洗 → 切分 → 落库 → 向量化 |
| 清洗规则层 | ✅ | `engine/rag/cleaner` | 六条规则（空白/符号行/重复短行/HTML/URL 与邮箱/自定义正则），默认全不启用，每条报告命中数、删除字数与被删样本 |
| 切片器（多模式） | ✅ | `engine/rag/chunker` | 按标题 / 固定长度 / 自定义分隔符（**支持多个分隔符**，按长度优先依次套用）；父子分段占位（选了会明确报错）；窗口重叠默认 10%；前瞻切分 + 超长块硬切；统计与可执行建议 |
| 分段工作台接口 | ✅ | `handler/chunkworkbench.go` | 选项 / 配置读写 / 预览（不落库）/ 入库 / 重切 / 重新解析，共 15 条路由；**配置不完整时不发预览**（例如分隔符模式没填分隔符），界面上给「填完会自动预览」的中性提示而不是一条注定失败的 400 |
| 知识库级检索配置 | ✅ | `datasets.retrieval_config` | 检索方式（向量 / 全文 / 混合）+ 混合融合方式（权重 / RRF）/ TopK / 召回数 / 相似度阈值 / 重排开关 / 语义与关键词权重；阈值过滤在重排之前生效并解释「为什么没结果」，且仅在向量检索下生效 |
| 向量化 | ✅ | `engine/model` · `engine/model/ollama` | `Embedder` 接口 + Ollama 实现（批量 /api/embed、维度一致性校验） |
| 模型解析（阶段 1 形态） | ✅ | `engine/model/resolver` | 配置驱动，启动即探测维度；阶段 2 换成数据库驱动，调用方不变 |
| Milvus 向量存储（v3） | ✅ | `repository/milvus` | 单集合 + dataset_id 过滤；HNSW(COSINE) + 标量倒排索引 + **BM25 稀疏索引**；正文与关键词写入单一 BM25 输入字段，加载字段白名单排除它（内存与语料体积脱钩）；启动校验维度 |
| 分段关键词 | ✅ | `engine/rag/keyword` | 入库自动抽取（默认最多 5 个，文档内 TF-IDF）；默认分词为 **gse 内嵌词典 + 领域词**（`keyword.tokenizer=dictionary`，可切回 `local`）；关键词同时进入 BM25 输入文本，因此手工修改会真实影响检索 |
| 三种检索方式 | ✅ | `engine/rag/retriever.go` · `repository/milvus` | `vector`（dense）/ `fulltext`（BM25 sparse）/ `hybrid`（两路 + 权重或 RRF 融合）；全文检索无需 embedding；阈值只在向量检索下生效 |
| 检索器 | ✅ | `engine/rag/retriever.go` | 按知识库配置选检索方式 → 召回 → 回表还原片段（含溯源字段）→ 可选重排 |
| 检索接口 | ✅ | `POST /api/v1/datasets/:id/search` | 返回命中片段 + page / locator / kind / titlePath + 排序分与两路原始分 |
| 检索留痕 | ✅ | `repository/postgres/retrieval.go` | `retrieval_runs`（含 search_mode + `config_snapshot` 参数快照）+ `retrieval_hits`（含 vector_score / fulltext_score，stage 区分 vector / fulltext / hybrid / rerank）；历史查询与召回测试页已落地（见 9.9） |
| 分段关键词编辑 API | ✅ | `PUT /api/v1/documents/:id/segments/:segmentId/keywords` | 覆盖式保存 → 同步重写该条向量（BM25 输入文本），保证「界面看到的关键词」与「检索用到的关键词」一致 |
| 分段浏览页 | ✅ | `web/app/(console)/datasets/[id]/documents/[documentId]/segments` | 真实入库分段列表（序号 / 页码 / 章节 / 字数 / 展开正文）+ 关键词行内增删改；文档列表页「查看分段」入口 |
| 上传大小上限 | ✅ | `configs/config.yaml` `upload.max_size_mb` | 默认 100 MB；两层拦截：Hertz `MaxRequestBodySize`（读请求体前拒绝）+ handler 按文件大小给出可读原因 |
| 二次确认弹窗 | ✅ | `web/components/ui/confirm-dialog.tsx` | 删除类操作统一走 `useConfirm()`：标题写清删什么、说明写清后果、默认焦点在「取消」、Esc 与点遮罩取消、焦点锁在弹窗内；已替换全部 5 处 `window.confirm`（应用 / 知识库 / 文档 / 厂商凭证 / 模型） |

### 3.5 模型与编排

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 凭证加密 | ✅ | `internal/security` | AES-256-GCM；密钥只从环境变量注入；接口与前端只接触掩码 |
| 厂商注册表（内置厂商） | ✅ | `engine/model/providers` | ollama / openai_compatible / deepseek / tei；声明凭证字段与能力；含连通性探测 |
| OpenAI 兼容向量化 | ✅ | `engine/model/openai` | `/v1/embeddings`，按 index 还原顺序，维度一致性校验 |
| 数据库驱动模型解析 | ✅ | `engine/model/resolver/database.go` | 知识库 > 应用 > 全局 三级覆盖 + 配置兜底 + 缓存与写后失效 |
| 模型配置 API | ✅ | `handler/modelconfig.go` | 厂商 4 条 / 模型 5 条 / 绑定 4 条，共 13 条路由 |
| 模型配置页 | ✅ | `web/app/settings/models` | 厂商凭证 + 模型清单 + 「当前生效模型」横幅 |
| Rerank 客户端 | ✅ | `engine/model/tei` | `/rerank` 调用：按 index 归位分数、分批、漏返回即报错 |
| 重排边车（TEI） | ✅ | `docker-compose.yml`（profile `rerank`） | 一次性下载器 + 本地路径加载（绕开镜像站无 ETag 的问题） |
| 两阶段检索 | ✅ | `engine/rag/retriever.go` | 召回 candidateTopK → 重排 → 取 topK；可逐请求开关；不静默降级 |
| 两阶段留痕 | ✅ | `retrieval_runs` / `retrieval_hits` | vector 阶段记全部候选、rerank 阶段记最终结果与两个分数 |
| 模型配置页（前端） | ✅ | `web/app/(console)/settings/models` | 对齐 Dify「模型供应商」：已配置 / 待配置 / 显示模型折叠区；对话模型也可设默认并如实显示 |
| 前端视觉方向 | ✅ | `web/AGENTS.md` 第 4 节 · `tailwind.config.ts` · `components/ui` | 工程图纸 + 仪表读数：墨/纸/网格/蓝图蓝 + 读数色；Geist Mono 只给读数；Panel 头部挂真实读数；共享 `Reading` 组件 |
| 控制台外壳（前端） | ✅ | `web/components/layout` | 全局导航 + 设置二级导航，三栏结构对齐 Dify；**只有内容区滚动**（见 web/AGENTS.md 第 4 节） |
| **LLM 调用** | ✅ | `engine/model/{ollama,openai}` | 对话链路已按厂商能力实际调用 llm 模型（含 reasoning 思考过程），见第 4 节 #8 |
| **Eino 内核接入** | 🚧 | `internal/engine` | RAG 问答与流式对话已经过 Eino Graph（`engine/chat`）；**ReAct 运行时已落地**（`engine/runtime/agent.go`，含轮次上限 / 失败回灌 / 事件与日志）；对话链路与工作流编排尚未接入，见 change `add-react-agent-chat` |
| 智能体工具注册 | 🚧 | `engine/runtime` | 工具注册表（支持按本次问答取子集）+ 计算演示工具 + 知识库检索工具已落地并测；检索范围按列表设计、当前只接受一个知识库（多个在发起检索前报错）；ReAct 运行时已测（默认最多 3 次工具调用，失败回灌不中断）；模型层工具调用已完成（OpenAI 兼容 / Ollama） |
| 流式对话（SSE） | ✅ | `handler/chat.go` · `service/chat.go` · `engine/chat` | 经 Eino Graph 流式转发；事件：`citations`（引用，先发）→ `reasoning`（思考过程，可选）→ `tool_call` / `tool_result`（智能体）→ `delta` → `done`；思考过程不进正文、不落库 |
| 智能体问答（agent 形态） | ✅ | `engine/runtime` · `service/chat.go` · `web/components/chat` | 后端 ReAct 运行时、工具事件、消息落库（`tool_calls` JSONB）已完成；前端工具调用块 / 展示开关 / 历史回看已完成；带知识库检索与无知识库计算两条链路均已实测（见 9.8） |
| 对话前端（流式渲染） | ✅ | `web/components/chat` | 打字机逐字揭示（`hooks/use-typewriter.ts`）+ 块状光标 + 「思考过程」折叠块 + 工具调用账本（状态轨 + 等宽读数，可展开入参/结果）+ 钉底滚动 + 尊重系统「减少动效」 |
| 检索追踪记录 | ⬜ | — | 表结构已设计（见 D6），未开始 |

### 3.6 前端

| 模块 | 状态 | 位置 | 说明 |
| --- | --- | --- | --- |
| 工程骨架 | ✅ | `web/` | Next.js 14 + TS 严格 + Tailwind + Zustand |
| 请求层与 SSE 封装 | ✅ | `web/lib/http.ts` · `web/lib/api/*` | 同源转发、统一错误处理；接口路径与出入参类型收敛在 `lib/api/`，组件不拼路径 |
| 类型契约 | ✅ | `web/types/api.ts` | 与后端响应结构对齐 |
| 页面：应用 | ✅ | `app/(console)/apps` | 列表 + 新建；形态选择（chat / rag / agent / workflow，后两者后端尚未支持） |
| 页面：知识库 | ✅ | `app/(console)/datasets` | 列表 + 文档表格（上传、状态轮询、失败原因悬浮、查看分段） |
| 页面：知识库设置 | ✅ | `app/(console)/datasets/[id]/settings` | 基本信息 + 分段默认值（含分隔符与关键词）+ 检索设置（三种检索方式 / 融合方式 / 阈值 / 重排）+ 向量模型绑定 |
| 页面：分段工作台 | ✅ | `app/(console)/datasets/[id]/documents/[documentId]/chunk` | 左配置 / 右预览；预览手动触发并标出「已过期」；清洗样本与健康度提示 |
| 页面：分段浏览 | ✅ | `app/(console)/datasets/[id]/documents/[documentId]/segments` | 真实入库分段 + 关键词行内增删改（改完同步检索索引） |
| 页面：对话调试 | ✅ | `app/(console)/apps/[id]` + `components/chat` | 流式问答：打字机 + 思考过程折叠块 + 钉底滚动 + 引用来源列表 |
| 页面：模型配置 | ✅ | `app/(console)/settings/models` | 厂商凭证 / 模型清单 / 三级绑定 / 连通性测试 |
| 共享组件 | ✅ | `components/ui` · `components/chunk` | Panel / Field / Reading / Button / **ConfirmDialog**（删除类二次确认，禁用 window.confirm）/ SeparatorField（多分隔符输入） |
| 复用逻辑（hooks） | ✅ | `web/hooks` | `use-typewriter`（打字机，积压自适应）、`use-prefers-reduced-motion`（减少动效降级） |
| **检索溯源 / 召回测试页** | ✅ | `web/app/(console)/datasets/[id]/retrieval` | 历史记录（配置快照 / 阶段分数 / 出处）+ 临时参数召回测试 + 多组并列对比；测试参数不写知识库配置 |
| **登录与账号体系** | ⬜ | — | V0.1 用固定占位用户（`local-user`），权限过滤字段已在向量库里预留 |

## 4. 未完成事项（附阻塞原因）

| # | 事项 | 阻塞原因 |
| --- | --- | --- |
| 1 | Rerank | ✅ 已完成（阶段 3）：TEI 协议两条实现（Linux 容器 / macOS 本机 GPU），两阶段分数落库 |
| 2 | **检索追踪 / 召回测试页面** | ✅ 已完成（2026-10-18，change `add-retrieval-trace-console`）：历史回看 + 临时参数测试 + 多组对比，实测见 9.9 |
| 3 | **ReAct / 智能体问答（app mode=agent）** | ✅ 已完成（2026-10-18）：工具注册表 + 知识库检索工具 + 计算工具 + Eino ReAct 运行时 + 工具事件 / 落库 + 前端工具块，端到端实测见 9.8；`workflow` 形态仍按 V1.0 计划拒绝 |
| 4 | 前端基础页面 | ✅ 已完成：应用列表 / 知识库三页 / 分段浏览 / 对话调试 / 模型配置 |
| 5 | 图片实体提取 | ⬜ MinerU 的 `image_body` 内容为空，需改用 `--format zip` 取图；当前切片里只有「这里有图」 |
| 6 | 检索相似度阈值过滤 | ✅ 已完成（2026-10-11）：阈值在重排前生效，并返回「被阈值筛掉几条」，只在向量检索下生效 |
| 7 | 应用级 / 知识库级模型覆盖的前端入口 | 🚧 知识库级已完成（设置页可绑定向量模型）；应用级仍未做 |
| 8 | LLM 模型的实际调用 | ✅ 已完成：对话链路消费 llm 模型，含思考过程（reasoning）事件 |
| 9 | `datasets.embedding_model` 列 | 🚧 仍不参与选型（选型由 model_bindings 决定）：后续要么接上要么删列 |
| 10 | 对话链路的单测 | ✅ 已完成（2026-10-12，change `add-chat-tests`）：Eino 链路 8 个用例 + service 层 7 个用例，覆盖事件顺序、思考分离、增量不攒批、形态路由与失败路径 |
| 11 | 腾讯云（CentOS / x86_64）部署验证 | ⬜ compose 与部署脚本已备好但未在目标机上实测；2 核 2G 需按技术债 15 拆分组件（解析/重排按需启动或外置） |
| 12 | 生产环境反向代理 | ⬜ 目前靠 Next 的 rewrites 做同源转发（演示可用）；正式部署用 Nginx 按路径直转后端时，注意 rewrites 不生效、且**别开压缩**（会攒住 SSE，见 D13） |

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
- **重排策略三档**（`retrieval.rerank`）：`auto`（默认，配了就用；未配置或服务不可用都降级为
  向量顺序，并在 `rerankSkipped` 里写明原因）、`off`（不用）、`strict`（不可用即检索失败）。
  取 `auto` 为默认是面向开源后的实际部署——重排是可选的额外组件，它挂了不该把检索一起拖下水；
  但降级**必须可见**：响应字段 + 服务端日志 + 界面提示，不做静默降级。
  分数条数不符属于接口异常，任何档位下都直接报错；
- **两阶段都留痕**：vector 阶段记全部候选（含向量分数），rerank 阶段记最终结果（含两个分数），
  `retrieval_runs` 补 `candidate_count` 与 `rerank_model`。
  只记最终结果的话，「重排把哪条提上来」无从回溯；
- **分数按 index 归位**：TEI 按分数降序返回，返回值里的 `index` 才是候选下标；
  直接按返回顺序当输入顺序会把分数配错片段——这类错误不报错，只让结果悄悄变差；
- **边车模型来自本地路径**：TEI 自带下载器要求响应带 ETag，hf-mirror 不返回该头，
  因此用一次性容器先把模型拉到卷里，再让边车按本地路径加载。
  模型名的比对容忍路径形式（`/data/models/BAAI/xxx` 视为与 `BAAI/xxx` 同一个模型）。

**D10 · 跨机器适配：环境差异收在「启动层」，代码只认协议**

开源之后，别人会在不同的机器上启动这个项目（Apple Silicon / Intel Mac、x86_64 服务器、
有 GPU / 没 GPU、国内 / 国外网络）。原则是：**代码只依赖协议与接口，差异在启动脚本里消掉。**

已落地的具体做法：

| 差异点 | 处理方式 |
| --- | --- |
| 重排服务的运行形态 | 官方 TEI 镜像只发布 linux/amd64；Apple Silicon 上容器里也拿不到 AVX2（只能走标量内核）。因此提供**协议一致**的两条路：Linux 用 `docker compose --profile rerank up -d tei`，macOS / 无 Docker 用 `deploy/rerank-local`（PyTorch 直接调本机 GPU）。后端只认 TEI 协议的 HTTP 接口，两种实现可互换 |
| 有无 GPU | `deploy/rerank-local` 的 `--device auto` 按 **mps → cuda → cpu** 探测；退到 CPU 时明确打日志（静默退会让使用者以为「开了 GPU 还是这么慢」） |
| 镜像平台 | compose 里 `platform: ${TEI_PLATFORM:-linux/amd64}` 可覆盖，x86 服务器与 arm64 镜像可各取所需 |
| HuggingFace 可达性 | 下载器容器**运行时探测**：先试 huggingface.co，不可达再用 hf-mirror；并设 `HF_HUB_DISABLE_XET=1`（镜像站不代理 Xet 存储，会返回 401） |
| 包管理器 / Python 源 | `bootstrap.sh` 优先用 uv、退回 venv；`PIP_INDEX_URL` / `TORCH_INDEX_URL` 可覆盖 |
| 端口检测 | 用 bash 内建 `/dev/tcp` 而不是 `nc`（各发行版预装情况不同，Alpine 默认没有） |
| 模型缓存位置 | 统一在 `deploy/models/`（compose 与本地服务共用一份，不重复下载 2GB） |

判断标准：**新同学只需要知道「我的机器有没有 NVIDIA GPU / 是不是 Apple Silicon」，
不需要读源码或改配置**；所有平台相关的判断都出现在启动脚本与文档里，不在业务代码里。

实测对比（同一模型 bge-reranker-v2-m3、同样 2 条候选）：
TEI 走 amd64 模拟 **216 秒**，本机 MPS 服务 **0.52 秒**；20 条候选 MPS 下 0.54 秒（批处理 + GPU 并行）。

**D11 · 文档切分改「两段式」，chunk_id 由业务生成**

按 2026-10-10 的讨论定稿（用户逐条拍板）：

- **两段式**：解析与入库分开，中间停在「待确认」。解析产物落对象存储，
  预览与重切都基于它——否则每次调参数都要重新解析（PDF 几十秒到几分钟），工作台没法用；
- **每次上传都走工作台确认**，不提供「静默按默认配置入库」的捷径；
  （2026-10-11 复认：当时考虑过三个替代方案——上传即按知识库默认配置自动入库、
  上传时给一个「直接入库 / 先待确认」的开关、按文件大小分流，最后决定**保持现状**。
  理由是参数不合适时自动入库的代价（删掉重传或重新解析）远大于点一次「保存并处理」；
  今后不要再把「上传后还要点一下」当成待修复的问题。）
- **配置作用域在知识库**：分段/清洗与检索参数都存在 `datasets`，
  分段页只读展示检索参数，改动入口在知识库设置页，避免每次上传重复配置；
- **窗口重叠默认 10%**（可调），只在同一章节内被长度切开的相邻片段之间生效：
  标题切换处不重叠（语义单元已变），表格不重叠（整块不切）；
- **自定义分隔符支持多个**（2026-10-11 补充，按用户要求）：界面上一个输入框、
  用**英文逗号**隔开（`\n\n, ###`），分隔符本身是逗号时写 `\,`；
  字段从 `separator`（单值）改成 `separators`（列表），老配置在读入时自动折叠，
  接口仍接受老的 `separator` 字段；
  切分按**长度优先**依次套用（`\n\n` 不会被 `\n` 抢先切成空段）；
  同时修掉一个老问题：以前界面用 `\n\n` 这种写法提示，但后端从不还原转义，
  于是「填了分隔符却没效果」——现在 `\n` `\r` `\t` `\\` 都会还原成真实字符；
- **父子分段先占位**：界面上展示但禁用，后端对它的提交明确报错，不做静默降级；
- **chunk_id 由业务生成（UUID）**：它是 Milvus 主键，同时写入 PG
  `document_segments.chunk_id`；检索拿它回查 PG 取正文。用它而不是业务库自增 ID，
  是为了让向量库与业务库各自独立演进（换库、分库、重建索引都不改主键语义）；
- **向量库带元数据**：dataset_id / document_id / user_id / title_path / page / kind，
  过滤在向量库内完成（按知识库、文档、上传人），为权限隔离与后续混合检索打底。
  这一轮只落元数据与过滤，**BM25 稀疏向量与权重融合留到下一轮**（与召回测试同批）；
- **集合换成 `einoflow_segments_v2`**：主键类型从 int64 变成 varchar，无法原地改；
  旧集合保留不删，老文档需要重新走一遍工作台才会带上元数据。

**D12 · 全文检索走「正文进 Milvus + BM25 稀疏向量」，关键词作为第二输入（方案 C）**

按 2026-10-11 的讨论定稿（用户拍板方案 C）：

- **正文进向量库**：Milvus 的 BM25 函数需要服务端掌握词频与 IDF，拿不到正文就只能自己维护
  一套倒排统计（那是另一个要持续维护的状态）。正文的权威副本仍在 PostgreSQL，
  检索时用 `chunk_id` 回表取原文，向量库里的正文**不加载进查询节点**（`loadFields` 白名单），
  因此查询节点内存与语料体积脱钩；
- **关键词并入 BM25 输入文本**：BM25 函数只接受**一个** VARCHAR 输入字段（实测传两个直接建集合失败），
  所以写入时把「正文 + 关键词」拼成一段文本。关键词在尾部重复出现，等于给它加了权重——
  这正是「手工改关键词能影响检索」的实现方式；界面上改关键词会同步重写那一条向量；
- **关键词自动生成**：入库时按整篇文档做 TF-IDF（默认最多 5 个，可配置开关与数量），
  在分段浏览页可手工增删改；
- **三种检索方式**（知识库级配置，对齐 Dify 的交互）：
  `vector` 只走向量、`fulltext` 只走 BM25、`hybrid` 两路并行召回后融合；
  混合检索的融合可选**权重**（Milvus WeightedReranker，滑块调语义 / 关键词占比）
  或 **RRF**（只看名次，免疫两路分数量纲差异）；
- **阈值作用在向量分上**（2026-10-11 补充，按用户反馈调整）：
  向量检索与混合检索都生效，全文检索下禁用（BM25 分数无上界，没有可卡的余弦分数，
  这一点在界面上以禁用态 + 悬浮说明表达，不做静默忽略）。
  混合检索下「只有关键词命中」的候选没有向量分（不在 ANN 的 TopK 里），
  按语义阈值筛它等于让关键词路失效，因此保留（见技术债 22）；
- **融合放在本地**（2026-10-11 补充）：原方案用 Milvus 的 `HybridSearch` + `WeightedReranker`/`RRFReranker`，
  但服务端融合只回传一个融合分，两路分数拿不到，「阈值只卡向量那一路」在服务端之外做不到；
  实测 Milvus 2.5.8 的 range 搜索参数（`radius`/`range_filter`）在我们的 HNSW + COSINE 索引上
  **被静默忽略**（既不过滤也不报错），下推阈值这条路也走不通。
  因此改为两路各查一次、在 Go 侧融合（加权 = 各路 min-max 归一化后按权重相加；RRF = 1/(60+名次) 相加），
  代价是两次 RPC，换来的是阈值可控、两路分数可留痕、排序逻辑可单测；
- **集合换成 `einoflow_segments_v3`**：新增正文 / 关键词字段与稀疏向量，schema 变更无法原地改。
  旧集合（v2）保留不删，**已有文档需要重走一遍工作台才会进入 v3**；
- **大文档加固**：向量化与写入按 500 段分批、上传大小上限可配置（默认 100 MB）。

实测（同一问题「怎么更换电池」，知识库 2 的 23 段手册）：

| 方式 | 首位命中分数 | 值 | 耗时 |
| --- | --- | --- | --- |
| vector | cosine 0.7449 | 更换电池（页 6） | 240 ms |
| fulltext | BM25 4.6085 | 更换电池（页 6） | 390 ms（**无 embedding 调用**） |
| hybrid/权重 | 融合分 0.8682 | 更换电池（页 6），第 2、3 位换成了 BM25 侧的「更换保险丝」「安全手册」 | 185 ms |
| hybrid/RRF | RRF 0.0328 | 更换电池（页 6） | 382 ms |

关键词联动的端到端验证：给某分段手工加上「示波器探针」后，全文检索该词命中该分段（BM25 12.0），
还原关键词后行为复原。

混合检索阈值实测（同一问题、阈值 0.75）：

| 配置 | 命中 | 被阈值筛掉 | 说明 |
| --- | --- | --- | --- |
| 混合/权重 · 无阈值 | 3（首位向量分 0.7449 + BM25 4.52） | 0 | 两路分数都能看到 |
| 混合/权重 · 阈值 0.75 | 1 | 9 | 低于 0.75 的向量命中被丢；仅剩的是「只有关键词命中」的候选 |
| 混合/RRF · 阈值 0.75 | 1 | 9 | 同上，RRF 分数不受阈值影响，被筛的是向量分 |

**D13 · 流式对话的实现约束（踩过的坑都在这里）**

按 2026-10-11 的实测结论定稿，四条都是「不做就会坏，而且坏了不容易看出来」：

- **Next 的 `compress` 必须关掉**（`web/next.config.mjs`）：开启时 Next 对**代理转发**的
  SSE 响应会先压缩再发送，于是整条流被攒成一大块——实测同一请求的 219 个事件全部在
  11.5 秒时一起到达，首字延迟从 3.1 秒退化到 11.5 秒，前端的流式渲染与打字机完全失效。
  关掉压缩后恢复正常。**这是最容易复发的一条**（有人为了「优化体积」把它打开就会坏）；
- **推理模型的思考过程要单独作为一种事件**（`reasoning`）：qwen3:8b 会先产出 8~10 秒的
  thinking 且 `content` 为空，不转发的话前端只能看到一片空白（实测首字延迟 11 秒里
  有 8.6 秒是思考）。思考内容只用于展示，不进正文、不落库（Eino 的 `ReasoningContent` 承载）；
- **打字机不能落后于模型**：前端把收到的文本按固定节奏揭示，积压越多揭示越快
  （见 `hooks/use-typewriter.ts`），否则长回答会出现「模型早写完了、界面还在慢慢打」；
- **钉底滚动要尊重用户**：只有用户本来就在底部附近时才跟随新内容，
  往上翻历史时不能被一行行拽回去（对话页最常见的体验投诉）。

另外两条前端约定：历史消息不重放打字动画（只有本次会话新产生的回答逐字揭示），
系统开启「减少动效」时打字机与光标自动退化为瞬时呈现。

**D14 · AI Coding 采用「harness + OpenSpec + Superpowers」三层**

按 2026-10-12 使用者拍板定稿（细则见 `docs/ai-coding/`，规则并入根 `AGENTS.md` 第 13 节）：

- **harness 管护栏**：规则（`AGENTS.md` × 3）、门禁（`make backend-check` / `make web-check`）、
  能力（Codex 技能与插件）、权限与上下文纪律；
- **OpenSpec 管「要做成什么样」**：`openspec/specs/` 记现状行为、`openspec/changes/<id>/` 记本次变更；
  CLI 由 0.21.0 升到 **1.14.1**（旧版给 Codex 写的是 `~/.codex/prompts/openspec-*.md`，而 Codex 不认
  `/openspec-*` 调用；新版写 `.agents/skills/`，用 `$openspec-propose` 等技能触发）；
  项目语言设为简体中文，但结构标题与 SHALL/MUST 关键词保持英文（校验器认这些词）；
- **Superpowers 管「怎么保证做对」**：只贡献手法、不落项目文件——brainstorming 当对话手段（产物归 OpenSpec），
  writing-plans 当 `tasks.md` 的写法规范（不另写计划文件）。插件 7.0.0 由**本地市场** `superpowers-dev` 安装
  （官方 curated 市场当前只有 6.3.0；`codex plugin marketplace add` 依赖 git clone，本机到 GitHub 直连被阻断，
  改用 `api.github.com` 的 tarball 接口）；
- **两套规矩合并**：第 11 节「先出方案再编码」的豁免清单与 OpenSpec 的决策树合成一张表（见第 13 节），
  避免出现「按哪个规矩办」的歧义；
- **不重复记录**：行为契约归 `openspec/specs/`；进度、技术决策、技术债仍归本文件。
  规格只写可观察行为，不写类名 / 表名等实现细节；
- **首批回填 5 个 capability**（`knowledge-base` / `rag-pipeline` / `rag-retrieval` / `model-config` /
  `chat-conversation`），只覆盖下一批变更要动的区域——刻意不做全仓库清点
  （批量回填的规格没人维护会迅速过期）。

**D15 · 关键词抽取改走「词典分词 + 保留后置过滤」（2026-10-12 拍板，实现见 change）**

按使用者决定：引入纯 Go 词典分词库 `github.com/go-ego/gse`（**Apache-2.0**；
2026-10-18 审查时更正：提案里曾误写为 MIT），替换现有的无词典统计分词。

- **为什么换**：现有四道启发式（频次、邻字多样性、首尾虚词、长词子串抑制）已是无词典方案的上限，
  残余的「断开电」这类片段是**词典缺失**问题——判断不了「断开电源」是词、「断开电」不是。
  关键词会拼进 BM25 输入文本、重复即加权，切断词会把权重落到错误粒度上；
- **不升 Milvus 拿服务端分析器**（技术债 18）：它的收益"抽词与索引同口径"与现状重叠
  （索引侧本来就会对拼接文本重新切一遍），代价是版本升级 + 数据卷/集合重建 + 全链路回归；
- **部署形态不变**：走 gse 的**内嵌词典**，不引入外部词典文件、不新增容器与挂载；
- **保留后置过滤**：词典只负责"候选是词"，子串抑制与虚词边界继续作为过滤层（正交能力，不倒退）；
- **可回退**：分词实现用配置项选择，默认在对比实测达标后才切换；已入库关键词不自动重算
  （重走分段工作台即生效）。

细节与任务拆分见 `openspec/changes/archive/2026-10-18-add-dictionary-keyword-tokenizer/`
（2026-10-18 实现并归档）。

**D16 · 词典分词用「内嵌简体词表 + 频次阈值 + 领域词」（2026-10-18 实测定稿）**

- 词表选 `gse` 内嵌 `zh_s`（简体），不引入外部词典文件；
- `MinTokenFreq=30` 过滤生僻词（保留 6.8 万词）：完整词表 RSS +220MB、初始化 2.1s，
  阈值 100 会把「前请」「先断」粘在一起，30 是内存与质量的折中点；
- 只保留**词典里真实存在的词**：HMM 产出的未知组合（实测「测物断」「盖装」「紧螺丝」）
  不再进入候选，否则会把旧实现的截断问题原样带回来；
- 首尾虚词规则只对未登录候选生效；词典词（例如「在线」）不再被这条规则误杀，
  子串抑制仍在最后统一执行；
- 领域词代码内置（`defaultDomainWords`），实测被切碎的仪器术语补进去；
  做成配置项属于 YAGNI，等出现真实维护需求再单开变更；
- **代价**：二进制 53MB → 85MB（+32MB，内嵌词表），常驻内存 38.3MB → 135.9MB（+97.6MB），
  启动多约 0.1~0.3s；在 2C2G 演示机上可接受，若将来吃紧再评估更小的词表。

## 6. 已知限制与技术债

| # | 限制 | 影响 | 处理计划 |
| --- | --- | --- | --- |
| 1 | 入库队列是**进程内**的 | 重启丢失排队任务；队列满只记日志，文档停在 `pending` | 引入持久队列（Redis）时替换 |
| 2 | `token_count` 按**字符数**估算 | 切片长度控制不精确 | 接入 embedding 后换真实分词器 |
| 3 | 图片实体未提取 | 切片里只有「这里有图」，没有图 | 改用 MinerU `--format zip` 取图 |
| 4 | ~~Eino 内核尚未接入~~ | **已解决**：RAG 问答、流式对话与 ReAct 均已过 Eino；只剩工作流 DSL 编排属于 V1.0 | — |
| 5 | Ollama 的 rerank 模型不可用 | 实测 `/api/embed` 与 `/api/generate` 均 GGML_ASSERT 崩溃，且无 `/rerank` 端点 | 用 TEI 边车替代 |
| 6 | `nomic-embed-text` 中文失效 | 实测排除，勿再考虑用于中文 | — |
| 7 | MinerU 只验证过 PDF | Office / HTML 等格式未经真实文档测试 | 遇到时补充验证 |
| 8 | 解析产物未保留 | 只留切分结果，原始 middle_json 未落库 | 需要排查解析质量时得重新解析 |
| 9 | Milvus 客户端依赖较重 | 引入 milvus/client v2 后 go.mod 多了约 130 个间接依赖（含 etcd、sentry、otel） | 已实测可接受：这些包不在我们的调用链上，不进最终二进制；若将来构建体积成为问题再评估改走 REST |
| 10 | 向量与分段的一致性靠流程保证 | PostgreSQL 与 Milvus 无跨库事务，写入中途失败会留下短暂不一致 | 检索侧已按「文档已就绪」过滤，不一致窗口内不会被召回；后续可加对账任务 |
| 11 | 模型配置缓存是**进程内**的 | 多实例部署时，A 实例改了配置，B 实例最长 60s 后才生效 | 单实例演示无影响；多实例时改为 Redis 失效广播 |
| 12 | `model_instances` 没有参数列 | 温度、top_p、rerank top_n 等模型参数暂无处可配 | 对话 / 重排落地时加 `params` JSONB 列 |
| 13 | 应用级 / 知识库级覆盖没有界面 | 只能用接口绑定，配置页只做了全局默认 | 阶段 4 做检索追踪页时一并补上 |
| 14 | 容器版 TEI 在 Apple Silicon 上极慢 | CPU 版镜像只有 linux/amd64，容器内无 AVX2，走标量路径：实测 2 条候选 216 秒 | **已解决**：改用 `deploy/rerank-local`（本机 GPU，0.52 秒），协议与 TEI 一致、后端无需改代码；x86 服务器仍用官方镜像 |
| 15 | **2 核 2G 服务器装不下全部组件** | Milvus + MinerU + TEI 三者叠加超出 2C2G | 演示只起核心（Postgres / Milvus / MinIO / 后端 / 前端），解析与重排按需启动或放到其它机器 |
| 16 | 重排超时是写死的 120 秒 | 远程或模拟环境下可能需要更长，也可能希望更短 | 接入配置项（`rerank.timeout`）而不是改常量 |
| 17 | ~~**关键词抽取质量一般**（无词典分词）~~ | **已解决（2026-10-18）**：默认切到 gse 内嵌词典分词；`断开电` / `交流电压测` / `开电池后盖` 已从演示文档关键词中消失，完整词（`断开电源` / `更换电池` / `背光灯键` / `短路蜂鸣`）正常出现 | 保留 `keyword.tokenizer=local` 作为回退；残余泛词（`允许` / `可能`）属于词性过滤范畴，不在本 change 内 |
| 18 | Milvus 2.5.8 没有 `RunAnalyzer` | 无法用服务端分析器给关键词抽取供词（实测返回 `Unimplemented`）；关键词改由本地分词器处理 | 已绕过：关键词写进 BM25 输入文本后由服务端再切一次，查询侧同样由服务端切词，索引与查询仍然同口径 |
| 19 | ~~混合检索的两路原始分拿不到~~ | **已解决（2026-10-11）**：融合改到 Go 侧做（两次检索 + 本地融合），两路分数都带回来，`retrieval_hits` 也就能记全 | 见 D12「融合放在本地」 |
| 20 | 检索方式切换后老文档搜不到 | v3 集合是新的，v2 里的向量不会自动迁移；旧文档必须重走工作台 | 演示环境已重跑；正式迁移可写一次性脚本（重切 + 重写向量） |
| 23 | ~~新建知识库 500~~ | **已修复（2026-10-11）**：`datasets.chunk_config` / `retrieval_config` 是 `NOT NULL DEFAULT '{}'`，而 Go 侧允许为 nil，GORM 于是显式写 SQL NULL 撞约束；`Update` 里同样的问题会把「只改分段配置」的保存打成 500 | 模型标签补 `default:'{}'`（INSERT 交给数据库默认值）+ `Update` 的两个配置列按需加入 Select（nil = 本次不改）；新增 PostgreSQL 集成测试（`EINOFLOW_TEST_PG_DSN` 开启）覆盖「新建 + 配置往返」，这类「代码没错、约束撞了」的问题只有真库能发现 |
| 21 | 加权融合的归一化依赖候选集合 | 两路各自做 min-max 归一化，某一路出现极端高分时其余候选会被压低；RRF 不吃这个亏 | 想要更稳时改用 RRF；将来也可换成「按分位数归一化」等更稳的做法 |
| 22 | 混合检索下「只有关键词命中」的候选不受相似度阈值约束 | 它没有向量分（不在 ANN 的 TopK 里），按语义阈值筛等于让关键词路失效，因此保留 | 如需「必须同时有向量分」的严格语义，可在设置里加开关（当前不做） |

## 7. 下一步（阶段划分，已确认）

| 阶段 | 内容 | 状态 |
| --- | --- | --- |
| 1 | 模型解析接口（配置驱动）+ Embedding(bge-m3) + Milvus + 检索接口 + 溯源字段返回 | ✅ 已完成（2026-10-10 实测通过） |
| 2 | 模型配置模块（表 + 加密 + API + 前端页面 + 连通性测试） | ✅ 已完成（2026-10-10 实测通过） |
| 3 | Rerank 边车（TEI）+ provider 路由 + 两阶段分数落库 | ✅ 已完成（2026-10-10，代码链路全部通过测试；本机模拟环境的性能见技术债 14） |
| 4 | 检索追踪页面（命中片段可视化） | ✅ 已完成（2026-10-18） |
| 5 | Eino 内核接入 + RAG 问答 + SSE 流式对话 | ✅ RAG 问答与 ReAct 智能体均已接入 Eino 并通过端到端实测；SSE 含 citations / reasoning / tool_call / tool_result / delta / done |
| 6 | 知识库完善（分段工作台 + 知识库设置 + 文档列表） | ✅ 前后端均已完成并实测（工作台：左配置 / 右预览；**预览为手动触发**——进页面自动算一次，改参数后要点「预览」并会标出「结果已过期」；清洗样本可见；预设与健康度提示） |
| 7 | 召回测试（与检索追踪页合并） | ✅ 已完成（2026-10-18，与检索追踪同页） |
| 8 | 全文检索 + 混合检索 + 分段关键词（方案 C，见 D12） | ✅ 已完成并实测（三种方式在同一问题下的差异见 D12 表格）；召回测试页与检索追踪页合并后放到下一轮 |
| 5（补充） | ReAct / 智能体问答（工具调用 + 多轮工具结果回灌） | ✅ 已完成（2026-10-18，change `add-react-agent-chat` 28/28） |

阶段 8 实测结论（2026-10-11）：

- Milvus 集成测试新增两条用例并通过（真实实例）：`TestStore_FullTextSearch`
  （相同向量、不同正文时排序由 BM25 决定；写入关键词后该片段可被检索到）、
  `TestStore_HybridSearch`（weighted / rrf 两种融合都同时召回「只有向量能命中」与
  「只有关键词能命中」的片段）；
- 端到端：v3 集合自动创建、文档重走入库后生成关键词、三种检索方式返回正确的分数形态；
- 关键词编辑 → 向量库 BM25 同步 → 全文检索命中，闭环验证通过。

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
- 本地 Dify 源码（克隆在仓库外，例如 `../dify`）可作设计参考，
  **禁止直接复制代码**，理由与许可证说明见根 `AGENTS.md` 第 11 节；
- 解析样本在 `docs/rag-test/`（其中含个人简历，注意个人信息）；
- 本地专用素材（**不进仓库**，规则见 `.gitignore`）：`docs/rag-test/`（RAG 解析样本）、
  `testdata/`（考勤制度测试文本、Dify 界面截图等只留在本机的素材）；
- 后端细节规则见 `backend/AGENTS.md`，前端见 `web/AGENTS.md`。

## 9. 会话交接（2026-10-11 收尾）

### 9.1 本次会话完成了什么

按提交顺序（`git log --oneline 8c24c43..HEAD`，共 21 个提交，**只在本地**，未 push）：

| 提交 | 内容 |
| --- | --- |
| `a7fb924` | 后端：全文检索 + 混合检索 + 分段关键词（方案 C） |
| `2d05e10` | 前端：检索方式设置 + 分段浏览页与关键词编辑 |
| `482f45f` | 文档：D12 决策与三种检索实测数据 |
| `f7865ca` / `b482d8e` | 混合检索改本地融合（阈值在混合下同样生效）+ 阈值 UI |
| `e9767a1` | 文档：D12 补「阈值作用在向量分」「融合放在本地」 |
| `8a1aace` / `a0c4f62` | 自定义分隔符支持多个（含转义修复）+ 前端输入组件 |
| `21a41c3` | 文档：台账补「自定义分隔符支持多个」与转义修复 |
| `786b51c` / `d99f281` | 思考过程转发 + 对话打字机渲染（含修掉 Next 代理攒流） |
| `2d9f823` | 文档：D13 流式对话的实现约束 |
| `33ec036` / `2d6ad5d` | 分隔符模式下不再发注定失败的预览请求 |
| `f8cce71` / `56651f2` | 修新建知识库 500（NOT NULL 配置列 + nil 指针） |
| `0b347a1` / `8cf95e7` | 分段预览改为手动触发并标出「结果已过期」 |
| `9cc98c0` | 文档：D11 复认「上传后必须人工确认分段」 |
| `70bc1fb` / `e18b0a8` | **docs/ 移出版本跟踪**（按使用者要求，文件保留在本地）+ README 同步 |

### 9.2 环境状态（下次接手先看这里）

- **依赖容器**：PostgreSQL / Redis / MinIO / etcd / Milvus（v2.5.8）/ Attu / MinerU 都在跑，
  由 `docker ps` 可见；数据在 volume 里，重启不丢。
- **后端 / 前端**：本次是在**会话内的持久终端**里前台运行的（沙箱会回收普通后台进程），
  所以会话关闭后会停。恢复方式：

  ```bash
  ./scripts/einoflow.sh start          # 起依赖 + 后端 + 前端（默认只本地访问）
  ./scripts/einoflow.sh start --tunnel # 额外开公网隧道（需要 .env 里的 CF_TUNNEL_TOKEN）
  ./scripts/einoflow.sh status         # 看容器 / 进程 / 健康检查
  ```

- 端口：后端 8888 · 前端 3001 · Attu 3000 · Milvus 19530 · MinerU 8000 · 本机重排服务见 `deploy/rerank-local`。
- 演示数据：知识库 2「产品手册库」（文档 5 已入库 23 段、带关键词；文档 6 停在「待确认分段」，
  是使用者上传的同名 PDF，未入库）；知识库 10「制度手册」（文档 7 已入库 7 段，使用者自建）；
  应用 4「万用表问答助手」（rag 形态）。联调期间产生的测试会话与临时知识库都已清理。
- `.env` 里有真实的 `EINOFLOW_SECRET_KEY` 与隧道 token，**不入库**（`.gitignore` 已覆盖）。

### 9.3 下次接手的最短路径

1. `./scripts/einoflow.sh start`，`curl -s localhost:8888/readyz` 确认 `status=ready`；
2. 读本文件第 1 节（还剩哪三件事）、第 4 节（未完成事项）、第 6 节（技术债）；
3. 想动检索/关键词相关代码时，先看 D12；动对话/流式时，先看 D13；
4. 提交前自检：`cd backend && gofmt -l . && go vet ./... && go test ./...`；
   `cd web && npx tsc --noEmit && pnpm build`。真库集成测试需要显式开环境变量：

   ```bash
   MILVUS_TEST_ADDRESS=127.0.0.1:19530 MILVUS_TEST_DATABASE=einoflow go test ./internal/repository/milvus/ -v
   EINOFLOW_TEST_PG_DSN='postgres://einoflow:einoflow@127.0.0.1:5432/einoflow?sslmode=disable' go test ./internal/repository/postgres/ -v
   ```

### 9.4 待使用者决策 / 已知取舍（别自作主张改）

| # | 事项 | 现状与建议 |
| --- | --- | --- |
| 1 | **关键词抽取质量**（技术债 17） | **已解决（2026-10-18）**：gse 内嵌词典分词已实现并切为默认（`keyword.tokenizer=dictionary`），可切回 `local`；实测数据见 9.10 |
| 2 | 上传后必须人工确认分段（D11） | 使用者 2026-10-11 复认「保持现状」，并明确了三个替代方案都被否掉，不要再提 |
| 3 | 混合检索阈值的语义（技术债 22） | 阈值只卡「有向量分」的候选，纯关键词命中的候选保留。使用者已知情 |
| 4 | 加权融合的归一化（技术债 21） | **2026-10-12 拍板：默认保持「加权」**，RRF 继续作为可选融合方式（界面上已能选）。技术债 21 作为**已知取舍保留**、不再单独修；要更稳时自己切 RRF 或改分位数归一化 |
| 5 | 文档与规则文件的版本策略 | **2026-10-12 全部纳入版本管理**：`docs/`、`openspec/`、`.agents/` 与三层规则（`AGENTS.md` × 3）。继续忽略：`docs/rag-test/`（**含个人简历与第三方文档**，规则是路径无关的 `**/rag-test/`）、`testdata/`（本地专用素材，规则是 `/testdata/` 只匹配仓库根）、`.env` 与各类密钥文件 |

### 9.5 容易复发的坑（都已写进对应 AGENTS 与决策条目）

- **不要打开 Next 的 `compress`**：会把代理转发的 SSE 攒成一大块（首字 3s → 11.5s，D13）；
- **v3 集合换名后旧文档搜不到**：旧文档必须重走工作台（技术债 20）；
- **`NOT NULL` 的 JSONB 列 + Go 侧允许 nil 的指针字段**：必须配 `default:'{}'`，
  `Update` 里这类列要按需加入 Select（技术债 23，踩过一次 500）；
- **前端不要发注定失败的请求**：必填项没填完时给中性提示（web/AGENTS.md）。

### 9.6 2026-10-12 补记：AI Coding 工作方式落地

1. **新增 `docs/ai-coding/`** 四份文档（`README.md` 主流程 / `harness.md` 护栏 / `openspec.md` 规格与变更 /
   `superpowers.md` 方法论），根 `AGENTS.md` 新增第 13 节并在第 11 节加了指引句；
2. **OpenSpec**：CLI 0.21.0 → 1.14.1，已执行
   `openspec init --tools codex --language "中文（简体）"`。产物：
   `openspec/config.yaml`（已补项目上下文：技术栈红线、分层、验证命令、规格写作约定）、
   `.agents/skills/openspec-*`（6 个技能：explore / propose / apply-change / update-change / sync-specs / archive-change；
   2026-10-12 启用扩展工作流后变为 12 个，见第 8 条）。
   旧版遗留的 `~/.codex/prompts/openspec-{proposal,apply,archive}.md` **已于 2026-10-12 删除**
   （删前确认三份文件都带 `<!-- OPENSPEC:START -->` 托管标记；备份在
   `/tmp/openspec-legacy-prompts-backup/`，`/tmp` 会被系统清理）；
3. **规格与变更**：回填 5 个 capability 规格；写入 3 个 change 草案（`add-react-agent-chat` /
   `add-retrieval-trace-console` / `add-chat-tests`，对应第 4 节 #3 / #2 / #10；
   第 4 个 `add-dictionary-keyword-tokenizer` 见第 7 条）。
   `openspec validate --all --strict` **8 项全绿**；写入时三个 change 均处于**未实现**状态
   （当期进度一律以第 9.7 节为准）；
   `add-react-agent-chat` 的四个待确认项已于 2026-10-12 拍板：**工具集含一个无副作用计算工具**、
   **轮次上限可配置（默认 3）**、**工具调用默认展示、可手动关闭**、
   **检索范围沿用现状（一次问答最多一个知识库；工具入参按列表设计留扩展位，多知识库留给 V1.0 的
   `add-multi-dataset-retrieval`）**；
4. **Superpowers**：`codex plugin add superpowers@superpowers-dev` 装好 7.0.0。注意事项：
   市场源是本地目录 `~/.codex/.tmp/marketplaces/superpowers-src`（**不能删**，删了插件就没了）；
   本机 `hooks = false` / `plugin_hooks = false`，且该插件 `hooks` 为空 → 没有会话启动钩子，
   技能需显式触发或靠描述命中；
5. **git 策略（2026-10-12 定稿）**：`openspec/`、`.agents/`、`docs/` 与**三层规则 `AGENTS.md` × 3**
   全部纳入版本管理；`docs/rag-test/` **继续忽略**（个人简历 + 第三方文档），规则写成路径无关的
   `**/rag-test/`，以后挪位置也不会进库；`.env` 与各类密钥继续忽略。
   附带发现：`.gitignore` 里原来那条 `AGENTS.md` 不带斜杠，**同时忽略了 `web/` 与 `backend/` 的两份规则**
   （无斜杠模式匹配任意层级），所以这两份当时也没进仓库；现已一并纳入；
   另：`docs/员工考勤制度.txt` 与 7 张 Dify 界面截图（均无人引用）已移到 `testdata/` 并移出版本库，
   忽略规则写成 `/testdata/`（**只匹配仓库根**）——避免连带忽略 `backend/testdata/` 这类
   Go 测试夹具目录（那是通常要提交的）；
6. **接手提醒**：`backend/go.mod` 存在一处**非本次改动**的未提交修改，接手时先用
   `git diff backend/go.mod` 确认来源。
7. **2026-10-12 追加的两项拍板**：混合检索**默认保持「加权」**（RRF 仍是可选，不改成默认，
   技术债 21 作为已知取舍保留）；关键词抽取**改走 gse 词典分词**——已开第 4 个 change
   `add-dictionary-keyword-tokenizer`（提案中，等审 proposal），落地顺序是"先实现 + 对比实测，
   数据达标才切默认"。
8. **OpenSpec 扩展工作流已启用**（2026-10-12）：全局 `profile: custom` + 12 个工作流，
   项目内 `openspec update` 后 `.agents/skills/` 从 6 个变为 12 个技能
   （新增 `new-change` / `continue-change` / `ff-change` / `verify-change` / `bulk-archive-change` / `onboard`）。
   全局配置在 `~/.config/openspec/config.json`；**新增技能要新会话才生效**。
9. **`add-chat-tests` 实施完成（16/16 任务，2026-10-12）**：新增
   `internal/engine/chat/rag_test.go`（8 个用例）与 `internal/service/chat_test.go`（7 个用例），
   把 D13 记录过的坑固化成断言——「引用 → 思考 → 增量 → 结束」的**显式序列断言**、
   思考过程的**双向断言**（事件里有、正文与落库内容里没有）、**增量不攒批**
   （用门控分块证明第一段增量早于第二块产出）、检索失败不落助手消息、四种形态的路由行为。
   验证：`make backend-check` 全绿（10.2 秒）；`go test ./internal/...` 全量 **5.7 秒**且不依赖外部服务。
   注：第 6 节技术债表里原本没有对话单测的条目，因此只更新了第 4 节 #10 与阶段表第 5 行。

### 9.7 2026-10-13 历史交接（已由 9.8 完成）

本节保留当时的接手过程；**当前状态以 9.10 为准**。

| change | 进度 | 状态 |
| --- | --- | --- |
| `add-react-agent-chat` | **28/28** | ✅ 已归档到 `openspec/changes/archive/2026-10-18-add-react-agent-chat/` |
| `add-dictionary-keyword-tokenizer` | 13/13 | ✅ 已归档到 `openspec/changes/archive/2026-10-18-add-dictionary-keyword-tokenizer/` |
| `add-retrieval-trace-console` | 17/17 | ✅ 已归档到 `openspec/changes/archive/2026-10-18-add-retrieval-trace-console/` |
| ~~`add-chat-tests`~~ | 16/16 | ✅ 已归档到 `openspec/changes/archive/2026-10-12-add-chat-tests/` |

**已完成的两件事（提交 `8b5b967`）**：

- **工具契约与注册表** `backend/internal/engine/runtime/tool.go`：注册时重名 / 空名 / 取不到工具信息
  一律报错，注册顺序稳定；契约**直接复用 Eino 的 `tool.InvokableTool`**（ReAct 组件只认这个类型，
  自造接口等于多一层双向适配，没有收益）；
- **计算演示工具** `backend/internal/engine/runtime/tools/calculator.go`：四则运算、浮点误差收敛
  （0.1+0.2 给 `0.3`）、错误信息按"给模型看"写（模型拿到错误文本才能自己纠正）。

**下一步从这里接**（顺序见 `openspec show add-react-agent-chat` 与 tasks.md）：

1. `1.2` 把知识库检索包成工具（复用 `engine/rag` 检索器，含三种检索方式与重排配置）；
2. `1.3` **按本次问答组装工具集**：未指定知识库就不注册检索工具（不是注册后返回空结果）；
3. `1.6` 检索范围参数按列表设计、当前只接受一个知识库，多于一个明确报错；
4. 第 6 组（模型层工具调用）与第 2 组（ReAct 运行时）——前者是后者能真正跑通的前提。

**这一轮撞到的计划外前提（重要）**：Eino 的 ReAct 组件要求模型实现 `ToolCallingChatModel`（`WithTools`），
而 `engine/model/{ollama,openai}` 只实现了基座接口，ollama 里甚至对 `ToolCalls` 硬报错。
因此 change 补了 **第 6 组任务（模型层工具调用）**：两条路各自实现（openai 兼容按 `tools` 传参并解析
`tool_calls`；Ollama 按 `/api/chat` 的 `tools`），流式下要**累积分片的工具调用参数**；
并且**模型不支持工具时必须明确报错、不静默降级**（已写进 `agent-chat` 规格）。
任务总数 23 → 28，决策记在 change 的 design **D10**。

**验证方式（都不依赖外部服务，约 10 秒）**：

```bash
cd backend && go test ./internal/engine/runtime/...   # 工具注册表 + 计算器（本轮新增）
make backend-check                                     # 全量门禁：格式化 + vet + 构建 + 全部测试
openspec validate --all --strict                       # 变更与规格的结构校验
openspec instructions apply --change add-react-agent-chat --json   # 看当前进度与下一批任务
```

**git 状态**：本轮共 14 个提交，其中 **5 个未 push**（`git log --oneline origin/main..HEAD` 可看）。
工作区当时只剩两处无害的空行改动，已随文档一起提交。

### 9.8 2026-10-18 交接：`add-react-agent-chat` 完成并归档（28/28）

**V0.1 的 ReAct 问答已补齐并实测。** 本 change 的 28 个任务全部完成，
`openspec validate --all --strict` 通过，后端与前端门禁均绿；
已归档到 `openspec/changes/archive/2026-10-18-add-react-agent-chat/`，规格已折回
`openspec/specs/agent-chat/` 与 `openspec/specs/chat-conversation/`。

**本会话完成的 5 组**（按提交顺序）：

| 提交 | 内容 |
| --- | --- |
| `242ea5d` | 知识库检索工具 + 按本次问答组装工具集（检索范围列表化、越界校验） |
| `41dc540` | 模型层工具调用：OpenAI 兼容 / Ollama 的 `tools`、流式分片、按能力取模型 |
| `8b75478` | Eino ReAct 运行时：轮次上限（默认 3）、失败回灌、事件与日志打点 |
| `95da583` | 对话链路接入 agent 形态、工具类 SSE 事件、`0011` 工具调用落库 |
| `fe3bb25` | 前端：放开 agent 形态、工具调用账本、展示开关、历史会话恢复 |
| （本提交） | 端到端修复：检索工具默认绑定本次知识库（模型无需猜 ID），智能体默认不选知识库 |

**端到端实测（本机 qwen3:8b + bge-m3 + Milvus，知识库 2「产品手册库」）**：

| 场景 | 结果 |
| --- | --- |
| 带知识库：「怎么测量直流电压？请根据手册说明步骤和注意事项」 | 模型调用 `knowledge_retrieval` 1 次（267 ms），命中段 199 / 第 4 页 / 0.7741；`tool_call → tool_result → citations → delta → done`；回答包含步骤、注意事项与来源；总耗时 **37.45 s**（其中大部分是 8B 推理模型的思考时间） |
| 不带知识库：「请计算 23 乘以 17 等于多少？必须使用工具计算」 | 模型调用 `calculator` 1 次（0 ms，结果 391），回答 `391`；总耗时 **40.58 s** |
| 失败态（前端实测） | 模型把运算符写成中文「乘」，工具返回可读错误；界面工具块显示「失败」与错误原因，模型继续作答（给出 144） |
| 前端 | 打开 `/apps/6` 自动恢复最近会话；工具块默认展示、可展开入参/结果；关闭开关后块消失，刷新后重新显示（记录已落库） |

**已知观察（不是缺陷）**：

- 知识库 2 的相似度阈值是 0.75；「怎么更换电池」的向量分约 0.7449，会被阈值筛掉，
  因此工具返回"没有命中"。这是知识库配置的预期行为，不是工具故障；
- 本地 qwen3:8b 的思考时间约 20~30 s，首字延迟与总耗时偏高；换更小的模型或云端模型会明显下降，
  代码侧没有额外等待；
- 计算工具只接受 `+ - * /`；模型写中文「乘」时会失败。失败原因对模型与用户都可见，
  模型可以改用符号重试（本次实测它直接用已有信息作答，没有重试）。

**接手建议**：

1. 下一块按优先级是 `add-retrieval-trace-console`（后端留痕已全，只缺接口与页面），
   再做 `add-dictionary-keyword-tokenizer`；
2. 本会话启动的依赖容器（Postgres / Redis / MinIO / etcd / Milvus / Attu）仍在运行；
   后端与前端是会话内持久终端，会话结束会停。重启用 `./scripts/einoflow.sh start`。

### 9.9 2026-10-18 交接：`add-retrieval-trace-console` 完成（17/17）

**检索追踪 / 召回测试已落地。** 后端两个只读接口 + 一个测试接口，前端页面
`/datasets/[id]/retrieval`，从知识库详情页可进入。

**本 change 做了什么**：

- `GET /datasets/:id/retrieval-runs`：按知识库 + 时间范围 + 分页查历史，返回参数快照与命中明细；
- `GET /datasets/:id/retrieval-runs/:runId`：单条详情；
- `POST /datasets/:id/recall-tests`：临时参数（方式 / 融合 / TopK / 候选数 / 阈值 / 重排 / 权重）
  复用现有检索入口，**不写知识库配置**；同知识库 2 秒内连续调用返回 429；
- 迁移 `0012_retrieval_run_config.sql`：`retrieval_runs.config_snapshot JSONB`。
  原提案写"无表结构变更"，但现有列缺阈值 / 融合权重，无法满足配置快照；
  已在 proposal / design D6 记录这处偏离；
- 前端：左侧临时参数表单，右侧多组结果并列（最多 4 组，可移除 / 清空），
  下方检索历史可展开看阶段分数与出处；参数标签固定在每组结果头部。

**端到端实测（知识库 2，bge-m3 + Milvus + 本机 MPS 重排）**：

| 对比 | 结果 |
| --- | --- |
| 阈值 0 | 召回 10 · 命中 3 · 筛掉 0；前三名：直流电压（0.779）、直流电流（0.750）、交流电压（0.699） |
| 阈值 0.75 | 召回 10 · 命中 2 · 筛掉 8；只剩直流电压（0.779）与直流电流（0.750） |
| 关重排 | 前三名：直流电压、直流电流、交流电压；耗时 226 ms |
| 开重排 | 前三名：直流电流（向量 0.750 / 重排 0.999）、直流电压（0.779 / 0.999）、安全手册（0.670 / 0.881）；耗时 1691 ms |

历史记录与测试结果一致（同一 `runId` 的 `config.filteredByThreshold` = 8/0）；
召回测试后 `GET /datasets/2/retrieval-config` 仍是阈值 0.75、重排关，确认临时参数未落库。
对话链路回归：一次 RAG 问答正常返回 citations / reasoning / delta / done（28.3 s）。

**顺手修掉的存量 bug（重要）**：

`retrieval_hits.full_text_score` 的 GORM 映射一直写成 `full_text_score`，
而 0010 迁移建的是 `fulltext_score`——结果是**每次检索的命中留痕都写入失败**，
只有 run 记录、没有 hit 明细。已在 `model.RetrievalHit.FullTextScore` 上显式标列名。
修复前产生的历史 run（例如 62/63）没有命中明细，属于当时写入失败，不是查询问题；
修复后的新 run（64 以后）明细完整。

**归档**：已归档到 `openspec/changes/archive/2026-10-18-add-retrieval-trace-console/`，
3 条新增要求已折回 `openspec/specs/rag-retrieval/spec.md`。

**下一步**：见 9.10；当时只剩的 `add-dictionary-keyword-tokenizer` 已完成并归档。

### 9.10 2026-10-18 交接：`add-dictionary-keyword-tokenizer` 完成（13/13）

**关键词分词已切到 gse 内嵌词典。** 默认配置 `keyword.tokenizer=dictionary`，
可切回 `local`；配置项在 `backend/configs/config.yaml` 与 `EINOFLOW_KEYWORD_TOKENIZER`。

**资源实测（改造前 → 改造后）**：

| 项 | 改造前 | 改造后 |
| --- | --- | --- |
| 后端二进制 | 53 MB | 85 MB（+32 MB） |
| 常驻内存 RSS | 38.3 MB | 135.9 MB（+97.6 MB） |
| 词典初始化 | — | 约 106~300 ms（启动时一次） |

调参：完整词表（35 万词）RSS 261.5 MB、初始化 2.1 s，不采用；
`MinTokenFreq=100` 会粘出「前请」「先断」，不采用；最终 `MinTokenFreq=30`（6.8 万词）。
把测试二进制复制到 `/tmp` 空目录仍能分词，确认走内嵌词表。

**关键词对照（知识库 2，文档 5 重新入库）**：

- 改造前：`断开电`、`交流电压测`、`开电池后盖`、`测电`、`路时` 等片段混在关键词里；
- 改造后：这些片段全部消失；出现 `断开电源`、`更换电池`、`背光灯键`、`短路蜂鸣`、
  `功能转盘`、`小数点`、`指示灯`、`万用表` 等完整词；
- 对比样本：MinerU 解析的 `docs/rag-test` 手册（167 段）+ 入库后的 23 段手册；
  残余泛词（`允许`、`可能`、`导致`）是词性过滤问题，不在本 change 范围。

**检索回归（重入后，同一问题）**：

| 方式 | 首位命中 | 分数 | 返回条数 |
| --- | --- | --- | --- |
| 全文（BM25） | 《万用表RS-12的使用.pdf》第 6 页 · 更换电池 | 5.4494 | 5 |
| 混合（RRF） | 同上 | 0.0327869 | 5 |
| 向量 | 第 4 页 · 直流电压测量 | 0.7789788 | 5 |

旧全文 run 36（2026-10-11）记录 `hitCount=3`，但命中明细因当时 `fulltext_score`
列名 bug 未写入；本次无法逐分数对比，但首位命中与命中数都没有退化。

**验证**：`make backend-check` 全绿；关键词包单测覆盖完整词、截断片段、ASCII 型号、
空文本、自定义领域词与配置切换。默认切换单独提交（`e85f600`）。

**归档**：已归档到 `openspec/changes/archive/2026-10-18-add-dictionary-keyword-tokenizer/`，
`rag-pipeline` 的关键词要求已折回主规格。至此 4 个 change 全部完成并归档。
