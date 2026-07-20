---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "knowledge-boundaries-and-write-safety",
  "scope": "project",
  "type": "rule",
  "title": "Knowledge Boundaries and Write Safety",
  "status": "active",
  "lifecycle": "current",
  "topic": "knowledge-governance"
}
---

# Knowledge Boundaries and Write Safety

## Purpose

Keep reusable knowledge authoritative, reviewable, and distinct from pending,
runtime, evidence, and derived records. Agents may draft semantic text, but
Worktrail CLI operations own persistent lifecycle changes.

## Layer boundaries

- Formal knowledge is reusable Markdown under `decisions/`, `architecture/`,
  `requirements/`, `rules/`, `workflows/`, `validation/`, `integrations/`,
  `glossary/`, `lessons/`, and `prompts/`, plus `project.md` and `index.md`.
- Formal knowledge becomes authoritative only after explicit review and a
  confirmed CLI promote, merge, or other supported formal-write operation.
- Candidate records are staging and audit records, not formal knowledge. A
  pending item must have a concrete next action and an identifiable reviewer or
  workflow responsible for that action.
- Evidence may remain in the candidate store after it leaves the default inbox
  when lifecycle and source-traceability rules require retention.
- State, Handoff V2 local/team records, runtime records, logs, and raw imports
  support recovery, audit, or distillation. They are not formal reusable
  knowledge or default semantic review items.
- Index databases, caches, previews, and exports are derived or presentation
  artifacts. They must remain rebuildable and must not become a source of truth.
- Routine automation should prefer runtime and audit records. It must not create
  long-lived pending items merely to record that an event occurred.

## Write ownership

- The agent may read evidence, summarize it, and draft titles, summaries,
  candidate bodies, proposal bodies, reasons, review summaries, and temporary
  intermediate artifacts.
- Persistent candidate creation must go through a supported `worktrail` CLI
  command. The agent must not write candidate storage directly as the normal
  workflow.
- Formal knowledge creation and mutation, including promote, merge, restore,
  retire, review-apply, and supported maintenance actions, must go through
  Worktrail CLI operations. The agent must not directly edit formal knowledge
  paths as the normal workflow.
- Governed mutations require explicit user approval and the command-specific
  confirmation gate when the command defines one. This includes formal writes,
  evidence archive or discard, proposal apply, runtime pruning or repair, and
  handoff publishing.
- Candidate creation always produces pending, non-authoritative records. A
  standalone creation command follows its persistence-intent guardrail and does
  not imply a formal-write confirmation gate; creation inside an apply workflow
  follows that apply command's confirmation contract.
- A pending candidate alone must not change a source document's governance
  status. After the corresponding promote or merge succeeds and inbound
  references are updated, a matching source under `docs/` may either remain as
  design evidence with a formal-path backlink or be deleted as a superseded
  duplicate. A deleted source must remain recoverable from version history or a
  reviewed backup, and the formal document must retain migration provenance.
- Every persistent write must remain traceable to the CLI operation, candidate
  or proposal, and source evidence when source candidates exist.

## Hook boundary

- Hooks may write only bounded runtime, checkpoint, receipt, binding, terminal,
  validation, and reason-code audit effects through the designated Worktrail CLI
  path.
- Hooks must not modify explicit active state or create takeover, handoff,
  candidate, or formal knowledge records.
- Hooks must never promote, merge, discard, restore, retire, archive, prune, or
  publish knowledge or handoffs.
- Hook payloads and records must not persist complete prompts, thoughts,
  transcripts, raw tool input or output, secrets, or absolute transcript paths.
- CLI validation and the candidate/review lifecycle remain authoritative even
  when a host hook can guard an operation before execution.

## Validate and apply boundary

- Read-only discovery and validation may run without confirmation.
- Proposal-based workflows must separate drafting, validation, user review, and
  confirmed apply into distinct steps.
- Validation must check schema, scope, target-path ownership, and semantic text
  safety before apply.
- Apply must execute only actions represented in the reviewed input; it must not
  add implicit semantic cleanup or unrelated mutations.
- Failures in JSON mode use `worktrail.cli.error.v1`. Agents must inspect the
  response envelope, including `ok` and `error_codes`, rather than relying only
  on process exit status.

## Semantic text safety

- `error_codes` identify validation or execution failures.
- `reason_codes` explain read-only recommendations or apply outcomes and are not
  validation failures.
- `warning_codes` are non-blocking hints and must not be treated as errors.
- Titles, summaries, reasons, and bodies must reject blocked secrets,
  redactable secrets or personal data, local absolute paths, and raw
  transcript-style conversation where the field contract requires it.
- Field-specific checks follow the semantic text safety contract matrix; not
  every field runs the same transcript or redactable-text checks.
- Formal writes must repeat the applicable safety checks on the final content;
  validating an earlier draft is not sufficient when the body has changed.

## Automation safeguards

- Automation must not silently promote, merge, discard, retire, archive, delete,
  or replace formal knowledge.
- Cleanup must use explicit lifecycle transitions rather than silent file
  deletion.
- Default review and context surfaces should remain focused on semantic work;
  evidence and operational records require explicit views when appropriate.
- New automation must justify candidate creation when a runtime or audit record
  is sufficient.

## Review checks

A workflow complies with this rule when:

1. agent-authored semantic text enters through a supported CLI command;
2. pending, runtime, evidence, derived, and formal layers remain distinct;
3. validation and confirmed apply are separate;
4. state-changing operations are traceable and auditable;
5. hooks stay inside their runtime/audit boundary;
6. JSON failures and safety codes are interpreted according to their contract;
7. no direct formal edit or silent lifecycle mutation bypasses review.

## Migration provenance

Distilled from source documents removed from the working tree after promotion.
Their original content remains recoverable from Git history:

- `docs/knowledge-layering-and-candidate-boundaries.md`
- `docs/agent-cli-write-boundary.md`
- `docs/semantic-text-safety-contract.md`

Aligned with retained project knowledge:

- `docs/README.md`
- `.worktrail/architecture/cursor-codex-local-hooks.md`
- `.worktrail/workflows/cursor-codex-local-hooks-implementation.md`
