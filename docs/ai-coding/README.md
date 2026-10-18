# EinoFlow 的 AI Coding 工作方式（harness + OpenSpec + Superpowers）

> 本文档回答一个问题：**在这个仓库里，用 Codex 写代码应该按什么流程走。**
>
> 一句话：**harness 管「不许乱来」，OpenSpec 管「要做成什么样」，Superpowers 管「怎么保证做对」。**

最后更新：2026-10-12（首次建立：OpenSpec 1.14.1 初始化 × superpowers 7.0.0 安装）

相关分册：

- [harness.md](./harness.md)：外壳——规则、能力、门禁、权限、上下文纪律
- [openspec.md](./openspec.md)：规格层——一次变更怎么提、怎么写、怎么归档
- [superpowers.md](./superpowers.md)：方法论层——brainstorm / 计划 / TDD / 复查 / 收尾

## 1. 三层分工（先记住这张表）

| 层 | 解决的问题 | 载体（本项目实际落地） | 明确不放在这一层的东西 |
| --- | --- | --- | --- |
| **harness** | 让我「不许乱来」：什么能做、什么必须问、做完怎么算过 | `AGENTS.md` × 3（根 / `web/` / `backend/`）、`Makefile` 自检目标、Codex 的 skills / 插件 / MCP、沙箱与授权 | 具体某个功能的产品决策 |
| **OpenSpec** | 「要做成什么样」：意图、验收标准、边界 | `openspec/specs/`（现状真相）、`openspec/changes/<id>/`（本次变更提案） | 实现状态、技术债、踩坑记录（那是 `docs/PROGRESS.md`） |
| **Superpowers** | 「怎么保证做对」：执行方法论 | Codex 插件里的技能（brainstorming / TDD / code review / worktree…），**不落项目文件** | 任何项目文档产物（避免与 OpenSpec 双份） |

三层的边界只有在下面这一条上会打架，先定死：

> **落文件的事一律由 OpenSpec 与 `docs/PROGRESS.md` 承担，Superpowers 只贡献"怎么做"的手法。**
> 它的 `brainstorming` 当作对话手段（不写 design.md），`writing-plans` 当作 `tasks.md` 的写法规范（不另写计划文件）。

## 2. 端到端流水线

每个阶段都标出**门禁**：门禁没过就不要进入下一阶段。

```
① 想清楚      $openspec-explore（或 brainstorming）       产物：无文件，只有对话结论
              ▲ 门禁：说不清「改什么、为谁改、怎么算成功」就继续问，别进入②

② 规格        $openspec-propose <change-id>
              产物：openspec/changes/<id>/{proposal.md, specs/**, design.md?, tasks.md}
              ▲ 门禁（硬）：你读完 proposal + 规格增量并明确点头，才允许写代码
                           ——这是根 AGENTS.md 第 11 节的「先对齐」，现在有了文件载体

③ 计划/隔离   分支 feat/xxx、fix/xxx（沿用 AGENTS.md 第 8 节）
              需要并行时才开 worktree（superpowers: using-git-worktrees）
              产物：tasks.md 里可勾选的任务清单（不再另写计划文档）

④ 实现        TDD：先写失败测试 → 最小实现 → 变绿 → 小步提交 <type>: <简述>
              产物：代码 + 测试
              ▲ 门禁：make backend-check / make web-check 必须全绿（见 harness.md）

⑤ 复查        superpowers: requesting-code-review + 本仓库规则
              产物：审查结论；严重问题清零后才继续
              ▲ 门禁：Critical 级问题未清零不得归档

⑥ 归档        $openspec-archive-change <id>
              产物：delta 折回 openspec/specs/，change 进 changes/archive/
              ▲ 门禁：openspec validate --strict --all 通过

⑦ 收尾        更新 docs/PROGRESS.md（模块状态 / 技术决策 / 技术债）→ 提交 / PR
```

关于 ⑦ 与 ⑥ 的顺序：**OpenSpec 归档的是「规格」，PROGRESS 记录的是「实现状态与踩过的坑」**，
两者都要更新，但内容不重复——PROGRESS 里引用 change-id 即可。

## 3. 谁写哪个文件（唯一真相源）

| 你想记录的东西 | 写到哪里 | 不该写到哪里 |
| --- | --- | --- |
| 这个系统现在是什么行为 | `openspec/specs/<capability>/spec.md` | PROGRESS 的模块表（只写状态，不写行为契约） |
| 这次要改什么、为什么、验收标准 | `openspec/changes/<id>/{proposal,specs,tasks}.md` | 临时聊天记录、PR 描述 |
| 技术选型与理由（"为什么用 GORM"这类） | `docs/PROGRESS.md` 第 5 节（D1…） | design.md（design.md 只管本次变更的取合，不承担历史） |
| 实现进度、模块状态、未完成事项 | `docs/PROGRESS.md` 第 3 / 4 节 | openspec/specs（specs 不描述"做到哪一步"） |
| 已知限制与技术债 | `docs/PROGRESS.md` 第 6 节 | — |
| 长期规则（技术栈红线、分层、提交规范） | `AGENTS.md` × 3 | 本文档（本文档只讲流程） |

## 4. 两个入口：直接改，还是走一次变更

这张表把根 `AGENTS.md` 第 11 节的豁免规则与 OpenSpec 自己的决策树合成了同一套判断，**只认这一套**：

| 情况 | 走法 |
| --- | --- |
| 明显的 bug 修复（恢复既有行为）、错别字 / 文案 / 格式化、注释 | **直接改**，不用建 change；commit 信息说清楚即可 |
| 非破坏性的依赖升级、配置调整、为既有行为补测试 | **直接改**，但要在 PROGRESS 里留一句（若影响使用方式） |
| 新功能、新能力、新接口 / 新 DSL 字段 | **建 change**（`$openspec-propose`） |
| 破坏性变更（DSL、表结构、接口语义）、架构调整 | **建 change**，且 design.md 必写，proposal 里标 **BREAKING** |
| 性能 / 安全相关且会改变行为 | **建 change** |
| 拿不准 | **建 change**（更安全的默认） |

判断标准与 AGENTS.md 第 11 节一致：**只要改动会让用户看到不一样的东西（页面 / 交互 / 接口 / 数据），就走 change。**

## 5. 日常命令速查

```bash
# —— OpenSpec ——
openspec list                      # 进行中的 change
openspec list --specs              # 已有的能力规格
openspec show <id> --diff          # 看某个 change 的规格增量
openspec validate <id> --strict --no-interactive
openspec validate --specs --strict # 校验全部主规格
openspec archive <id> --yes        # 归档并折回主规格
openspec view                      # 交互式看板

# —— 自检门禁 ——
make backend-check                 # 格式化 + vet + 构建 + 测试
make web-check                     # lint + 类型检查 + 构建

# —— 插件 / 技能 ——
codex plugin list                  # 已装插件与市场
```

在 Codex 会话里，OpenSpec 的技能用 `$` 前缀调用（共 12 个：下面核心 6 个 + 扩展 6 个，扩展的见 openspec.md 第 3 节）：
`$openspec-explore` · `$openspec-propose` · `$openspec-apply-change` ·
`$openspec-update-change` · `$openspec-sync-specs` · `$openspec-archive-change`。

## 6. 本仓库当前落地状态

| 项 | 状态 | 说明 |
| --- | --- | --- |
| OpenSpec CLI | ✅ 1.14.1（全局 npm） | 由 0.21.0 升级而来；旧版的 `~/.codex/prompts/openspec-*.md` 形态已废弃 |
| `openspec/` 骨架 | ✅ 已初始化 | `openspec init --tools codex --language 中文（简体）`，项目语言设为简体中文 |
| Codex 侧技能 | ✅ 12 个 | 生成在 `.agents/skills/openspec-*/SKILL.md`（Codex 认 skills，不认 `/openspec-*` 命令）；核心 6 + 扩展 6 |
| capability 规格 | ✅ 6 个 | `knowledge-base` / `rag-pipeline` / `rag-retrieval` / `model-config` / `chat-conversation` / `agent-chat`，见 openspec.md 第 5 节 |
| change 清单 | ✅ 4 个（全部归档） | `add-react-agent-chat`（28/28）/ `add-retrieval-trace-console`（17/17）/ `add-dictionary-keyword-tokenizer`（13/13）/ `add-chat-tests`（16/16）。状态见 `docs/PROGRESS.md` 第 9.10 节 |
| Superpowers | ✅ 7.0.0（插件 `superpowers@superpowers-dev`） | 见 superpowers.md；本机用本地市场安装，官方 curated 市场当前是 6.3.0 |
| 扩展工作流 | ✅ 已启用（2026-10-12） | 全局 `profile: custom` + 12 个工作流；新增 6 个技能 `new / continue / ff / verify / bulk-archive / onboard` |

## 7. 三条容易踩的坑

1. **不要为了「补全」而批量回填规格。** OpenSpec 官方明确建议按变更逐步长成；
   本项目回填的 6 个 capability 都是「下一批变更要动的区域」，不是全仓库清点。
2. **版本管理范围是明确的**（2026-10-12 定稿）：`openspec/`、`.agents/`、`docs/` 与三层
   `AGENTS.md` 都进库；`docs/rag-test/`、`testdata/`、`.env` 继续忽略。
   别顺手放宽忽略规则——**被忽略的文件在 `git status` 里根本不显示**，
   容易长期"以为提交了"（`web/AGENTS.md` 与 `backend/AGENTS.md` 就这样被漏掉过整整一轮）。
3. **Specs 描述行为，不描述实现。** 类名、函数名、表名不进 `spec.md`；
   能被实现替换而不改变外部行为的细节，都不属于规格。
