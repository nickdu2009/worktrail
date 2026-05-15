# Evidence Lifecycle Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail needs an explicit lifecycle for `transcript_notes` evidence and KDD
split-source candidates after they have been distilled into semantic pending
candidates. Evidence should remain available for traceability while review is
active, then be cleaned up or archived only through an explicit workflow.

This requirement does not change the rule that formal knowledge can only be
created through review and explicit promote or merge.

## Goals

- Help agents identify evidence that has already been distilled.
- Separate evidence cleanup from semantic candidate review.
- Avoid promoting raw `transcript_notes` or KDD split-source lessons as formal
  knowledge.
- Provide a safe path to archive or discard obsolete evidence with explicit
  confirmation.
- Keep source traceability available until referenced semantic candidates are
  resolved.

## Non-Goals

- Do not automatically delete evidence after `distill apply`.
- Do not archive evidence while pending semantic candidates still reference it.
- Do not read legacy KDD roots as long-term sources.
- Do not make evidence cleanup part of `worktrail review plan` v1.
- Do not include operational candidates such as `handoff` in evidence lifecycle
  reports.

## Proposed Command

Future command:

```bash
worktrail evidence plan [--scope project|user] [--status active|archived|all] [--format text|json]
```

The command is read-only. It reports evidence candidates and recommended next
steps. It must not mutate candidates or formal knowledge.

`--status` controls which evidence candidates are included:

- `active` is the default. It includes pending evidence and any legacy applied
  evidence that still participates in active source traceability.
- `archived` includes only archived evidence.
- `all` means all v1 plan-visible evidence statuses. It includes active and
  archived evidence, and explicitly excludes discarded evidence.

Archived evidence is hidden unless `--status archived` or `--status all` is
provided.

Discarded evidence is hidden from `evidence plan` v1, including `--status all`.
A future historical or audit command may expose discarded evidence with a
separate explicit contract.

## Evidence Inputs

Evidence lifecycle v1 should inspect:

- pending `transcript_notes`
- applied `transcript_notes` if such records exist from older workflows
- pending KDD split-source `lesson` candidates
- archived `transcript_notes` when `--status archived` or `--status all` is
  provided
- archived KDD split-source `lesson` candidates when `--status archived` or
  `--status all` is provided

KDD split-source detection follows the same compatibility rules used by review
plan:

- `target_path == "lessons/kdd-active-knowledge-log.md"`
- or tags include `split-source`
- or summary/body contains `Do not promote directly`

In this lifecycle, an evidence candidate is either `transcript_notes` or a KDD
split-source `lesson`. KDD split-source lessons are evidence candidates even
though their candidate type is `lesson`; semantic knowledge candidates exclude
KDD split-source lessons for evidence archive and discard checks.

## Reference Analysis

For each evidence candidate, report:

- evidence id
- type
- status
- redaction status
- count of pending semantic candidates referencing it through
  `source_candidate_ids`
- count of applied semantic candidates referencing it through
  `source_candidate_ids`
- whether it is still needed for active review
- recommended evidence action

Recommended evidence actions:

- `keep`
- `archive`
- `discard`
- `needs_human_review`

## Recommendation Rules

Recommend `keep` when:

- at least one pending semantic candidate references the evidence
- or the evidence has blocked or uncertain redaction that needs manual review
- or the evidence is a KDD split source and its downstream semantic candidates
  are not resolved

Recommend `archive` when:

- no pending semantic candidate references the evidence
- at least one applied semantic candidate references it
- the evidence is not blocked
- retaining traceability without active review noise is useful

Recommend `discard` only when:

- no pending or applied semantic candidate references the evidence
- the evidence is not a KDD split source
- the evidence body is empty or duplicate evidence has been superseded

Recommend `needs_human_review` for all other uncertain cases.

## Archive State Contract

Evidence archive is a candidate lifecycle state, not a formal knowledge state.
The first implementation should add candidate status `archived` for evidence
candidates only.

Archive behavior:

- Keep the candidate file under the candidate store so source traceability can
  still resolve the id.
- Set candidate status to `archived`.
- Preserve candidate metadata, body hash, source ids, redaction status, and event
  log history.
- Add an event log entry containing the archive reason when provided.
- Hide archived evidence from default pending review, default context, and
  default evidence plan active-review sections.
- Include archived evidence only when a command explicitly asks for archived or
  historical evidence.

Archive must not:

- create, modify, promote, merge, or delete formal knowledge
- remove source traceability for applied semantic candidates
- operate on semantic knowledge candidates, except KDD split-source lessons that
  satisfy the evidence candidate predicate above
- operate on operational candidates such as `handoff`

Restore behavior is out of scope for v1. If a future restore command is added,
it must be explicit and must move archived evidence back to a non-applied
candidate status only after validating that doing so does not imply formal
knowledge changes.

## Discard State Contract

Evidence discard should reuse the existing candidate discard lifecycle instead
of deleting candidate files or inventing a separate evidence-only state.

Discard behavior:

- Keep the candidate file under the candidate store so audit history remains
  available.
- Set candidate status to `discarded`.
- Preserve candidate id, type, target path, source ids, redaction status, and
  enough metadata for future audit.
- Add an event log entry containing the discard reason when provided.
- Hide discarded evidence from default pending review, default context, and
  default evidence plan active-review sections.

Discard must not:

- delete the candidate file
- create, modify, promote, merge, or delete formal knowledge
- operate on evidence still referenced by pending semantic candidates
- operate on semantic knowledge candidates, except KDD split-source lessons that
  satisfy the evidence candidate predicate above
- operate on operational candidates such as `handoff`

Discarded evidence may be included only by future explicit historical or audit
commands. `worktrail evidence plan --status archived` is for archived evidence,
not discarded evidence; `--status all` also excludes discarded evidence in v1.

## Future Mutating Commands

Potential explicit commands:

```bash
worktrail evidence archive <candidate-id> --confirm
worktrail evidence discard <candidate-id> --confirm
```

Mutating commands must:

- require `--confirm`
- refuse to operate on semantic formal knowledge candidates
- validate that the evidence is not still required by pending semantic
  candidates unless a future force flag is explicitly designed
- write event log entries
- preserve enough metadata for traceability after archive or discard

## Text and JSON Output

JSON should be the agent contract. Text output should group evidence by
recommended action:

- Keep
- Archive
- Discard
- Needs human review

Each item should show evidence id, evidence type, redaction status, reference
counts, and the next safe command.

## Acceptance Criteria

- `worktrail evidence plan --format json` reports evidence candidates and
  reference counts without mutating state.
- `worktrail evidence plan --status archived --format json` includes archived
  evidence and excludes active evidence.
- `worktrail evidence plan --status all --format json` includes active and
  archived evidence.
- Evidence referenced by pending semantic candidates is never recommended for
  archive or discard.
- KDD split-source candidates are not promoted or discarded by default.
- Future archive or discard commands require explicit confirmation.
- Future archive commands set evidence candidate status to `archived` without
  deleting candidate files.
- Future discard commands set evidence candidate status to `discarded` without
  deleting candidate files.
- Existing `worktrail review`, `worktrail context`, and `worktrail distill`
  behavior remains compatible.
