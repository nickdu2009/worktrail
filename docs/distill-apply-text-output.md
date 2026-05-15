# Distill Apply Text Output Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

`worktrail distill apply <proposal.json>` already has a structured report path.
The default text output should become readable enough for humans and agents to
quickly see which proposal items were created, skipped, blocked, or rejected
without rerunning the command with `--format json`.

JSON remains the stable machine contract. Text output is an operator summary
rendered from the same report model.

## Goals

- Make partial success obvious.
- Show created, skipped, blocked, and error items in separate sections.
- Preserve warning codes in text output.
- Keep the text output deterministic and compact.
- Avoid exposing local absolute proposal paths or sensitive redaction details.

## Non-Goals

- Do not change proposal validation semantics.
- Do not change `--format json`.
- Do not make text output the canonical machine contract.
- Do not change candidate creation, redaction scanning, or id generation.
- Do not automatically promote, merge, discard, or mutate source candidates.

## Output Model

Text output should render from the same report fields as JSON:

- `valid`
- `created`
- `skipped`
- `blocked`
- `warnings`
- `items[]`

Item statuses:

- `created`
- `skipped`
- `blocked`
- `error`

For `distill validate`, the same renderer can use `valid` instead of
`created` for item status if shared rendering is practical.

## Text Structure

Recommended default shape:

```text
Distill apply: partial success

Summary: created=2 skipped=1 blocked=1 errors=1 warnings=2

Created
- [0] distill-rule-example -> rules/example.md (rule, replace)
  warnings: target_exists

Skipped
- [1] distill-rule-example -> rules/example.md (rule, replace)
  warnings: duplicate_id_existing_body_may_differ

Blocked
- [2] rules/secret.md (rule, replace)
  errors: redaction_blocked

Errors
- [3] decisions/bad.md (decision, replace)
  errors: source_missing:note-404
```

If a section has no items, omit that section. Always include the summary line.
When there are global warnings, include them after the summary line.

## Status Line

The first line should be one of:

- `Distill apply: success`
- `Distill apply: partial success`
- `Distill apply: completed with issues`
- `Distill apply: no changes`
- `Distill apply: failed`

Rules:

- `failed` is for fatal errors.
- `success` is for reports with one or more created items and zero skipped,
  blocked, or error items.
- `partial success` is for reports with one or more created items plus any
  skipped, blocked, or error item.
- `completed with issues` is for reports with zero created items and at least
  one blocked or error item.
- `no changes` is for reports with zero created items, zero blocked items, and
  zero error items. This includes pure skipped duplicate reports.

Warnings do not determine the status line by themselves. They remain visible in
the summary and item details.

## Item Rendering

Each item line should include:

- proposal index
- candidate id when known
- target path
- candidate type
- operation

Follow-up lines should include non-empty warning and error code lists.

Do not print candidate bodies in `distill apply` text output. The reviewer can
use `worktrail candidates diff <id>` after candidates are created.

## Acceptance Criteria

- Partial apply with created, skipped, blocked, and error items is readable in
  default text output.
- Warning codes remain visible in text output.
- Fatal proposal read or JSON parse errors return a clear failed text message.
- `--format json` remains unchanged.
- Text output does not include candidate bodies or local absolute source paths.
