# EinoFlow Backend（后端）规则

> 适用范围：`backend/` 目录下全部代码。
> 本文件在 `backend/` 内叠加于根 `AGENTS.md` 之上；冲突时以更具体的本文件为准。

## 1. 技术栈（固定）

- 语言/服务：Go 1.25 + CloudWeGo Hertz（module `einoflow-backend`）
- AI 内核：CloudWeGo Eino（Graph / Workflow / ReAct / Agent / Memory）
- 存储：PostgreSQL（业务）、Milvus（向量）、MinIO（对象存储）、Redis（会话/限流/缓存）

存储接入约定：PostgreSQL / Redis / MinIO / Milvus 全部由本项目根目录 `docker-compose.yml` 统一编排
（Milvus 默认 `127.0.0.1:19530`）。所有连接参数从 `internal/config` 读取，禁止硬编码。
- 可观测：OpenTelemetry + Eino Callback
- 扩展：MCP 工具协议适配层

依赖管理：统一用 Go Modules（`go get` / `go mod tidy`），不引入 vendor 目录；
新增依赖前先说明用途，提交时保证 `go.mod` 与 `go.sum` 同步。

## 2. 分层（强制）

调用方向固定为：`handler → service → engine → repository`

- handler：只做请求解析、参数校验、响应组装、路由注册，不写业务逻辑。
- service：业务编排、事务边界、权限校验、跨模块组合。
- engine：DSL 解析、Eino Graph 组装、Workflow/Agent 运行时、节点调度；所有 AI 编排只在这一层。
- repository：数据访问的接口与实现（PostgreSQL / Milvus / MinIO / Redis），只做存取，不含业务。

依赖只能自上而下，禁止反向依赖、禁止跨层跳调、禁止循环依赖。

## 3. 目录约定

```
backend/
├── cmd/server/          # 入口 main
├── internal/
│   ├── handler/         # Hertz handler + 路由注册
│   ├── service/         # 业务逻辑
│   ├── engine/          # 所有 AI 编排与模型调用
│   │   ├── model/       # 模型能力抽象（Embedder / Reranker）+ 厂商实现（ollama / openai / tei）+ resolver 解析
│   │   │   └── providers/  # 内置厂商注册表（厂商是代码、凭证是数据）
│   │   │   └── providers/  # 内置厂商注册表（厂商是代码、凭证是数据）
│   │   ├── rag/         # 入库流水线（loader / chunker / pipeline）与检索器
│   │   ├── dsl/         # （待建）DSL 解析
│   │   ├── nodes/       # （待建）Eino 节点
│   │   └── runtime/     # （待建）工作流 / Agent 运行时
│   ├── repository/      # 数据访问接口（实现在同名的子包内）
│   │   ├── postgres/    # 业务库（GORM 只允许出现在这里）
│   │   ├── milvus/      # 向量库
│   │   └── minio/       # 对象存储
│   ├── model/           # 领域模型与结构体
│   ├── middleware/      # 鉴权、日志、trace、限流
│   └── config/          # 配置加载
├── api/                 # 路由/IDL/协议定义
└── configs/             # 配置文件
```

没有特殊理由就按此组织；新增顶层结构前先说明。

三条补充约定（2026-10-10 补，源于 RAG 与模型配置落地时的实际取舍）：

- **模型必须经接口访问**：调用方依赖 `engine/model` 里的接口，不直接 new 厂商实现；
  「用哪个模型」由 `resolver` 解析（当前读配置，阶段 2 改读数据库），调用方不感知。
- **向量维度一律校验、绝不假设**：建集合、启动比对、写入逐条比对三处都要校验，
  不一致返回 `model.ErrDimensionMismatch` 并失败。静默写入不同维度的向量只会得到噪声，且极难排查。
- **凭证只存密文、只回掩码**：加解密统一走 `internal/security`，密钥只从环境变量读；
  任何接口、日志、错误信息里都不得出现凭证明文（新增字段时也要遵守）。
- **外部模型服务返回的数组一律按 index 归位**：embedding 与 rerank 都可能乱序返回，
  按返回顺序当输入顺序会把分数/向量配错对象，且不会报错、只让结果悄悄变差；
  条数对不上时必须报错，不允许把缺失项当 0 处理。

## 4. 编码规则

- 错误必须包装并向上传递（fmt.Errorf + %w），禁止吞错；业务代码禁止用 panic 兜底。
- context.Context 贯穿全链路（handler → service → repository），用于超时、取消与 trace 透传。
- 类型优先：对外结构体与 DSL 必须有明确的 Go 类型和 json tag，禁止用 interface{} 或 map[string]any 承载业务数据（DSL 动态数据区除外）。
- 日志使用统一 logger 并携带 trace_id；关键执行路径必须打点（节点入参/出参/耗时/Token）。
- 配置统一从 config 加载，禁止硬编码；密钥从环境变量或配置读取，不进仓库。
- 数据库访问必须参数化；事务边界放在 service 层；repository 不感知业务含义。
- 并发必须可管理：用 errgroup 或 context 收敛，禁止裸 go func 泄漏协程。
- 先定接口/契约再写实现，接口先于实现。
- 复用：能复用的逻辑抽离为独立包或函数，禁止复制粘贴；公共能力集中放置，不要把同一段逻辑散落在多处。
- 面向接口：模型适配、向量库、对象存储、缓存、工具、插件等边界一律先用 Go interface 定义，实现通过依赖注入装配，保证可替换、可扩展。
- 扩展性：新增模型 / 工具 / 节点 / 存储以「新增一个实现」接入，不要在核心流程里堆 switch/if 分支判断类型。
- 注释：对外接口、公共函数、复杂逻辑、非直觉实现必须有注释，**注释一律用中文**，解释原因与约束而非复述代码。

## 5. 关键模块规则

- DSL 解析：前端 DSL → Eino Graph 动态构建，分层解析、统一编译；DSL 带版本字段并处理兼容。
- RAG：解析 → 切片 → embedding → 入库 → 检索 各阶段独立且可观测，禁止把整条链路塞进一个函数。
- 智能体：ReAct / 多智能体 / Memory / Human-in-the-loop 复用统一运行时，不要各写一套调度。
- 插件：沙箱隔离执行，权限最小化，禁止插件直接访问主进程资源；插件注册/版本/启停集中管理。
- MCP：适配层统一工具协议，工具调用统一超时、重试与错误处理。
- 可观测：每次执行生成唯一 TraceID，节点入参/出参/耗时/Token 全部记录；对接 OTel + Eino Callback。
- 模型适配：多模型统一接口（OpenAI / 豆包 Ark / Ollama / 通义千问等），密钥加密存储。
- **GORM 与 NOT NULL 列**：Go 侧允许为 nil 而数据库列是 `NOT NULL` 的字段
  （典型是 JSONB 配置列），必须给 GORM 标签加 `default:'{}'`，否则 INSERT 会显式写
  SQL NULL 直接撞约束（新建知识库就这么 500 过）；`Updates` 里这类列要**按需加入 Select**，
  传 nil 表示「本次不改这一列」。改动后跑一次数据库集成测试
  （`EINOFLOW_TEST_PG_DSN=... go test ./internal/repository/postgres/`）。

## 6. 禁止事项（红线）

- 把 AI 编排逻辑写进 handler 或 service（AI 编排只能在 engine）。
- 跨层反向依赖、循环依赖，handler 直接调 repository。
- 硬编码配置、密钥、模型名、连接串。
- 无 context 的阻塞调用、无法取消的长耗时任务。
- 绕过 repository 直接访问数据库或向量库。
- 复制粘贴重复实现同一逻辑，或在 handler/service/engine 里直接写死具体实现（未经 interface 抽象）。
- 未经阶段确认就实现 V2 功能（插件市场、多智能体、MCP、热执行等）。

## 7. 自检

提交前依次执行并通过：

```
gofmt -l .
go mod tidy
go vet ./...
go build ./...
go test ./...
```

要求 golangci-lint 无新增告警；`go.mod` / `go.sum` 与代码一致。

## 8. Eino 参考资料（写 Eino 相关代码前必看）

- 主库（核心内核）：https://github.com/cloudwego/eino
- 扩展库（模型/向量库/工具等官方集成）：https://github.com/cloudwego/eino-ext
- 示例库（可运行范例）：https://github.com/cloudwego/eino-examples
- 官方文档：https://www.cloudwego.io/zh/docs/eino/

使用约定：

- 写 Eino 相关代码前，先查官方文档与示例库，按官方推荐用法实现（Graph / Workflow / ReAct / Agent / Memory / Callback）。
- 需要模型、Embedding、向量库（含 Milvus）、工具等集成时，**优先复用 eino-ext 的官方实现**，不要自己重造轮子。
- 引入依赖时用 `go get` 明确版本并记录到 `go.mod`，不要写版本猜测；不确定 API 时以文档/示例为准，不要凭记忆写。
- 若官方文档与示例有出入，以最新文档为准，并在关键实现处留注释说明依据。

## 9. 参考实现：本地 Dify 后端（只参考设计）

路径：`/Users/shiqingliang/Documents/Projects/PublicProjects/dify/api`（版本 1.17.1，Python/Flask，只读参考）。

重点参考位置：

- 工作流引擎与 DSL 编排：`core/workflow/`（`graph_topology.py`、`node_factory.py`、`generator/`）
- 节点实现：`core/workflow/nodes/`、`llm_node.py`、`node_runtime.py`
- RAG 管线：`core/rag/`（`splitter`、`embedding`、`retrieval`、`rerank`、`index_processor`、`pipeline`）
- 智能体：`core/agent/`（`base_agent_runner.py`、`cot_*`、`fc_agent_runner.py`、`strategy/`）
- 插件与 MCP：`core/plugin/`、`core/mcp/`
- 可观测与记忆：`core/telemetry/`、`core/callback_handler/`、`core/memory/`

约定：只借鉴架构分层、接口边界与设计取舍，**禁止跨语言照搬代码**，实现一律用 Go + Eino 重写；
不要引入 Dify 的任何 Python 依赖或源文件。参考了某处设计时，在实现处用中文注释写明来源与差异。
