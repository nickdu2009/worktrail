---
name: worktrail-review
description: Review Worktrail candidates in chat before any promotion or merge.
---

# Worktrail Review

Use this skill to review pending candidates in Codex or Claude Code chat.

1. Run `worktrail candidates list --format json`.
2. Show pending candidates with scope, type, target path, source, and risk.
3. For a selected candidate, run `worktrail candidates diff <candidate-id>`.
4. Explain value, duplication risk, and redaction status.
5. Wait for explicit user confirmation.
6. Only after confirmation, run the requested non-interactive CLI command: `worktrail promote`, `worktrail merge`, or `worktrail discard`.

Never promote, merge, discard, delete, or replace from hooks or default MCP tools.
