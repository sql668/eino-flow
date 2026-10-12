# Superpowers：执行方法论（怎么保证做对）

> Superpowers 是一套「技能 + 流程」的开发方法论：先问清楚要什么，再写可执行计划，
> 用 TDD 实现，逐任务复查，最后干净地收尾分支。
> **它不产出项目文档**——在本项目里，落文件的事由 OpenSpec 承担。

最后更新：2026-10-12（已装 `superpowers@superpowers-dev` 7.0.0）

## 1. 它提供什么

七个串联的环节（技能名保留英文，方便直接点名调用）：

| 环节 | 技能 | 在本项目的落点 |
| --- | --- | --- |
| ① 把意图问出来 | `brainstorming` | 与 `$openspec-explore` 重合，**只当对话手段**，产物归 OpenSpec |
| ② 隔离工作区 | `using-git-worktrees` | 需要并行时用；否则按 AGENTS.md 开 `feat/xxx` 分支即可 |
| ③ 写可执行计划 | `writing-plans` | **不另写计划文件**，作为 `openspec/changes/<id>/tasks.md` 的写法规范 |
| ④ 逐任务执行 | `subagent-driven-development` / `executing-plans` | 任务多、彼此独立时用；本项目多数任务串行，用后者 |
| ⑤ 实现 | `test-driven-development` | **必须**：RED → GREEN → REFACTOR，先写失败测试 |
| ⑥ 复查 | `requesting-code-review` / `receiving-code-review` | 每个任务组之后；与本仓库的分层规则一起看 |
| ⑦ 收尾 | `finishing-a-development-branch` | 合并 / PR / 保留 / 丢弃四选一，附带测试确认 |

另有两个高频辅助技能：

- `systematic-debugging`：四阶段定位根因（本项目排 RAG / SSE 这类问题很实用）；
- `verification-before-completion`：宣称「做完了」之前先跑证据，与 harness 的门禁配合。

## 2. 与 OpenSpec 的边界（唯一的冲突点）

两者都会「先计划再动手」，所以必须分工，否则会出现两份互相漂移的计划文档：

| 问题 | 由谁回答 | 落在哪 |
| --- | --- | --- |
| 要建成什么样、验收标准是什么 | OpenSpec | `changes/<id>/proposal.md` + `specs/**` |
| 分成哪些任务、每个任务怎么验证 | OpenSpec（写法照 `writing-plans` 的标准） | `changes/<id>/tasks.md` |
| 用什么手法把任务做对（TDD 节奏、调试方法、复查清单） | Superpowers | 不进文件，体现在过程里 |

一句话：**OpenSpec 决定"做什么"，Superpowers 决定"怎么做"；两边都不写对方的产物。**

## 3. 本机安装方式（实测记录）

```bash
codex plugin list                                   # 看已装插件
codex plugin add superpowers@superpowers-dev        # 本次实际用的命令
```

安装细节与原因：

| 项 | 值 | 说明 |
| --- | --- | --- |
| 插件标识 | `superpowers@superpowers-dev` | 版本 **7.0.0**，状态 installed + enabled |
| 插件根目录 | `~/.codex/plugins/cache/superpowers-dev/superpowers/7.0.0/` | Codex 管理的缓存目录 |
| 市场来源 | 本地目录 `~/.codex/.tmp/marketplaces/superpowers-src` | 该目录是 **main 分支的 tarball 快照 + 本地 git 仓库**，不能删 |
| 为什么不用官方市场 | `codex plugin marketplace add` 依赖 `git clone`，本机沙箱内 GitHub 直连被网络阻断；改用 `api.github.com` 的 tarball 接口下载 | 官方 curated 市场里也有 `superpowers@openai-api-curated`，但版本是 **6.3.0**（更旧），且随 App 更新 |

维护方式：

```bash
# 想升级到更新版本（重新拉 main 快照）
curl -sL -H "Accept: application/vnd.github+json" \
  https://api.github.com/repos/obra/superpowers/tarball/main -o /tmp/sp.tar.gz
tar -xzf /tmp/sp.tar.gz -C ~/.codex/.tmp/marketplaces/superpowers-src --strip-components=1
cd ~/.codex/.tmp/marketplaces/superpowers-src && git add -A && git commit -m "update"
codex plugin add superpowers@superpowers-dev

# 想换成官方 curated 的 6.3.0（一条命令，但更旧）
codex plugin add superpowers@openai-api-curated
```

## 4. 触发与验证

- 技能靠**描述命中自动触发**，也可以在会话里直接点名，例如
  「用 superpowers 的 `test-driven-development` 做这一步」；
- 本机 `~/.codex/config.toml` 里 `hooks = false`、`plugin_hooks = false`，且 superpowers 的
  `plugin.json` 声明的是空 `hooks: {}` → **没有会话启动钩子做自动注入**，
  所以「开场自动进入方法论」这件事在本机不会发生，需要显式触发；
- 会话内验证：让 Codex 列出技能，或直接查看
  `~/.codex/plugins/cache/superpowers-dev/superpowers/7.0.0/skills/` 下的技能目录。

## 5. 本项目的实际用法（最小集）

不做全套仪式，只保留对这个仓库真正有用的四件：

1. **开始一个 change 前**：`brainstorming` 式追问（配合 `$openspec-explore`），把「ReAct 问答到底要支持哪些工具」「召回测试页要回答什么问题」这类问题问清楚；
2. **写 tasks.md 时**：按 `writing-plans` 的标准——每个任务小到一次会话能完成，且自带验证方式；
3. **实现时**：`test-driven-development`——先写失败测试（Go 表驱动测试 / 前端 Vitest），再实现；
4. **收尾前**：`requesting-code-review` + `verification-before-completion`，把 `make backend-check` / `make web-check` 的输出当作证据，而不是凭印象宣布完成。

`using-git-worktrees` 只在真的并行做两件事时用；本项目分支策略以根 `AGENTS.md` 第 8 节为准。
