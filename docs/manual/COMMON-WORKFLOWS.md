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

适合开始一项新的代码任务、恢复旧任务、切换 Agent 或需要项目记忆时。

### 如何运行
<div class="title-en">How It Runs</div>

1. 先用一句话描述当前任务。
2. 运行 `context` 读取正式知识、状态、候选提示和维护提示。
3. 如果任务处于明确阶段，用 `--stage` 帮助排序。

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
4. 结束时关闭状态，或生成 handoff。

```bash
worktrail state start "review import workflow docs"
worktrail state update --session latest "Read manual style and selected docs/manual layout."
worktrail state checkpoint --reason "before applying documentation patch"
worktrail state close "manual created and linked from README"
```

如果要把状态交给下一个 Agent：

```bash
worktrail state close --to handoff "continue from validation and README link review"
```

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

如果使用已安装的 Agent skills，可以让 Agent 使用 `/worktrail-maintain`。它会先执行只读发现链，再等待明确确认后才执行状态改变命令。

### 何时停止
<div class="title-en">Stop When</div>

- 所有建议动作都已经被分组，且需要人工确认的项没有被自动处理
- `promote`、`merge`、`discard`、`archive`、`restore`、`retire` 等动作都有明确确认
- 维护过程没有把 raw evidence 直接提升为正式知识

## 工作流 6：切换工具或结束会话
<div class="title-en">Workflow 6: Handoff or End a Session</div>

### 何时使用
<div class="title-en">When to Use</div>

适合切换 Agent、打开新会话、准备压缩上下文、结束当天工作或需要留下恢复入口时。

### 如何运行
<div class="title-en">How It Runs</div>

```bash
worktrail handoff "Goal, current diff intent, validation, risks, open questions, and next step."
```

handoff 会创建 pending handoff candidate，不会提升正式知识。后续如需把其中可复用的内容沉淀为规则、决策或工作流，应先 distill 或手动创建 semantic candidate，再进入 review。

## 工作流 7：预览 Worktrail 文档
<div class="title-en">Workflow 7: Preview Worktrail Documents</div>

### 何时使用
<div class="title-en">When to Use</div>

适合整体浏览当前 scope 下的正式知识和 pending candidates，而不是单独预览某一个文件。

### 如何运行
<div class="title-en">How It Runs</div>

`preview` 现在默认渲染当前 scope 的整体知识库页面。项目级正式知识通常在 `.worktrail/` 下；页面会合并正式知识和 `Pending Candidates`，不再要求传文件路径或 candidate id。

```bash
worktrail preview
worktrail preview --scope user
```

如果只想生成预览文件而不自动打开浏览器：

```bash
worktrail preview --no-open
worktrail preview --scope user --no-open
```

如果需要清理预览缓存：

```bash
worktrail preview --clear-cache
worktrail preview --scope user --clear-cache
```

如果你已经把 Worktrail skill/rule 安装到 Cursor、Codex 或 Claude，升级到这个整体预览行为后，记得重新运行 `worktrail install <tool> --user`（以及需要时的 `--project`）刷新 agent 侧规则。
