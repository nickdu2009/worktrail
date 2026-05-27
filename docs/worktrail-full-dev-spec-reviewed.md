# Worktrail 完整版开发说明文档

> 用途：交给 Codex / Claude Code 直接开发完整版本。  
> 仓库名：`worktrail`  
> 命令名：`worktrail`  
> 目标形态：Codex / Claude Code Native Knowledge & State Layer  
> 开发语言：Go  
> 文档日期：2026-05-13

---

## 0. 一句话定义

`worktrail` 是一个本地优先的 AI coding session 知识与状态管理工具。它把 Codex / Claude Code 长对话中的高价值内容沉淀为用户级和项目级知识库，同时把正在进行的任务状态外部化为 State Capsule，避免 compact、resume、fork、换模型、换工具、换 agent 后丢失上下文。

它不是聊天记录管理器，而是一个：

```text
AI Coding Session State + Knowledge System
```

核心输出：

```text
1. User-level Knowledge
   用户级知识，跨项目复用。

2. Project-level Knowledge
   项目级知识，只属于当前项目。

3. State Capsule
   当前任务状态，防止长会话压缩后状态丢失。

4. Context Pack
   每次任务开始时给 Codex / Claude Code 的动态上下文。

5. Handoff
   长任务中断、交接、切换 AI 时的接手说明。

6. Candidate Review
   AI 只能生成候选知识，用户确认后才能进入正式知识库。
```

---

## 1. 产品原则

### 1.1 不做 MVP，直接做完整本地产品

本项目直接实现完整版本，不做“只有 init/context/extract 的简化 MVP”。

完整版本必须包含：

```text
1. 本地用户级知识库
2. 本地项目级知识库
3. State Capsule 生命周期管理
4. Checkpoint / Handoff / Context Pack
5. Candidates / Review / Promote 机制
6. Codex 集成：AGENTS.md、hooks、skills、MCP stdio 配置
7. Claude Code 集成：CLAUDE.md、hooks、skills、MCP stdio 配置
8. Transcript 采集和 raw source 管理
9. Secret / PII redaction
10. 本地搜索索引
11. 本地 MCP stdio server
12. CLI + Codex / Claude Code 对话内 review
13. 自动安装 / 卸载 / 状态诊断
14. 测试、fixture、文档
```

### 1.1.1 明确不做的范围

为避免出现类似 TUI 的“额外界面 / 额外运行面 / 额外权限面”，完整版本明确不做：

```text
1. 不做 TUI。
2. 不做 Web UI / dashboard。
3. 不做 HTTP MCP server；MCP 只做 stdio，由 Codex / Claude Code 启动。
4. 不做后台 daemon / 常驻服务。
5. 不做本地 embedding / vector index / 向量数据库。
6. 不做 custom external command provider，避免 arbitrary command execution。
7. 不通过 MCP 默认暴露 promote / merge / discard 这类正式知识写入工具。
8. 不允许 hooks 自动 promote。
9. 不做自动同步后台 watcher；sync 只能显式触发或由 hooks 触发。
```

### 1.2 Source of Truth

正式知识以 Markdown 文件为 source of truth。

```text
Markdown 文件 = 人可读、可 Git 管理、可手工修改的真相源
本地索引      = 搜索和检索加速
Transcript    = 原始证据
State Capsule = 当前任务状态
```

不要把索引数据库作为唯一数据源。索引可以重建。

### 1.3 AI 不能直接污染正式知识库

AI 可以生成：

```text
candidate
state update
handoff draft
ADR draft
context pack
```

但不能直接写入正式知识库。

正式写入必须经过：

```text
candidate -> review -> promote / merge / discard
```

### 1.4 用户级和项目级必须分开

```text
用户级：~/.worktrail/
项目级：<repo>/.worktrail/
```

判断规则：

```text
能跨项目复用的，进入用户级。
只对当前项目有效的，进入项目级。
```

---

## 2. 为什么使用 Go

完整版本更适合使用 Go，而不是 Python / Node / Rust。

原因：

```text
1. worktrail 是本地 CLI + hook runner + MCP stdio server + 文件系统工具。
2. Go 可以发布为跨平台单文件二进制，适合被 Codex / Claude Code hooks 稳定调用。
3. Hook 执行需要快速启动、清晰 stdin/stdout、明确 exit code。
4. Go 适合文件系统扫描、Markdown 处理、JSONL transcript 解析、git 命令调用。
5. Go 适合实现 MCP stdio server、JSONL 解析和并发 hook 事件处理。
6. 完整版本需要长期可维护的模块化架构，Go 的 internal package 结构适合。
7. 依赖可以控制在较小范围，降低本地工具的安装和运行风险。
```

技术定位：

```text
Go core runtime
  - CLI
  - store
  - indexer
  - state manager
  - candidate manager
  - hook handlers
  - integrations
  - MCP stdio server
  - secret redactor
```

不引入 Web UI、TUI 或后台 daemon，保持本地 CLI-first。

---

## 3. 总体架构

```text
Codex / Claude Code Session
        |
        v
Raw Transcript Collector
        |
        v
Redaction Layer
        |
        +-------------------------+
        |                         |
        v                         v
State Capsule Manager       Knowledge Extractor
        |                         |
        v                         v
Checkpoint / Inject         Candidates
        |                         |
        +-----------+-------------+
                    |
                    v
              Review / Promote
                    |
                    v
     User KB                 Project KB
~/.worktrail/           <repo>/.worktrail/
                    |
                    v
              Local Index
                    |
                    v
              Context Pack
                    |
                    v
         Codex / Claude Code / MCP
```

---

## 4. 目录结构

### 4.1 用户级目录

```text
~/.worktrail/
  config.json

  profile/
    preferences.md
    coding-style.md
    architecture-style.md
    tools.md

  workflows/
    bug-handoff.md
    long-session-management.md
    ai-coding-review.md
    project-bootstrap.md

  prompts/
    codex-task.md
    claude-design-review.md
    agent-handoff.md

  lessons/
    ai-coding-gotchas.md
    context-management.md

  state/
    active/
    checkpoints/
    archived/

  candidates/
    user/

  raw/
    codex/
    claude/

  exports/
    user-brief.md
    user-context.md

  index/
    index.db
    manifest.json

  logs/
    events.jsonl

  index.md
  log.md
```

### 4.2 项目级目录

```text
<repo>/
  .worktrail/
    config.json

    project.md
    current-state.md

    decisions/
      ADR-0001-example.md

    handoffs/
      2026-05-13-example.md

    rules/
      coding-rules.md
      testing-rules.md
      security-rules.md

    prompts/
      project-review.md
      generate-config-draft.md

    state/
      active/
      checkpoints/
      archived/

    candidates/
      project/

    raw/
      codex/
      claude/

    exports/
      project-brief.md
      context-pack.md

    index/
      index.db
      manifest.json

    logs/
      events.jsonl

    index.md
    log.md

  AGENTS.md
  CLAUDE.md
```

---

## 5. 文件格式

所有正式知识文件和 state 文件使用 Markdown + JSON frontmatter。

使用 JSON frontmatter 的原因：

```text
1. Go 标准库可以直接解析 JSON。
2. 避免 YAML 依赖和解析歧义。
3. 仍然保持 Markdown 可读。
```

格式：

```markdown
---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "ki_20260513_001",
  "scope": "project",
  "type": "decision",
  "title": "Config must be draft-first",
  "status": "approved",
  "created_at": "2026-05-13T15:30:00+08:00",
  "updated_at": "2026-05-13T15:30:00+08:00",
  "source_sessions": ["claude:abc123"],
  "tags": ["architecture", "config", "review"]
}
---

# Config must be draft-first

正文内容。
```

---

## 6. 核心数据类型

### 6.1 Knowledge Item

```json
{
  "schema": "worktrail.knowledge.v1",
  "id": "ki_20260513_001",
  "scope": "user | project",
  "type": "preference | workflow | prompt | lesson | project_overview | current_state | decision | rule | handoff | bug_fix | architecture",
  "title": "string",
  "status": "approved | archived",
  "source_sessions": ["codex:...", "claude:..."],
  "source_files": ["path"],
  "tags": ["string"],
  "confidence": 0.0,
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### 6.2 Candidate

```json
{
  "schema": "worktrail.candidate.v1",
  "id": "cand_20260513_001",
  "scope": "user | project",
  "candidate_type": "knowledge | state | handoff | adr | rule | prompt | lesson",
  "target_path": "string",
  "title": "string",
  "summary": "string",
  "operation": "create | append | merge | replace",
  "status": "pending | promoted | discarded | merged",
  "source_sessions": ["string"],
  "redaction_status": "clean | redacted | blocked",
  "created_at": "timestamp"
}
```

### 6.3 State Capsule

```json
{
  "schema": "worktrail.state.v1",
  "id": "st_20260513_001",
  "scope": "user | project",
  "type": "bug | feature | design | research | implementation | prompt | workflow | experiment | decision",
  "title": "string",
  "status": "active | blocked | resolved | archived",
  "source_tool": "codex | claude-code | manual",
  "source_sessions": ["string"],
  "created_at": "timestamp",
  "updated_at": "timestamp",
  "tags": ["string"]
}
```

### 6.4 Event Log

所有重要动作写入 JSONL：

```json
{"time":"2026-05-13T15:30:00+08:00","event":"state.update","id":"st_...","actor":"hook:claude-post-tool-use"}
```

必须记录：

```text
init
install
extract
candidate.create
candidate.promote
candidate.discard
state.start
state.update
state.checkpoint
state.archive
context.generate
hook.run
redaction.block
index.rebuild
```

---

## 7. State Capsule System

### 7.1 目的

解决：

```text
Volatile Session State Loss
会话状态易失问题
```

也就是：

> 关键任务状态只存在于聊天上下文里。一旦 compact、resume、fork、换模型、换工具、切换 agent 或上下文截断，任务状态就可能丢失或变形。

### 7.2 State Capsule 模板

```markdown
---worktrail
{
  "schema": "worktrail.state.v1",
  "id": "st_20260513_pdf_preview",
  "scope": "project",
  "type": "bug",
  "title": "PDF preview field mapping issue",
  "status": "active",
  "source_tool": "claude-code",
  "source_sessions": ["claude:abc123"],
  "created_at": "2026-05-13T15:30:00+08:00",
  "updated_at": "2026-05-13T15:40:00+08:00",
  "tags": ["bug", "pdf", "preview"]
}
---

# State Capsule: PDF preview field mapping issue

## Original Intent
最初用户到底想解决什么。

## Current Goal
当前这轮任务的目标是什么。

## Constraints
必须遵守的约束。

## Relevant Context
和任务有关的背景。

## Evidence
已经看到的证据，例如错误日志、测试输出、用户反馈、文件内容摘要。

## Decisions Made
已经达成的决策。

## Assumptions
当前假设。

## Ruled Out
已经排除的方案或原因。

## Work Done
已经完成的工作。

## Current Diff Intent
当前代码改动的意图。

## Validation
已经运行过的测试、命令、检查结果。

## Open Questions
还没确认的问题。

## Next Step
下一步应该做什么。

## Do Not Forget
压缩、切换会话、换 AI 后也必须保留的关键点。
```

### 7.3 State 生命周期

```text
start
  创建 State Capsule

update
  每轮或关键操作后更新

checkpoint
  compact / resume / fork / switch agent 前保存快照

inject
  新会话、压缩后、任务继续时重新注入

close
  任务完成后生成 handoff / candidate

archive
  状态归档
```

### 7.4 State 命令

```bash
worktrail state start "<title>" --type bug --scope project
worktrail state update
worktrail state checkpoint --reason pre-compact
worktrail state inject "<task>"
worktrail state close --to handoff
worktrail state archive <state-id>
worktrail state list --active
worktrail state show <state-id>
```

---

## 8. Context Pack

### 8.1 目标

每次 Codex / Claude Code 开始任务时，生成一个短而准确的上下文包。

输入：

```bash
worktrail context "修复 PDF preview 字段映射问题"
```

输出：

```markdown
# Context Pack

## Task
修复 PDF preview 字段映射问题。

## User-level Context
- 用户偏好先设计再实现。
- 长任务必须维护 State Capsule。
- AI 生成内容必须先作为 draft。

## Project-level Context
- 当前项目目标。
- 当前实现状态。
- 相关项目规则。

## Active State
- 当前任务状态。
- 当前假设。
- 已完成工作。
- 下一步。

## Relevant Decisions
- ADR-0001: ...

## Relevant Handoffs
- 2026-05-13: ...

## Relevant Files
- internal/example.go

## Task Instruction
基于以上上下文继续任务，不要重复已经排除的方向。
```

### 8.2 检索策略

完整版本需要实现本地搜索和排序。

基础数据源：

```text
1. 用户级正式知识
2. 项目级正式知识
3. 当前 active state
4. 最近 handoff
5. 相关 decisions / rules
6. candidates 中尚未确认但和任务高度相关的内容，必须标记为 unapproved
```

排序因素：

```text
- scope 匹配：项目级优先于用户级
- type 权重：active state、decision、handoff、rule 权重高
- task query 命中
- 最近更新时间
- tag 命中
- source confidence
```

---

## 9. Candidates / Review / Promote

### 9.1 Candidate 目录

```text
~/.worktrail/candidates/user/
<repo>/.worktrail/candidates/project/
```

### 9.2 Candidate 文件格式

```markdown
---worktrail
{
  "schema": "worktrail.candidate.v1",
  "id": "cand_20260513_001",
  "scope": "project",
  "candidate_type": "decision",
  "target_path": ".worktrail/decisions/ADR-0003-example.md",
  "operation": "create",
  "title": "Preview and Generate must stay separate",
  "summary": "...",
  "status": "pending",
  "source_sessions": ["claude:abc123"],
  "redaction_status": "clean",
  "created_at": "2026-05-13T15:30:00+08:00"
}
---

# Candidate: Preview and Generate must stay separate

## Proposed Content
...

## Why this should be promoted
...

## Source Evidence
...
```

### 9.3 Review 命令与对话内 Review

`worktrail` 不做 TUI。默认 review 入口是 Codex / Claude Code 对话本身。

用户通过 `/worktrail-review` skill 或 MCP tools 在 Codex / Claude Code 中查看 candidates、diff、来源和风险，然后在对话中确认 `promote` / `merge` / `discard`。

底层仍必须提供非交互 CLI，供 skills、MCP、hooks 和脚本调用：

```bash
worktrail review
worktrail review --scope project
worktrail review --scope user
worktrail candidates list --format json
worktrail candidates show <candidate-id> --format markdown
worktrail candidates diff <candidate-id>
worktrail promote <candidate-id>
worktrail discard <candidate-id>
worktrail merge <candidate-id> <target-file>
```

要求：review 的读操作必须可以通过 CLI 和 MCP 完成；promote / merge / discard 这类写操作通过非交互 CLI 完成，不能依赖独立 TUI，也不能作为默认 MCP tools 暴露。

### 9.4 Chat-native Review Skill

需要生成 Codex / Claude Code skill：

```text
/worktrail-review
```

skill 流程：

```text
1. 调用 worktrail candidates list --format json
2. 展示待处理 candidates：scope、type、target_path、risk、source
3. 对用户选中的 candidate 调用 worktrail candidates diff
4. 解释 candidate 的价值、风险、是否重复、是否有敏感信息
5. 等待用户明确确认
6. 根据用户指令执行 promote / merge / discard
7. 返回已更新文件和 event log 摘要
```

禁止事项：

```text
1. 不允许在没有用户确认时 promote / merge / discard。
2. 不允许 hook 自动 promote。
3. 不允许把 TUI 作为 review 依赖。
4. 不允许跳过 redaction scan。
```

### 9.5 MCP Review Tools

MCP server 只暴露 review 读操作：

```text
worktrail_list_candidates
worktrail_show_candidate
worktrail_preview_candidate_diff
```

`promote` / `merge` / `discard` 不作为默认 MCP tools 暴露。

原因：这些操作会修改正式知识库或 candidate 状态，必须经过用户在 Codex / Claude Code 对话中的明确确认，然后由 agent 调用非交互 CLI 执行：

```bash
worktrail promote <candidate-id>
worktrail merge <candidate-id> <target-file>
worktrail discard <candidate-id>
```

如果未来支持 MCP write tools，必须是显式 opt-in，并且不属于当前完整版本范围。

### 9.6 Promote 规则

promote 必须：

```text
1. 重新检查 redaction_status
2. 检查 target_path 是否在允许目录内
3. 如果是 replace / merge，先创建 backup
4. 写入正式 Markdown
5. 更新 index.md
6. 更新 event log
7. 重建索引
8. 将 candidate 标记为 promoted
```

---

## 10. Raw Transcript 管理

### 10.1 Sources

支持：

```text
Codex transcript
Claude Code transcript
手动提供的 markdown / jsonl session
```

### 10.2 Raw 目录

```text
~/.worktrail/raw/codex/
~/.worktrail/raw/claude/
<repo>/.worktrail/raw/codex/
<repo>/.worktrail/raw/claude/
```

### 10.3 Sync 命令

```bash
worktrail sync codex
worktrail sync claude
worktrail sync all
worktrail sync --project
worktrail sync --user
```

同步规则：

```text
1. 发现本地 transcript
2. 复制或建立引用记录
3. 保存 metadata
4. 先做 redaction scan
5. 生成 raw manifest
6. 不自动生成正式知识
```

---

## 11. Extraction Engine

### 11.1 目标

从 transcript 中提炼：

```text
user-level candidates
project-level candidates
state updates
handoff candidates
ADR candidates
prompt candidates
lesson candidates
```

### 11.2 Provider 接口

完整版本需要设计 provider 接口，但不把某个模型写死。

```text
ExtractorProvider
  - Name()
  - Extract(input, schema) -> structured result
```

可支持：

```text
1. manual provider
   基于规则和模板生成基础 candidate。

2. codex provider
   调用本地 Codex 能力生成结构化 extraction。

3. claude provider
   调用本地 Claude Code 能力生成结构化 extraction。

4. 不实现 custom command provider
   避免 arbitrary command execution。只支持 manual / codex / claude 三类 provider。
```

### 11.3 Extract 命令

```bash
worktrail extract --source claude --session latest
worktrail extract --source codex --session latest
worktrail extract ./session.jsonl
worktrail extract ./session.md --scope project
worktrail extract --write-candidates
```

输出必须先进入 candidates。

---

## 12. Redaction / 安全策略

### 12.1 必须阻止沉淀的内容

```text
API keys
JWT
SSH private keys
.env 内容
数据库密码
OAuth tokens
session cookies
客户数据
生产日志中的 PII
本机认证文件
```

### 12.2 Redaction 命令

```bash
worktrail redact scan <file>
worktrail redact scan --session latest
worktrail redact scan --project
```

### 12.3 Redaction 状态

```text
clean      没有发现敏感内容
redacted   已替换敏感片段
blocked    发现高风险内容，禁止写入 candidate / raw / index
```

### 12.4 Hook 场景

在 hook 中：

```text
PreToolUse / PostToolUse / Stop / SessionEnd
```

必须先做 redaction scan，再写入 state 或 candidate。

---

## 13. Codex 集成

### 13.1 install 命令

```bash
worktrail install codex
worktrail install codex --user
worktrail install codex --project
worktrail uninstall codex
worktrail doctor codex
```

### 13.2 生成内容

用户级：

```text
~/.codex/AGENTS.md
~/.agents/skills/worktrail-context/SKILL.md
~/.agents/skills/worktrail-handoff/SKILL.md
~/.agents/skills/worktrail-review/SKILL.md
```

项目级：

```text
<repo>/AGENTS.md
<repo>/.agents/skills/worktrail-context/SKILL.md
<repo>/.agents/skills/worktrail-state/SKILL.md
<repo>/.agents/skills/worktrail-handoff/SKILL.md
<repo>/.codex/hooks.json
```

### 13.3 Codex hooks

实现 hook handlers：

```bash
worktrail hook codex session-start
worktrail hook codex user-prompt
worktrail hook codex post-tool-use
worktrail hook codex stop
```

行为：

```text
SessionStart
  - 加载 user brief / project brief
  - 如有 active state，输出简短提示

UserPromptSubmit
  - 根据 prompt 检索 context
  - 注入 Context Pack / Active State 摘要

PostToolUse
  - 记录关键文件读取、修改、命令、测试结果
  - 更新 event log

Stop
  - 更新 State Capsule
  - 生成 candidate draft，但不 promote
```

---

## 14. Claude Code 集成

### 14.1 install 命令

```bash
worktrail install claude
worktrail install claude --user
worktrail install claude --project
worktrail uninstall claude
worktrail doctor claude
```

### 14.2 生成内容

用户级：

```text
~/.claude/CLAUDE.md
~/.claude/skills/worktrail-context/SKILL.md
~/.claude/skills/worktrail-handoff/SKILL.md
~/.claude/skills/worktrail-review/SKILL.md
```

项目级：

```text
<repo>/CLAUDE.md
<repo>/.claude/settings.json
<repo>/.claude/skills/worktrail-context/SKILL.md
<repo>/.claude/skills/worktrail-state/SKILL.md
<repo>/.claude/skills/worktrail-handoff/SKILL.md
```

### 14.3 Claude hooks

实现 hook handlers：

```bash
worktrail hook claude session-start
worktrail hook claude user-prompt
worktrail hook claude pre-compact
worktrail hook claude post-compact
worktrail hook claude post-tool-use
worktrail hook claude session-end
```

行为：

```text
SessionStart
  - startup / resume / compact 后加载 active state

UserPromptSubmit
  - 自动注入 Context Pack / State Capsule 摘要

PreCompact
  - 更新 active state
  - 写 checkpoint
  - 对 bug / design / implementation 任务做 compact guard

PostCompact
  - 保存 compact summary
  - 对比 active state，发现遗漏则生成 warning candidate

PostToolUse
  - 记录关键文件、命令、测试结果

SessionEnd
  - 生成 handoff candidate
  - 生成 knowledge candidates
```

---

## 15. Skills / Slash Commands

完整版本需要生成 Codex 和 Claude Code 两套 skills。

### 15.1 worktrail-context

用途：任务开始时生成上下文。

```bash
worktrail context "$ARGUMENTS"
```

### 15.2 worktrail-state

用途：管理当前任务状态。

```bash
worktrail state start "$ARGUMENTS"
worktrail state update
worktrail state checkpoint --reason manual
worktrail state inject "$ARGUMENTS"
```

### 15.3 worktrail-handoff

用途：任务结束或切换 AI 时生成 handoff。

```bash
worktrail handoff
```

### 15.4 worktrail-review

用途：review candidates。

```bash
worktrail review
```

---

## 16. MCP stdio Server

完整版本需要实现本地 MCP stdio server。

命令：

```bash
worktrail mcp serve --stdio
```

MCP 只支持 stdio。不要实现 HTTP server、端口监听或后台 daemon。

### 16.1 MCP tools

默认只开放 read 和 draft-write。

```text
worktrail.search
worktrail.context_pack
worktrail.state.active
worktrail.state.read
worktrail.state.create
worktrail.state.update
worktrail.candidate.create
worktrail.candidate.list
worktrail.handoff.create
worktrail.redact.scan
```

危险操作默认不开放：

```text
candidate.promote
candidate.merge
candidate.discard
knowledge.delete
knowledge.replace
```

当前完整版本不实现这些危险 MCP tools。正式知识写入通过 CLI 命令执行，并依赖 Codex / Claude Code 对话中的用户确认。

### 16.2 MCP resources

```text
user://brief
user://profile/preferences
user://workflows
project://overview
project://current-state
project://decisions
project://handoffs
project://active-state
```

---

## 17. 本地索引

完整版本需要本地索引，但 Markdown 仍是 source of truth。

### 17.1 索引内容

```text
path
scope
type
title
tags
status
created_at
updated_at
plain_text
frontmatter
source_sessions
```

### 17.2 命令

```bash
worktrail index rebuild
worktrail index status
worktrail search "query"
```

### 17.3 搜索要求

必须支持：

```text
scope filter
content search
tag filter
type filter
active state boost
recent update boost
```

明确不做：

```text
本地 embedding / vector index
远程 embedding
向量数据库
```

搜索优先使用本地文本索引、frontmatter metadata、tag/type/scope filter、recent/active-state boost。

---

## 18. CLI 命令总表

```bash
# 初始化
worktrail init-user
worktrail init-project
worktrail init

# 安装集成
worktrail install codex
worktrail install claude
worktrail install all
worktrail uninstall codex
worktrail uninstall claude
worktrail doctor
worktrail doctor codex
worktrail doctor claude

# 上下文
worktrail context "<task>"
worktrail search "<query>"

# State Capsule
worktrail state start "<title>" --type bug --scope project
worktrail state update
worktrail state checkpoint --reason pre-compact
worktrail state inject "<task>"
worktrail state close --to handoff
worktrail state archive <state-id>
worktrail state list --active
worktrail state show <state-id>

# 知识提炼
worktrail sync codex
worktrail sync claude
worktrail sync all
worktrail extract --source claude --session latest
worktrail extract --source codex --session latest
worktrail extract <session-file>

# candidates / chat-native review
worktrail review
worktrail review --scope project
worktrail review --scope user
worktrail candidates list --format json
worktrail candidates show <candidate-id> --format markdown
worktrail candidates diff <candidate-id>
worktrail promote <candidate-id>
worktrail discard <candidate-id>
worktrail merge <candidate-id> <target-file>

# handoff / ADR
worktrail handoff
worktrail adr create

# redaction
worktrail redact scan <file>
worktrail redact scan --session latest

# index
worktrail index rebuild
worktrail index status

# hooks
worktrail hook codex session-start
worktrail hook codex user-prompt
worktrail hook codex post-tool-use
worktrail hook codex stop
worktrail hook claude session-start
worktrail hook claude user-prompt
worktrail hook claude pre-compact
worktrail hook claude post-compact
worktrail hook claude post-tool-use
worktrail hook claude session-end

# MCP
worktrail mcp serve --stdio
```

---

## 19. Go 项目结构

```text
worktrail/
  go.mod
  README.md
  AGENTS.md
  CLAUDE.md

  cmd/
    worktrail/
      main.go

  internal/
    app/
      app.go
      command.go

    config/
      config.go
      discovery.go

    paths/
      paths.go
      project_root.go

    model/
      knowledge.go
      candidate.go
      state.go
      event.go
      transcript.go

    store/
      markdown.go
      frontmatter.go
      user_store.go
      project_store.go
      manifest.go

    index/
      indexer.go
      search.go
      ranking.go

    state/
      manager.go
      checkpoint.go
      inject.go
      lifecycle.go

    candidate/
      manager.go
      review.go
      promote.go
      merge.go

    contextpack/
      builder.go
      renderer.go
      ranking.go

    transcript/
      codex.go
      claude.go
      jsonl.go
      sync.go

    extract/
      extractor.go
      provider.go
      manual_provider.go
      codex_provider.go
      claude_provider.go
      schema.go

    redact/
      scanner.go
      patterns.go
      policy.go

    integrations/
      codex/
        install.go
        hooks.go
        skills.go
        agents_md.go

      claude/
        install.go
        hooks.go
        skills.go
        claude_md.go

    hooks/
      input.go
      output.go
      codex.go
      claude.go

    mcp/
      server.go
      tools.go
      resources.go
      jsonrpc.go

    log/
      events.go

    util/
      time.go
      slug.go
      exec.go
      git.go
      atomic_write.go

  templates/
    user/
    project/
    skills/
    hooks/
    knowledge/
    state/
    candidates/

  testdata/
    transcripts/
      codex/
      claude/
    fixtures/
      hooks/
      states/
      candidates/

  docs/
    user-guide.md
    developer-guide.md
    codex-integration.md
    claude-integration.md
    state-capsule.md
    mcp.md
```

---

## 20. 安装和配置要求

### 20.1 init-user

必须创建：

```text
~/.worktrail/config.json
~/.worktrail/profile/*.md
~/.worktrail/workflows/*.md
~/.worktrail/state/{active,checkpoints,archived}
~/.worktrail/candidates/user
~/.worktrail/raw/{codex,claude}
~/.worktrail/exports
~/.worktrail/index
~/.worktrail/logs/events.jsonl
```

### 20.2 init-project

必须创建：

```text
.worktrail/config.json
.worktrail/project.md
.worktrail/current-state.md
.worktrail/decisions
.worktrail/handoffs
.worktrail/rules
.worktrail/prompts
.worktrail/state/{active,checkpoints,archived}
.worktrail/candidates/project
.worktrail/raw/{codex,claude}
.worktrail/exports
.worktrail/index
.worktrail/logs/events.jsonl
AGENTS.md
CLAUDE.md
```

### 20.3 install

install 必须是幂等的。

规则：

```text
1. 不覆盖用户已有文件。
2. 如果需要修改已有文件，先备份。
3. 使用明确的 managed block。
4. uninstall 只能删除 managed block，不破坏用户原内容。
```

managed block 示例：

```markdown
<!-- BEGIN WORKTRAIL MANAGED BLOCK -->
...
<!-- END WORKTRAIL MANAGED BLOCK -->
```

---

## 21. 测试要求

必须包含：

```text
1. frontmatter parser tests
2. path discovery tests
3. atomic write tests
4. state lifecycle tests
5. checkpoint tests
6. context pack builder tests
7. candidate promote / discard / merge tests
8. redaction scanner tests
9. transcript parser tests
10. codex hook fixture tests
11. claude hook fixture tests
12. index rebuild / search tests
13. install / uninstall idempotency tests
14. MCP JSON-RPC tests
```

测试应使用临时目录，不修改真实 home。

支持环境变量：

```text
WORKTRAIL_HOME=<tempdir>
WORKTRAIL_PROJECT_ROOT=<tempdir>
```

---

## 22. 验收标准

完整版本交付时必须满足：

```text
1. `worktrail init-user` 能完整初始化用户级目录。
2. `worktrail init-project` 能完整初始化项目级目录。
3. `worktrail context "task"` 能生成包含 user/project/state 的 Context Pack。
4. `worktrail state start/update/checkpoint/inject/close/archive` 全部可用。
5. `worktrail extract` 只生成 candidates，不直接写正式知识库。
6. `worktrail review/promote/discard/merge` 可用。
7. `worktrail sync codex/claude` 能采集 raw transcript metadata。
8. `worktrail install codex` 能安装 AGENTS.md、skills、hooks。
9. `worktrail install claude` 能安装 CLAUDE.md、skills、hooks。
10. hook handlers 能从 stdin 读取事件 JSON，并输出可用结果。
11. Claude pre-compact 能生成 checkpoint。
12. Codex stop 能更新 active state。
13. Secret redaction 能阻止敏感内容写入 candidate。
14. index rebuild/search 可用。
15. MCP stdio server 可启动，并只暴露 read / draft-write tools，不暴露 promote / merge / discard。
16. 所有正式知识文件都是 Markdown，可手动阅读和 Git 管理。
17. 所有重要动作写入 event log。
18. 测试全部通过。
```

---

## 23. Codex 开发要求

开发时请遵守：

```text
1. 直接实现完整版本，不要停留在 MVP scaffold。
2. 先建立数据模型、路径系统、Markdown store、event log。
3. 再实现 state/candidate/context/index。
4. 再实现 Codex / Claude integrations。
5. 再实现 MCP。
6. 每个模块必须有测试。
7. 所有写文件操作使用 atomic write。
8. 所有路径必须防止越权写入。
9. 所有 hook 输入都必须容错处理。
10. 所有 promote 操作必须经过 redaction scan。
```

推荐实现顺序：

```text
1. repo skeleton
2. config + paths
3. model + frontmatter
4. store + atomic write
5. init-user / init-project
6. event log
7. state lifecycle
8. candidate lifecycle
9. context pack
10. redaction scanner
11. transcript parsers
12. extract provider interface
13. index/search
14. Codex install + hooks + skills
15. Claude install + hooks + skills
16. MCP stdio server
17. chat-native review skills / MCP flow
18. docs + tests
```

---

## 24. 最终总结

`worktrail` 的最终定位是：

```text
Codex / Claude Code Native Knowledge & State Layer
```

它解决两个核心问题：

```text
1. AI 对话中的长期知识无法沉淀。
2. 长会话中的关键任务状态在 compact / resume / fork / 切换 agent 后丢失。
```

最终能力：

```text
Raw Transcript -> Redaction -> State Capsule -> Candidate -> Review -> Knowledge -> Context Pack -> Next AI Session
```

关键原则：

```text
- 用户级和项目级分离
- State 和 Knowledge 分离
- Markdown 是 source of truth
- 索引可以重建
- AI 只能生成 candidate
- 人确认后才能 promote
- 压缩前 checkpoint
- 压缩后 inject active state
- Codex / Claude Code hooks 和 skills 深度集成
```
