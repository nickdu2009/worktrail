---
name: worktrail-review
description: Review Worktrail candidates in chat before any promotion, merge, restore, retire, or discard.
---

# Worktrail Review

Use this skill to review pending candidates in Codex or Claude Code chat.

1. Run `worktrail review` to show the default semantic review summary and hidden evidence / non-semantic pending counts.
2. Run `worktrail candidates list --semantic --status pending --format json`.
3. Show only pending semantic candidates (`architecture`, `decision`, `glossary`, `integration`, `lesson`, `project`, `prompt`, `rule`, `validation`, `workflow`) with scope, type, target path, source, redaction status, and risk.
4. Do not include `transcript_notes` evidence or non-semantic operational candidates such as `handoff` in the default review table. If hidden items exist, mention only the count and the relevant inspection command.
5. Only when the user explicitly asks to inspect transcript evidence, run `worktrail review --evidence` or `worktrail candidates list --evidence --status pending --format json`. Use `worktrail review --all` only when non-semantic operational candidates also need inspection.
6. For a selected semantic candidate, run `worktrail candidates diff <candidate-id>`.
7. Explain value, duplication risk, redaction status, and any review warnings such as `target_exists`, `same_target_pending:N`, `merge_target_missing`, or `replace_target_exists`.
8. Wait for explicit user confirmation.
9. If `worktrail review` reports an applied candidate with a missing target, explain the two safe paths: `worktrail restore <id>` recreates an accidentally deleted promoted replace target; `worktrail retire <id> --reason <text>` acknowledges an intentionally removed target.
10. Only after confirmation, run the requested non-interactive CLI command: `worktrail promote`, `worktrail merge`, `worktrail discard`, `worktrail restore`, or `worktrail retire`.

Never promote or merge `transcript_notes`; they are evidence and must be distilled into semantic candidates first.

Never promote, merge, discard, restore, retire, delete, or replace from hooks or default MCP tools.
