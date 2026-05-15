# Low-Intervention Knowledge Workflow Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail already supports a safe knowledge lifecycle: transcript evidence can
be imported, distilled into semantic pending candidates, reviewed through an
agent-readable plan, promoted or merged after explicit confirmation, and cleaned
up through evidence lifecycle commands.

The next product step is to make that workflow easier for everyday users. The
user should not need to remember every maintenance command or manually author
proposal JSON. The current AI coding agent should be able to discover pending
work, prepare proposals, summarize review decisions, and ask for explicit
confirmation only at the points where Worktrail mutates candidate state or
formal knowledge.

## Goals

- Make knowledge maintenance discoverable from normal `context` and review
  workflows.
- Reduce the need for users to hand-write distillation proposal JSON.
- Let agents summarize pending review decisions by recommended action.
- Keep source traceability visible without exposing raw transcript body by
  default.
- Keep explicit user confirmation as the boundary for promote, merge, discard,
  archive, and evidence discard.
- Provide a daily or weekly maintenance workflow that agents can run safely.

## Non-Goals

- Do not add a daemon, watcher, TUI, Web UI, or background scheduler.
- Do not add an LLM provider, embedding model, vector database, or semantic
  similarity service.
- Do not automatically promote, merge, discard, archive, or evidence-discard
  without explicit confirmation.
- Do not make Worktrail itself judge semantic quality. Semantic distillation is
  still authored by the current AI agent and reviewed by the user.
- Do not commit raw transcript bodies, local absolute paths, or runtime
  candidate artifacts as project documentation.

## Current Baseline

The current stable workflow is:

```text
import transcript -> transcript_notes evidence
distill proposal -> semantic pending candidates
review plan -> recommended actions
promote/merge/discard -> formal knowledge after confirmation
evidence plan/archive/discard -> evidence lifecycle
context -> load formal knowledge
```

Stable contracts already available:

- `worktrail.distill.proposal.v1`
- `worktrail.review.plan.v1`
- `worktrail.evidence.plan.v1`

## Functional Requirements

### Context Maintenance Hints

`worktrail context <task>` should continue to prioritize relevant knowledge, but
it should also expose compact maintenance hints when there is pending Worktrail
maintenance.

Text output should include concise hints such as:

```text
Pending evidence candidates: 3.
Next: run `worktrail distill --pending --summary` or ask the agent to use /worktrail-distill.
```

```text
Pending review candidates: 2.
Next: run `worktrail review plan --format json` or ask the agent to use /worktrail-review.
```

```text
Evidence lifecycle actions available.
Next: run `worktrail evidence plan --format json`.
```

JSON context output should include a stable `maintenance` object:

```json
{
  "maintenance": {
    "pending_evidence_candidates": 0,
    "pending_semantic_candidates": 0,
    "evidence_lifecycle_candidates": 0,
    "next_steps": []
  }
}
```

Requirements:

- Do not include transcript body in maintenance hints.
- Do not make the context pack noisy when counts are zero.
- Do not mutate candidate state or formal knowledge from `context`.
- Counts should respect the same default hiding rules used by review and
  evidence plan.
- The top-level JSON context contract should remain backward compatible. New
  fields should be added under `maintenance` rather than scattered across the
  top-level object.

### `/worktrail-distill` Skill

Add a new skill template:

```text
templates/skills/worktrail-distill/SKILL.md
```

This skill is an agent workflow, not a built-in Worktrail LLM feature. Worktrail
provides evidence packs, proposal schema validation, and apply/reporting. The
current AI coding agent is responsible for authoring the semantic proposal and
the user remains responsible for confirming apply.

Default agent workflow:

1. Run `worktrail distill --pending --summary`.
2. If useful evidence exists, run
   `worktrail distill --pending --all --write-pack <temporary-file>`.
3. Read the pack.
4. Draft a `worktrail.distill.proposal.v1` proposal.
5. Run `worktrail distill validate <proposal.json>`.
6. Summarize the proposal and validation result.
7. Wait for explicit user confirmation before applying.
8. Run `worktrail distill apply <proposal.json>`.
9. Run `worktrail review plan --format json`.

Safety requirements:

- Do not commit temporary proposal or pack files unless the user explicitly
  requests it.
- Do not copy private transcript body into project docs or final responses.
- Proposal bodies must be durable semantic knowledge, not transcript summaries.
- KDD split-source candidates may be used as evidence sources but must not be
  promoted directly.
- Temporary pack and proposal file names should not include transcript ids,
  session ids, local user names, or local absolute paths.
- Prefer a system temporary directory or an ignored workspace-local temporary
  file. If a workspace-local file is used, the skill must delete it after apply
  unless the user explicitly asks to keep it.
- When summarizing a proposal, show candidate ids, target paths, candidate
  types, operations, warning codes, and error codes. Do not paste transcript
  evidence bodies.

### `/worktrail-review` Batch Summary

The existing review skill should continue to default to:

```bash
worktrail review plan --format json
```

It should summarize the plan by `recommended_action`:

```text
Recommended promote: N
Recommended merge: N
Recommended discard: N
Needs human review: N
```

For items recommended as `promote`, `merge`, or `discard`, the agent may prepare
a candidate command list for user confirmation. Items recommended as
`needs_human_review` must not include state-changing commands.

Requirements:

- Each command list must start from the `commands` array in the review plan.
- The agent must explain source traceability and warnings before asking for
  confirmation.
- KDD split-source lessons must never be promoted, merged, or discarded by the
  review workflow.
- The agent must wait for explicit user confirmation before running any
  state-changing command.

### Optional Review Apply Plan

Future CLI command:

```bash
worktrail review apply-plan <plan.json> --confirm
```

Requirements:

- Refuse to run without `--confirm`.
- Validate schema `worktrail.review.plan.v1`.
- Validate candidate snapshots before applying actions.
- Treat the plan as stale when status, operation, target path, target existence,
  source ids, redaction status, metadata hash, or body hash no longer matches.
- Execute only `promote`, `merge`, and `discard` recommendations.
- Skip `needs_human_review`.
- Emit a partial report for created, applied, skipped, stale, and failed items.
- Do not perform evidence cleanup.

This command is the main path to reduce repetitive user confirmation after the
agent has already summarized a plan, but it should be implemented after the skill
workflow is proven.

### `/worktrail-maintain` Workflow

Add a maintenance skill or documented workflow for periodic use.

Default workflow:

1. Run `worktrail context "maintenance"`.
2. Run `worktrail distill --pending --summary`.
3. Run `worktrail review plan --format json`.
4. Run `worktrail evidence plan --format json`.
5. Summarize pending evidence, pending review items, and evidence lifecycle
   actions.
6. Ask the user which confirmed actions to run.

Non-goals:

- Do not automatically write formal knowledge.
- Do not automatically clean evidence.
- Do not automatically create git commits.

## Non-Functional Requirements

### Safety

- Confirmation remains mandatory for every command that changes candidate state
  or formal knowledge.
- Transcript evidence remains hidden by default.
- Temporary proposal and distill-pack files must be local working artifacts
  unless explicitly requested for commit.

### Compatibility

- Existing `worktrail review`, `worktrail review plan`,
  `worktrail distill validate/apply`, `worktrail evidence plan`, and
  `worktrail context` contracts must remain compatible.
- Existing JSON schemas must not be broken without a new schema version.

### Operability

- All maintenance hints and skill steps should be executable by Codex or Claude
  Code without special environment setup beyond installed Worktrail.
- Failure output should point to the next safe command, not to broad manual
  debugging.

## Acceptance Criteria

- `worktrail context <task>` reports maintenance counts and next steps without
  leaking transcript body.
- `worktrail context --format json <task>` includes
  `maintenance.pending_evidence_candidates`,
  `maintenance.pending_semantic_candidates`,
  `maintenance.evidence_lifecycle_candidates`, and `maintenance.next_steps`.
- `/worktrail-distill` can guide an agent through evidence summary, pack
  creation, proposal authoring, validation, apply, and review plan.
- `/worktrail-review` summarizes `worktrail.review.plan.v1` by recommended
  action and asks for explicit confirmation before any mutation.
- Evidence lifecycle recommendations remain available through
  `worktrail.evidence.plan.v1`.
- No workflow promotes or merges `transcript_notes` or KDD split-source
  candidates directly.
- `go test ./...` passes.
- `git diff --check` passes.
- Fixture-based validation covers context hints and skill workflows where
  practical.
- Real transcript dogfood validation is recorded without transcript body or
  local absolute paths.

## Suggested Delivery Phases

### Phase 1: Discoverability And Skills

- Add context maintenance hints.
- Add `/worktrail-distill` skill template.
- Update `/worktrail-review` with batch confirmation guidance.
- Add validation docs.

Phase 1 explicitly excludes `worktrail review apply-plan`. Batch execution from
a saved review plan belongs to Phase 2 after the skill-based workflow has been
validated.

Phase 1 validation boundary:

- CLI: unit or app tests cover context text hints, context JSON `maintenance`
  fields, and zero-count quiet behavior.
- Skills: installation tests verify the distill and review skill templates are
  installed with the expected workflow commands.
- Docs: fixture-based validation records the low-intervention workflow without
  transcript body or local absolute paths.

### Phase 2: Confirmed Review Apply Plan

- Implement `worktrail review apply-plan <plan.json> --confirm`.
- Validate stale snapshots.
- Emit partial reports.

### Phase 3: Maintenance Workflow

- Add `/worktrail-maintain` skill or workflow.
- Dogfood daily or weekly maintenance against real transcript evidence.
- Record validation results.
