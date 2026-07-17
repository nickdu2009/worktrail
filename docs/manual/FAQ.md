# 常见问题
<div class="title-en">FAQ</div>

如果你需要完整解释，优先回到对应章节：第一次上手看[快速开始](QUICK-START.md)，安装和 Agent 集成看[安装说明](INSTALLATION.md)，真实任务流程看[常见工作流](COMMON-WORKFLOWS.md)，问题定位看[故障排查](TROUBLESHOOTING.md)。

## Worktrail 是什么？
<div class="title-en">What Is Worktrail?</div>

Worktrail 是一个 local-first 的文档知识库、工作记录和交接工具。它帮助 Agent 在任务开始前读取项目知识和状态，在长任务中保留进度，在任务结束或切换工具前留下 durable handoff，并把确认过的经验沉淀成可 review 的知识候选。

它不是 TUI、Web UI、通用 daemon、云 embedding 服务或独立向量数据库。
v1.0.0 计划提供一个需要显式安装和调用的本地语义召回例外，但它仍以
Markdown 为 source of truth，并保留现有 lexical 路径。

## 本地语义召回现在可用了吗？
<div class="title-en">Is Local Semantic Recall Available Now?</div>

v1.0.0 已把本地语义召回纳入发布范围，并保留 SQLite + FTS5 lexical
search 作为基线。M1 是唯一的 `verified` runtime 变体，继续要求完整的
真机、离线、隐私、生命周期、检索质量和资源验证。M2、M3、M4、M5 仅作为
需要显式选择的 `experimental` 变体发布：每种芯片只使用自己的 pinned
official artifact，并且必须在安装时通过本地 integrity、authenticated
loopback、alias、tokenization、embedding shape、CLS pooling 和 L2
normalization self-check 后才能激活，绝不回退到其他芯片的 artifact。

`experimental` 不代表 `compatible` 或 `verified`。它不承诺性能、隐私、
最低 macOS 版本或运营支持，也不要求在发布前提供每种芯片的 self-check
报告。自检失败时，semantic `auto` mode 会显式降级到 lexical；需要
semantic 的严格模式会返回稳定的失败原因。

该能力不会随安装 Worktrail 二进制自动下载模型。核心 `worktrail init`
默认不访问网络；只有 `worktrail init --semantic` 会安装 semantic bundle，
而 `worktrail init --no-semantic` 会明确禁用语义安装。安装后仍需显式执行
`worktrail semantic rebuild --scope all`。未安装或不可用时，现有 lexical CLI
继续工作。

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

## 只需要 Worktrail 正式知识时，还要先创建 `docs/` 文件吗？
<div class="title-en">Do Worktrail-Only Artifacts Need a docs File?</div>

不需要。显式要求 requirement、architecture、implementation plan、rule 或 workflow 只作为 Worktrail 知识存在时，`worktrail-draft` 会通过带 frontmatter 的 stdin 内容直接创建 pending semantic candidate，不创建 `docs/`、`.plans/` 或其他 standalone copy。

只有用户明确要求普通文件，或已经提供现有文件时，才使用并保留 standalone artifact。candidate 仍需经过 review 和明确确认后才能 promote 或 merge。

## ADR 应该用 `adr create` 还是 `note add --type decision`？
<div class="title-en">adr create vs note add --type decision</div>

已有标准 ADR Markdown 时优先用 `worktrail adr create`。它会校验 ADR ID、文档状态和必填章节，保留正文语义，并把新数据规范化为 pending `decision` candidate。普通决策笔记、不需要 ADR 结构时仍可使用 `note add --type decision`。

ADR 有三个独立状态维度：

- 文档状态：`Proposed`、`Accepted`、`Deprecated`、`Superseded`
- candidate status：`pending`、`promoted`、`merged`、`archived`
- formal lifecycle：`current`、`historical`、`retired`

Promote 不会把 Proposed 自动改成 Accepted。只有 Accepted ADR 可以写入 `supersedes` 元数据；旧决策的退役仍需维护计划和明确确认。

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

`worktrail handoff` 会直接写入 `.worktrail/handoffs/` 下的真实交接记录；`stop` / `session-end` hooks 现在默认只保留 runtime records，不再自动起草 pending handoff candidate。

## 什么时候用 `resume`？
<div class="title-en">When Should I Use resume</div>

当你在新 session 里接上一个任务，不想手工拼 `context`、`state show` 和 handoff 文件时使用：

```bash
worktrail resume
worktrail resume "continue review follow-up"
```

`resume` 会基于最近 active state 和最近 durable handoff 创建一个新的 active state，作为新的恢复入口。

## Cursor 会自动使用 Worktrail 吗？
<div class="title-en">Does Cursor Use Worktrail Automatically?</div>

安装 Cursor 用户级集成后，Cursor 可以看到 Worktrail rule 和 skills。用户级 skills 只有在当前 workspace 或 repo root 存在 `.worktrail/` 时才应自动运行常规 Worktrail 工作流。

如果没有 `.worktrail/`，Worktrail 仍可用于显式 init、install、inspect 或 repair 请求。

## ZCode Agent 会自动使用 Worktrail 吗？
<div class="title-en">Does ZCode Agent Use Worktrail Automatically?</div>

会，但最佳实践要理解成“语义自动化”而不是“hooks 自动触发”。

安装 `worktrail install zcode --user` 后，ZCode Agent 会读取 `~/.zcode/AGENTS.md` 和 `~/.zcode/skills/`。当当前 workspace 或 repo root 已经存在 `.worktrail/` 时，Agent 应该根据这些规则主动选择 Worktrail skill 或直接运行对应 CLI，例如 `worktrail context`、`worktrail resume`、`worktrail search`、`worktrail state`、`worktrail handoff`。

ZCode Agent 当前没有 Worktrail-managed 的项目级 hooks、runtime settings 或 transcript import 支持，所以不要把它理解成 Cursor / Claude Code 那种事件驱动自动化。

## 什么时候需要 `doctor knowledge`？
<div class="title-en">When Should I Run doctor knowledge?</div>

当你新增或整理正式知识、怀疑 requirements/design/decision 混放、多个 source of truth 冲突、superseded 文档仍被索引引用，或想做维护检查时运行：

```bash
worktrail doctor knowledge
```

它重点检查需要 review 管理的正式知识漂移，也会报告绕过 candidate/review 流的直接 formal edits。与此同时，`worktrail init` 生成的 starter docs、`worktrail handoff` 生成的 durable handoff 记录，以及没有 thread/topic 语义压力的全局规则、决策或日志，不会再被当成低信号噪音 warning。

## 我可以自动清理所有 pending evidence 吗？
<div class="title-en">Can I Automatically Clean All Pending Evidence?</div>

不建议。先看只读 evidence plan：

```bash
worktrail evidence plan --format json
```

只有当 plan 推荐 `archive` 或 `discard`，并且你明确确认原因后，才执行对应动作。`needs_human_review` 不应该自动处理。
