# Worktrail Release Acceptance Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail now has the core low-intervention knowledge workflow: context
maintenance hints, agent distillation guidance, batch review planning,
confirmed `review apply-plan`, evidence lifecycle planning, and a maintenance
skill.

The next requirements should decide what "release-ready" means before adding
more features. The main risk is no longer missing capability; it is inconsistent
scope behavior, unclear confirmation boundaries, low-quality distilled
knowledge, or insufficient dogfood evidence.

## Goals

- Define release acceptance gates for the current CLI and skill workflow.
- Make `user` and `project` scope behavior predictable across commands, JSON
  contracts, text output, and skills.
- Keep `/worktrail-maintain` useful without making users babysit every low-risk
  read-only step.
- Define quality standards for semantic knowledge produced from transcript
  evidence.
- Strengthen `review apply-plan` safety and auditability before broader use.
- Turn real dogfood feedback into requirements instead of ad hoc fixes.

## Non-Goals

- Do not add a daemon, watcher, scheduler, TUI, Web UI, HTTP API, vector store,
  or embedded LLM provider.
- Do not make Worktrail automatically promote, merge, discard, archive, retire,
  or apply plans without explicit confirmation.
- Do not require private transcripts, local absolute paths, or runtime proposal
  files to be committed as validation artifacts.
- Do not replace agent-authored semantic judgment with Worktrail-internal
  semantic scoring.

## Current Baseline

The release candidate baseline includes these user-facing contracts:

- `worktrail.context_pack.v1`
- `worktrail.distill.proposal.v1`
- `worktrail.review.plan.v1`
- `worktrail.review.apply_plan.report.v1`
- `worktrail.evidence.plan.v1`
- `worktrail.doctor.migration.v1`
- `worktrail.cli.error.v1` for direct write-path CLI preflight failures in
  `--format json` (`note add`, `draft create`, candidate lifecycle writes,
  evidence archive/discard, distill proposal load/schema failures, review
  apply-plan/apply-candidates preflight failures)

The release candidate baseline includes the KDD migration path:

- `worktrail migrate kdd` is the public entry point for migrating legacy KDD
  knowledge into pending Worktrail candidates.
- `worktrail doctor migration` is the acceptance check for migration hygiene.
- `worktrail import kdd` is not a public migration entry point.
- Legacy KDD roots must not remain after migration cleanup.
- `docs/knowledge-driven-development/local/**` migrates only to user-scope
  pending candidates.
- `active-knowledge-log.md` migrates as `migration_source` evidence and must be
  distilled before any formal knowledge is promoted or merged.

Installed agent skills include:

- `/worktrail-context`
- `/worktrail-import`
- `/worktrail-distill`
- `/worktrail-review`
- `/worktrail-maintain`

The intended maintenance flow is:

```text
context maintenance hints
-> distill evidence into semantic candidates
-> review plan groups recommendations
-> optional apply-plan after confirmation
-> evidence lifecycle plan after review
-> context reflects formal knowledge
```

## Requirement Language

The following words are used as requirements language:

- **MUST**: required for release acceptance.
- **SHOULD**: expected for release acceptance unless a documented trade-off is
  accepted.
- **MAY**: optional behavior that can be implemented when it does not weaken
  safety, compatibility, or operability.

Every MUST requirement should have either a test, a fixture, a CLI smoke, or a
dated dogfood record before the release gate is considered complete.

The Detailed Requirement Catalog is authoritative for implementation and
validation. Earlier functional requirement sections summarize intent and should
be updated when the catalog changes, but implementation plans should trace work
to the numbered requirement ids.

## Functional Requirements

### Release Acceptance

Worktrail should be considered release-ready only when the primary workflows are
validated end to end:

- Fresh install can initialize a repository and install Codex, Claude Code, and
  Cursor integrations.
- `doctor` can detect installed user skills and project configuration.
- `context` can load user and project knowledge without leaking transcript body
  by default.
- `distill` can summarize pending evidence, write a pack, validate a proposal,
  and apply it into pending semantic candidates.
- `review plan` can produce stable recommendations for pending semantic
  candidates.
- `review apply-plan --confirm` can apply a fresh plan, skip human-review items,
  and reject stale items.
- `evidence plan` can summarize evidence lifecycle actions without modifying
  candidates.
- `/worktrail-maintain` can guide a read-only maintenance pass and preserve
  scope in follow-up commands.

Agent integration capability matrix:

- Codex supports install/doctor/uninstall, skills, hooks, MCP configuration,
  current-project `import codex` discovery, and explicit transcript
  `sync`/`extract`.
- Claude Code supports install/doctor/uninstall, skills, hooks/settings, and
  explicit transcript `sync claude <file>` / `extract --source claude`; it does
  not yet support automatic `worktrail import claude` discovery.
- Cursor supports install/doctor/uninstall, MCP, rules, hooks, Cursor-visible
  Worktrail skills, observed transcript metadata, and `import cursor` from
  explicit `--file` paths or Worktrail-observed registry entries. It does not
  scan undocumented private Cursor directories.

Acceptance evidence has two layers:

- Reproducible release gate evidence: synthetic or fixture-based validation from
  a clean checkout, independent of private local state.
- Supplemental dogfood evidence: at least one real local maintenance pass with
  private content redacted from the committed record.

### Scope Model

Worktrail must make scope behavior explicit enough that users and agents do not
have to infer it.

Requirements:

- Every command that defaults to `project` but can operate on `user` should
  document that default in help text or workflow docs.
- Any aggregate view that counts across both `user` and `project` should emit
  scope-aware next steps.
- JSON contracts that include counts should either include scope-specific counts
  or scope-specific commands.
- Agent skills should follow commands from Worktrail-generated contracts instead
  of recreating scope assumptions.
- Text output should prefer concrete commands over prose when a scope matters.

Scope defaults should remain backward compatible unless a separate migration
requirement is accepted.

### Maintenance UX

`/worktrail-maintain` should reduce user intervention by doing read-only
discovery automatically, then asking for confirmation only when state would
change.

Requirements:

- The skill should run the read-only chain first: `context "maintenance"`,
  `distill --pending --summary`, `review plan --format json`, and
  `evidence plan --format json`, using scoped variants from `maintenance`.
- The agent should summarize counts, recommended action groups, blockers, and
  exact confirmation choices.
- The user should be able to choose a lane: distill proposal, review apply-plan,
  single candidate command, evidence archive/discard, or no action.
- The skill should not ask the user to confirm read-only commands.
- The skill should never paste transcript bodies, local absolute paths, session
  ids, or temporary pack/proposal paths into durable documentation.
- The skill should not commit git changes automatically.

### Knowledge Quality

Distilled semantic candidates should be useful formal knowledge, not transcript
summaries.

Requirements:

- Candidate bodies should state durable rules, decisions, workflows,
  validations, glossary entries, project facts, prompts, or lessons.
- Candidate summaries should explain the durable value, not merely describe the
  transcript.
- `target_path` should be stable, scoped to the candidate type, and not include
  local machine details.
- `source_candidate_ids` should be present for distilled semantic candidates.
- Redaction status must be visible in review and plan outputs.
- Duplicate or empty semantic candidates should be recommended conservatively.
- `migration_source` and legacy KDD split-source candidates should remain
  evidence-like and should not be promoted, merged, or discarded directly
  through review automation.

### Apply-Plan Safety

`worktrail review apply-plan` should remain a convenience wrapper around explicit
review decisions, not a hidden automation path.

Requirements:

- The command must require `--confirm`.
- The command must reject unsupported schemas.
- The command must verify candidate status, operation, target path, body hash,
  metadata hash, target existence, and source candidate ids before applying.
- The command must execute only `promote`, `merge`, and `discard`.
- The command must skip `needs_human_review`.
- The command must emit a stable report with `applied`, `skipped`, `stale`, and
  `failed` outcomes.
- The command must not archive or discard evidence.
- The command should be safe to re-run against an old plan: previously changed
  candidates should become `stale`, not be applied again.

Future requirements may add an explicit dry-run mode or richer audit log, but
those are not required for the current release gate unless dogfood exposes a
blocking need.

### Dogfood Feedback

Real usage should drive the next iteration.

Requirements:

- Each dogfood record should list commands run, whether they were read-only or
  mutating, and whether formal knowledge changed.
- Records should include counts and result categories, not transcript bodies.
- Scope surprises should become scope model requirements.
- Confirmation friction should become maintenance UX requirements.
- Low-quality distilled candidates should become skill/workflow review examples,
  dogfood findings, docs examples, or validation records. Worktrail CLI
  validation should not become a semantic quality judge.

## Detailed Requirement Catalog

### Release Gate Requirements

#### REQ-REL-001: Clean Checkout Validation

Priority: MUST

The reproducible release gate MUST run from a clean checkout without relying on
private transcripts or existing local Worktrail state.

Acceptance:

- A release validation record names the checkout commit.
- The record includes `go test ./...`, `git diff --check`, `go build
  ./cmd/worktrail`, and `go install ./cmd/worktrail`.
- The record states whether the working tree was clean before and after
  validation.

#### REQ-REL-002: Isolated Install And Doctor

Priority: MUST

Codex and Claude install/doctor validation MUST run in an isolated environment
with disposable `HOME`, `WORKTRAIL_HOME`, and `WORKTRAIL_PROJECT_ROOT` values.

Acceptance:

- `worktrail install codex` creates the expected user skills.
- `worktrail doctor codex` reports the expected installed assets.
- `worktrail install claude` creates the expected user skills and project
  configuration when requested.
- `worktrail doctor claude` reports the expected installed assets.
- No installed test artifact is committed.

#### REQ-REL-003: End-To-End Knowledge Lifecycle Smoke

Priority: MUST

Release validation MUST exercise the full local lifecycle with synthetic or
redacted data:

```text
init -> create/import evidence -> distill proposal validate/apply
-> review plan -> apply-plan or explicit candidate action
-> evidence plan -> context
```

Acceptance:

- Evidence starts as `transcript_notes`.
- Distillation creates pending semantic candidates with `source_candidate_ids`.
- Review plan produces `worktrail.review.plan.v1`.
- Apply-plan or explicit actions mutate only after confirmation.
- Evidence remains available for traceability until evidence lifecycle commands
  are explicitly confirmed.

#### REQ-REL-004: Validation Records

Priority: MUST

Each release gate pass MUST produce a dated validation record.

Acceptance:

- The record lists commands, result categories, and whether each command was
  read-only or mutating.
- The record includes candidate counts before and after any mutating step.
- The record includes known gaps and release blockers.
- The record excludes transcript body, session ids, local usernames, local
  absolute paths, and temporary proposal/pack file contents.

#### REQ-REL-005: Supplemental Real Dogfood

Priority: MUST

At least one supplemental real local dogfood pass MUST be recorded before release
acceptance, but it is not required to run from a clean checkout and may depend on
the developer's local Worktrail state.

Acceptance:

- The dogfood record identifies that it is supplemental, not the reproducible
  release gate.
- The record redacts private content and records only counts, result categories,
  and safe ids when needed.
- Any finding from dogfood is triaged as release-blocking, post-release, or
  non-blocking polish.

### Scope Model Requirements

#### REQ-SCOPE-001: Command Scope Defaults

Priority: MUST

Every scope-aware command MUST have a documented default scope.

Scope defaults:

- `worktrail context`: MAY aggregate `user` and `project` knowledge.
- `worktrail candidates *`: defaults to `project` unless `--scope user` is
  supplied.
- `worktrail distill`: defaults to `project` unless `--scope user` is supplied.
- `worktrail review plan`: defaults to `project` unless `--scope user` is
  supplied.
- `worktrail review apply-plan`: defaults to the plan scope unless `--scope` is
  supplied.
- `worktrail evidence plan`: defaults to `project` unless `--scope user` is
  supplied.
- `worktrail evidence archive/discard`: defaults to `project` unless `--scope
  user` is supplied.

Acceptance:

- Help text or README docs state the default for each public command listed
  above.
- Tests cover at least one user-scope path and one project-scope path for
  maintenance hints.

#### REQ-SCOPE-002: Aggregate Output Must Emit Scoped Next Steps

Priority: MUST

Any output that aggregates counts across multiple scopes MUST provide
scope-specific next steps.

Acceptance:

- If pending evidence exists only in user scope, `context` recommends
  `worktrail distill --pending --summary --scope user`.
- If evidence lifecycle actions exist only in user scope, `context` recommends
  `worktrail evidence plan --format json --scope user`.
- If work exists in both scopes, `context` lists both project and user commands.

#### REQ-SCOPE-003: JSON Scope Contract

Priority: SHOULD

JSON contracts that include aggregate maintenance counts SHOULD include enough
information for agents to preserve scope.

Acceptance:

- `maintenance.next_steps` includes concrete scoped commands.
- Future additions may include scope-specific count objects, but existing
  top-level JSON fields must remain backward compatible.
- Agent skills must treat Worktrail-generated commands as authoritative.

#### REQ-SCOPE-004: Scope Mismatch Errors

Priority: MUST

When a command is run against a scope with no matching work but another scope has
matching work, Worktrail MUST provide a helpful next step instead of a dead-end
message.

Acceptance:

- `distill --pending --summary` run in project scope suggests `--scope user`
  when user-scope pending evidence exists and project-scope evidence does not.
- `evidence plan` run in project scope suggests `--scope user` when user-scope
  evidence lifecycle candidates exist and project-scope evidence lifecycle
  candidates do not.
- Existing machine-readable JSON contracts should remain stable; text hints may
  add next-step guidance.
- Tests or CLI smoke cover at least one scope mismatch path.

### Maintenance UX Requirements

#### REQ-MAINT-001: Read-Only Discovery First

Priority: MUST

`/worktrail-maintain` MUST run read-only discovery before asking the user to
choose a mutating lane.

Required discovery sources:

- `worktrail context "maintenance"`
- `worktrail distill --pending --summary` or scoped equivalent
- `worktrail review plan --format json` or scoped equivalent
- `worktrail evidence plan --format json` or scoped equivalent

Acceptance:

- The skill template instructs agents not to ask for confirmation before
  read-only discovery.
- The skill template instructs agents to preserve scope from
  `maintenance.next_steps`.

#### REQ-MAINT-002: Maintenance Summary

Priority: MUST

The maintenance summary MUST be concise and action-oriented.

The summary should include:

- pending evidence count by scope
- pending semantic review count by recommended action
- evidence lifecycle count by recommended action
- blockers or human-review items
- exact commands that may be run after confirmation

The summary must not include:

- transcript body
- local absolute paths
- temporary proposal or pack paths
- session ids
- user names from local paths

Acceptance:

- A dogfood record includes a redacted example or description of the summary.
- The skill template describes the allowed and forbidden content.

#### REQ-MAINT-003: Confirmation Lanes

Priority: MUST

The user MUST be able to choose among these lanes:

- distill proposal workflow
- review apply-plan workflow
- single candidate command
- evidence archive/discard
- no action

Acceptance:

- The skill template lists these lanes.
- Mutating commands are not run until the user names the lane and the relevant
  candidate id, evidence id, or plan file.

#### REQ-MAINT-004: No Git Automation

Priority: MUST

`/worktrail-maintain` MUST NOT create git commits, amend commits, push branches,
or delete git-tracked files automatically.

Acceptance:

- The skill template states this.
- Any validation record that includes git activity must identify it as a
  separate user-requested action, not part of maintenance automation.

#### REQ-MAINT-005: Low-Noise Behavior

Priority: SHOULD

When no pending maintenance exists, Worktrail and the skill SHOULD report a
short no-op summary rather than a long checklist.

Acceptance:

- Zero-count `context` output omits the maintenance section.
- `/worktrail-maintain` reports no-op lanes without suggesting unnecessary
  commands.

### Knowledge Quality Requirements

#### REQ-KQ-001: Semantic Candidate Body Quality

Priority: MUST

Distilled candidate bodies MUST be reusable knowledge, not chat summaries.

Acceptable body types:

- rule: normative guidance that can be followed later
- decision: selected approach plus rationale
- workflow: repeatable steps
- validation: acceptance evidence or test procedure
- glossary: terminology definition
- lesson: durable learning with future applicability
- project: stable project fact
- prompt: reusable AI instruction
- architecture or integration: stable system design or external integration
  knowledge

Unacceptable body patterns:

- "In this conversation we..."
- raw transcript excerpts
- local path-heavy debugging notes
- temporary task status with no future value
- secrets or unredacted sensitive content

Acceptance:

- Distillation skill text explicitly distinguishes semantic knowledge from
  transcript summary.
- Docs examples or validation records include at least one high-quality
  candidate and one low-quality pattern rejected by the agent/user-review
  workflow.
- If fixture files are added for this requirement, they validate workflow
  documentation or examples only; they must not imply CLI semantic rejection.
- `worktrail distill validate` must not reject items solely because the CLI
  infers low semantic quality.

#### REQ-KQ-002: Source Traceability

Priority: MUST

Distilled semantic candidates MUST preserve `source_candidate_ids` unless they
are manually created without evidence.

Acceptance:

- `review` shows source candidate ids or `source_candidate_ids_empty` warnings.
- `review plan` includes `source_candidate_ids` and `source_statuses`.
- Candidate quality review treats missing sources as a reason for human review.

#### REQ-KQ-003: Target Path Quality

Priority: SHOULD

Candidate target paths SHOULD be stable, type-appropriate, and privacy-safe.

Acceptance:

- Target paths do not include transcript ids, local usernames, temporary
  directory names, or absolute paths.
- Candidate type and target directory align when a clear convention exists.
- Duplicate target paths are surfaced by review warnings or reason codes.

#### REQ-KQ-004: Redaction Visibility

Priority: MUST

Redaction status MUST stay visible through distill, review, apply-plan, and
evidence lifecycle outputs.

Acceptance:

- `review` and `review plan` include candidate and source redaction status.
- Blocked or unreviewed redaction status prevents automatic promote/merge
  recommendation.
- Dogfood records mention redaction result categories without including body
  content.

#### REQ-KQ-005: Duplicate And Empty Candidate Handling

Priority: MUST

Duplicate and empty semantic candidates MUST be handled conservatively.

Acceptance:

- Empty semantic candidates are recommended for discard.
- Newer duplicate candidates with the same target and body are recommended for
  discard.
- Older duplicates and same-target conflicts remain human-reviewable unless the
  existing deterministic rule safely handles them.

### Apply-Plan Safety Requirements

#### REQ-APPLY-001: Confirmation Required

Priority: MUST

`worktrail review apply-plan` MUST refuse to run without `--confirm`.

Acceptance:

- CLI smoke captures the non-zero exit or returned error.
- Error text names `--confirm`.

#### REQ-APPLY-002: Schema Validation

Priority: MUST

The command MUST accept only `worktrail.review.plan.v1` input.

Acceptance:

- Unsupported schema returns an error.
- Malformed JSON returns an error.
- The command does not mutate candidates before schema validation succeeds.

#### REQ-APPLY-002A: Scope Consistency

Priority: MUST

`worktrail review apply-plan` MUST reject explicit `--scope` values that do not
match the plan's `scope`.

Rationale:

Review plan snapshots are generated from one scope's candidate set. Applying the
same plan against another scope is harder to reason about than a stale candidate
and risks confusing audit records. Users should generate a fresh plan in the
intended scope instead.

Acceptance:

- Applying a project-scope plan with `--scope user` returns an error before
  mutating candidates.
- Applying a user-scope plan with `--scope project` returns an error before
  mutating candidates.
- Omitting `--scope` continues to use `plan.scope`, preserving existing intended
  behavior.

#### REQ-APPLY-003: Snapshot Validation

Priority: MUST

The command MUST treat a plan item as stale when any of these fields changed:

- candidate status
- candidate operation
- candidate target path
- candidate redaction status
- candidate body hash
- candidate metadata hash
- target existence
- source candidate ids hash

Acceptance:

- Focused tests cover status, operation, target path, source ids, body hash, and
  metadata hash.
- At least one smoke validates that reusing an already-applied plan reports a
  stale item.

#### REQ-APPLY-004: Action Boundary

Priority: MUST

The command MUST execute only:

- `promote`
- `merge`
- `discard`

The command MUST skip:

- `needs_human_review`
- unknown or unsupported recommended actions

The command MUST NOT:

- archive evidence
- evidence-discard evidence
- restore candidates
- retire candidates
- create candidates
- delete files directly

Acceptance:

- Report includes skipped `needs_human_review` items.
- Tests assert no state-changing command is generated for human-review items.

#### REQ-APPLY-005: Partial Report Contract

Priority: MUST

Apply-plan output MUST remain useful when only some items apply.

Report requirements:

- Text output groups `Applied`, `Skipped`, `Stale`, and `Failed`.
- JSON output uses `worktrail.review.apply_plan.report.v1`.
- Summary includes total, applied, skipped, stale, and failed counts.
- Each item includes candidate id, planned action, result, target path when
  known, reason codes when relevant, and error text when failed.

Acceptance:

- Focused tests validate a partial report.
- The report is safe to paste into a validation record because it does not
  include candidate body.

### Dogfood Feedback Requirements

#### REQ-DOG-001: Dogfood Record Structure

Priority: MUST

Dogfood records MUST be structured enough to produce follow-up requirements.

Required fields:

- date
- repository or fixture description
- Worktrail commit or version
- commands run
- read-only vs mutating classification
- expected result
- actual result
- candidate counts before and after mutating actions
- warnings, skipped items, stale items, blocked items, and errors
- formal knowledge changed: yes/no
- cleanup performed
- known gaps

#### REQ-DOG-002: Privacy Boundary

Priority: MUST

Dogfood records MUST NOT include private transcript bodies, local absolute
paths, session ids, usernames, or temporary proposal/pack file contents.

Acceptance:

- Records use counts, ids only when safe, and result categories.
- Any real transcript validation states that private content was not committed.

#### REQ-DOG-003: Feedback Triage

Priority: SHOULD

Dogfood findings SHOULD be classified into one of:

- scope model issue
- maintenance UX issue
- knowledge quality issue
- apply-plan safety issue
- validation coverage gap
- non-blocking polish

Acceptance:

- Blocking findings become backlog items or requirement updates.
- Non-blocking polish is recorded separately from release blockers.

## Interface Contracts

### `maintenance` Object

The `worktrail.context_pack.v1` JSON contract includes:

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

Contract requirements:

- `next_steps` contains executable Worktrail commands.
- Scope-specific work should be represented in `next_steps` with explicit
  `--scope`.
- Existing top-level context fields remain backward compatible.

### Review Apply-Plan Report

The `worktrail.review.apply_plan.report.v1` JSON contract includes:

```json
{
  "schema": "worktrail.review.apply_plan.report.v1",
  "plan_schema": "worktrail.review.plan.v1",
  "scope": "project",
  "summary": {
    "total": 0,
    "applied": 0,
    "skipped": 0,
    "stale": 0,
    "failed": 0
  },
  "items": []
}
```

Contract requirements:

- `items[].result` is one of `applied`, `skipped`, `stale`, or `failed`.
- `items[].planned_action` echoes the plan recommendation.
- `items[].reason_codes` explains skipped or stale items.
- `items[].error` is present only for failed items.

## Validation Matrix

| Area | Required Evidence | Minimum Validation |
| --- | --- | --- |
| Release gate | clean checkout validation record | `go test ./...`, `git diff --check`, build, install |
| Install/doctor | isolated Codex and Claude install | integration tests plus CLI smoke |
| Scope model | user/project scoped commands | contextpack tests, scope mismatch suggestion smoke, real maintenance smoke |
| Maintenance UX | skill workflow text | integration tests and dogfood record |
| Knowledge quality | docs examples or validation records | skill/docs examples plus dogfood review notes |
| Apply-plan safety | stale, partial report, and scope consistency behavior | focused tests plus isolated CLI smoke |
| Dogfood feedback | dated records | privacy-safe validation docs |

## Non-Functional Requirements

### Safety

- Mutating commands require explicit confirmation.
- Read-only commands must not modify candidate status or formal knowledge.
- Runtime proposal packs, temporary plans, transcript bodies, and local absolute
  paths must not be committed.

### Compatibility

- Existing JSON schemas should remain backward compatible.
- Additive fields are allowed when consumers can ignore them.
- Scope-aware command hints should not remove existing default project behavior.

### Operability

- Release validation should be runnable from a clean checkout.
- Validation should avoid depending on private transcripts.
- When real dogfood uses private local data, committed records should include
  only redacted counts and outcomes.

## Acceptance Criteria

Release acceptance is complete when all MUST requirements in the detailed
catalog are either validated or explicitly listed as release blockers.

- `go test ./...` passes.
- `git diff --check` passes.
- `go build ./cmd/worktrail` passes.
- `go install ./cmd/worktrail` passes.
- `worktrail install codex` and `worktrail doctor codex` pass in an isolated
  environment.
- `worktrail install claude` and `worktrail doctor claude` pass in an isolated
  environment.
- `worktrail context "maintenance"` emits scope-aware maintenance next steps
  when pending work is outside the default project scope.
- `worktrail distill --pending --summary` and scoped variants behave consistently
  with the maintenance hints.
- Scope mismatch paths suggest the matching scope when another scope has pending
  matching work.
- `worktrail review plan --format json` produces `worktrail.review.plan.v1`.
- `worktrail review apply-plan <plan.json> --confirm` produces
  `worktrail.review.apply_plan.report.v1` and rejects stale snapshots.
- `worktrail review apply-plan <plan.json> --confirm --scope <scope>` rejects
  explicit scope values that do not match the plan scope.
- `worktrail evidence plan --format json` produces `worktrail.evidence.plan.v1`.
- `/worktrail-maintain` can complete a read-only pass without transcript body
  leakage.
- At least one dated validation record covers the release acceptance workflow.
- The validation record maps results back to requirement ids where practical.

## Suggested Delivery Phases

### Phase 1: Release Gate Definition

- Convert these requirements into a validation checklist.
- Add or update validation docs for install, doctor, context, distill, review,
  apply-plan, evidence, and maintain.
- Decide whether the release gate is project-only, user-only, or both.
- Cover `REQ-REL-001` through `REQ-REL-005`.

### Phase 2: Scope Contract Hardening

- Audit commands and skills for implicit scope assumptions.
- Update help text and JSON/text outputs where scope ambiguity remains.
- Add tests for user-scope and project-scope maintenance paths.
- Implement `REQ-SCOPE-004`: scope mismatch guidance for `distill --pending
  --summary` and `evidence plan` when another scope has matching work.
- Add CLI smoke or focused tests for the scope mismatch guidance.
- Cover `REQ-SCOPE-001` through `REQ-SCOPE-004`.

### Phase 3: Apply-Plan Scope Safety

- Implement `REQ-APPLY-002A`: reject explicit `--scope` values that do not match
  the saved review plan scope.
- Add focused tests for project-plan-with-user-scope and
  user-plan-with-project-scope rejection.
- Re-run stale snapshot and partial report validation after the change.

### Phase 4: Maintenance Dogfood

- Run `/worktrail-maintain` on real daily work.
- Record friction, false positives, scope surprises, and candidate quality
  issues.
- Promote confirmed findings into backlog items or examples.
- Cover `REQ-MAINT-*`, `REQ-KQ-*`, and `REQ-DOG-*`.

### Phase 5: Release Decision

- Run the full acceptance checklist from a clean checkout.
- Record final known gaps and release blockers.
- Decide whether remaining issues block release or become post-release backlog.
- Confirm `REQ-APPLY-*`, including `REQ-APPLY-002A`, remains satisfied after any
  scope or UX changes.
