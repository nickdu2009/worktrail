# Worktrail

`worktrail` is a local-first AI coding session knowledge and state layer for Codex and Claude Code.

The command name is `worktrail`; the Go module path is `github.com/nickdu2009/worktrail`.

Hard boundaries:

- no TUI
- no Web UI or dashboard
- no HTTP MCP server
- no daemon, watcher, or background service
- no embedding or vector database
- no custom external command provider
- no default MCP promote, merge, discard, restore, retire, delete, or replace tools

Formal knowledge is Markdown with JSON frontmatter. Local indexes are rebuildable acceleration data, not source of truth.

## Minimal workflow

```bash
worktrail init
worktrail context "current task"
worktrail candidates create --type rule --target rules/example.md --title "Example Rule" "Rule body."
worktrail review
worktrail candidates diff <candidate-id>
worktrail promote <candidate-id>
worktrail index rebuild
worktrail context "current task"
```

`worktrail context <task>` hides pending transcript evidence by default and reports how many evidence candidates are hidden. Use `worktrail context --evidence <task>` when the raw transcript notes themselves need to be included in the Pending Candidates section.

## Restore vs retire

`worktrail review` warns when an applied candidate says a formal target exists but the target file is missing.

- Use `worktrail restore <candidate-id>` when the formal target was deleted by mistake and should be recreated from the applied candidate body.
- Use `worktrail retire <candidate-id> --reason <text>` when the formal target was intentionally deleted and Worktrail should stop warning about it.
