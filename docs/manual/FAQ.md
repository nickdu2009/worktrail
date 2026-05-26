# 常见问题
<div class="title-en">FAQ</div>

如果你需要完整解释，优先回到对应章节：第一次上手看[快速开始](QUICK-START.md)，安装和 Agent 集成看[安装说明](INSTALLATION.md)，真实任务流程看[常见工作流](COMMON-WORKFLOWS.md)，问题定位看[故障排查](TROUBLESHOOTING.md)。

## Worktrail 是什么？
<div class="title-en">What Is Worktrail?</div>

Worktrail 是一个 local-first 的 AI coding session knowledge and state layer。它帮助 Agent 在任务开始前读取项目知识和状态，在长任务中保留进度，在任务结束或切换工具前留下 handoff，并把确认过的经验沉淀成可 review 的知识候选。

它不是 TUI、Web UI、daemon、向量数据库或后台服务。

## 用户级 scope 和项目级 scope 有什么区别？
<div class="title-en">User Scope vs Project Scope</div>

用户级 scope 跟随个人机器，适合跨项目复用的偏好、工作流、prompt 和通用经验。项目级 scope 位于当前仓库的 `.worktrail/`，适合团队共享的项目规则、决策、架构、验证记录和状态。

日常默认使用项目级 scope。需要操作用户级内容时，传入 `--scope user`。

## 为什么 pending candidate 不会直接进入正式知识？
<div class="title-en">Why Pending Candidates Are Not Formal Knowledge</div>

Worktrail 把“捕获”和“应用”分开。`note add`、`import`、`distill apply`、`handoff` 等命令会创建候选或操作记录；正式知识变更需要显式 review 后再 `promote` 或 `merge`。

这个设计可以避免未经确认的会话内容直接污染正式知识。

## 我应该用 `note add` 还是 `candidates create`？
<div class="title-en">note add vs candidates create</div>

普通使用者优先用 `note add`。它是低摩擦入口，要求 semantic type、target、title、summary、evidence label 和 body，适合记录已经确认的规则、决策、工作流或教训。

`candidates create` 更通用，适合需要显式控制 candidate id、类型、operation、tags 或 stdin 内容的场景。

## `review`、`review plan` 和 `apply-plan` 有什么区别？
<div class="title-en">review vs review plan vs apply-plan</div>

`worktrail review` 给人读，默认显示 pending semantic candidates。`worktrail review plan --format json` 给 Agent 或自动化读，它是只读计划，会按推荐动作分组。

`worktrail review apply-plan <plan.json> --confirm` 才是状态改变命令；它会验证 plan schema 和 candidate snapshot，跳过 `needs_human_review`，并只在明确确认后执行。

## 为什么 `review` 默认隐藏 transcript evidence？
<div class="title-en">Why Review Hides Transcript Evidence</div>

Transcript evidence 往往是原始会话材料，不等于可复用知识。默认隐藏可以让 review 聚焦 semantic candidates。

需要检查证据时使用：

```bash
worktrail review --evidence
worktrail review --all
```

需要把证据变成知识时先走 distill。

## `distill apply` 会修改正式知识吗？
<div class="title-en">Does distill apply Change Formal Knowledge?</div>

不会。`distill apply` 只根据 proposal 创建 pending semantic candidates。后续仍然需要 `review`、`candidates diff`、`promote` 或 `merge`。

## 什么时候用 `handoff`？
<div class="title-en">When Should I Use handoff?</div>

在结束会话、切换 Agent、准备压缩上下文、跨工具交接或需要留下恢复入口时使用：

```bash
worktrail handoff "summary, validation, risks, open questions, and next step"
```

handoff 是 pending operational candidate，不会直接进入正式知识。

## Cursor 会自动使用 Worktrail 吗？
<div class="title-en">Does Cursor Use Worktrail Automatically?</div>

安装 Cursor 用户级集成后，Cursor 可以看到 Worktrail rule 和 skills。用户级 skills 只有在当前 workspace 或 repo root 存在 `.worktrail/` 时才应自动运行常规 Worktrail 工作流。

如果没有 `.worktrail/`，Worktrail 仍可用于显式 init、install、inspect 或 repair 请求。

## 什么时候需要 `doctor knowledge`？
<div class="title-en">When Should I Run doctor knowledge?</div>

当你新增或整理正式知识、怀疑 requirements/design/decision 混放、多个 source of truth 冲突、superseded 文档仍被索引引用，或想做维护检查时运行：

```bash
worktrail doctor knowledge
```

## 我可以自动清理所有 pending evidence 吗？
<div class="title-en">Can I Automatically Clean All Pending Evidence?</div>

不建议。先看只读 evidence plan：

```bash
worktrail evidence plan --format json
```

只有当 plan 推荐 `archive` 或 `discard`，并且你明确确认原因后，才执行对应动作。`needs_human_review` 不应该自动处理。
