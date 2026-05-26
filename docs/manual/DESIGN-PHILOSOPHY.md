# 设计理念
<div class="title-en">Design Philosophy</div>

## 目标
<div class="title-en">Goal</div>

Worktrail 的目标不是再造一个新的知识平台，而是把 AI 编程会话里真正有价值、需要跨会话保留的内容，以最低额外复杂度沉淀下来。

这一章回答的是方法论层面的“为什么”，重点解释这套手册背后的整体偏向。

它的总体偏向很明确：

- 本地优先，而不是依赖中心化服务
- Markdown 优先，而不是把知识锁进私有数据库
- 先保留证据，再形成候选知识，最后才进入正式知识
- review 保持在 Agent 对话里，而不是再引入一个额外 UI
- 写操作必须经过明确确认，而不是让自动化越过人审
- 维护尽量低干预，而不是依赖后台服务和隐式自动化

这意味着 Worktrail 有意放弃一部分“看起来更自动”“看起来更智能”的功能，换取更清晰的边界、更低的误写风险，以及更稳定的长期可维护性。

## 为什么坚持 Local-First
<div class="title-en">Why Local-First Matters</div>

Worktrail 解决的问题，发生在本地真实仓库和本地 Agent 会话里。

如果把这类知识和状态管理建立在远端服务上，通常会引入新的问题：

- 项目上下文和会话状态需要额外同步
- 用户要多维护一套服务生命周期和权限边界
- 工具越像平台，越容易偏离“辅助编码会话”这个原始目标

所以 Worktrail 把核心数据放在用户目录和项目目录中，让知识、状态、候选记录都能跟着本地仓库与本地工作流一起运行。

## 为什么 Markdown 是 Source of Truth
<div class="title-en">Why Markdown Is the Source of Truth</div>

Worktrail 不把正式知识放进一套只能由工具自己理解的内部存储，而是使用 Markdown 和 frontmatter 作为长期真相来源。

这个选择背后的考虑很直接：

- 文档本身可以直接读、直接 review、直接纳入版本控制
- 知识不会被绑死在某个私有索引或二进制格式里
- Agent、脚本和人类都可以围绕同一份内容工作

本地索引仍然存在，但它只是可重建的加速层，不是最终真相。

## 为什么要把证据、候选知识和正式知识分层
<div class="title-en">Why Evidence, Candidates, and Formal Knowledge Are Separate</div>

会话里出现的内容，并不天然等于正式知识。

Worktrail 把知识生命周期拆成三个层次：

1. 原始证据，例如 transcript evidence、导入记录、handoff
2. pending semantic candidates，也就是已经被提炼但还未正式采纳的知识候选
3. formal knowledge，也就是经过 review 和明确确认后进入正式目录的内容

这样做的原因是：

- 原始证据需要保留 traceability，但不应该默认进入正式知识
- 提炼后的候选知识需要 review，而不是直接生效
- 正式知识需要更稳定、更可复查的标准

这也是为什么 `note add`、`import`、`distill apply`、`handoff` 都默认只会生成候选或操作记录，而不会直接修改正式知识。

## 为什么 Review 保持 Chat-Native
<div class="title-en">Why Review Stays Chat-Native</div>

Worktrail 明确不做 TUI，也不做独立 Web UI 或 dashboard。

不是因为 review 不重要，而是因为 review 已经有一个天然界面：你当前正在使用的 Agent 对话。

如果再引入额外界面，通常会带来三类成本：

- 用户必须离开当前编码会话，切到另一个界面做确认
- 工具需要多维护一套交互层、状态层和权限边界
- review 的上下文会被拆散，不再和当前任务对话保持一致

所以 Worktrail 的设计是：CLI、skills 和 MCP 提供读取与执行能力，真正的人审和确认留在对话里完成。

## 为什么写操作必须显式确认
<div class="title-en">Why Explicit Confirmation Is the Write Boundary</div>

Worktrail 不把“自动判断对不对”当成默认前提，而把“明确确认后再写入”当成安全边界。

这条边界尤其适用于：

- `promote`
- `merge`
- `discard`
- `archive`
- `retire`
- 任何会改变 candidate 状态或正式知识的动作

背后的原因很简单：Agent 可以帮助你发现、总结、起草、分组、解释风险，但最终是否采纳一条知识，应该由使用者在明确上下文中做决定。

## 为什么不要额外运行面
<div class="title-en">Why Worktrail Avoids Extra Runtime Surface</div>

Worktrail 明确不做这些东西：

- daemon、watcher、后台常驻服务
- HTTP MCP server
- Web dashboard
- embedding / vector database
- custom external command provider

这些能力看起来能提升“自动化程度”，但同时也会放大运行面、权限面和维护成本。

Worktrail 更偏向显式触发：

- CLI 在需要时运行
- hooks 在明确事件点运行
- MCP 只通过 stdio 由 Agent 启动
- maintenance 通过上下文提示和只读计划逐步展开

这让系统更可理解，也更容易在本地环境里长期稳定运行。

## 为什么区分用户级和项目级
<div class="title-en">Why User Scope and Project Scope Are Separate</div>

不是所有知识都应该留在项目里，也不是所有偏好都应该跟着仓库走。

因此 Worktrail 明确区分：

- 用户级 scope：跨项目复用的偏好、workflow、prompt、通用 lesson
- 项目级 scope：当前仓库专属的规则、决策、架构、验证和状态

这种分离能减少两种常见问题：

- 把个人偏好误写进项目正式知识
- 把项目专属上下文错误复用到其他仓库

## 为什么维护流程强调低干预
<div class="title-en">Why Maintenance Is Low-Intervention</div>

Worktrail 承认维护是长期需求，但不希望用户靠记住一长串命令来维持系统。

因此它的方向不是做“自动后台整理”，而是：

- 在 `context` 中暴露 maintenance hints
- 用 `review plan`、`evidence plan`、`maintain knowledge` 提供只读计划
- 由当前 Agent 帮助起草 distill proposal 或汇总建议动作
- 在真正要改变状态时再请求明确确认

这让日常维护变得更容易发现、更容易委托给 Agent，但不会悄悄跨过写入边界。

## 实际取舍
<div class="title-en">The Practical Trade-Off</div>

从使用者角度看，Worktrail 的核心取舍是：

- 少一点“默认全自动”的幻觉
- 多一点“边界清楚、流程可查”的纪律

它追求的不是把所有会话内容都自动吸进去，而是在长期使用里，让知识、状态和 review 过程保持可解释、可恢复、可审计。
