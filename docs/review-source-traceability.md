# Review Source Traceability Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

`worktrail review` should make distilled candidate sources visible enough for a
reviewer or agent to understand where a semantic candidate came from before
running `promote`, `merge`, or `discard`.

This is a lightweight review aid for the existing human review command. It is
not the full agent-readable review plan contract, which is covered by
[review-plan-automation.md](review-plan-automation.md).

## Goals

- Show `source_candidate_ids` for pending semantic candidates when present.
- Report whether each source candidate is usable review evidence.
- Keep transcript evidence and operational candidates hidden from the default
  review list.
- Give the reviewer a focused next step with `worktrail candidates diff <id>`.
- Preserve the current explicit-confirmation boundary for state-changing
  commands.

## Non-Goals

- Do not make `worktrail review` parseable as the long-term agent contract.
- Do not add automatic approval or automatic discard.
- Do not show raw transcript evidence by default.
- Do not inspect source candidates across scopes.
- Do not change candidate metadata or formal knowledge.

## Behavior

Default `worktrail review` should continue to show pending semantic candidates
only. For each semantic candidate:

- If `source_candidate_ids` is empty, show a concise source warning.
- If source ids are present, show the ids in a compact line.
- For each source id, resolve it within the same scope as the reviewed
  candidate.
- If the source exists, classify it as valid evidence only when it is pending
  `transcript_notes` or an allowed KDD split-source lesson.
- If the source is missing, not pending, or not an allowed evidence type, show a
  warning code.
- Always show a next command that starts with
  `worktrail candidates diff <candidate-id>`.

`worktrail review --evidence` remains the explicit path for raw
`transcript_notes`. `worktrail review --all` remains the explicit path for
operational candidates such as `handoff`.

## Warning Codes

Initial source warning codes:

- `source_missing:<id>`
- `source_not_pending:<id>`
- `source_not_evidence:<id>`
- `source_candidate_ids_empty`

Existing target and duplicate warnings remain available:

- `target_exists`
- `same_target_pending:N`
- `merge_target_missing`
- `replace_target_exists`

Warnings are informational. They do not block `promote`, `merge`, or `discard`;
the human confirmation step remains the boundary.

## Warning Code Boundary

`worktrail review` uses human-facing warning codes. They are intentionally
compact and may include candidate ids, for example `source_missing:<id>`.

`worktrail review plan` uses agent-facing `reason_codes` and structured
`source_statuses`. Those reason codes should stay stable and should not require
agents to parse ids out of strings.

Mapping for v1:

| `worktrail review` warning | `worktrail review plan` representation |
| --- | --- |
| `source_missing:<id>` | `source_missing` plus `source_statuses[].candidate_id` |
| `source_not_pending:<id>` | `source_not_pending` plus `source_statuses[].candidate_id` |
| `source_not_evidence:<id>` | `source_type_unexpected` plus `source_statuses[].candidate_id` |
| `source_candidate_ids_empty` | `source_candidate_ids_empty` reason code |
| `target_exists` | `target_exists` warning and reason code when relevant |
| `same_target_pending:N` | `same_target_pending` reason code, with count in warning text or plan item metadata when available |
| `merge_target_missing` | `merge_target_missing` warning and reason code |
| `replace_target_exists` | `replace_target_exists` warning and reason code |

New review warnings should either map to an existing plan reason code or add a
new reason code in [review-plan-automation.md](review-plan-automation.md) before
they become part of the stable agent contract.

## Split-Source Detection

A pending `lesson` source counts as KDD split-source evidence when any of these
conditions hold:

- `target_path == "lessons/kdd-active-knowledge-log.md"`
- tags include `split-source`
- summary or body contains `Do not promote directly`

Split-source candidates should not be presented as normal formal knowledge
targets in default review output.

## Acceptance Criteria

- Pending semantic candidates with `source_candidate_ids` display those ids in
  `worktrail review`.
- Missing, non-pending, or unexpected source candidates display source warning
  codes.
- Default review does not list raw transcript evidence or `handoff`
  candidates.
- `worktrail review --evidence` and `worktrail review --all` behavior remains
  compatible.
- The command does not mutate candidates or formal knowledge.
