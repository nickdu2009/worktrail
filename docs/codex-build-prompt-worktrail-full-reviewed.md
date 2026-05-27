# Codex 开发 Prompt：Worktrail 完整版本

你现在要开发一个本地 CLI 工具，仓库名为 `worktrail`，命令名为 `worktrail`。

这不是 MVP。请直接按完整本地产品实现。

## 产品定义

`worktrail` 是一个本地优先的 AI coding session 知识与状态管理工具。它把 Codex / Claude Code 长对话中的高价值内容沉淀为用户级和项目级知识库，同时把正在进行的任务状态外部化为 State Capsule，避免 compact、resume、fork、换模型、换工具、换 agent 后丢失上下文。

它不是聊天记录管理器，而是：

```text
AI Coding Session State + Knowledge System
```

核心能力：

```text
1. User-level Knowledge
2. Project-level Knowledge
3. State Capsule
4. Checkpoint
5. Context Pack
6. Handoff
7. Candidate Review / Promote
8. Codex integration
9. Claude Code integration
10. Local MCP stdio server
11. Redaction / Secret scanning
12. Local index / search
```

## 技术要求

- 使用 Go。
- 命令名：`worktrail`。
- 正式知识以 Markdown + JSON frontmatter 为 source of truth。
- 本地索引用于搜索和排序，但索引必须可重建。
- 不允许 AI 直接写正式知识库。
- 所有 extraction 结果必须先进入 candidates。
- promote / merge / discard 必须由用户在 Codex / Claude Code 对话中明确确认后，通过非交互 CLI 触发。
- 所有写文件操作必须 atomic write。
- 所有路径必须防止越权写入。
- 所有敏感信息必须经过 redaction scan。
- hooks 必须能从 stdin 读取事件 JSON，并容错处理。
- 测试必须使用临时目录，不能污染真实 home。

明确不要实现：

```text
1. TUI
2. Web UI / dashboard
3. HTTP MCP server
4. 后台 daemon / 常驻服务
5. 本地 embedding / vector index / 向量数据库
6. custom external command provider
7. 默认 MCP promote / merge / discard tools
8. hooks 自动 promote
9. 自动后台 watcher
```

## 目录约定

用户级：

```text
~/.worktrail/
```

项目级：

```text
<repo>/.worktrail/
```

项目根目录同时生成：

```text
AGENTS.md
CLAUDE.md
```

## 必须实现的命令

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

# transcript / extraction
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

Review requirement:

```text
Do not implement a TUI.
The primary review interface is Codex / Claude Code chat via /worktrail-review skill and MCP tools.
Review read actions must have CLI and MCP equivalents. Review write actions use non-interactive CLI, not default MCP write tools.
Hooks may generate candidates but must never promote them automatically.
```

## 用户级目录

```text
~/.worktrail/
  config.json
  profile/
    preferences.md
    coding-style.md
    architecture-style.md
    tools.md
  workflows/
  prompts/
  lessons/
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
  index/
  logs/
    events.jsonl
  index.md
  log.md
```

## 项目级目录

```text
<repo>/.worktrail/
  config.json
  project.md
  current-state.md
  decisions/
  handoffs/
  rules/
  prompts/
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
  index/
  logs/
    events.jsonl
  index.md
  log.md
```

## Markdown + JSON frontmatter 格式

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

## State Capsule 模板

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

## Current Goal

## Constraints

## Relevant Context

## Evidence

## Decisions Made

## Assumptions

## Ruled Out

## Work Done

## Current Diff Intent

## Validation

## Open Questions

## Next Step

## Do Not Forget
```

## Codex 集成要求

`worktrail install codex` 必须安装或更新：

```text
~/.codex/AGENTS.md
<repo>/AGENTS.md
~/.agents/skills/worktrail-context/SKILL.md
~/.agents/skills/worktrail-handoff/SKILL.md
~/.agents/skills/worktrail-review/SKILL.md
<repo>/.agents/skills/worktrail-context/SKILL.md
<repo>/.agents/skills/worktrail-state/SKILL.md
<repo>/.codex/hooks.json
```

Codex hook handlers：

```text
SessionStart      -> 加载 brief / active state
UserPromptSubmit  -> 注入 Context Pack
PostToolUse       -> 记录文件、命令、测试结果
Stop              -> 更新 State Capsule，生成 candidates
```

## Claude Code 集成要求

`worktrail install claude` 必须安装或更新：

```text
~/.claude/CLAUDE.md
<repo>/CLAUDE.md
~/.claude/skills/worktrail-context/SKILL.md
~/.claude/skills/worktrail-handoff/SKILL.md
~/.claude/skills/worktrail-review/SKILL.md
<repo>/.claude/skills/worktrail-context/SKILL.md
<repo>/.claude/skills/worktrail-state/SKILL.md
<repo>/.claude/settings.json
```

Claude hook handlers：

```text
SessionStart      -> startup / resume / compact 后加载 active state
UserPromptSubmit  -> 注入 Context Pack
PreCompact        -> 更新 active state，生成 checkpoint
PostCompact       -> 保存 compact summary，检查遗漏
PostToolUse       -> 记录文件、命令、测试结果
SessionEnd        -> 生成 handoff candidate 和 knowledge candidates
```

## MCP Server 要求

实现：

```bash
worktrail mcp serve --stdio
```

默认只暴露 read 和 draft-write tools：

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

危险工具不实现为默认 MCP tools：

```text
candidate.promote
candidate.merge
candidate.discard
knowledge.delete
knowledge.replace
```

Promote / merge / discard 通过非交互 CLI 执行，并由 /worktrail-review skill 在用户明确确认后调用。

## Go 项目结构

```text
cmd/worktrail/main.go
internal/app/
internal/config/
internal/paths/
internal/model/
internal/store/
internal/index/
internal/state/
internal/candidate/
internal/contextpack/
internal/transcript/
internal/extract/
internal/redact/
internal/integrations/codex/
internal/integrations/claude/
internal/hooks/
internal/mcp/
internal/log/
internal/util/
templates/
testdata/
docs/
```

## 实现顺序

请按这个顺序开发，不要只写 scaffold：

```text
1. repo skeleton
2. config + paths
3. model + JSON frontmatter parser
4. markdown store + atomic write
5. init-user / init-project
6. event log
7. state lifecycle
8. checkpoint / inject
9. candidate lifecycle
10. review / promote / discard / merge; review must be chat-native through Codex / Claude Code skills and MCP, not TUI
11. context pack builder
12. redaction scanner
13. transcript parsers
14. sync codex / claude
15. extraction provider interface
16. local index / search
17. Codex install + skills + hooks
18. Claude install + skills + hooks
19. MCP stdio server only
20. chat-native review skill / MCP flow
21. docs + tests
```

## 测试要求

必须实现测试：

```text
frontmatter parser
path discovery
atomic write
state lifecycle
checkpoint
context pack builder
candidate promote / discard / merge
redaction scanner
transcript parser
codex hook fixture
claude hook fixture
index rebuild / search
install / uninstall idempotency
MCP JSON-RPC
```

测试必须使用临时目录，并支持：

```text
WORKTRAIL_HOME=<tempdir>
WORKTRAIL_PROJECT_ROOT=<tempdir>
```

## 验收标准

交付时必须满足：

```text
1. init-user / init-project 可用。
2. context 能生成 user + project + active state 的 Context Pack。
3. state 生命周期全可用。
4. extract 只生成 candidates。
5. review/promote/discard/merge 可用。
6. sync codex/claude 可采集 raw transcript metadata。
7. install codex/claude 可生成 AGENTS.md / CLAUDE.md / hooks / skills。
8. hook handlers 能从 stdin 读取事件 JSON。
9. Claude pre-compact 能生成 checkpoint。
10. Codex stop 能更新 active state。
11. redaction 能阻止敏感内容写入 candidate。
12. index rebuild/search 可用。
13. MCP stdio server 可启动，且不暴露 promote / merge / discard。
14. 所有正式知识文件都是 Markdown。
15. 所有重要动作写入 event log。
16. 测试通过。
```

现在请开始实现完整版本。
