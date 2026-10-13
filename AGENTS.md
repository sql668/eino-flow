# EinoFlow 项目规则（AI 必读 · 全仓库生效）

> 本文件是项目级规则，对 `web/` 和 `backend/` 同时生效。
> 前端细则见 `web/AGENTS.md`，后端细则见 `backend/AGENTS.md`。
> 需求与背景文档见 `docs/`（PRD、任务计划书以 docs 为准）。

**开工前先读 `docs/PROGRESS.md`**：那里记录模块划分、完成情况、未完成事项与已定技术决策。
每完成一个阶段或做出架构决定，必须同步更新该文档（更新约定见文档开头）。

## 1. 项目定位

EinoFlow：类 Dify 的低代码 AI 工作流 / 智能体平台。基于字节 CloudWeGo **Eino** 作为编排内核，
**Go 高性能架构**，自带插件市场、MCP 工具生态、全链路可观测，对标并超越 Dify。

仓库结构：单仓库简易 Monorepo（无重型构建工具），本地开发 + Docker Compose 一键部署。

版本节奏：`V0.1 极速 Demo` → `V1.0 对标 Dify 完整版` → `V2.0 差异化亮点最终版`。

## 2. 技术栈（不得擅自替换）

| 层 | 选型 |
| --- | --- |
| 前端 | Next.js 14+（App Router）· React 18 · TypeScript（严格模式）· @xyflow/react · Zustand · TailwindCSS + shadcn/ui · Monaco Editor |
| 后端 | Golang · CloudWeGo Hertz（HTTP）· CloudWeGo Eino（Graph / Workflow / ReAct / Agent / Memory） |
| 存储 | PostgreSQL（业务数据）· Milvus（向量检索）· MinIO（对象存储）· Redis（会话 / 限流 / 缓存） |
| 可观测 | OpenTelemetry + Eino Callback 链路追踪 |
| 扩展协议 | MCP 工具协议适配层 |

工程与工具链（已确定）：

- 后端：Go 1.25（module `einoflow-backend`），依赖用 Go Modules 管理，不使用 vendor。
- 前端：包管理器统一使用 **pnpm**，禁止 npm / yarn（不要生成 `package-lock.json`、`yarn.lock`）。
- 前端提交必须包含 `pnpm-lock.yaml`；本地环境参考 Node 24 + pnpm 12。

本地依赖分工（已确定）：

- PostgreSQL / Redis / MinIO / etcd / Milvus / Attu 全部由本项目根目录 `docker-compose.yml` 编排（`make up`）。
- Milvus 走 `127.0.0.1:19530`；MinIO 由业务对象存储与 Milvus 后端存储共用（bucket 分别为 `einoflow` / `milvus`）。
- Attu 占用 3000，因此**前端 dev 端口固定 3001**，改端口需同步 `package.json` 与 README。

文档解析约定（已确定）：

- 解析统一调用 **MinerU 的 HTTP V1 API**（容器 `einoflow-mineru`，端口 8000），Go 侧不做 PDF/OCR 解析。
- 使用异步任务模型：`POST /v1/uploads` → `PUT .../content` → `POST .../complete` → `POST /v1/parse/jobs` → 轮询 `GET /v1/parse/jobs/{id}`。
- 消费的产物是 **`middle_json`**（`schema=docvortex.middle`），必须显式放进 `output_formats`——它默认只返回 markdown。
- 解析结果映射进本项目自己的 IR（`internal/engine/rag/loader`），**不要让切片逻辑直接依赖 MinerU 的字段**，否则换解析器要动全身。
- 如果本机已经有另一套自己的 Milvus 编排（放在仓库外的某个目录），它会与本项目抢占
  `19530 / 9091 / 3000` 端口：启动本项目前，先到那套编排的目录执行 `docker compose down`。
  本项目不依赖任何外部 Milvus 编排。

需要引入上表以外的框架 / 中间件时，先说明理由并征得确认，不要默认加依赖。

Eino 框架的官方仓库与文档地址统一记录在 `backend/AGENTS.md` 第 8 节，写后端 AI 相关代码前先查阅。

## 3. 硬性约束（红线，违反即返工）

1. **前端不写任何 AI 执行逻辑**，所有 AI 运行必须由后端 Eino 控制。
2. 所有工作流运行必须走「前端 DSL → 后端解析 → 动态 Eino Graph」链路，禁止绕过。
3. 所有配置持久化到 PostgreSQL，**禁止硬编码**。
4. 严格按三阶段推进，**不提前开发高阶功能**。
5. 后端分层严格：`handler → service → engine → repository`。
6. 前端全局状态统一使用 **Zustand**，禁止零散状态管理。
7. 所有接口、DSL、结构体必须有严格的 TS / Go 类型定义。

## 4. 前后端职责边界

**前端只负责**：界面与路由、表单交互、画布拖拽/连线/变量拾取、画布状态（撤销重做/小地图/缩放）、
DSL 序列化与反序列化、SSE 流式渲染、日志实时展示、插件市场 UI、知识库上传与列表、模型密钥配置页。

**后端只负责**：DSL 解析器、Eino 工作流/Agent 运行时与节点调度、RAG 全管线、
多模型适配与密钥管理、插件系统内核（注册/加载/沙箱/执行）、MCP 对接、Trace/Token/日志、业务 CRUD 与权限。

判断标准：**只要涉及模型调用、检索、向量、编排、工具执行，一律在后端。**

## 5. 模块依赖顺序（遵守执行顺序，不要跳级）

`基础服务 > Eino 内核 > RAG > 智能体 > 工作流 DSL > 画布 > 插件市场 > MCP > 可观测`

依赖未完成时，不要提前实现上层模块；若无法避免，先显式说明风险和原因。

## 6. 阶段门禁（开发前先确认阶段）

- **V0.1 极速 Demo**：表单式配置跑通「创建应用 → 上传知识库 → ReAct/RAG 问答 → 流式对话」。
  禁止做：拖拽画布、插件市场、MCP、多智能体、完整观测面板。
- **V1.0 对标 Dify**：xyflow 拖拽画布、DSL ↔ 动态 Eino Graph、全节点、模型/密钥管理、应用发布与 API 开放。
- **V2.0 差异化**：插件市场、多智能体协同、MCP 适配、全链路 OTel 可观测、工作流热执行。

当需求超出当前阶段时，先指出「这属于 Vx.x」，再决定是否执行，不要静默开发。

## 7. DSL 契约（前后端唯一协议）

- 工作流以 **标准化 JSON DSL** 为唯一协议：前端产出配置，后端解析并动态编译 Eino Graph。
- DSL 的结构定义是**前后端共享契约**，改动必须两边同步，禁止单边改字段名/结构。
- 类型定义放在 `web/`（TS 类型）与 `backend/`（Go 结构体）中并保持一一对应；
  复杂协议文档记录到 `docs/`。
- DSL 必须带版本字段，解析侧要考虑向前兼容，禁止破坏性变更无提示上线。

## 8. 交付与协作约定

- **分支**：`feat/xxx`、`fix/xxx`、`docs/xxx`、`refactor/xxx`、`chore/xxx`。
- **提交**：小步提交，信息格式 `<type>: <简述>`，一提交只做一件事。
- **提交前自检**：格式化 + lint + 类型检查 + 相关测试必须通过，不允许带病提交。
- **密钥安全**：密钥、Token、连接串一律走环境变量 / 配置文件，禁止写入仓库。
- **公开仓库规范**：本仓库对外公开——文档、注释、示例里**不写本机绝对路径**（用相对路径或
  `<占位>` 写法）、不写个人信息、不放内部资料；引用第三方源码一律说"克隆到仓库外"，不写具体机器路径。
- **可运行**：每个阶段的产出必须可本地部署、可演示，不做只能编译不能跑的半成品。
- **文档同步**：改动架构、协议、目录约定时，同步更新对应 `AGENTS.md` 或 `docs/`。

## 9. 通用编码要求

- 类型优先：先定义类型/接口，再写实现；禁止用 `any` / `interface{}` / `map[string]any` 承载业务数据
  （DSL 中的动态数据区除外）。
- 不吞错、不静默失败；错误要有上下文并向上传递。
- 命名清晰、函数单一职责；避免超长函数与深层嵌套。
- 新增依赖前先评估是否必要，能用手写的小工具解决就不引依赖。

## 10. 可维护性要求（前端 / 后端通用，重要）

**复用优先**

- 能复用的代码一律抽离：公共逻辑抽成独立函数 / 模块 / 组件，禁止复制粘贴；同一段逻辑出现第二次就该考虑抽离。
- 抽离单元要有清晰边界和命名；抽离后必须被真实复用，不要为「看起来通用」而过度设计。

**面向接口编程**

- 先定义接口 / 契约，再写实现；调用方依赖抽象，不依赖具体实现。
- 后端用 Go `interface` 定义边界（模型、向量库、存储、工具、插件等），实现放在可替换的位置，通过依赖注入装配。
- 前端用 TS `interface` / `type` 描述契约，把请求、存储、画布数据源等易变部分封装成可替换模块。
- 新增能力优先以「新增一个实现」的方式接入（开闭原则），不要在核心流程里堆 if/else 分支判断。

**注释**

- 重要地方必须写注释：对外接口与公共函数、复杂业务逻辑、非直觉实现、临时方案与约束、坑点及其原因。
- **注释一律使用中文**；注释解释「为什么」，不要复述代码「做了什么」。
- 改代码时同步更新对应注释，禁止留下过期注释。

## 11. 开工前先对齐（硬性，最优先）

**任何新任务，先讨论、后编码。** 得到确认之前不要动手写代码。

流程：

1. **先出方案**：把要做的东西讲清楚——页面长什么样、有哪些功能、交互怎么走、
   后端逻辑与接口怎么设计、数据怎么存、会影响哪些已有模块；
2. **列出不确定项**：凡是我不确定的（产品意图、取值、边界行为、技术取舍），
   必须提出来问，不要自己拍脑袋决定；
3. **给出我的建议**：可以并鼓励提出自己的方案与理由（哪个更好、为什么、代价是什么），
   但只是建议，**决定权在用户**；
4. **等确认**：用户同意（或改过方案）之后再开始写代码。方案有变化要重新确认。

豁免（不必走完整讨论，但要说一句再做）：

- 明显的 bug 修复、拼写与文案修正、格式化等**不改变行为**的小改动；
- 用户明确说「直接做」「不用问」的任务。

判断标准：**只要这次改动会让用户看到不一样的东西（页面/交互/接口/数据），就要先讨论。**

> 落地说明：从 2026-10-12 起，「方案」有了固定载体——见第 13 节与 `docs/ai-coding/README.md`。
> 不是每个 task 都要写文档；小改动口头说一句即可，判断标准同上。

## 12. 参考项目：本地 Dify 源码（只参考设计，不照搬代码）

参考源码克隆在**仓库之外**（例如与本项目平级的 `../dify`），本文以 **1.17.1**（Python/Flask + Next.js）
为对齐版本；换版本时以实际代码为准。

Dify 与本项目功能高度重合，可作为**设计参考**：借鉴模块划分、接口边界、数据模型与交互流程。
但技术栈不同（Python → Go），**禁止直接复制代码**，必须用本项目技术栈重新实现。

参考位置对照：

| 本项目模块 | Dify 参考位置 |
| --- | --- |
| 后端：工作流引擎 / DSL 编排 | `api/core/workflow/`（`graph_topology.py`、`node_factory.py`、`generator/`） |
| 后端：工作流节点 | `api/core/workflow/nodes/`、`llm_node.py`、`node_runtime.py` |
| 后端：RAG 管线 | `api/core/rag/`（`splitter`、`embedding`、`retrieval`、`rerank`、`index_processor`、`pipeline`） |
| 后端：智能体 | `api/core/agent/`（`base_agent_runner.py`、`cot_*`、`fc_agent_runner.py`、`strategy/`） |
| 后端：插件系统 | `api/core/plugin/`（`plugin_service.py`、`impl/`、`entities/`） |
| 后端：MCP 适配 | `api/core/mcp/`（`client/`、`server/`、`session/`、`auth/`） |
| 后端：模型 / 供应商管理 | `api/core/model_manager.py`、`provider_manager.py` |
| 后端：可观测 / 记忆 | `api/core/telemetry/`、`callback_handler/`、`memory/` |
| 前端：工作流画布 | `web/app/components/workflow/`（`index.tsx`、`custom-edge.tsx`、`nodes/`、`hooks-store/`） |
| 前端：请求层与服务 | `web/service/` |
| 前端：状态 / 上下文 / hooks | `web/context/`、`web/hooks/`、`web/features/` |
| 前端：规则写法范例 | `web/AGENTS.md`（Dify 自己的前端规则，可参考组织方式） |

使用规则：

- 该目录为**只读参考**，不要把 Dify 的源码、依赖、静态资源或文案复制进本仓库。
- 借鉴的是设计思路与边界划分，实现必须自己写；接口、类型、目录按本项目规范定义。
- 许可证注意：Dify 采用修改版 Apache 2.0，多租户商用需另行授权，且不得移除其前端 LOGO / 版权信息。本项目是独立实现，不要粘贴其代码或资源。
- 参考了某处设计时，在对应实现处用中文注释说明参考来源与本项目差异。

## 13. AI Coding 流程（harness + OpenSpec + Superpowers）

> 本章规定「在这个仓库里用 Codex 写代码该怎么走」。完整细则见 `docs/ai-coding/`：
> 主文档 `README.md`、`harness.md`（护栏）、`openspec.md`（规格与变更）、`superpowers.md`（执行方法论）。
> 与第 11 节的关系：第 11 节要求「先出方案再编码」，OpenSpec 的 change 就是那份方案的文件载体。

三层分工（细节见主文档）：

| 层 | 管什么 | 载体 |
| --- | --- | --- |
| **harness** | 规则、门禁、能力、权限、上下文纪律 | 本文件 × 3 层、`Makefile` 自检、Codex 技能与插件 |
| **OpenSpec** | 一次变更「要做成什么样」 | `openspec/specs/`（现状真相）、`openspec/changes/<id>/`（本次变更） |
| **Superpowers** | 「怎么保证做对」（TDD、复查、调试手法） | Codex 插件技能，**不落项目文件** |

一次变更的流程与门禁：

1. **想清楚**：`$openspec-explore`（或 brainstorming），只对话、不落文件；
2. **出方案**：`$openspec-propose <change-id>` → `openspec/changes/<id>/`（proposal / 规格增量 / design / tasks）；
   **门禁**：使用者读完 proposal 与规格增量并确认后，才允许写实现代码（即第 11 节）；
3. **实现**：`$openspec-apply-change`，TDD 先写失败测试、小步提交（第 8 节的提交规范不变）；
   **门禁**：`make backend-check` / `make web-check` 必须全绿；
4. **复查**：code review（Superpowers 的 `requesting-code-review` 或 `ecc` 的 `code-review` 技能），
   严重问题清零后才继续；
5. **归档**：`openspec validate --strict` 通过后 `$openspec-archive-change <id>`，规格增量折回 `openspec/specs/`；
6. **收尾**：同步更新 `docs/PROGRESS.md`（状态表 / 技术决策 / 技术债 / 会话交接）。

什么时候必须开 change（与第 11 节的豁免合并成一套判断，不要两套）：

| 情况 | 走法 |
| --- | --- |
| 明显 bug 修复、错别字 / 文案 / 格式化、注释 | **直接改**，不必建 change |
| 非破坏性依赖升级、配置调整、为既有行为补测试 | **直接改**；若影响使用方式，在 PROGRESS 留一句 |
| 新功能 / 新能力 / 新接口 / 新 DSL 字段 | **建 change** |
| 破坏性变更（DSL、表结构、接口语义）、架构调整 | **建 change**，design.md 必写，proposal 标 **BREAKING** |
| 拿不准 | **建 change**（更安全的默认） |

硬性约定：

1. **版本管理范围**（2026-10-12 定稿）：`openspec/`、`.agents/`、`docs/` 与三层规则
   （`AGENTS.md` × 3）全部纳入版本管理；以下内容继续忽略且不得进库——
   `docs/rag-test/`（含个人简历）、`testdata/`（本地专用素材）、`.env` 与各类密钥文件；
2. 规格只描述**可观察行为**，不写类名 / 函数名 / 表名等实现细节；
3. 内容归属不重复：行为契约 → `openspec/specs/`；变更意图 → `openspec/changes/`；
   实现进度、技术决策、技术债、会话交接 → `docs/PROGRESS.md`；
4. 一个 change 一个会话；跨会话接手先读 `docs/PROGRESS.md` 第 9 节，再 `openspec show <id>`；
5. 新增能力先写接口 / 契约（第 10 节「面向接口编程」），AI 能力一律留在后端（第 3 节红线 1）。


