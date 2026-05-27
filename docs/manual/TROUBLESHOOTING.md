# 故障排查
<div class="title-en">Troubleshooting</div>

## 先确认三件事
<div class="title-en">First Checks</div>

遇到问题时，先从三件事开始：

```bash
worktrail --help
worktrail doctor
worktrail context "debug current worktrail setup"
```

- `worktrail --help` 确认 CLI 可用
- `worktrail doctor` 确认当前路径发现的用户级和项目级根目录
- `worktrail context ...` 确认上下文读取链路可用

## 命令不存在
<div class="title-en">Command Not Found</div>

### 现象
<div class="title-en">Symptom</div>

运行 `worktrail` 时提示 command not found。

### 处理
<div class="title-en">Fix</div>

1. 确认 Worktrail CLI 已安装。
2. 确认安装目录已经加入 `PATH`。
3. 重新打开终端后运行：

```bash
worktrail --help
```

Agent 集成会调用 `worktrail` 命令，所以 CLI 不可用时，hooks、skills 或 MCP 相关能力也会失败。

## 上下文里没有项目知识
<div class="title-en">No Project Knowledge in Context</div>

### 现象
<div class="title-en">Symptom</div>

`worktrail context "task"` 能运行，但没有读到预期项目知识。

### 处理
<div class="title-en">Fix</div>

1. 确认你在目标仓库根目录或其子目录中。
2. 确认项目已经初始化：

```bash
worktrail init-project
```

3. 确认正式知识已经进入 `.worktrail/` 下对应目录，并重建索引：

```bash
worktrail index rebuild
worktrail context "task"
```

如果 `worktrail context "task"` 提示 stale index，先看差异再重建：

```bash
worktrail index diff
worktrail index rebuild
worktrail context "task"
```

如果只想读用户级知识，确认内容在用户级 scope 中，并按需要传入 `--scope user` 的命令。历史知识默认不进入主 context；需要时显式传入：

```bash
worktrail context --include-lifecycle historical "task"
```

## Agent 没有自动使用 Worktrail
<div class="title-en">Agent Does Not Use Worktrail Automatically</div>

### 现象
<div class="title-en">Symptom</div>

你已经安装了 skills，但 Agent 没有在任务开始或结束时运行 Worktrail。

### 处理
<div class="title-en">Fix</div>

1. 确认当前项目有 `.worktrail/` 标记：

```bash
worktrail init-project
```

2. 检查对应 Agent 集成：

```bash
worktrail doctor cursor --user --project
worktrail doctor codex --user --project
worktrail doctor claude --user --project
```

3. 重新打开或重启对应 Agent，让它重新发现用户级 rules 和 skills。

用户级 Worktrail skills 默认只在存在 `.worktrail/` 的项目中自动运行常规工作流。没有这个标记时，Worktrail 只应响应显式 init、install、inspect 或 repair 请求。

## Review 看不到刚才的证据
<div class="title-en">Review Does Not Show Evidence</div>

### 现象
<div class="title-en">Symptom</div>

`worktrail review` 没有显示刚导入的 transcript evidence。

### 处理
<div class="title-en">Fix</div>

默认 `review` 主要显示 pending semantic candidates，并隐藏 transcript evidence 和非语义操作候选。按需要使用：

```bash
worktrail review --evidence
worktrail review --all
```

如果你要把 evidence 变成可复用知识，先走 distill：

```bash
worktrail distill --pending --summary
worktrail distill --pending --all --write-pack worktrail-distill.md
```

## Promote 或 Merge 被拒绝
<div class="title-en">Promote or Merge Is Rejected</div>

### 现象
<div class="title-en">Symptom</div>

`worktrail promote <candidate-id>` 或 `worktrail merge <candidate-id>` 失败。

### 处理
<div class="title-en">Fix</div>

先检查候选详情和 diff：

```bash
worktrail candidates show <candidate-id>
worktrail candidates diff <candidate-id>
worktrail review plan --format json
```

常见原因包括：

- 候选不是 pending 状态
- 候选类型是 `transcript_notes` 或 `migration_source`，需要先 distill
- `target_path` 和 candidate type 不匹配
- 目标文档缺失、冲突或需要人工 review

## 删除知识前不确定是否安全
<div class="title-en">Unsure Whether Deleting Knowledge Is Safe</div>

### 现象
<div class="title-en">Symptom</div>

你想删除 `.worktrail` 下的正式知识文档，但不确定它是否仍被 starter 文档、候选知识或 agent 配置引用。

### 处理
<div class="title-en">Fix</div>

先运行只读预检：

```bash
worktrail doctor delete rules/example.md
```

如果需要机器可读输出，用：

```bash
worktrail doctor delete --format json rules/example.md
```

`blockers` 表示仍有结构化依赖，例如 pending candidate target、`supersedes` / `superseded_by` 关系或 starter 文档中的 Markdown 链接。`warnings` 表示普通正文、candidate body 或 governance 文件中的文本提及，需要人工判断是否真的要一起清理。

## Evidence 无法归档或丢弃
<div class="title-en">Evidence Archive or Discard Fails</div>

### 现象
<div class="title-en">Symptom</div>

`worktrail evidence archive` 或 `worktrail evidence discard` 被拒绝。

### 处理
<div class="title-en">Fix</div>

先读取当前 evidence plan：

```bash
worktrail evidence plan --format json
```

归档或丢弃必须满足当前 plan 推荐的动作，并带上确认和原因：

```bash
worktrail evidence archive <candidate-id> --confirm --reason "covered by applied knowledge"
worktrail evidence discard <candidate-id> --confirm --reason "empty duplicate evidence"
```

如果 plan 推荐 `needs_human_review`，不要强行处理，先人工确认。

## 手册预览打不开
<div class="title-en">Manual Preview Does Not Open</div>

### 现象
<div class="title-en">Symptom</div>

`make docs-manual-serve` 启动失败，或浏览器里打不开用户手册。

### 处理
<div class="title-en">Fix</div>

先确认你在仓库根目录运行命令：

```bash
make docs-manual-serve
```

默认端口是 `3000`。如果端口被占用，换一个端口：

```bash
make docs-manual-serve PORT=3001
```

这个预览使用 Docsify，通过 `npx docsify-cli serve docs/manual -p <port>` 启动。本机需要可用的 Node.js/npm；第一次运行时，`npx` 可能需要下载 `docsify-cli`。

## Worktrail 文档预览打不开
<div class="title-en">Worktrail Document Preview Does Not Open</div>

### 现象
<div class="title-en">Symptom</div>

`worktrail preview` 没有打开浏览器，或你不确定预览文件生成到了哪里。

### 处理
<div class="title-en">Fix</div>

先直接运行：

```bash
worktrail preview --no-open
```

命令会输出站点入口页路径，例如 `index\t/path/to/.worktrail/.cache/preview/index.html`。确认这个入口文件存在后，再手动打开它。

如果要看用户级知识库，用：

```bash
worktrail preview --scope user --no-open
```

如果怀疑缓存结果过旧，先清理再重新生成：

```bash
worktrail preview --clear-cache
worktrail preview --scope user --clear-cache
```

`worktrail preview` 现在生成的是一组相互链接的静态页面，所以排查时要打开入口页，而不是只移动或单独打开缓存目录里的某一个子页面。

## 维护提示太多
<div class="title-en">Too Many Maintenance Hints</div>

### 现象
<div class="title-en">Symptom</div>

`worktrail context` 输出里显示 pending evidence、review 或 import 提示很多，不确定先做什么。

### 处理
<div class="title-en">Fix</div>

先只做只读发现，不要直接执行状态改变命令：

```bash
worktrail context "maintenance"
worktrail distill --pending --summary
worktrail review plan --format json
worktrail evidence plan --format json
worktrail maintain knowledge --format json
```

再按当前目标选择一条 lane。`promote`、`merge`、`discard`、`archive`、`restore`、`retire`、`maintain apply` 都应该有明确确认后再执行。
