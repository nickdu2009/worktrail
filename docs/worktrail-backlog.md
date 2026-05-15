# Worktrail Backlog

Last updated: 2026-05-15

This backlog tracks follow-up work after the KDD compatibility import and
agent-assisted distillation dogfood pass.

Each pending item links to the current requirements document that defines scope,
non-goals, behavior, and acceptance criteria.

## Pending

- Implement read-only `worktrail review plan` as an agent-readable review
  contract for pending semantic candidates.
  Requirements: [review-plan-automation.md](review-plan-automation.md).
- Improve `worktrail distill apply` text output so partial success, skipped
  items, blocked items, and warning codes are easier to read without requiring
  `--format json`.
  Requirements:
  [distill-apply-text-output.md](distill-apply-text-output.md).
- Add proposal schema examples and fixtures for
  `worktrail.distill.proposal.v1`, covering valid proposals, item errors,
  blocked items, skipped duplicates, and split-source usage.
  Requirements:
  [distill-proposal-fixtures.md](distill-proposal-fixtures.md).
- Add a dedicated command or documented workflow to clean up or archive
  `transcript_notes` evidence after it has been distilled and reviewed.
  Requirements: [evidence-lifecycle.md](evidence-lifecycle.md).
- Promote the KDD dogfood findings into a formal validation document, including
  what was tested with real transcript evidence and KDD split-source input.
  Requirements:
  [distill-review-dogfood-validation.md](distill-review-dogfood-validation.md).

## Local Progress

- Make `source_candidate_ids` more visible in `worktrail review` for semantic
  candidates, so reviewers can trace distilled knowledge back to its evidence.
  This is implemented in the current working tree and still needs commit-level
  validation before it should be treated as mainline done. The requirement is
  documented here, but the behavior should not be treated as a released
  mainline capability until the related implementation changes are validated and
  committed.
  Requirements:
  [review-source-traceability.md](review-source-traceability.md).
