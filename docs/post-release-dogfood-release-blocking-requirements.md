# Post-Release Dogfood Release-Blocking Requirements

Last updated: 2026-05-25

Status: draft

## Summary

Delivery-experts dogfood analysis found that several Worktrail paths are safe in
principle but fail in normal long-session use. Hook-generated state can become
generic event metadata, compact checkpoints can miss the actual task context,
transcript import is not discoverable at cold start, and agents bypass the formal
distill / candidate lifecycle when the workflow feels heavier than writing
formal knowledge directly. The same dogfood pass also showed that Worktrail has
no first-class detector for formal knowledge files that were changed outside the
candidate / review / promote chain.

This document turns the release-blocking findings into numbered post-release
requirements. It intentionally records only aggregate observations and command
behavior. It does not commit raw transcript bodies, local paths, private session
ids, or user-identifying details.

## Goals

- Make automatically generated state and checkpoints useful enough for session
  recovery.
- Make transcript import and distillation discoverable before agents hand-write
  formal knowledge.
- Detect formal knowledge edits that bypass the candidate / review / promote
  lifecycle.
- Fix CLI and sandbox blockers that repeatedly derail Worktrail workflows.
- Preserve Worktrail's explicit review boundary: no automatic promote, merge,
  discard, archive, restore, retire, delete, or replace.

## Non-Goals

- Do not add a daemon, watcher, TUI, Web UI, dashboard, vector search, or
  background transcript scanner.
- Do not store raw transcript bodies in committed project documentation.
- Do not make hooks promote or merge formal knowledge.
- Do not make Worktrail judge semantic correctness beyond structural and safety
  checks.

## Requirements

#### REQ-POST-001: Hook State Must Capture Real Task Context

Priority: MUST

Problem:

Cursor stop and compact hooks can currently overwrite the active state with a
generic event capsule such as "Worktrail stop". That makes `latest.md` look
current while failing to tell the next agent what the user was actually trying to
do, what changed, or what remains open.

Acceptance:

- Hook-generated state must include a task title derived from a real user task
  signal when one is available, such as an explicit payload task field or a
  bounded transcript summary.
- If no meaningful task signal is available, the hook must not overwrite
  `state/active/latest.md` with a generic event-only state.
- Hook-generated state must clearly separate durable task facts from runtime
  telemetry such as token counts, generation ids, model names, or event names.
- The state body must include useful sections for current goal, work done,
  validation status, open questions, and next step, even when some sections are
  explicitly marked unknown.
- Tests must cover both a meaningful payload and an event-only payload.

Evidence:

- 2026-05-25 delivery-experts Cursor dogfood analysis found oversized sessions
  where no useful handoff was created.
- Direct inspection of generated state showed active state being overwritten by
  hook event metadata instead of task context.

#### REQ-POST-002: Pre-Compact Checkpoints Must Support Recovery

Priority: MUST

Problem:

Pre-compact checkpoints currently preserve runtime compact metadata but can miss
the task narrative needed to resume work. In long sessions, this forces users to
write manual continuation prompts or causes agents to repeat prior design work.

Acceptance:

- A pre-compact checkpoint must include a bounded recovery summary when
  transcript context is available.
- The recovery summary must identify the active goal, recent user decisions,
  work completed since the previous checkpoint, validation already run, and the
  next safe action.
- The checkpoint must cap captured text by size and avoid storing full
  transcript bodies.
- If transcript context is unavailable, the checkpoint must state that recovery
  context was unavailable and must not imply that it contains a usable handoff.
- Tests must cover a compact payload with transcript context, a compact payload
  without transcript context, and redaction of runtime-only fields from the
  recovery narrative.

Evidence:

- 2026-05-25 delivery-experts Cursor dogfood analysis found continue-style
  sessions where the user had to provide manual recovery context.
- 2026-05-25 Codex dogfood analysis found repeated design discussion across
  sessions when no state checkpoint protected the accepted design context.

#### REQ-POST-003: Context Must Surface Unimported Current-Project Transcripts

Priority: MUST

Problem:

Relevant Codex and Cursor sessions can exist for the current project without
being imported into Worktrail evidence. In that state, `worktrail context` can
load formal knowledge while hiding the fact that recent high-signal transcripts
have not entered the reviewable lifecycle.

Acceptance:

- `worktrail context` must report a concise maintenance hint when current-project
  transcript evidence appears importable but is not yet represented in
  Worktrail candidates or raw observed metadata.
- The hint must be scoped to the current project and must suggest an explicit
  import command rather than importing automatically.
- The hint must distinguish raw transcript discovery from pending semantic
  candidates so users understand that import is evidence intake, not promotion.
- The feature must support a bounded time window or equivalent scope control so
  users are not pushed toward importing an unbounded transcript history.
- Tests must cover no importable sessions, importable sessions, and already
  imported sessions.

Evidence:

- 2026-05-25 delivery-experts Codex dogfood analysis found many project-related
  Codex sessions, while the Worktrail project evidence path was not populated
  for normal user sessions.
- Existing maintenance hints already surface pending evidence and review work,
  so importable-but-missing evidence belongs in the same discovery surface.

#### REQ-POST-004: Provide A Low-Friction Knowledge Capture Path

Priority: MUST

Problem:

Agents repeatedly bypass the formal distill / candidate workflow and write
formal Worktrail knowledge directly. This preserves some content but loses the
intended review boundary, source traceability, and evidence lifecycle.

Acceptance:

- Worktrail must provide a short command or skill path for capturing a validated
  dogfood finding as a pending candidate without requiring the user to hand-write
  proposal JSON.
- The shortcut must create pending candidates only. It must not promote, merge,
  discard, archive, restore, retire, delete, or replace formal knowledge.
- The shortcut must require the agent to identify the candidate type, target
  path, summary, confidence, and source evidence label.
- The output must tell the user the next review command and must avoid printing
  private transcript bodies by default.
- Documentation and installed skills must tell agents to use this shortcut when
  a user says to "write this into Worktrail", "落知识库", or equivalent wording.
- Tests must cover candidate creation, duplicate target handling, missing fields,
  and the no-auto-promote boundary.

Evidence:

- 2026-05-25 delivery-experts Codex dogfood analysis found the distill/candidate
  workflow absent from normal user sessions, with agents writing formal
  `.worktrail` documents directly instead.
- The existing low-intervention workflow reduces review friction after evidence
  exists, but normal capture still has too much up-front ceremony.

#### REQ-POST-005: Subcommand Help Must Be Consistent

Priority: MUST

Problem:

`worktrail state --help` can fail as an unknown subcommand. Agents then spend
context and shell attempts reverse-engineering command behavior instead of using
the documented workflow.

Acceptance:

- Every public top-level command and nested subcommand must accept `--help` and
  `help` consistently.
- Help requests must exit with code 0 and print usage text.
- Unknown subcommands must still exit non-zero with an actionable error.
- A CLI smoke test must cover at least `context`, `state`, `state start`,
  `state update`, `state checkpoint`, `state inject`, `handoff`, `import`,
  `review`, `distill`, and `evidence`.

Evidence:

- 2026-05-25 delivery-experts Codex dogfood analysis found repeated failures
  around `worktrail state --help`, which caused agents to inspect binaries or
  source instead of following normal help output.

#### REQ-POST-006: Handoff Must Diagnose Sandbox Write Boundaries

Priority: MUST

Problem:

`worktrail handoff` can fail in sandboxed agent environments when the configured
write boundary does not include the handoff target. The failure pushes users back
to manual continuation prompts, which is exactly the workflow Worktrail should
replace.

Acceptance:

- When `worktrail handoff` cannot write because of permissions or sandbox
  boundaries, the error must identify the intended Worktrail target directory and
  the likely configuration action.
- `worktrail doctor codex --project` and equivalent applicable doctor checks
  must detect common sandbox write-boundary mismatches where possible.
- Installed agent guidance must explain which project Worktrail directories need
  write access for state, checkpoints, handoffs, candidates, and logs.
- Handoff must remain explicit. The fix must not introduce automatic background
  handoff writes or automatic promotion of handoff candidates.
- Tests must cover successful handoff writing, permission-denied diagnostics,
  and doctor output for a simulated unwritable target.

Evidence:

- 2026-05-25 delivery-experts Codex dogfood analysis found handoff attempts
  failing under sandbox write boundaries.
- 2026-05-25 delivery-experts Cursor dogfood analysis found long sessions where
  useful handoff did not happen and users had to carry context manually.

#### REQ-POST-007: Detect Formal Knowledge Write Escape

Priority: MUST

Problem:

Agents can read Worktrail context correctly and still mutate formal knowledge
files directly with editor tools or patches. This bypasses the intended
candidate / review / promote lifecycle and leaves no source candidate, review
decision, or promotion event for later audit.

Acceptance:

- `worktrail doctor knowledge` must report formal knowledge files that appear to
  have no matching candidate create, promote, merge, restore, or retire trail.
- The detection must include formal knowledge directories such as
  `architecture`, `decisions`, `requirements`, `workflows`, `validation`,
  `integrations`, `glossary`, `rules`, `project.md`, and `index.md`.
- The report must distinguish initial imported / promoted knowledge from later
  direct edits when git metadata or Worktrail event data makes that distinction
  available.
- The report must flag untracked or modified formal knowledge files in the
  current worktree as likely write-escape candidates.
- The report must recommend a recovery path, such as creating a pending
  candidate from the current formal file, using the low-friction knowledge
  capture shortcut, or explicitly retiring the file when deletion was intended.
- The detector must be read-only. It must not create candidates, edit formal
  knowledge, promote, merge, discard, restore, or retire without a separate
  explicit command and confirmation.
- Tests must cover a fully promoted file, a direct formal edit, an untracked
  formal file, a direct deletion when detectable, and a non-formal runtime file
  that should not be reported.

Evidence:

- 2026-05-25 delivery-experts provenance analysis found a mixed knowledge base:
  roughly half of the formal knowledge came through KDD import and promote, while
  many later requirements, architecture, rules, validation, and project index
  updates had no matching create / promote / merge event.
- 2026-05-25 Codex transcript analysis found repeated `apply_patch` mutations to
  `.worktrail/architecture` and `project.md` after `worktrail context` was used,
  with no `candidates`, `review`, `promote`, `merge`, or `distill` write chain.
- 2026-05-25 Cursor transcript analysis found direct `StrReplace` edits to
  formal `.worktrail` paths and no use of promote / merge / distill / review for
  those document updates.

## Acceptance

This requirement set is ready for implementation planning when:

- Each `REQ-POST-*` item is either accepted, revised, or explicitly rejected.
- Any implementation plan preserves the no-daemon, no-Web-UI, no-vector-store,
  no-automatic-promotion boundaries.
- Validation includes both CLI unit tests and at least one dogfood smoke pass on
  a project with recent Cursor or Codex sessions.
- The backlog links to this document as the active release-blocking post-release
  requirement set.
