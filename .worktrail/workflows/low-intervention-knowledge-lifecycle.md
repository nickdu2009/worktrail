---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "low-intervention-knowledge-lifecycle",
  "scope": "project",
  "type": "workflow",
  "title": "Low-Intervention Knowledge Lifecycle",
  "status": "active",
  "lifecycle": "current",
  "topic": "knowledge-governance"
}
---

# Low-Intervention Knowledge Lifecycle

## Purpose

Make routine knowledge maintenance discoverable and safe without embedding an
LLM in Worktrail or allowing agents to mutate governed knowledge directly.
Worktrail supplies deterministic discovery, validation, lifecycle operations,
and audit records; the agent supplies semantic drafting; the user approves each
governed mutation.

## Core lifecycle

```text
source evidence
  -> read-only discovery
  -> agent-authored semantic proposal
  -> CLI validation
  -> explicit user confirmation
  -> CLI proposal apply creates pending semantic candidates
  -> read-only review plan
  -> explicit promote, merge, or discard decision
  -> formal knowledge and evidence lifecycle follow-up
```

## 1. Discover current work

1. Run the scope-appropriate context or maintenance discovery command.
2. Run `worktrail distill --pending --summary` for transcript evidence. Add
   `--split-sources` only when `migration_source` or KDD split-source evidence is
   intentionally in scope.
3. Run `worktrail review plan --scope <scope> --format json` for pending
   semantic candidates.
4. Run `worktrail evidence plan --scope <scope> --format json` for evidence
   lifecycle actions.
5. Summarize counts, blockers, and next safe commands without copying raw
   evidence bodies.

Discovery and plan commands are read-only. Empty queues are valid no-ops. The
scope reported by discovery remains authoritative for every follow-up command.

## 2. Distill evidence into pending knowledge

1. Inspect only the evidence needed for the task. Keep transcript and imported
   source bodies hidden from default summaries.
2. Create a temporary evidence pack with `worktrail distill --pending --all
   --write-pack <temporary-pack>` or the scope-aware, `--split-sources` variant.
3. Draft a `worktrail.distill.proposal.v1` proposal containing durable semantic
   knowledge and the applicable `source_candidate_ids`.
4. Run `worktrail distill validate <proposal.json>` and report ids, types,
   targets, operations, warnings, and errors without exposing evidence bodies.
5. Ask for confirmation that identifies the proposal and scope.
6. Run `worktrail distill apply <proposal.json>` with the validated scope only
   after that confirmation.
7. Remove agent-created temporary pack and proposal files unless the user
   explicitly requested a durable copy.
8. Run the same-scope review plan immediately after candidate creation.

When durable knowledge is distilled directly from repository documents rather
than Worktrail evidence, use `worktrail draft create` after explicit persistence
intent. Preserve human-readable migration provenance even though no
`source_candidate_ids` exist; the resulting candidate remains subject to human
review. After promotion succeeds and inbound references are updated, a
superseded duplicate source may be deleted only when the formal document records
its provenance and version history or a reviewed backup retains the original.

Temporary pack and proposal filenames must not expose transcript IDs, session
IDs, local usernames, absolute paths, or other sensitive runtime identifiers.
Their contents must not contain secrets or unnecessary personal data.

## 3. Review pending semantic candidates

1. Treat `worktrail.review.plan.v1` JSON as the agent-facing contract.
2. Inspect candidate type, operation, target, warnings, reason codes, source
   statuses, hashes, and recommended action.
3. Resolve source candidate identifiers only within the candidate scope.
4. Missing, non-pending, unexpected, cross-scope, or absent source candidates
   require human review; they must not be silently inferred.
5. A direct semantic draft derived from repository documents may have no source
   candidate identifiers. Its migration-provenance section supports human
   review, and the review plan should conservatively leave it for human judgment.
6. Build proposed commands from the review plan rather than inventing actions.
7. For a confirmed same-scope batch with one action, use `worktrail review
   apply-candidates --promote|--merge|--discard <id...> [--scope <scope>]`.
   Never mix actions or scopes, and never include `needs_human_review` items.
8. A saved plan may be applied only through `worktrail review apply-plan
   <plan.json> --confirm` after the user confirms that exact plan and scope.
9. Ask for confirmation that identifies the action, exact candidate ids, and
   scope before every state-changing review command.
10. Re-run the read-only review plan after a confirmed action.

`reason_codes` explain deterministic recommendations. Warnings remain advisory.
Neither should be reinterpreted as validation errors.

## 4. Preserve source traceability

- The evidence input set includes `transcript_notes`, `migration_source`, and KDD
  split-source records. Semantic candidates should carry
  `source_candidate_ids` when derived from any of them.
- Pending transcript evidence and allowed migration or split-source evidence may
  be valid distillation inputs, but raw evidence remains hidden from default
  semantic review.
- `migration_source` and KDD split-source records are evidence inputs, not formal
  knowledge targets. Never promote, merge, or discard them through the default
  semantic review lane.
- Candidate and source redaction states are separate. Candidate safety governs
  whether its body may become formal knowledge.
- Repository-document distillation must preserve human-readable migration
  provenance even when no candidate source identifier exists.

## 5. Handle evidence after semantic review

Use the current evidence plan instead of coupling cleanup to semantic review:

- `keep` evidence while a pending semantic candidate references it, when its
  redaction state needs review, or when it is already archived;
- `archive` evidence when applied semantic knowledge references it and active
  review visibility is no longer needed;
- `discard` only when the evidence plan recommends that action; currently this
  is limited to empty-body, unreferenced `transcript_notes`;
- leave unreferenced `migration_source` and KDD split-source evidence as
  `needs_human_review` unless a later plan defines another safe action;
- use `needs_human_review` for every uncertain case.

Archive and discard are explicit candidate lifecycle transitions. They preserve
candidate ids, metadata, source links, and audit history; they do not delete
evidence files or modify formal knowledge. Execute only the action recommended
by the current plan, using `worktrail evidence archive|discard <id> --confirm
--reason <text>`.

## 6. Maintain long-lived formal knowledge

1. Run `worktrail maintain knowledge --format json` for deterministic,
   read-only formal-knowledge discovery. This is distinct from the context,
   distill, review, and evidence inbox chain in Section 1.
2. Let the agent compare only the reported documents and draft an explicit
   `worktrail.knowledge.maintenance.proposal.v1` proposal.
3. Run `worktrail maintain validate <proposal.json>` and validate schema,
   sources, targets, safety, and destructive reasons.
4. Present the validated actions and affected paths to the user.
5. Wait for confirmation that identifies the proposal and scope.
6. Run `worktrail maintain apply <proposal.json> --confirm`.
7. Record audit events, rebuild derived indexes when required, and rerun the
   relevant read-only plans.

Deterministic scanning must not depend on model judgment. The agent must not edit
formal paths directly, and apply must not add implicit cleanup.

## Safety and compatibility rules

- No workflow automatically promotes, merges, discards, archives, restores,
  retires, prunes, commits, or publishes.
- No workflow promotes raw transcript evidence, `migration_source`, or KDD
  split-source evidence.
- Read-only plans never mutate candidate state or formal knowledge.
- Existing command and JSON contracts remain compatible unless a new schema
  version is introduced.
- Default context and review surfaces remain concise and hide evidence and
  operational records unless explicitly requested.
- Every failure should report a bounded next safe action. JSON clients inspect
  structured envelopes and codes rather than relying only on exit status.
- Git commits, remote operations, and destructive maintenance remain separate
  explicit user decisions.

## Completion checks

A maintenance pass is complete when:

1. discovery queues and blockers are summarized;
2. every created candidate is visible in the same-scope review plan;
3. no governed mutation occurred without exact user confirmation;
4. source traceability and evidence retention remain intact;
5. formal knowledge changed only through the approved CLI lifecycle;
6. follow-up read-only plans reflect the resulting state.

## Migration provenance

Distilled from source documents removed from the working tree after promotion.
Their original content remains recoverable from Git history:

- `docs/low-intervention-knowledge-workflow.md`
- `docs/evidence-lifecycle.md`
- `docs/review-plan-automation.md`
- `docs/review-source-traceability.md`
- `docs/knowledge-maintenance-proposal-workflow.md`
