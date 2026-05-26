# 快速开始
<div class="title-en">Quick Start</div>

## 目标
<div class="title-en">Goal</div>

用最短路径完成一次可用的 Worktrail 初始化，并确认当前任务能拿到上下文。

这一章只保留第一次上手最需要的步骤。

如果你想完整比较安装模式和 Agent 集成边界，继续读[安装说明](INSTALLATION.md)。

## 适合谁
<div class="title-en">Who This Is For</div>

- 第一次接触 `worktrail`
- 已经有 CLI，但还没有在当前项目里跑通过
- 希望先知道日常使用时最短命令链是什么

## 第一次跑通
<div class="title-en">First Successful Run</div>

### 1. 确认 CLI 可用
<div class="title-en">Confirm the CLI</div>

先确认 `worktrail` 已经在 `PATH` 中：

```bash
worktrail --help
```

如果命令不存在，先回到[安装说明](INSTALLATION.md)补齐 CLI 安装。

### 2. 初始化当前项目
<div class="title-en">Initialize the Current Project</div>

在需要使用 Worktrail 的仓库根目录运行：

```bash
worktrail init
```

这会初始化用户级和项目级 Worktrail 目录。项目级目录通常是当前仓库下的 `.worktrail/`，它是 Agent 自动使用 Worktrail 工作流的重要标记。

### 3. 读取一次任务上下文
<div class="title-en">Read Task Context</div>

为当前任务生成上下文包：

```bash
worktrail context "current task"
```

如果你正在做需求、设计或实现阶段的任务，可以用 `--stage` 帮助上下文排序：

```bash
worktrail context --stage requirements "define reporting requirements"
worktrail context --stage design "design the sync flow"
worktrail context --stage implementation "implement the sync flow"
```

### 4. 记录一个已确认结论
<div class="title-en">Capture a Confirmed Finding</div>

如果会话里已经确认了一条可复用规则，可以先创建 pending candidate：

```bash
worktrail note add \
  --type rule \
  --target rules/testing.md \
  --title "Testing Rule" \
  --summary "Keep validation focused on the affected behavior." \
  --evidence-label "manual note" \
  "Run the narrowest meaningful validation before broad suites."
```

`note add` 只创建待评审候选知识，不会直接修改正式知识。

### 5. 评审并应用候选知识
<div class="title-en">Review and Apply Candidate Knowledge</div>

先看 pending candidates：

```bash
worktrail review
worktrail review plan --format json
```

确认内容后再手动选择应用方式：

```bash
worktrail candidates diff <candidate-id>
worktrail promote <candidate-id>
```

如果候选内容应该并入已有文档，用 `merge`；如果不再需要，用 `discard`。

## 接入 Agent 后的最短用法
<div class="title-en">Shortest Agent Workflow</div>

安装 Agent 集成后，日常可以让 Agent 在任务开始时运行：

```bash
worktrail context "task description"
```

长任务中间需要保留进度时运行：

```bash
worktrail state start "task title"
worktrail state update --session latest "what changed and what remains"
worktrail state checkpoint --reason "safe checkpoint before next step"
```

结束或切换工具前运行：

```bash
worktrail handoff "summary of current state, validation, risks, and next step"
```

## 两个常见误区
<div class="title-en">Two Common Mistakes</div>

- 不要把 `context` 当成会修改知识的命令。它只读取知识、状态和维护提示。
- 不要把 pending candidate 当成正式知识。`note add`、`import`、`distill apply`、`handoff` 默认都只生成候选或操作记录，真正进入正式知识需要显式 review 后再 `promote` 或 `merge`。

## 接着读什么
<div class="title-en">Read This Next</div>

如果你已经跑通第一次流程，下一步建议阅读：

- [安装说明](INSTALLATION.md)，理解用户级、项目级和 Agent 集成的边界
- [常见工作流](COMMON-WORKFLOWS.md)，把命令串进真实开发任务
