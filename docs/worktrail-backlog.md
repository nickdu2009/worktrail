# Worktrail Backlog

Last updated: 2026-05-15

This backlog tracks follow-up work after the KDD compatibility import,
agent-assisted distillation dogfood pass, and review/evidence lifecycle
automation work.

Each pending item links to the current requirements document that defines scope,
non-goals, behavior, and acceptance criteria.

## Pending

- Run post-release dogfood passes and convert repeated feedback into numbered
  follow-up requirements before the next implementation round.
  Requirements:
  [post-release-dogfood-feedback.md](post-release-dogfood-feedback.md).

## Completed

- Define and implement release acceptance, scope behavior, maintenance UX,
  knowledge quality, apply-plan safety, and dogfood feedback requirements.
  Requirements:
  [worktrail-release-acceptance.md](worktrail-release-acceptance.md).
  Validation:
  [worktrail-release-validation-2026-05-15.md](worktrail-release-validation-2026-05-15.md)
  and
  [worktrail-release-validation-checklist.md](worktrail-release-validation-checklist.md).
- Reduce user intervention in knowledge distillation by adding context
  maintenance hints, a distillation skill, batch review guidance,
  `review apply-plan --confirm`, and a maintenance workflow.
  Requirements:
  [low-intervention-knowledge-workflow.md](low-intervention-knowledge-workflow.md).
  Validation:
  [low-intervention-workflow-validation-2026-05-15.md](low-intervention-workflow-validation-2026-05-15.md)
  and
  [low-intervention-maintenance-validation-2026-05-15.md](low-intervention-maintenance-validation-2026-05-15.md).
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
- Add a dedicated command and workflow to clean up or archive
  `transcript_notes` evidence after it has been distilled and reviewed.
  Requirements: [evidence-lifecycle.md](evidence-lifecycle.md).
- Promote the KDD dogfood findings into formal validation records, including
  fixture-based validation and real transcript evidence validation.
  Requirements:
  [distill-review-dogfood-validation.md](distill-review-dogfood-validation.md).
- Make `source_candidate_ids` more visible in `worktrail review` for semantic
  candidates, so reviewers can trace distilled knowledge back to its evidence.
  Requirements:
  [review-source-traceability.md](review-source-traceability.md).
