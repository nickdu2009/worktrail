# 常见问题
<div class="title-en">FAQ</div>

如果你需要完整解释，优先回到对应章节：第一次上手看[快速开始](QUICK-START.md)，安装和 Agent 集成看[安装说明](INSTALLATION.md)，真实任务流程看[常见工作流](COMMON-WORKFLOWS.md)，问题定位看[故障排查](TROUBLESHOOTING.md)。

## Worktrail 是什么？
<div class="title-en">What Is Worktrail?</div>

Worktrail 是一个 local-first 的文档知识库、工作记录和交接工具。它帮助 Agent 在任务开始前读取项目知识和状态，在长任务中保留进度，在显式跨 chat 或切换 Agent 时留下 task-scoped handoff，并把确认过的经验沉淀成可 review 的知识候选。

它不是 TUI、Web UI、通用 daemon、云 embedding 服务或独立向量数据库。
本地语义召回是需要显式安装和调用的受限例外：代码已合入仓库，正式发行
仍受 release gate 约束；它仍以 Markdown 为 source of truth，并保留现有
lexical 路径。

## 本地语义召回现在可用了吗？
<div class="title-en">Is Local Semantic Recall Available Now?</div>

可用，但属于 opt-in 能力，且正式 v1.0.0 发行仍需 clean-checkout 等
release gate。M1 是唯一 `verified` 变体；M2–M5 为 opt-in
`experimental`（不承诺 `compatible`/`verified`、性能、隐私、最低 macOS
或运营支持）。未安装、自检失败或 runtime 不可用时，`auto` mode 显式降级
到 lexical；严格模式返回稳定失败原因。

安装与 rebuild 步骤见[安装说明](INSTALLATION.md)。架构与支持层级细节见
[`docs/worktrail-local-semantic-recall-architecture.md`](../worktrail-local-semantic-recall-architecture.md)。

## 用户级 scope 和项目级 scope 有什么区别？
<div class="title-en">User Scope vs Project Scope</div>

用户级 scope 跟随个人机器，适合跨项目复用的偏好、工作流、prompt 和通用经验。项目级 scope 位于当前仓库的 `.worktrail/`，适合团队共享的项目规则、决策、架构、验证记录和状态。

日常默认使用项目级 scope。需要操作用户级内容时，传入 `--scope user`。

## 为什么 pending candidate 不会直接进入正式知识？
<div class="title-en">Why Pending Candidates Are Not Formal Knowledge</div>

Worktrail 把“捕获”和“应用”分开。`note add`、`import`、`distill apply` 会创建候选或证据；正式知识变更需要显式 review 后再 `promote` 或 `merge`。Handoff 是独立的 task-scoped runtime record，不属于 formal knowledge 或 candidate review。

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

只在用户明确要求跨 chat、切换 Agent、稍后继续或留下恢复入口时使用。普通进度、正常回复结束、context compact 和 hook 事件本身都不触发 handoff。

```bash
worktrail state close --to handoff --next-step "continue the task" "summary, validation, risks, open questions, and next step"
```

默认创建 `.worktrail/handoffs/local/` 下的 private local handoff。没有 active state 时使用 `worktrail handoff create --next-step "<action>" "<summary>"`；任务已完成时明确传 `--complete`。`stop` / `session-end` hooks 只保留 runtime records，永远不创建 handoff。

## 怎样把 Handoff 分享给团队？
<div class="title-en">How Do I Share a Handoff With the Team?</div>

显式发布一个 local handoff：

```bash
worktrail handoff publish <local-id>
```

Publish 会创建新的 `.worktrail/handoffs/team/` immutable DAG 节点，不会运行 `git add`、`git commit` 或 `git push`。Dirty worktree 默认被拒绝；例外必须同时使用 `--allow-dirty --confirm`，且 team 记录会标明代码不可用。多个 team head 必须通过 `--supersedes <id,...>` 显式 reconciliation。

## 什么时候用 `resume`？
<div class="title-en">When Should I Use resume</div>

当你在新 session 里接上一个任务，不想手工拼 `context`、`state show` 和 handoff 文件时使用：

```bash
worktrail resume
worktrail resume --task-id <task-id>
worktrail resume --ref checkpoint:<checkpoint-id>
```

`resume` 一次只恢复一个 task，优先 local handoff、team handoff、explicit state、explicit checkpoint、runtime checkpoint、runtime session。无 selector 时只能自动选择唯一 task；如果存在多个 task，会返回 ambiguity 并要求 `--task-id`、`--task-title` 或 `--ref`。Runtime fallback 会标为 degraded。

## Runtime 恢复记录会永久保留吗？
<div class="title-en">Are Runtime Recovery Records Kept Forever?</div>

不会。Runtime session/checkpoint 的保留窗口是 14 天，恢复读取每个 task 最多取最新 5 条有效记录。Prune 使用显式 plan/apply 语义；hooks 不会自动删除，也不会把 runtime 记录升级成 handoff。

## 旧 Handoff 和 `candidate_type=handoff` 怎么处理？
<div class="title-en">How Do I Migrate Legacy Handoffs?</div>

`candidate_type=handoff` 已退休，完全排除在 candidates list/show/diff、review（包括 `--all`）和 apply-candidates 之外；显式访问会返回 migration-required，promote/merge 会拒绝。先运行只读 dry-run：

```bash
worktrail migrate handoff-v2
```

确认报告后再运行 `worktrail migrate handoff-v2 --apply --confirm`。已 discarded/archived 的 handoff candidate 会先进入可审计备份，再迁移成带同名终态 lifecycle 的 V2 local 记录并从 candidate surface 删除；它们不会复活为 current handoff。Dry-run 会在任何写入前验证完整 V2 metadata、正文安全、content hash 和大小，非法旧 ID/source_tool 或不安全 source_state 会显示为 `invalid`。默认备份位于迁移 `.worktrail` 之外并由 `/.worktrail-handoff-v2-backups/` 精确忽略；manifest 带 inventory hash、文件数和逐文件 hash。冲突不会覆盖，恢复不会覆盖已变化的目标，成功后会强制重建项目 index。可用 `--backup-dir` 指定其他外置且不冲突的目录。

## Cursor 会自动使用 Worktrail 吗？
<div class="title-en">Does Cursor Use Worktrail Automatically?</div>

安装 Cursor 用户级集成后，Cursor 可以看到 Worktrail rule 和 skills。用户级 skills 只有在当前 workspace 或 repo root 存在 `.worktrail/` 时才应自动运行常规 Worktrail 工作流。

如果没有 `.worktrail/`，Worktrail 仍可用于显式 init、install、inspect 或 repair 请求。

## ZCode Agent 会自动使用 Worktrail 吗？
<div class="title-en">Does ZCode Agent Use Worktrail Automatically?</div>

会，但最佳实践要理解成“语义自动化”而不是“hooks 自动触发”。

安装 `worktrail install zcode --user` 后，ZCode Agent 会读取 `~/.zcode/AGENTS.md` 和 `~/.zcode/skills/`。当当前 workspace 或 repo root 已经存在 `.worktrail/` 时，Agent 应该根据这些规则主动选择 Worktrail skill 或直接运行对应 CLI，例如 `worktrail context`、`worktrail resume`、`worktrail search`、`worktrail state`；交接时使用 `worktrail handoff create --next-step "<action>" "<summary>"`，完成任务则明确传 `--complete`。

ZCode Agent 当前没有 Worktrail-managed 的项目级 hooks、runtime settings 或 transcript import 支持，所以不要把它理解成 Cursor / Claude Code 那种事件驱动自动化。

## 什么时候需要 `doctor knowledge`？
<div class="title-en">When Should I Run doctor knowledge?</div>

当你新增或整理正式知识、怀疑 requirements/design/decision 混放、多个 source of truth 冲突、superseded 文档仍被索引引用，或想做维护检查时运行：

```bash
worktrail doctor knowledge
```

它重点检查需要 review 管理的正式知识漂移，也会报告绕过 candidate/review 流的直接 formal edits。Handoff runtime 和已退休 handoff candidate 不属于这条知识治理链，`doctor knowledge` 不扫描或暴露它们；旧 handoff candidate 只由 `worktrail migrate handoff-v2` 发现。用 `worktrail handoff doctor` 检查 malformed handoff、hash、权限、local current、team tracking 和 DAG heads，用默认 dry-run 的 `worktrail handoff repair` 规划 local 修复或 malformed local handoff 隔离。

## Malformed handoff、state 和 runtime 怎样隔离？
<div class="title-en">How Are Malformed Handoff, State, and Runtime Records Quarantined?</div>

它们有两个明确入口，均先 dry-run：

```bash
worktrail handoff repair
worktrail doctor recovery
```

确认后分别运行：

```bash
worktrail handoff repair --apply --confirm
worktrail doctor recovery --apply --confirm
```

第一条只隔离 malformed local handoff，team handoff 保持 immutable；第二条同时隔离 repairable malformed state 和 runtime。目标都位于 `.worktrail/runtime/quarantine/` 下对应的 `handoff/`、`state/`、`sessions/`、`checkpoints/` 或 `recovery/` 子目录。Recovery 入口不接受 `--repair`。

## 我可以自动清理所有 pending evidence 吗？
<div class="title-en">Can I Automatically Clean All Pending Evidence?</div>

不建议。先看只读 evidence plan：

```bash
worktrail evidence plan --format json
```

只有当 plan 推荐 `archive` 或 `discard`，并且你明确确认原因后，才执行对应动作。`needs_human_review` 不应该自动处理。
