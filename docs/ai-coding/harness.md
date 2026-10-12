# harness：让 Codex 在这个仓库里「有护栏地干活」

> harness = 规则 + 记忆 + 能力 + 门禁 + 权限 + 上下文纪律。
> 它不产出业务功能，但决定了「同一个模型，在这个仓库里是不是靠谱」。

最后更新：2026-10-12

## 1. 规则层（改代码前先读）

| 文件 | 作用范围 | 关键内容 |
| --- | --- | --- |
| `AGENTS.md`（根） | 全仓库 | 技术栈红线、分层与阶段门禁、DSL 契约、提交规范、第 11 节「开工前先对齐」 |
| `web/AGENTS.md` | `web/` | 前端技术栈、目录约定、**视觉方向（工程图纸 + 仪表读数）**、布局与滚动约定 |
| `backend/AGENTS.md` | `backend/` | 四层调用方向、模型/向量/凭证三条纪律、Eino 资料位置 |

就近生效：越具体的文件优先级越高。冲突时以子目录规则为准。

**给 AI 的实操含义**：动前端页面前先读 `web/AGENTS.md` 第 4 节并按其中要求走 `frontend-design` 流程；
动 RAG / 模型 / 向量前先读 `backend/AGENTS.md` 的三条补充约定（接口访问模型、维度必校验、凭证只存密文）。

## 2. 记忆层（「现在到哪一步了」）

| 文件 | 记什么 | 更新时机 |
| --- | --- | --- |
| `docs/PROGRESS.md` | 模块状态、未完成事项、技术决策（D1…）、技术债、会话交接 | 每完成一个阶段 / 做出架构决定 |
| `openspec/specs/**/spec.md` | 系统**当前行为**的契约 | 每归档一个 change |
| `openspec/changes/<id>/` | 进行中的变更意图与清单 | 提变更时创建、归档时收走 |

`docs/PROGRESS.md` 开头的「更新约定」必须遵守；第 9 节是会话交接区，
**新会话接手的第一件事就是读它**（而不是重新把仓库读一遍）。

## 3. 能力层（Codex 侧实际装了什么）

### 3.1 技能（skills）

Codex 从这个顺序里找技能，目录名即技能名，每个技能一个 `SKILL.md`：

| 位置 | 作用范围 | 本仓库现状 |
| --- | --- | --- |
| 内置（系统提供） | 全局 | `imagegen`、`openai-docs`、`skill-creator`、`skill-installer` 等 |
| `~/.codex/skills` | 全局（个人） | `archify`、`code-review`、`frontend-design`、`find-skills`、`skill-creator` 等 |
| `~/.agents/skills` | 全局（跨工具共享） | `archify`、`frontend-design`、`find-skills` |
| `<repo>/.agents/skills` | **本项目** | OpenSpec 生成的 **12 个**：核心 6 个（`explore` / `propose` / `apply-change` / `update-change` / `sync-specs` / `archive-change`）+ 扩展 6 个（`new-change` / `continue-change` / `ff-change` / `verify-change` / `bulk-archive-change` / `onboard`） |

补充：扩展工作流已于 2026-10-12 启用（全局 `profile: custom`，12 个工作流）；改完全局配置后必须在**每个项目**里
跑一次 `openspec update` 才会落到 `.agents/skills/`。**新增的技能在下一个会话才会被加载**——技能清单在会话启动时固定。

### 3.2 插件（plugins）

插件通过市场安装，配置写在 `~/.codex/config.toml`；常用操作：

```bash
codex plugin list                      # 已装 / 可用
codex plugin marketplace add <path|owner/repo>
codex plugin add <plugin>@<marketplace>
```

本机已启用、与本项目相关的插件：

| 插件 | 用途 |
| --- | --- |
| `superpowers@superpowers-dev` | 方法论层（brainstorm / TDD / 计划 / 复查 / worktree），见 [superpowers.md](./superpowers.md) |
| `ecc@ecc` | 大量工程类技能（含 `code-review`、`verification-loop`、`terminal-ops` 等） |
| `context-mode@context-mode` | 大输出走沙箱处理 + 知识库检索，省上下文 |
| `github@openai-api-curated` | GitHub 操作 |
| `product-design` / `figma@openai-api-curated` | 设计方向与 Figma 互转 |

### 3.3 MCP 与功能开关

`~/.codex/config.toml` 里当前与本项目有关的几项：

- `[features] multi_agent = true`：允许子代理并行；`hooks = false`、`plugin_hooks = false`：**插件钩子未启用**，
  所以依赖钩子的自动化（如会话开始注入引导）不会生效，技能需要显式触发或由描述命中自动触发；
- `[mcp_servers.node_repl]`：持久 JS 执行环境（浏览器 / computer-use 相关）；`computer-use` 当前 `enabled = false`；
- `[projects."…/einoflow"] trust_level = "trusted"`：本仓库被标记为受信任目录。

## 4. 门禁层（做完 ≠ 做完，要能证明）

```bash
make backend-check     # gofmt + go vet + go build + go test（后端自检）
make web-check         # eslint + tsc + next build（前端自检）
make backend-test      # 只跑测试
```

集成测试需要真实依赖，环境变量开启：

```bash
./scripts/einoflow.sh start          # 起 PostgreSQL / Milvus / MinIO 等本地依赖
curl -s localhost:8888/readyz        # 期望 status=ready
EINOFLOW_TEST_PG_DSN=… go test ./... # 覆盖「代码没错、约束撞了」这类只有真库能发现的问题
```

规格侧的门禁：

```bash
openspec validate <change-id> --strict --no-interactive   # 提交变更前
openspec validate --specs --strict                        # 归档后校验主规格
```

**硬性约定**：门禁没过不进下一阶段；不允许「带病提交」（根 `AGENTS.md` 第 8 节）。
验证不了的事要如实说「没验证」，不要用「应该没问题」结案。

## 5. 权限与网络（会卡住你有两种，早知道早省事）

当前 Codex 会话的沙箱策略是 `workspace-write`：

- 可自由读；可写 `einoflow/`、`/tmp` 等授权目录；
- **写工作区之外**（`~/.codex`、全局 npm 目录等）需要授权；
- **创建 / 修改 / 安装类操作通常需要授权**：`npm install -g`、`codex plugin add`、`docker` 等；
- **沙箱内网络受限**：`raw.githubusercontent.com`、`developers.openai.com` 直连会被拒
  （本机实测：GitHub 走 `api.github.com` 可用，网页与 raw 域名不可用；git clone 会卡住）。

实操建议：

1. 需要联网或写工作区外时，直接带理由请求授权，不要绕；
2. 拉 GitHub 内容优先用 `https://api.github.com/repos/<o>/<r>/contents/<path>` 加
   `Accept: application/vnd.github.raw`，或 `.../tarball/<ref>`，不要用 `git clone`；
3. 需要外部 HTTP 服务（Ollama / MinerU / TEI）跑测试时，先确认依赖容器在跑。

## 6. 上下文纪律（决定长任务成败）

- **一个 change 一个会话**：从 proposal 到 archive 尽量不跨会话；跨了就先读 `PROGRESS.md` 第 9 节 + `openspec show <id>`；
- 大输出不进上下文：日志、构建输出、批量文件分析交给 `ctx_execute` / `ctx_execute_file`，只把结论带回来；
- 长会话到达阶段边界时主动收尾（更新 PROGRESS、提交），不要拖到自动压缩；
- 一次只推进一个 change；同时开多个会让 `tasks.md` 与 PROGRESS 的"当前状态"互相打架。

## 7. 可观测（跑起来看什么）

```bash
./scripts/einoflow.sh status     # 容器 / 进程 / 健康检查
./scripts/einoflow.sh logs       # make logs ARG=backend 看后端日志
curl -s localhost:8888/healthz   # 不查依赖，只看进程活着
curl -s localhost:8888/readyz    # 逐项探 PostgreSQL / MinIO / Milvus
```

端口：后端 `8888`、前端 `3001`、Attu `3000`、Milvus `19530`、MinerU `8000`。
