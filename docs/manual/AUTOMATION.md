# 自动化
<div class="title-en">Automation</div>

## 目标
<div class="title-en">Goal</div>

这一章解释的不是“Worktrail 有多少命令”，而是“哪些事情会被自动化、由什么触发，以及自动化到哪里为止”。

Worktrail 的自动化目标很克制：

- 让 Agent 更容易发现上下文、状态和待维护工作
- 让重复性的读取、汇总、起草和提示尽量自动化
- 把真正改变 candidate 状态或正式知识的动作留在明确确认之后

它追求的是低干预自动化，而不是无人值守自动运行。

## 什么算 Worktrail 的自动化
<div class="title-en">What Counts as Automation</div>

在 Worktrail 里，自动化主要来自四层：

1. agent 集成安装后的运行时文件，例如 hooks、settings 配置和用户级 skills
2. 对话内 skills，例如 `worktrail-context`、`worktrail-review`、`worktrail-distill`、`worktrail-maintain`、`worktrail-handoff`
3. `context` 暴露的 maintenance hints 和只读计划命令
4. hooks 在特定事件点生成 state、checkpoint、runtime records 或其他操作记录的能力

Worktrail 的自动化不是“后台一直运行的系统”，而是围绕当前 Agent 会话按事件点显式触发。

## 自动化从哪里开始生效
<div class="title-en">When Automation Becomes Active</div>

自动化要满足两个前提：

1. 当前机器上的 `worktrail` CLI 可用
2. 当前项目已经显式 opt-in，也就是存在 `.worktrail/`

通常顺序是：

```bash
worktrail init
worktrail install <tool> --user --project
```

这里要区分两件事：

- `worktrail init` 负责初始化用户级和项目级 Worktrail 根目录
- `worktrail install ...` 负责安装工具集成文件，例如 hooks、settings 和用户级 rules/skills

用户级 Worktrail skills 不应在没有 `.worktrail/` 的项目里自动跑常规工作流；没有这个标记时，Worktrail 仍然只适合显式 init、install、inspect 或 repair 请求。

## Hooks 自动化了什么
<div class="title-en">What Hooks Automate</div>

hooks 的职责是把会话事件转换成 Worktrail 可用的运行材料。

它们适合做的事情包括：

- 在会话开始或恢复时帮助载入上下文
- 在 compact、stop 或 session end 之类的事件点更新 active state 或写入 checkpoint
- 生成工作记录、checkpoint、takeover/runtime 记录或其他操作记录
- 把某些事件转化为后续 review/maintenance 可见的输入

它们不适合做的事情包括：

- 自动 promote
- 自动 merge
- 自动 discard
- 自动修改正式知识

也就是说，hooks 可以“自动采集和准备”，但不能“自动采纳”。

## ZCode Agent 的自动化边界
<div class="title-en">ZCode Agent Automation Boundary</div>

对 ZCode Agent 来说，Worktrail 的自动化最佳实践不是依赖 hooks，而是依赖三件事：

1. `~/.zcode/AGENTS.md` 中的长期规则，用来把任务语义路由到正确的 Worktrail 工作流
2. `~/.zcode/skills/` 中已安装的 Worktrail skills，用来封装可复用流程
3. `worktrail` CLI 本身，用来执行 `context`、`resume`、`search`、`state`、`handoff` 等命令

因此，在 ZCode Agent 中更准确的说法是“语义自动化”：

- Agent 在读到规则后，会在合适任务场景下主动选择 Worktrail 技能或对应 CLI
- 这类自动化依然受 `.worktrail/` opt-in 门控约束
- 它不依赖 Worktrail-managed 的项目级 hooks、runtime settings 或 transcript import

## Skills 自动化了什么
<div class="title-en">What Skills Automate</div>

skills 是 Worktrail 的对话内工作流入口。它们把多步命令链包装成可复用流程，让 Agent 在正确时机跑正确的读写顺序。

常见分工是：

- `worktrail-context`：任务开始、恢复旧任务、继续长任务时先生成 Context Pack
- `worktrail-review`：把 pending semantic candidates 按推荐动作分组，并在确认后执行安全的 CLI 写操作
- `worktrail-distill`：从 evidence 生成 distill pack、proposal、validate/apply 链路
- `worktrail-draft`：在显式持久化请求后，把 requirement、architecture、workflow 等非 ADR 语义产物直接写成 pending candidate；当 Worktrail 是唯一目标时不创建额外 `docs/` 或 `.plans/` 副本
- `worktrail-adr`：在显式持久化请求和中立内容就绪门禁之后，把标准 ADR 写成 pending `decision` candidate，再进入 review
- `worktrail-maintain`：串起 `context "maintenance"`、`distill --summary`、`review plan`、`evidence plan`
- `worktrail-handoff`：只在显式交接边界创建 durable handoff，例如用户明确要求 handoff、切 Agent、切 chat 或结束当天工作

skills 自动化的是流程编排，不是绕过边界。

`worktrail-draft` 默认使用带 Worktrail frontmatter 的 stdin 内容，确保 promote 后保留 stable id、topic、scope、type 和 lifecycle。只有用户明确要求普通文件，或已经提供现有文件时，才使用并保留 standalone artifact；它不会自动 promote。

`worktrail-adr` 不要求安装 `design-review-loop` 或其他 agent-skills。若上下文已有兼容的 ADR clean review，可把它当作就绪证据；否则只执行标准结构校验，并要求用户明确确认内容评审已完成。它不会自动 promote，也不会直接写 `.worktrail/decisions/`。

## Hooks 和 Skills 的边界
<div class="title-en">Hook and Skill Boundaries</div>

Worktrail 只保留 CLI、hooks 和 skills 这三类自动化入口。

- hooks 负责在明确事件点自动写工作记录、checkpoint、runtime records 和日志
- skills 负责在对话里编排 `context`、`review`、`distill`、`handoff`、`resume` 等流程
- 高风险写动作仍然通过显式 CLI 执行，并保留人工确认边界

## 低干预维护如何自动发现工作
<div class="title-en">How Low-Intervention Maintenance Works</div>

Worktrail 不要求用户记住所有维护命令，而是尽量把“还有什么待处理”暴露在正常工作流里。

核心入口是：

```bash
worktrail context "maintenance"
```

当存在 pending evidence、pending semantic candidates 或 evidence lifecycle 动作时，`context` 会给出 maintenance hints，例如：

- `worktrail distill --pending --summary`
- `worktrail review plan --format json`
- `worktrail evidence plan --format json`

如果使用已安装的 `worktrail-maintain` skill，Agent 会先跑只读发现链，再在需要改变状态时请求明确确认。

这类自动化的重点是：

- 先让待处理工作变得可见
- 先输出只读计划
- 只有在真正要变更状态时，才请求确认

## 哪些事情仍然不会自动做
<div class="title-en">What Worktrail Will Not Automate</div>

Worktrail 明确不做这些自动化：

- 不做后台 daemon、watcher 或 scheduler
- 不做 Web dashboard 或 TUI
- 不做自动 promote / merge / discard / archive
- 不做 hooks 自动采纳知识
- 不做隐式后台 transcript 扫描和后台同步

这些边界不是缺点，而是为了避免自动化越过 review、权限和可解释性边界。

## 什么时候必须人工确认
<div class="title-en">When Human Confirmation Is Required</div>

只要动作会改变 candidate 状态或正式知识，就应保留明确确认边界。

典型例子包括：

- `worktrail promote`
- `worktrail merge`
- `worktrail discard`
- `worktrail restore`
- `worktrail retire`
- `worktrail evidence archive`
- `worktrail evidence discard`
- `worktrail review apply-plan --confirm`
- `worktrail review apply-candidates ...`
- `worktrail distill apply`

Agent 可以提前完成这些准备工作：

- 汇总推荐动作
- 解释 warning 和 source traceability
- 生成候选命令
- 起草 distill proposal

但最后的“执行写操作”仍然应该在你确认后才发生。

## 一个典型的自动化生命周期
<div class="title-en">A Typical Automation Lifecycle</div>

从使用者角度看，常见链路大致是：

1. `worktrail init` 创建 `.worktrail/`
2. `worktrail install <tool> --user --project` 安装工具集成
3. 任务开始时，通过已安装的 `worktrail-context` skill 或等价 CLI 命令载入上下文
4. 长任务中，通过 state、checkpoint、hooks 或 handoff 保留进度
5. evidence 和 semantic candidates 通过 review/distill/maintain 被自动发现
6. Agent 总结建议动作并等待确认
7. 只有确认后，CLI 才执行正式写入

这就是 Worktrail 的自动化风格：自动发现、自动汇总、自动编排，但不自动越过人审。

## 接着读什么
<div class="title-en">Read This Next</div>

如果你已经理解自动化边界，下一步建议阅读：

- [安装说明](INSTALLATION.md)，理解自动化能力是如何被安装进具体工具的
- [常见工作流](COMMON-WORKFLOWS.md)，看自动化如何落到任务开始、维护和 handoff
- [设计理念](DESIGN-PHILOSOPHY.md)，理解为什么 Worktrail 的自动化保持低干预
