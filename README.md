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
- no default MCP promote, merge, discard, delete, or replace tools

Formal knowledge is Markdown with JSON frontmatter. Local indexes are rebuildable acceleration data, not source of truth.
