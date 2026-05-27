---
name: worktrail-review
description: Review pending Worktrail candidates and decide whether knowledge should be promoted, merged, discarded, restored, or retired after explicit user confirmation.
---

# Worktrail Review

Use this skill when the user asks to review candidates, inspect pending Worktrail knowledge, or decide whether knowledge should be promoted, merged, discarded, restored, or retired.

1. If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
2. Run `worktrail review plan --format json` to get the read-only agent contract for pending semantic candidates. Read-only review commands do not need user confirmation.
3. Group the JSON items by `recommended_action`: `promote`, `merge`, `discard`, and `needs_human_review`, and show counts for each group before item details.
4. Show only pending semantic candidates (`architecture`, `decision`, `glossary`, `integration`, `lesson`, `project`, `prompt`, `rule`, `validation`, `workflow`) with scope, type, operation, target path, `source_candidate_ids`, source status, redaction status, warnings, reason codes, and suggested commands.
5. Do not include `transcript_notes` evidence or non-semantic operational candidates in the default review table. Apply the same default-hidden treatment to other evidence candidates such as `migration_source`, and keep operational candidates such as `handoff` out of the default review table as well. If hidden items may exist, run `worktrail review` for the human summary and mention only the count plus the relevant inspection command. If the plan JSON is unavailable or raw pending semantic records are needed for troubleshooting, run `worktrail candidates list --semantic --status pending --format json`.
6. Only when the user explicitly asks to inspect evidence, run `worktrail review --evidence` or `worktrail candidates list --evidence --status pending --format json`. Use `worktrail review --all` only when non-semantic operational candidates also need inspection. Preview may still show operational candidates in a separate bucket, but that does not make them part of the default review inbox.
7. For a selected semantic candidate, run `worktrail candidates diff <candidate-id>`.
8. Explain value, source traceability, duplication risk, redaction status, warnings, and plan `reason_codes`. Do not parse ids out of warning strings; use `source_statuses` from the review plan.
9. Prepare single-candidate commands from each item's plan `commands` array only for `promote`, `merge`, and `discard`. Preserve any `--scope` flags from the plan commands. Do not generate a state-changing command for `needs_human_review`.
10. For a confirmed batch of semantic candidates with the same action and scope, prepare `worktrail review apply-candidates --promote|--merge|--discard <id...> [--scope ...]`. Do not batch candidates with different actions or scopes.
11. Wait for explicit user confirmation before executing any state-changing command. Confirmation must identify the action, the exact candidate id list, and the scope.
12. If `worktrail review` reports an applied candidate with a missing target, explain the two safe paths: `worktrail restore <id>` recreates an accidentally deleted promoted replace target; `worktrail retire <id> --reason <text>` acknowledges an intentionally removed target.
13. Only after confirmation, run the requested non-interactive CLI command: `worktrail promote`, `worktrail merge`, `worktrail discard`, `worktrail review apply-candidates`, `worktrail restore`, or `worktrail retire`.

Do not automatically commit git changes.

Never promote, merge, or discard `transcript_notes` from review actions; they are evidence and must be distilled into semantic candidates first or handled through the evidence lifecycle workflow.

Never promote, merge, or discard `migration_source` or legacy KDD split-source candidates directly from the review plan. They are evidence-like sources and should remain for the evidence lifecycle workflow.

Never promote, merge, discard, restore, retire, delete, or replace from hooks or any implicit runtime automation.
