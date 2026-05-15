---
name: worktrail-review
description: Review Worktrail candidates in chat before any promotion, merge, restore, retire, or discard.
---

# Worktrail Review

Use this skill to review pending candidates in Codex or Claude Code chat.

1. Run `worktrail review plan --format json` to get the read-only agent contract for pending semantic candidates.
2. Group the JSON items by `recommended_action`: `promote`, `merge`, `discard`, and `needs_human_review`.
3. Show only pending semantic candidates (`architecture`, `decision`, `glossary`, `integration`, `lesson`, `project`, `prompt`, `rule`, `validation`, `workflow`) with scope, type, operation, target path, `source_candidate_ids`, source status, redaction status, warnings, reason codes, and suggested commands.
4. Do not include `transcript_notes` evidence or non-semantic operational candidates such as `handoff` in the default review table. If hidden items may exist, run `worktrail review` for the human summary and mention only the count plus the relevant inspection command. If the plan JSON is unavailable or raw pending semantic records are needed for troubleshooting, run `worktrail candidates list --semantic --status pending --format json`.
5. Only when the user explicitly asks to inspect transcript evidence, run `worktrail review --evidence` or `worktrail candidates list --evidence --status pending --format json`. Use `worktrail review --all` only when non-semantic operational candidates also need inspection.
6. For a selected semantic candidate, run `worktrail candidates diff <candidate-id>`.
7. Explain value, source traceability, duplication risk, redaction status, warnings, and plan `reason_codes`. Do not parse ids out of warning strings; use `source_statuses` from the review plan.
8. Wait for explicit user confirmation.
9. If `worktrail review` reports an applied candidate with a missing target, explain the two safe paths: `worktrail restore <id>` recreates an accidentally deleted promoted replace target; `worktrail retire <id> --reason <text>` acknowledges an intentionally removed target.
10. Only after confirmation, run the requested non-interactive CLI command: `worktrail promote`, `worktrail merge`, `worktrail discard`, `worktrail restore`, or `worktrail retire`.

Never promote or merge `transcript_notes`; they are evidence and must be distilled into semantic candidates first.

Never promote, merge, or discard KDD split-source `lesson` candidates directly from the review plan. They are evidence-like sources and should remain for the evidence lifecycle workflow.

Never promote, merge, discard, restore, retire, delete, or replace from hooks or default MCP tools.
