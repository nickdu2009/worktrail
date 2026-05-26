# Post-Release Dogfood P0 Implementation Plan

Last updated: 2026-05-25

Status: draft

## Summary

This plan implements the P0 post-release dogfood blockers before the next
release candidate. The work should be delivered as small, reviewable increments
that preserve Worktrail's core boundaries:

- no daemon, watcher, TUI, Web UI, vector store, or embedded LLM
- no automatic promote, merge, discard, archive, restore, retire, delete, or
  replace
- no raw transcript bodies in committed project documentation
- formal knowledge remains Markdown source of truth, but formal writes must be
  traceable through controlled Worktrail operations

## Implementation Order

### PR 1: CLI Help Consistency

Requirements:

- `REQ-POST-005`

Goal:

- Make all public subcommands consistently support `--help`, `-h`, and `help`
  without mutating state.

Scope:

- `internal/app/state.go`
- command help tests for all top-level and nested public commands

Design:

- Add `wantsHelp(args)` handling before `runState` requires a subcommand.
- Add a state help printer that lists `start`, `update`, `checkpoint`,
  `inject`, `close`, `archive`, `list`, and `show`.
- Add a table-driven CLI smoke test covering `context`, `state`, `state start`,
  `state update`, `state checkpoint`, `state inject`, `handoff`, `import`,
  `review`, `distill`, and `evidence`.

Acceptance:

- `worktrail state --help` exits 0 and prints usage.
- Unknown subcommands still fail with a non-zero exit.
- Existing state behavior is unchanged.

Validation:

```bash
go test ./internal/app -run 'Help|State'
go test ./...
```

### PR 2: Hook State And Compact Recovery Quality

Requirements:

- `REQ-POST-001`
- `REQ-POST-002`

Goal:

- Stop writing misleading generic state capsules and make compact checkpoints
  useful for recovery.

Scope:

- `internal/hooks/hooks.go`
- a bounded transcript tail extraction helper, kept local or in
  `internal/transcript`
- hook tests and fixtures

Design:

- Add a bounded transcript context reader that can extract:
  - first user task signal
  - recent user decisions
  - recent assistant summary / next step signal
  - last validation-looking command or statement when present
- Use transcript context to derive hook state title and body.
- If no meaningful task signal is available, do not overwrite
  `state/active/latest.md` with `Worktrail stop` / `Worktrail pre-compact`.
  The hook should still log the hook event.
- For pre-compact and post-compact events, write a checkpoint with an explicit
  `Recovery Summary` section when transcript context is available.
- If transcript context is unavailable, the checkpoint must say that recovery
  context was unavailable.
- Keep captured transcript text bounded by byte count and message count.
- Do not scan private Cursor directories from the hook. Only read transcript
  paths already provided by the hook payload or Worktrail observed metadata.
- Add fixtures for the transcript shapes used by Cursor hook payloads and Codex
  JSONL sessions when both are supported by the helper.

Acceptance:

- Event-only payloads no longer overwrite `latest.md` with generic state.
- Payloads with a usable task or transcript signal produce meaningful state.
- Compact checkpoints clearly distinguish usable recovery summaries from runtime
  payload-only checkpoints.
- No full transcript body is copied into state or checkpoints.

Validation:

```bash
go test ./internal/hooks ./internal/transcript
go test ./...
```

### PR 3: Context Import Discovery

Requirements:

- `REQ-POST-003`

Goal:

- Make `worktrail context` surface current-project Cursor / Codex sessions that
  are likely importable but not yet represented as Worktrail evidence.

Scope:

- `internal/contextpack`
- `internal/transcript`
- `internal/app/import.go` discovery helpers and bounded import flags

Design:

- Extend maintenance hints with an import-discovery section.
- Reuse existing transcript discovery and observed Cursor metadata where
  possible.
- Keep discovery scoped and bounded. P0 should add an explicit bound such as
  `--since <duration>` or `--limit N` to `worktrail import codex|cursor` before
  `context` suggests an import command for projects with many sessions.
- Report only counts and explicit next-step commands. Do not import
  automatically.
- Distinguish:
  - importable raw sessions
  - already observed Cursor sessions
  - pending transcript evidence candidates
  - pending semantic candidates

Acceptance:

- `worktrail context <task>` can tell the user when current-project transcript
  evidence appears importable.
- The hint includes an explicit bounded command such as
  `worktrail import codex --since 14d --all` or explains why only a dry-run
  command is safe.
- No transcript body is printed by default.

Validation:

```bash
go test ./internal/contextpack ./internal/transcript ./internal/app -run 'Context|Import'
go test ./...
```

### PR 4: Low-Friction Knowledge Capture

Requirements:

- `REQ-POST-004`

Goal:

- Give agents a short safe path for "write this into Worktrail" / "落知识库"
  without directly editing formal knowledge.

Scope:

- `internal/app/app.go`
- new `internal/app/note.go` or equivalent command handler
- `internal/app`
- `internal/candidate`
- installed skill templates, especially `templates/skills/worktrail-distill`,
  `templates/skills/worktrail-review`, `templates/skills/worktrail-maintain`,
  and root agent guidance that currently tells agents how to handle
  knowledge-writing requests
- README help text if command surface changes

Chosen command:

```bash
worktrail note add --type decision|architecture|requirement|workflow|validation|rule|glossary|project \
  --target <formal-path> \
  --title <title> \
  --summary <summary> \
  --evidence-label <label> \
  [--confidence 0.7] \
  [--from-file draft.md | body text...]
```

Design:

- `note add` creates pending semantic candidates only.
- It must never promote, merge, retire, discard, archive, or edit formal
  knowledge.
- It should be a convenience wrapper over the existing candidate manager and
  semantic candidate metadata.
- It should reject missing target, type, title, summary, or body.
- It should print the created candidate id and the next review command.
- Update installed skills so "落知识库" routes to this command instead of direct
  formal edits.

Acceptance:

- Agents can create a pending candidate with one command.
- Formal knowledge files are unchanged until review / promote / merge.
- Duplicate target handling remains explicit and reviewable.

Validation:

```bash
go test ./internal/app -run 'Note|Candidate'
go test ./internal/candidate
go test ./...
```

### PR 5: Handoff Permission Diagnostics

Requirements:

- `REQ-POST-006`

Goal:

- Make sandbox write-boundary failures actionable instead of pushing users back
  to manual continuation prompts.

Scope:

- `internal/app/drafts.go`
- `internal/app/integration.go`
- `internal/integrations`
- templates for Codex / Cursor project guidance

Design:

- Wrap handoff write failures with the intended target directory and the
  Worktrail directories that need write access.
- Add doctor checks where the project install target or known sandbox config can
  be inspected deterministically.
- When sandbox configuration is not available, doctor output should report an
  informational note rather than guessing or failing.
- Keep handoff explicit. Do not add automatic background handoff writes.

Acceptance:

- Permission denied during handoff reports the relevant `.worktrail` target.
- Doctor output gives the user a concrete configuration direction when the
  mismatch is detectable.
- Existing successful handoff behavior is unchanged.

Validation:

```bash
go test ./internal/app -run 'Handoff|Doctor'
go test ./...
```

### PR 6: Formal Knowledge Write-Escape Detection

Requirements:

- `REQ-POST-007`

Goal:

- Let `worktrail doctor knowledge` detect formal knowledge files that appear to
  have bypassed candidate / review / promote / merge traceability.

Scope:

- `internal/app/knowledge_doctor.go`
- `internal/log/events.go`
- `internal/candidate`
- optional git worktree inspection helper

Design:

- Add doctor findings for:
  - untracked formal knowledge files
  - modified formal knowledge files in the current worktree
  - formal knowledge paths with no matching candidate create / promote / merge /
    restore / retire trail
  - files promoted once but modified later when git metadata can prove it
- Keep the detector read-only.
- Use stable finding codes, for example:
  - `ESCAPE001` untracked formal knowledge
  - `ESCAPE002` modified formal knowledge
  - `ESCAPE003` missing promotion trail
  - `ESCAPE004` changed after promotion
- Recommend recovery commands, preferring `worktrail note add` or candidate
  creation before any promote / merge.
- When the project is not a git repository or git metadata is unavailable, keep
  candidate/event-log checks active and report that worktree-based checks were
  skipped.

Acceptance:

- `doctor knowledge` reports direct formal edits without changing files.
- Existing governance checks still run.
- Runtime directories such as `state`, `logs`, `raw`, `candidates`, and derived
  indexes are not reported as formal write escapes.

Validation:

```bash
go test ./internal/app -run 'KnowledgeDoctor|Escape'
go test ./...
```

### PR 7: Knowledge Maintenance Proposal Workflow

Requirements:

- `REQ-POST-004`
- `REQ-POST-007`
- `knowledge-maintenance-proposal-workflow.md`

Goal:

- Add the first controlled maintenance workflow:
  deterministic scan -> agent-authored proposal -> validation -> confirmed
  apply.

Scope:

- `internal/app/app.go`
- new `internal/app/maintain.go` or equivalent command handler
- new `worktrail maintain knowledge`
- new `worktrail maintain validate <proposal.json>`
- new `worktrail maintain apply <proposal.json> --confirm`
- proposal schema and tests

Design:

- `maintain knowledge --format json` is read-only and should compose existing
  `doctor knowledge`, `review plan`, and `evidence plan` signals where possible.
- Proposal schema:

```json
{
  "schema": "worktrail.knowledge.maintenance.proposal.v1",
  "actions": [
    {
      "action": "create_candidate|promote_candidate|merge_candidate|retire_missing_target|archive_evidence|rebuild_index",
      "source_paths": ["architecture/old.md"],
      "target_path": "architecture/current.md",
      "candidate_id": "optional-existing-candidate",
      "body": "optional markdown when creating a candidate",
      "reason": "why this action is safe"
    }
  ]
}
```

- P0 apply should support only action families that can be implemented through
  existing safe primitives:
  - create pending candidate
  - apply existing promote / merge by candidate id
  - retire an already missing formal target through the existing candidate-based
    retire primitive
  - archive evidence when `evidence plan` recommends archive
  - rebuild derived indexes
- Direct mutation of a present formal document to mark it retired is out of P0.
  When a proposal asks to retire a still-existing formal document, validation
  should reject it with a recovery hint to create a pending replacement or
  retirement candidate first.
- `update_index` should be represented as a candidate or rejected in P0. Direct
  index mutation should be deferred unless a later plan adds a narrowly scoped
  controlled operation with tests.
- Apply must re-read candidate and evidence state immediately before mutation and
  reject stale proposals when the referenced candidate status, target path,
  operation, or redaction status changed since validation.

Acceptance:

- The maintain scan runs without LLM and without writes.
- Agents can draft proposals without touching formal knowledge.
- Validation rejects unsafe paths, missing sources, raw transcript bodies,
  destructive actions without reasons, and unsupported action types.
- Apply requires `--confirm` and writes audit events for every mutation.

Validation:

```bash
go test ./internal/app -run 'Maintain|Knowledge'
go test ./...
```

### PR 8: Dogfood Smoke And Release Gate Update

Requirements:

- all `REQ-POST-*`

Goal:

- Prove the full P0 path on a real or fixture-backed project before release
  packaging resumes.

Scope:

- validation docs
- release checklist updates
- no new product behavior unless gaps are found

Dogfood scenario:

1. Run `worktrail context "maintenance"` in a project with recent Cursor or
   Codex sessions.
2. Verify import-discovery and maintenance hints.
3. Create a knowledge candidate with `worktrail note add`.
4. Run `doctor knowledge` and verify write-escape findings.
5. Run `maintain knowledge --format json`.
6. Draft and validate a small maintenance proposal that creates a pending
   candidate and archives eligible evidence.
7. Apply the proposal only after confirmation in a disposable fixture or test
   project.

Acceptance:

- P0 release-blocking requirements are either implemented or explicitly moved
  out of P0 with a rationale.
- The backlog can move P0 to completed only after this smoke pass is recorded.

Validation:

```bash
go test ./...
worktrail note add --help
worktrail context "maintenance"
worktrail doctor knowledge
worktrail evidence plan --format json
worktrail maintain knowledge --format json
worktrail maintain validate <proposal.json>
```

## Dependency Notes

- PR 1 can be done first and independently.
- PR 2 should precede dogfood smoke because it changes hook-generated state and
  compact recovery semantics.
- PR 3 should land before PR 8 so `context` can guide evidence intake.
- PR 4 should land before PR 6 and PR 7 because both need a safe recovery path
  for formal write escapes.
- PR 6 should land before PR 7 because `maintain knowledge` should reuse
  write-escape findings rather than reimplementing them.
- PR 8 is the final P0 gate before resuming `v1.0.0` release packaging.

## Out Of Scope For P0

- Embedded LLM, embeddings, vector search, or semantic similarity scoring.
- Background scan, daemon, watcher, or automatic cleanup.
- Enforcing filesystem-level write locks on `.worktrail`.
- Automatically promoting, merging, retiring, archiving, or deleting formal
  knowledge.
- Full semantic deduplication. P0 may surface candidates and validate proposals;
  semantic judgment remains with the agent and user.
- Direct retire-by-path for still-existing formal documents. P0 may reject such
  proposals with a recovery hint to create a pending candidate first.

## Completion Criteria

P0 is complete when:

- every `REQ-POST-001` through `REQ-POST-007` has implementation evidence or a
  documented deferral accepted by the user
- `go test ./...` passes
- the dogfood smoke pass is recorded
- `docs/worktrail-backlog.md` can mark the P0 item as implemented
- the `v1.0.0` release package work can resume without known release-blocking
  dogfood findings
