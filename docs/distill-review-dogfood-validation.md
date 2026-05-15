# Distill and Review Dogfood Validation Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail should keep a formal validation record for the real dogfood paths that
exercise transcript evidence, KDD split-source evidence, distill proposals,
review warnings, and eventual promote or merge behavior.

This document defines what should be validated and recorded. Actual acceptance
results should be written as dated validation records after each dogfood pass.

## Goals

- Capture real end-to-end evidence for the distill and review workflow.
- Validate that KDD compatibility flows into Worktrail without a second
  knowledge root.
- Validate that transcript evidence becomes semantic pending candidates through
  proposals.
- Confirm that review aids and review plan automation stay read-only.
- Make dogfood results reusable for future regression checks.

## Non-Goals

- Do not use private project content as committed fixture data.
- Do not require every developer to have the same external repository checkout.
- Do not promote validation artifacts as durable project knowledge unless they
  pass normal review.
- Do not replace unit tests or fixture tests.

## Validation Scenarios

### Transcript Evidence Distillation

Steps:

1. Import or create a small pending `transcript_notes` candidate.
2. Generate a `worktrail.distill.proposal.v1` proposal from that evidence.
3. Run `worktrail distill validate <proposal.json>`.
4. Run `worktrail distill apply <proposal.json>`.
5. Confirm pending semantic candidates reference the transcript source through
   `source_candidate_ids`.
6. Run `worktrail review` and confirm source traceability is visible.

Expected result:

- Source evidence remains pending.
- Semantic candidates are pending.
- No formal knowledge is changed until explicit promote or merge.

### KDD Split-Source Distillation

Steps:

1. Run `worktrail import kdd --all` in a temporary or disposable acceptance
   workspace.
2. Confirm the active-log candidate is marked with `kdd` and `split-source`
   tags or otherwise matches split-source detection.
3. Generate a proposal from the split-source lesson.
4. Run validate and apply.
5. Confirm semantic candidates reference the split source.
6. Confirm the split-source candidate itself is not recommended for direct
   promotion.

Expected result:

- KDD active log acts as evidence only.
- Derived semantic candidates are reviewable through normal Worktrail flow.
- Legacy KDD root is not used as a long-term source of truth.

### Review Aid and Review Plan

Steps:

1. Create or reuse pending semantic candidates for clean promote, clean merge,
   target conflict, missing source, duplicate body, empty body, and split-source
   deferral.
2. Run `worktrail review`.
3. Run `worktrail review plan --format json` after that command exists.
4. Compare text and JSON recommendations against expected actions.

Expected result:

- `worktrail review` remains a human review aid.
- `worktrail review plan` is the agent-readable contract.
- Both commands are read-only.
- Recommendations are conservative and deterministic.

### Promote and Merge Smoke

Steps:

1. Select one clean `replace` semantic candidate.
2. Run `worktrail candidates diff <id>`.
3. After explicit confirmation in the dogfood record, run
   `worktrail promote <id>`.
4. Select one clean `merge` semantic candidate.
5. Run `worktrail candidates diff <id>`.
6. After explicit confirmation, run `worktrail merge <id>`.
7. Run `worktrail context <task>` and confirm promoted or merged knowledge is
   loaded.

Expected result:

- Applied candidates leave the pending review list.
- Formal knowledge is loaded by context.
- Evidence candidates remain pending or are handled by a separate evidence
  lifecycle workflow.

## Validation Record Template

Each dated validation record should include:

- date
- repository or fixture description
- Worktrail commit or version
- commands run
- command exit codes or pass/fail result
- expected result
- actual result
- candidate counts before and after
- created, skipped, blocked, and warning counts
- selected candidate ids
- whether formal knowledge changed
- cleanup performed
- known gaps

Do not commit private transcript bodies or local absolute machine paths.

## Validation Records

- [2026-05-15 fixture-based validation](distill-review-dogfood-validation-2026-05-15.md)

## Acceptance Criteria

- A real transcript evidence dogfood pass has a dated validation record.
- A real KDD split-source dogfood pass has a dated validation record.
- Review source traceability is validated before it is treated as complete.
- Review plan automation has fixture validation and at least one dogfood
  validation record before release.
- Validation artifacts that are only acceptance output are cleaned up or clearly
  marked as disposable.
