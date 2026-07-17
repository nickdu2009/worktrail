# Worktrail Backlog

Last updated: 2026-07-15

This backlog tracks follow-up work after the KDD compatibility import,
agent-assisted distillation dogfood pass, and review/evidence lifecycle
automation work.

Each pending item links to the current requirements document that defines scope,
non-goals, behavior, and acceptance criteria.

## Pending

Pending items are ordered by dependency and release priority.

- P1: Prepare the `v1.0.0` release package with the approved opt-in local
  semantic-recall scope expansion. Preserve the lexical baseline while adding
  the pinned BGE-M3 Q8_0/llama.app runtime gate, rebuild-only sqlite-vec
  generations, hybrid retrieval, explicit degradation, and Context Pack
  knowledge-only integration. Complete the Proposed-to-Accepted ADR gate,
  physically validate the verified M1 variant, keep M2-M5 experimental behind
  their per-chip pinned-artifact and installation-time self-check gates, add
  release notes or `CHANGELOG.md`, rerun the expanded release gate, and only
  then tag `v1.0.0`.
  Requirements:
  [worktrail-release-acceptance.md](worktrail-release-acceptance.md) and
  [worktrail-long-term-vision-discussion.md](worktrail-long-term-vision-discussion.md).
  Architecture:
  [worktrail-local-semantic-recall-architecture.md](worktrail-local-semantic-recall-architecture.md).

## Completed

- Fix post-release dogfood blockers before the next release candidate.
  Implementation status: implemented.
  Requirements:
  [post-release-dogfood-feedback.md](post-release-dogfood-feedback.md) and
  [post-release-dogfood-release-blocking-requirements.md](post-release-dogfood-release-blocking-requirements.md).
  Design:
  [knowledge-maintenance-proposal-workflow.md](knowledge-maintenance-proposal-workflow.md).
  Plan:
  [post-release-dogfood-p0-implementation-plan.md](post-release-dogfood-p0-implementation-plan.md).
  Validation:
  [post-release-dogfood-p0-validation-2026-05-26.md](post-release-dogfood-p0-validation-2026-05-26.md).
- Define and implement release acceptance, scope behavior, maintenance UX,
  knowledge quality, apply-plan safety, and dogfood feedback requirements.
  Implementation status: implemented.
  Requirements:
  [worktrail-release-acceptance.md](worktrail-release-acceptance.md).
  Validation:
  [worktrail-release-validation-2026-05-15.md](worktrail-release-validation-2026-05-15.md)
  and
  [worktrail-release-validation-checklist.md](worktrail-release-validation-checklist.md).
- Reduce user intervention in knowledge distillation by adding context
  maintenance hints, a distillation skill, batch review guidance,
  `review apply-plan --confirm`, and a maintenance workflow.
  Implementation status: implemented.
  Requirements:
  [low-intervention-knowledge-workflow.md](low-intervention-knowledge-workflow.md).
  Validation:
  [low-intervention-workflow-validation-2026-05-15.md](low-intervention-workflow-validation-2026-05-15.md)
  and
  [low-intervention-maintenance-validation-2026-05-15.md](low-intervention-maintenance-validation-2026-05-15.md).
- Implement read-only `worktrail review plan` as an agent-readable review
  contract for pending semantic candidates.
  Implementation status: implemented.
  Requirements: [review-plan-automation.md](review-plan-automation.md).
- Improve `worktrail distill apply` text output so partial success, skipped
  items, blocked items, and warning codes are easier to read without requiring
  `--format json`.
  Implementation status: implemented.
  Requirements:
  [distill-apply-text-output.md](distill-apply-text-output.md).
- Add proposal schema examples and fixtures for
  `worktrail.distill.proposal.v1`, covering valid proposals, item errors,
  blocked items, skipped duplicates, and split-source usage.
  Implementation status: implemented.
  Requirements:
  [distill-proposal-fixtures.md](distill-proposal-fixtures.md).
- Add a dedicated command and workflow to clean up or archive
  `transcript_notes` evidence after it has been distilled and reviewed.
  Implementation status: implemented.
  Requirements: [evidence-lifecycle.md](evidence-lifecycle.md).
- Promote the KDD dogfood findings into formal validation records, including
  fixture-based validation and real transcript evidence validation.
  Implementation status: implemented.
  Requirements:
  [distill-review-dogfood-validation.md](distill-review-dogfood-validation.md).
- Make `source_candidate_ids` more visible in `worktrail review` for semantic
  candidates, so reviewers can trace distilled knowledge back to its evidence.
  Implementation status: implemented.
  Requirements:
  [review-source-traceability.md](review-source-traceability.md).
