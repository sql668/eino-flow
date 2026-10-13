# OpenSpec：本项目的规格与变更协议

> OpenSpec 是「你和 AI 之间关于『要建成什么样』的协议」。
> 它让改动在写代码之前就有可评审的载体，让半年后的会话知道系统为什么是这样。

最后更新：2026-10-12（CLI 1.14.1，schema `spec-driven`）

## 1. 心智模型（三个词）

| 词 | 含义 | 位置 |
| --- | --- | --- |
| **spec** | 系统**当前**的行为契约（要求 + 场景），不是愿望清单 | `openspec/specs/<capability>/spec.md` |
| **change** | 一次变更的全部材料：为什么、改什么、怎么做、任务清单 | `openspec/changes/<id>/` |
| **archive** | 变更做完后，把规格增量**折回**主规格，change 收进归档目录 | `openspec/changes/archive/<日期>-<id>/` |

关键点：change 里写的是 **delta（增量）**——`ADDED` / `MODIFIED` / `REMOVED` / `RENAMED`，
不是把整份规格重抄一遍。这正是它能用在既有项目上的原因。

## 2. 本仓库实际生成的结构

```text
openspec/
├── config.yaml                 # schema + 项目上下文（语言、约定）
├── specs/                      # 现状真相：每个 capability 一份规格
│   └── <capability>/spec.md
└── changes/
    ├── <change-id>/            # 一次变更
    │   ├── proposal.md         # Why / What Changes / Capabilities / Impact
    │   ├── specs/<capability>/spec.md   # 规格增量（ADDED/MODIFIED/…）
    │   ├── design.md           # 技术方案（跨模块 / 新依赖 / 取舍复杂时才写）
    │   └── tasks.md            # 可勾选的任务清单
    └── archive/                # 归档后的 change

.agents/skills/openspec-*/SKILL.md   # Codex 侧技能（12 个：核心 6 + 扩展 6）
```

## 3. 在 Codex 里怎么用

**Codex 不识别 `/openspec-*` 这类命令**（官方文档明确标注），而是通过技能调用：

| 技能（`$` 前缀） | 干什么 | 什么时候用 |
| --- | --- | --- |
| `$openspec-explore` | 读代码、摆选项、把模糊想法聊成具体方案 | 还不确定要怎么做时（不落任何文件） |
| `$openspec-propose <id>` | 一次生成 change 的全部规划产物 | 确定要做这个变更时 |
| `$openspec-apply-change <id>` | 按 tasks.md 逐条实现并勾选 | proposal 被批准之后 |
| `$openspec-update-change` | 实现中发现问题，回头改规划产物 | 方案要调整时（而不是硬着头皮往下写） |
| `$openspec-sync-specs` | 把增量合进主规格但不归档 | 变更还没做完、但规格要提前生效 |
| `$openspec-archive-change <id>` | 归档并把增量折回主规格 | 门禁全绿、收尾时 |

扩展技能（2026-10-12 启用，全局 `profile: custom`）：

| 技能 | 干什么 |
| --- | --- |
| `$openspec-new-change` | 分步新建变更（一次一个产物，逐步确认） |
| `$openspec-continue-change` | 接着补下一个还没写的产物 |
| `$openspec-ff-change` | 快进：一次生成全部规划产物 |
| `$openspec-verify-change` | 核查"实现是否真的对上了产物"（**归档前用**） |
| `$openspec-bulk-archive-change` | 批量归档多个已完成的变更 |
| `$openspec-onboard` | 带讲解地在自己代码库上走一遍完整流程（熟悉工具用） |

**注意**：新增技能在**下一个会话**才会被加载——技能清单在会话启动时就固定了。

**规划边界（硬约定，来自 OpenSpec 自身）**：`propose` 只产出规划文件，**不允许顺手写实现代码**；
实现必须在用户看完 proposal 之后另起 `apply` 流程。这与根 `AGENTS.md` 第 11 节「先对齐后编码」是同一件事的两面。

## 4. 产物怎么写（附校验规则，照这个写就不会返工）

### 4.1 proposal.md

四节：`Why`（1–2 句问题 / 机会）→ `What Changes`（具体到能力、接口、数据，破坏性变更标 **BREAKING**）
→ `Capabilities`（新增哪些 capability、改哪些既有 capability，**路径必须与 `openspec/specs/` 里一致**）
→ `Impact`（受影响的代码、接口、依赖）。

### 4.2 规格增量

```markdown
## Purpose          # 仅"新 capability"要写：一两句说这个能力干什么（≥50 字符）

## ADDED Requirements

### Requirement: <要求名>
系统 SHALL ……（一条要求只写一个行为）

#### Scenario: <场景名>
- **WHEN** 条件
- **THEN** 预期结果
```

会被 `openspec validate --strict` 拦下来的四条硬规则：

1. 场景标题必须正好 4 个井号（`#### Scenario:`），用 3 个井号会**静默失效**；
2. 每条 `Requirement` 至少一个场景；
3. `Requirement` 名下的描述正文 **≤500 字符**（超了 strict 不过；所以一条要求只写一个行为，边界情况放进场景）；
4. `MODIFIED` / `REMOVED` 必须**整块照抄**原有要求再改（只写一半，归档时会把原内容丢掉）。

### 4.3 design.md

只在「跨模块 / 新外部依赖 / 数据模型变化 / 安全或性能有取舍」时写。
小节：`Context` → `Goals / Non-Goals` → `Decisions`（含被否掉的方案与理由）→ `Risks / Trade-offs` → `Open Questions`。
**会改变规格、方案或任务拆解的疑问不算 Open Questions，必须当场问用户。**

### 4.4 tasks.md

```markdown
## 1. 阶段名
- [ ] 1.1 可完成于一次会话的任务（验证方式写在括号里）
```

规则：必须带复选框（只有 `x` 算完成，`- [~]`、`- [-]`、`- []` 都算未完成）；
**每个任务自带验证方式**；测试与文档写在各自所属的任务组里，**不要堆到最后再补**。

## 5. 本仓库的落地内容

### 5.1 首批 capability 规格（已回填）

回填原则：只回填**下一批变更要动的区域**，不做全仓库清点（OpenSpec 官方也建议按变更逐步长成）。

| capability | 覆盖的行为 |
| --- | --- |
| `knowledge-base` | 知识库 / 文档 / 应用 CRUD、上传、分段工作台（配置·预览·入库）、分段浏览与关键词编辑 |
| `rag-pipeline` | 解析（MinerU + 纯文本兜底）、清洗规则、多模式切片、向量化入库、维度校验、关键词抽取 |
| `rag-retrieval` | 三种检索方式（向量 / 全文 / 混合）、阈值、两级重排、检索留痕 |
| `model-config` | 厂商凭证、模型清单、三级绑定、凭证加密与掩码、连通性测试 |
| `chat-conversation` | 流式对话的四类事件、引用来源、思考过程展示、会话与消息落库 |

### 5.2 change 清单与状态

| change-id | 对应 PROGRESS 未完成项 | 状态（2026-10-13） |
| --- | --- | --- |
| `add-react-agent-chat` | 第 4 节 #3：ReAct / 智能体问答（V0.1 目标「ReAct/RAG 问答」的另一半） | 🚧 2/28（工具注册表 + 计算演示工具已落地） |
| `add-retrieval-trace-console` | 第 4 节 #2：检索追踪 / 召回测试页面（后端留痕数据已就绪） | ⬜ 0/17 已批准未开工 |
| `add-chat-tests` | 第 4 节 #10：对话链路单测（其他模块都有，只有 `service/chat.go` 与 Eino RAG chain 没有） | ✅ 已归档（`archive/2026-10-12-add-chat-tests/`） |
| `add-dictionary-keyword-tokenizer` | 技术债 17：关键词抽取改走词典分词（gse），保留后置过滤与可回退配置 | ⬜ 0/13 已批准未开工 |

进度随时用 `openspec list` 查；某个 change 的下一批任务用
`openspec instructions apply --change <id> --json` 看。

**当前进度与下一步统一记在 `docs/PROGRESS.md` 第 9.7 节**（避免两处各写一份而漂移）。
按流程：读 proposal → 用户点头 → 才动手；2026-10-12 起这几个 change 已获批准，可连续实施。

## 6. 与其他文档的分工

| 内容 | 归属 |
| --- | --- |
| 系统当前行为（可测试的契约） | `openspec/specs/` |
| 一次变更的意图与任务 | `openspec/changes/<id>/` |
| 技术选型的历史理由（D1…） | `docs/PROGRESS.md` 第 5 节 |
| 实现进度 / 技术债 / 会话交接 | `docs/PROGRESS.md` 第 3、4、6、9 节 |
| 长期规则 | `AGENTS.md` × 3 |
| 需求背景（PRD / 任务计划书） | `docs/` 下的原始文档（作为 explore 与 propose 的输入材料，不做批量转换） |

## 7. 维护动作

```bash
npm install -g @fission-ai/openspec@latest   # 升级 CLI
openspec update                              # 让本项目的技能文件跟上新版本
openspec config profile                      # 配置工作流（2026-10-12 已启用全部 12 个，无需再跑）
openspec view                                # 交互式看板
```

顺序要求：**先升 CLI，再在项目里跑 `openspec update`**（技能文件由本地 CLI 生成，CLI 旧了就不会补新工作流）。

## 8. 两个已知历史包袱（本机实测）

1. **旧版遗留文件**：0.21.0 曾在 `~/.codex/prompts/` 生成
   `openspec-proposal.md` / `openspec-apply.md` / `openspec-archive.md`。
   1.14.1 改用 `.agents/skills/`，**不再维护这三个文件**，且 Codex 本来也不识别 `/openspec-*` 调用。
   **2026-10-12 已删除**（删前确认三份文件都带 `<!-- OPENSPEC:START -->` 托管标记，说明确是生成的；
   备份放在 `/tmp/openspec-legacy-prompts-backup/`——注意 `/tmp` 会被系统清理，需要长期留存请自己挪走）。
   `~/.codex/prompts/` 目录保留为空，Codex 不受影响。
2. **语言设置**：`openspec/config.yaml` 的 `context` 里已写明产物用简体中文，
   但**结构标题与 SHALL/MUST 等关键词保持英文**——校验器认这些英文关键词，别改成中文。
