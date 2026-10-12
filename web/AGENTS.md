# EinoFlow Web（前端）规则

> 适用范围：`web/` 目录下全部代码。
> 本文件在 `web/` 内叠加于根 `AGENTS.md` 之上；冲突时以更具体的本文件为准。

## 1. 技术栈（固定）

- Next.js 14+ App Router + React 18
- TypeScript 严格模式（strict: true）
- 包管理器：pnpm（禁止 npm / yarn，不要生成 package-lock.json 或 yarn.lock）
- 画布：@xyflow/react
- 状态：Zustand
- 样式：TailwindCSS + shadcn/ui
- 编辑器：Monaco Editor
- 网络：自定义 SSE / HTTP 统一请求封装

依赖管理：一律用 `pnpm add` / `pnpm add -D` / `pnpm remove`，安装用 `pnpm install`；
新增依赖前先说明用途。只提交 `pnpm-lock.yaml`，不提交其它 lockfile。

## 2. 目录约定

```
web/
├── app/            # App Router：路由、layout、page、loading、error
├── components/     # 通用组件；shadcn/ui 产物放 components/ui
├── features/       # 按业务域组织：workflow / knowledge / chat / plugin / model / observability
├── stores/         # Zustand store（按域拆分）
├── lib/            # 请求封装、SSE 封装、工具函数
├── types/          # 全局类型，含 DSL 类型定义
├── hooks/          # 复用 hooks
└── public/
```

没有特殊理由就按此组织；新增顶层目录前先说明。业务代码进 `features/`，不要把所有东西堆进 `components/`。

## 3. 编码规则

- TS 严格：禁止 any；确实不确定用 unknown + 类型收窄；禁止 @ts-ignore（必要时用 @ts-expect-error 并写明原因）。
- 组件边界：默认 Server Component，只有需要交互或浏览器 API 时才加 'use client'，并尽量放在叶子组件，避免整页变客户端组件。
- 请求：所有后端调用走 lib/ 的统一请求封装，禁止在组件里写裸 fetch，禁止硬编码后端地址（用环境变量）。

## 4. 视觉方向（硬性，改页面前先读）

**所有页面相关的开发都要先走 `frontend-design` skill**（brainstorm → 自我批判 → 再写代码），
并遵循下面这套已经定下来的方向。改颜色 / 字体 / 圆角前先说明「为什么现在的方向不够用」。

方向：**工程图纸 + 仪表读数**（面向工程师的 AI 管线控制台，不是消费级产品页）。

| 轴 | 规则 |
| --- | --- |
| 色 | `ink #101828` 结构与他人文字 · `paper #F2F4F8` 冷纸底 · `grid #D8DEE9` 网格线与分隔 ·
`pri #2A4FD7` **唯一的饱和色**，只给交互与主操作 · 读数色 `ok #17835A` / `warn #A9660A` / `fault #B3261E` |
| 字 | Geist Sans 做界面；**Geist Mono 只给读数**（分数、字数、段数、耗时、指纹、id），
统一用 `.reading`（含 tabular-nums，多行数字才能对齐）。区块眉标用 `.eyebrow`（大写 + 0.14em 字距） |
| 形 | 圆角 2~3px；**不用阴影**；面板用 `Panel`（1px 网格边框 + 头部细规则线）；
面板头部右端挂这一块的真实读数，像工程图上的尺寸标注 |
| 记住点 | 主内容区铺一层极淡的图纸网格（`.blueprint-grid`，4.5% 不透明度）+ 仪表读数条。
**只有这两个记忆点**，其余保持克制 |
| 动效 | 不加装饰性动画：这个主题里动效只会让人感觉像 AI 生成的页面。
只保留状态变化的过渡色 |

不要做的事（都是「看起来像模板」的信号）：

- 白卡片 + 灰底 + 大圆角 + 阴影的组合（上一版的观感就是这个问题）；
- 用 Tailwind 默认的 emerald / amber / red 表达状态（要用上面的读数色）；
- 数字用非等宽字体、或让同一列数字左右参差；
- 为了「好看」而加的图标、渐变、编号（`01 / 02` 只有在内容真的是序列时才用）。

质量底线（不许砍）：响应式到移动端、键盘可见焦点（`:focus-visible` 已全局定义）、尊重
`prefers-reduced-motion`。

## 5. 布局与滚动（硬性约定）

**文档本身永远不滚动。** 只有明确指定的滚动区才有滚动条；侧边栏、二级导航、工具条都不随内容滚动。

落地方式（改动布局时按此检查）：

1. `app/globals.css` 把 `html, body` 固定为一屏高且 `overflow: hidden`；
2. `components/layout/app-shell.tsx` 用 `h-full overflow-hidden`，侧栏自己 `overflow-y-auto`（导航项过多时才滚动）；
3. 页面内容一律用 `components/layout/page-container.tsx` 包裹——**它是唯一的滚动层**；
4. flex 布局里作为滚动/收缩父级的子项要加 `min-h-0` / `min-w-0`，否则内容会把容器撑开、滚动条跑到别的层上；
5. 不要用 `min-h-screen` 给页面撑高（那会让整页滚动）；需要满屏高度用 `h-full`；
6. 横向不滚动（`overflow-x-hidden`）：出现横向滚动条通常是某个子项缺 `min-w-0`，
   要修布局，不要放开横向滚动把问题藏起来。

判断标准：打开页面后 `document.documentElement.scrollHeight` 应等于窗口高度，
纵向滚动条只出现在内容区那一个容器上。

### 表单与对齐（同样硬性）

7. **字段说明用悬浮图标**：写成标签旁的小 `(?)`（`Field` 的 `hint`，见 `components/ui/field.tsx`），
   鼠标悬浮或键盘聚焦时显示。不要写成输入框下方的常驻文字——那会让同一行各字段高度不一致，
   标签与控件被迫错位（已经踩过一次）。确实需要常显的状态（如「已保存：sk-***0001」）才用 `help`；
8. **同行控件必须等高**：输入框、下拉、按钮统一用 `CONTROL_HEIGHT`（field.tsx 导出），
   否则按底边对齐时矮一点的控件顶部会低几像素，看起来就是没对齐；
9. 横排表单里按钮要与输入框对齐：给按钮加一个与「标签行 + 间距」等高的占位列，
   不要靠 `pt-*` 之类的手调数值（换个字号就散了）。
10. **删除类操作必须二次确认**，且要用 `components/ui/confirm-dialog.tsx`
   （`useConfirm()`）：禁止用 `window.confirm`——它是浏览器原生弹窗，样式不可控、
   文案只能一行纯文本，也无法说明「删的是什么、后果是什么」。
   弹窗文案三件套：标题写清「对什么做什么」，说明写清后果（可恢复 / 不可恢复），
   确认按钮与标题里的动作同名（删除 → 删除），默认焦点落在「取消」上。
- SSE 流式：统一封装解析逻辑，必须处理中断、超时、错误回退与取消。
- **别发注定失败的请求**：必填项还没填完时不要提交（例如分隔符模式还没填分隔符就
  请求预览），否则用户看到的是后端的一句「入参不合法」，而实际情况只是表单还没填完。
  做法是前端先自查完整性，给出中性提示（「填完会自动预览」），再在填好后自动发起。
- **流式渲染三件套**（对话类页面必须都做到）：
  ① 打字机逐字揭示（`hooks/use-typewriter.ts`），积压越多揭示越快，绝不落后于模型；
  ② 钉底滚动（`scrollTop = scrollHeight`，且只在用户本来就在底部时才跟随）；
  ③ 系统「减少动效」开启时退化为瞬时呈现（`hooks/use-prefers-reduced-motion.ts`）。
  另外：历史消息不要重放打字动画（只有本次会话新产生的回答逐字揭示）。
- **禁止打开 Next 的 `compress`**（`next.config.mjs`）：开启后 Next 会把**代理转发**的
  SSE 响应先压缩再发送，整条流被攒成一大块，实测首字延迟从 3 秒退化到 11.5 秒。
  这一条被踩过一次，属于「改一行配置静默毁掉流式」的坑。
- 状态：跨组件共享状态放 Zustand，组件内局部状态用 useState，派生数据即时计算，不要塞进 store；禁止另起一套全局状态。
- 样式：Tailwind 原子类 + shadcn/ui，合并类名用 cn()；避免内联 style，避免重复造已有 UI 组件。
- 表单：统一用 zod 做校验，错误提示与字段类型要和后端契约一致。
- 命名：组件 PascalCase，hook 用 useXxx，其余变量与函数 camelCase；文件名与默认导出的组件名一致；目录名用 kebab-case。
- 复用：可复用逻辑分别抽到 hooks/（逻辑复用）、components/（UI 复用）、lib/（工具复用）；禁止复制粘贴，同一段逻辑第二次出现就抽离。
- 面向接口：请求、存储、画布数据源等易变能力先用 TS interface / type 定义契约，组件依赖抽象，实现可替换、可扩展。
- 注释：重要逻辑、复杂交互、非直觉实现必须写注释，**注释一律用中文**，解释原因而非复述代码。

## 6. 画布与 DSL（核心模块规则）

- @xyflow/react 的节点类型、节点数据、边数据必须有明确 TS 类型，禁止用松散的通用对象。
- 节点配置（右侧配置面板）与画布状态（节点/边/视口）分离管理，不要混在一个 store 里。
- DSL 序列化/反序列化集中在一个模块（lib/ 或 features/workflow/），其他组件只调用，不各自拼 JSON。
- 前端只负责把画布转成 DSL、把 DSL 还原成画布，不做任何执行逻辑。
- DSL 结构改动必须与后端同步，并在 types/ 中更新对应类型。

## 7. 禁止事项（红线）

- 在前端实现任何 AI 逻辑：模型调用、RAG 检索、向量、Agent 编排、工具执行。
- 组件内裸 fetch、硬编码后端 URL 或端口。
- 绕过 Zustand 零散管理全局状态，例如用全局变量、事件总线替代 store。
- 单边修改 DSL 字段或接口结构而不与后端对齐。
- 使用 npm 或 yarn 安装/运行依赖，或提交 npm/yarn 的 lockfile。
- 在页面组件里复制粘贴重复实现同一逻辑，或把明显的可复用逻辑写死在页面里而不抽离。
- 提交密钥、Token、.env（提交 .env.example 模板即可）。
- 图省事把 'use client' 加在 app/ 的 layout 上，导致整站客户端渲染。

## 8. 自检

提交前依次执行并通过：

```
pnpm install
pnpm lint
pnpm exec tsc --noEmit
pnpm build
```

要求无类型错误、无未使用变量、无遗留 console.log。

## 9. 参考实现（本地 Dify 前端）

路径：`/Users/shiqingliang/Documents/Projects/PublicProjects/dify/web`（只读参考）。

重点参考：`app/components/workflow/`（画布、节点、`hooks-store` 状态组织）、`service/`（请求层划分）、`context/`、`features/`。

只借鉴 UI 结构与交互设计，**不要复制其代码、静态资源与文案**（Dify 有独立许可证与版权约束）；实现按本项目技术栈与目录规范自行编写。
