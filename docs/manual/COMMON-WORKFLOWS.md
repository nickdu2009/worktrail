# 常见工作流
<div class="title-en">Common Workflows</div>

## 为什么有这一章
<div class="title-en">Why This Chapter Exists</div>

使用者最需要的不是命令清单，而是“面对一个真实任务时该怎么走”。

这一章给的是执行主线，不替代[安装说明](INSTALLATION.md)和[故障排查](TROUBLESHOOTING.md)。

## 工作流 1：任务开始前加载上下文
<div class="title-en">Workflow 1: Load Context Before Work</div>

### 何时使用
<div class="title-en">When to Use</div>

适合开始一项新的代码任务或需要项目记忆时。恢复旧任务、切换 Agent 后继续已有工作应使用 `resume`。

### 如何运行
<div class="title-en">How It Runs</div>

1. 先用一句话描述当前任务。
2. 运行 `context` 读取正式知识、状态、候选提示和维护提示。
3. 如果任务处于明确阶段，用 `--stage` 帮助排序。
4. 检查 Task Recovery Summary；不同 `task_id` 的 state、handoff、checkpoint 和 runtime 不会被拼成一条恢复链。

```bash
worktrail context "fix failing import review"
worktrail context --stage requirements "define user guide acceptance criteria"
worktrail context --stage implementation "implement cursor import limits"
```

### 何时停止
<div class="title-en">Stop When</div>

- 上下文包已经包含当前任务需要的规则、决策或状态
- 维护提示已经被识别，但不会抢占当前任务
- Agent 已经知道下一步应该读哪些文件或执行哪些检查

## 工作流 2：长任务中保持状态
<div class="title-en">Workflow 2: Keep State During Long Work</div>

### 何时使用
<div class="title-en">When to Use</div>

适合多步、风险较高、可能切换工具、可能压缩上下文或需要中途恢复的任务。

### 如何运行
<div class="title-en">How It Runs</div>

1. 开始时创建状态。
2. 每个关键决策、验证结果或剩余风险发生变化时追加更新。
3. 在进入高风险步骤前创建 checkpoint。
4. 普通结束时关闭状态；只有显式跨 chat、切 Agent 或稍后继续时才创建 handoff。

```bash
worktrail state start "review import workflow docs"
worktrail state update "Read manual style and selected docs/manual layout."
worktrail state checkpoint --reason "before applying documentation patch"
worktrail state close "manual created and linked from README"
```

如果要把状态交给下一个 Agent：

```bash
worktrail state close --to handoff --next-step "continue validation and README link review" "current state summary"
```

如果当前没有 active explicit state，或者你明确需要保留 state 不关闭时，才单独写一份 local handoff-only 交接记录：

```bash
worktrail handoff create --next-step "continue the task" "Goal, current diff intent, validation, risks, open questions, and next step."
```

Local handoff 是默认恢复记录，不是 formal knowledge 或默认 review 项。只有需要通过仓库共享时才显式发布：

```bash
worktrail handoff publish <local-handoff-id>
```

Publish 只创建 Worktrail team 文件，不会 stage、commit 或 push。工作树非 clean 时默认拒绝；例外必须同时使用 `--allow-dirty --confirm`，并把 `code_availability` 标为 unavailable。

## 工作流 3：沉淀一条人工确认的知识
<div class="title-en">Workflow 3: Capture Confirmed Knowledge</div>

### 何时使用
<div class="title-en">When to Use</div>

适合会话里已经确认了一条规则、决策、工作流或教训，而且值得未来复用。

### 如何运行
<div class="title-en">How It Runs</div>

1. 用 `note add` 创建 pending semantic candidate。
2. 用 `review` 和 `review plan` 看推荐动作。
3. 用 `candidates diff` 检查正式知识会怎样变化。
4. 明确确认后 `promote`、`merge` 或 `discard`。

```bash
worktrail note add \
  --type workflow \
  --target workflows/release-check.md \
  --title "Release Check Workflow" \
  --summary "Run release checks before publishing." \
  --evidence-label "manual note" \
  "Before release, run doctor, focused tests, and review pending candidates."

worktrail review
worktrail review plan --format json
worktrail candidates diff <candidate-id>
worktrail promote <candidate-id>
```

如果 requirement、architecture、implementation plan、rule 或 workflow 只需要存在于 Worktrail，不要先创建 `docs/` 或 `.plans/`。使用 `worktrail-draft` 的带 frontmatter stdin 流程直接创建 pending candidate：

```bash
worktrail draft create \
  --scope project \
  --topic browser-timer \
  --type requirement \
  --target requirements/browser-timer.md \
  --title "Browser Timer Requirements" \
  --summary "Requirements for a browser timer." \
  --format json <<'WORKTRAIL_DRAFT'
---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "browser-timer-requirements",
  "scope": "project",
  "type": "requirement",
  "title": "Browser Timer Requirements",
  "status": "active",
  "lifecycle": "current",
  "topic": "browser-timer"
}
---

# Browser Timer Requirements

The timer must support start, pause, resume, and reset.
WORKTRAIL_DRAFT

worktrail review plan --format json
```

如果用户明确要求普通文件，或已经提供现有文件，才使用 `--from-file` 并保留该文件。`worktrail-draft` 只创建 pending candidate，仍需在 review 后明确确认才能进入正式知识。

已经完成内容评审的 ADR 使用专用入口：

```bash
worktrail adr create "Use SQLite" \
  --from-file docs/adr/ADR-0001-use-sqlite.md \
  --decision-status Accepted \
  --format json
worktrail review plan --format json
```

`adr create` 只创建 pending `decision` candidate。ADR 文档状态、candidate status 和 formal lifecycle 相互独立；只有 Accepted ADR 能通过 `--supersedes decisions/<old>.md` 建立替代关系。旧决策变为 historical/retired 仍需显式维护和确认。`worktrail-adr` 可以复用兼容的 ADR 内容评审结果，但不依赖任何外部 skill；没有兼容评审器时，需要完整结构和用户明确确认“内容已评审、可持久化”。

### 何时停止
<div class="title-en">Stop When</div>

- 候选内容已经进入正式知识，或被明确丢弃
- `worktrail context "same task"` 能重新读到新知识
- 没有把未经确认的会话片段直接写入正式知识

## 工作流 4：从会话证据提炼知识
<div class="title-en">Workflow 4: Distill Knowledge From Evidence</div>

### 何时使用
<div class="title-en">When to Use</div>

适合已有 transcript evidence、Cursor/Codex 导入记录、KDD 迁移证据或其他 pending evidence，需要提炼成可复用语义知识。

### 如何运行
<div class="title-en">How It Runs</div>

1. 先 dry-run 或摘要查看证据。
2. 需要人工或 Agent 起草 proposal 时，输出 evidence pack。
3. 验证 proposal。
4. apply proposal 只创建 pending semantic candidates。
5. 再进入 review/promote/merge。

```bash
worktrail distill --pending --summary
worktrail distill --pending --all --write-pack worktrail-distill.md
worktrail distill validate proposal.json
worktrail distill apply proposal.json
worktrail review
```

`distill apply` 不会直接修改正式知识。它创建的是新的 pending semantic candidates。

## 工作流 5：低干预维护
<div class="title-en">Workflow 5: Low-Intervention Maintenance</div>

### 何时使用
<div class="title-en">When to Use</div>

适合定期清理 pending candidates、证据生命周期、知识治理漂移或维护提示。

### 如何运行
<div class="title-en">How It Runs</div>

先读取维护提示：

```bash
worktrail context "maintenance"
```

再看 read-only 计划：

```bash
worktrail distill --pending --summary
worktrail review plan --format json
worktrail evidence plan --format json
worktrail maintain knowledge --format json
```

如果使用已安装的 Agent skills，可以让 Agent 使用 `worktrail-maintain`。它会先执行只读发现链，再等待明确确认后才执行状态改变命令。

### 何时停止
<div class="title-en">Stop When</div>

- 所有建议动作都已经被分组，且需要人工确认的项没有被自动处理
- `promote`、`merge`、`discard`、`archive`、`restore`、`retire` 等动作都有明确确认
- 维护过程没有把 raw evidence 直接提升为正式知识

## 工作流 6：切换工具或结束会话
<div class="title-en">Workflow 6: Handoff or End a Session</div>

### 何时使用
<div class="title-en">When to Use</div>

适合切换 Agent、打开新会话、结束当天工作，或者用户明确要求留下恢复入口时。普通进度更新、正常回复结束和 hook 事件都不构成交接触发条件。

### 如何运行
<div class="title-en">How It Runs</div>

推荐主路径是 `worktrail state close --to handoff --next-step "<action>" "<summary>"`：它通过事务同时关闭 explicit state，并写入 `.worktrail/handoffs/local/`。没有 active explicit state 时使用 `worktrail handoff create --next-step "<action>" "<summary>"`；没有后续工作时明确传 `--complete`。Handoff 正文必须是结构化摘要，不能嵌入完整 state snapshot。

需要团队交接时，对 local id 显式运行 `worktrail handoff publish <local-id>`。Team 记录是 immutable DAG 节点；单 head 会自动成为 `supersedes`，多个 head 必须通过显式 `--supersedes <team-id,...>` 发布 reconciliation 节点。Team 记录不能原地 close 或 repair。

`stop` / `session-end` hooks 只写 runtime session、checkpoint、takeover note 和审计日志，永远不创建或发布 handoff。对 ZCode Agent 来说，这条工作流仍然成立，只是触发方式来自 `AGENTS.md` 路由和已安装 skills，而不是 Worktrail-managed hooks。

新 session 开始时优先用：

```bash
worktrail resume
worktrail resume --task-id <task-id>
worktrail resume --ref checkpoint:<checkpoint-id>
```

`resume` 一次只恢复一个 task，优先级依次是 local handoff、team handoff、explicit state、explicit checkpoint、runtime checkpoint、runtime session。无 selector 且只有一个 task 时可自动选择；多个 task 会返回 ambiguity，并列出可选 task，必须用 `--task-id`、`--task-title` 或 `--ref [scope:]kind:id` 消歧。Runtime fallback 属于 degraded recovery。

显式 checkpoint 必须由用户或 Agent 主动运行：

```bash
worktrail state checkpoint --reason "before risky migration"
```

Hooks 生成的 runtime checkpoint 不是 explicit checkpoint。Runtime 恢复材料有效期为 14 天，每个 task 读取最多最新 5 条；清理采用显式 plan/apply 语义，hooks 不会自动 prune。

## 工作流 7：检查、修复与迁移 Handoff
<div class="title-en">Workflow 7: Diagnose, Repair, and Migrate Handoffs</div>

先运行只读检查和修复计划：

```bash
worktrail handoff doctor
worktrail handoff repair
```

`doctor` 会检查 malformed handoff、内容 hash、local 文件权限、同一 task 的多个 current local 记录、未被 Git 跟踪的 team 文件和多个 team head。`repair` 默认只输出计划；只有 `worktrail handoff repair --apply --confirm` 才把 malformed local handoff 隔离到 `.worktrail/runtime/quarantine/handoff/` 并修复其它可修复的 local 问题，team 文件永不原地改写。Create、state close 与 publish 使用 ops journal 事务，未完成事务可安全重放。

运行时和事务维护也先走只读命令：

```bash
worktrail runtime prune
worktrail doctor recovery
worktrail doctor ops status
```

`worktrail doctor recovery` 同时列出 malformed state 和 malformed runtime。确认计划后，使用 `worktrail doctor recovery --apply --confirm` 把它们分别隔离到 `.worktrail/runtime/quarantine/state/` 和对应 runtime 子目录；该命令不接受 `--repair`。Runtime 过期清理使用 `worktrail runtime prune --apply --confirm`，ops 重放或 stale-lock 修复使用 `worktrail doctor ops repair --confirm`。Hooks 不会隐式 prune、quarantine 或 repair。

旧的根目录 handoff 和已退休的 `candidate_type=handoff` 使用专用迁移：

```bash
worktrail migrate handoff-v2
worktrail migrate handoff-v2 --apply --confirm
```

默认是只读 dry-run。Apply 必须同时带 `--apply --confirm`，未知 flag 和多余位置参数会被拒绝。Dry-run 会完整验证即将生成的 V2 metadata、正文安全、content hash 和大小；非法旧 ID/source_tool、越界 source_state 和 symlink reference 会成为 `invalid` plan item，在此之前不会创建备份或目标。默认备份位于 `.worktrail` 根目录之外、并由项目 `.gitignore` 精确忽略的 `/.worktrail-handoff-v2-backups/`；manifest 记录 inventory hash、文件数和逐文件 hash。Discarded/archived handoff candidate 会先备份，再以对应终态 lifecycle 迁移到 V2 local，并从 candidate surface 删除。显式 `--backup-dir` 也必须外置且不能冲突。迁移只重放自己的 journal intent；目标/源 hash 的最终验证与源文件删除共用 cleanup transaction，任一 hash 变化都会停止清理。成功后 CLI 强制重建项目 index。`doctor knowledge` 不扫描 handoff candidate；发现职责只属于这条迁移命令。

## 工作流 8：预览 Worktrail 文档
<div class="title-en">Workflow 8: Preview Worktrail Documents</div>

### 何时使用
<div class="title-en">When to Use</div>

适合整体浏览当前 scope 下的正式知识、task-scoped runtime 恢复入口和 pending drafts/evidence，而不是单独预览某一个文件。Local/team handoff 会作为 runtime recovery 页面展示，不属于 formal knowledge 或默认 semantic review。

### 如何运行
<div class="title-en">How It Runs</div>

`preview` 现在默认渲染当前 scope 的整体知识库静态多页站点。项目级正式知识通常在 `.worktrail/` 下；入口页会先展示 sections、统计信息和 pending drafts/evidence 分组，再通过分区页、文档页和详情页逐层展开，不再要求传文件路径或 candidate id。

```bash
worktrail preview
worktrail preview --scope user
```

如果只想生成预览文件而不自动打开浏览器：

```bash
worktrail preview --no-open
worktrail preview --scope user --no-open
```

`--no-open` 输出的是站点入口页路径，通常是 `.worktrail/.cache/preview/index.html`。

如果需要清理预览缓存：

```bash
worktrail preview --clear-cache
worktrail preview --scope user --clear-cache
```

如果你已经把 Worktrail skill/指令安装到 Cursor、Codex、Claude 或 ZCode，升级到这个整体预览行为后，记得重新运行 `worktrail install <tool> --user`（以及需要时的 `--project`）刷新 agent 侧规则。
