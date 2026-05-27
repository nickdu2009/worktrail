# Knowledge Layering And Candidate Boundaries

Last updated: 2026-05-27

Status: implemented

## Summary

Worktrail currently uses `.worktrail/` for formal knowledge, pending candidates,
runtime state, audit logs, raw imports, indexes, and preview/export artifacts.
That layering is directionally correct, but the current `candidates/` store is
too permissive: it contains real semantic candidates and transcript evidence,
but also a large amount of operational stop/session-end drafts.

This draft defines the intended responsibilities of each layer under
`.worktrail/`, with a specific goal: keep the default pending candidate surface
as a short-lived staging area for items with a clear next action, rather than a
long-term sink for every automatically generated artifact.

## Problem

When `candidates/` mixes semantic drafts, evidence, handoff drafts, and generic
runtime traces, several problems follow:

- default review becomes noisy even when operational items are hidden
- maintenance counts stop reflecting actual knowledge work
- users cannot easily tell which pending items still need judgment
- hooks have no clear boundary between "capture runtime state" and "create a
  candidate"
- operational drafts accumulate faster than they are reviewed or cleaned up

The result is that pending candidate volume becomes normal background noise
instead of a meaningful inbox.

## Goals

- Define a directory-layer model for the `.worktrail/` knowledge root.
- Keep Markdown formal knowledge as the source of truth.
- Clarify when an item belongs in `candidates/` versus `handoffs/`, `state/`,
  `logs/`, `raw/`, or derived caches.
- Preserve the existing explicit-review boundary for formal knowledge changes.
- Make hook-generated operational artifacts predictable and easier to clean up.
- Stay compatible with evidence lifecycle rules that retain archived or
  discarded evidence in the candidate store for audit and traceability.

## Non-Goals

- Do not change the rule that formal knowledge is only created or updated
  through explicit Worktrail review flows such as `promote` or `merge`.
- Do not add a daemon, watcher, background cleanup job, or automatic semantic
  classifier.
- Do not require every handoff or operational draft to become formal knowledge.
- Do not remove evidence traceability after distillation.
- Do not redesign the preview site, search index, or export formats.

## Design Decision

This design separates two concepts that are currently easy to conflate:

- **candidate store:** the on-disk lifecycle store under `.worktrail/candidates/`
  that may retain pending, archived, discarded, or otherwise auditable
  candidate records
- **pending candidate inbox:** the default active-review surface shown by
  commands such as `review` and maintenance hints

The main correction in this draft is about the inbox, not about deleting all
non-pending files from the candidate store. Evidence lifecycle rules still
allow archived or discarded evidence to remain in the candidate store for
traceability. What should shrink is the set of operational items that remain
pending without any real next action.

## Alternatives Considered

### Alternative A: Keep all automatically generated handoff drafts as pending candidates

Rejected because it keeps the implementation simple for hooks but turns the
pending candidate pool into a runtime dump. Review and maintenance surfaces then
stop representing actual human decision work.

### Alternative B: Never allow operational candidates at all

Rejected because some handoff drafts do require explicit human judgment, active
takeover, or distillation into durable knowledge. Banning operational candidates
entirely would force those cases into ad hoc files or informal chat-only state.

### Chosen Direction

Allow operational candidates only when they still represent unresolved
follow-up work. Prefer runtime and audit layers for routine stop/session-end
capture. Keep archived or discarded evidence in the candidate store when needed
for audit, but keep the default pending inbox focused.

Implementation note:

- default `stop` / `session-end` hooks now write runtime state and logs first,
  and only create a pending `handoff` candidate when captured context suggests
  actual follow-up work
- default review and context surfaces hide evidence and operational candidates
  unless the caller explicitly asks for them
- preview still renders operational candidates in a separate bucket, but labels
  them as visible-for-inspection rather than part of the default inbox

## Layer Model

Worktrail should treat `.worktrail/` as four conceptual layers:

### 1. Formal Knowledge Layer

Examples:

- `decisions/`
- `handoffs/`
- future formal roots such as `requirements/`, `rules/`, `workflows/`,
  `prompts/`, `lessons/`, or `glossary/`

Properties:

- Markdown is the source of truth.
- Files are expected to be reusable across sessions.
- `context` may surface these files directly as durable project or user
  knowledge.
- Formal writes must remain gated by explicit review and confirmation.

### 2. Candidate Staging Layer

Examples:

- `candidates/` semantic pending candidates
- `candidates/` transcript evidence waiting for distillation
- a small number of operational drafts that still require a concrete human or
  agent follow-up step

Properties:

- The pending inbox within this layer is transitional, not archival.
- Every item should have a clear next safe action.
- Candidate state is part of governance, not formal knowledge.
- Candidate volume should roughly correspond to pending human judgment.
- Some non-pending candidate records may remain in the candidate store for audit
  and source traceability, especially for evidence lifecycle compatibility.

### 3. Runtime And Audit Layer

Examples:

- `state/`
- `logs/`
- `raw/`

Properties:

- These files capture execution context, audit history, and raw imported input.
- They may be useful for recovery, traceability, or debugging.
- They are not part of the formal reusable knowledge surface.
- Hooks should prefer this layer when recording routine session activity.

### 4. Derived Artifact Layer

Examples:

- `index/`
- `.cache/`
- `exports/`

Properties:

- These files are rebuildable or presentation-oriented.
- They must not become the source of truth.
- Their lifecycle may be shorter and more disposable than candidate or formal
  knowledge files.

## Directory Responsibilities

The intended meaning of key existing directories is:

| Path | Intended role | Should be source of truth? |
|---|---|---|
| `.worktrail/decisions/` | durable decision knowledge | yes |
| `.worktrail/handoffs/` | durable handoff knowledge worth reusing | yes |
| `.worktrail/candidates/` | pending semantic, evidence, or operational staging items with a clear next action | no |
| `.worktrail/state/` | short-lived recovery and progress snapshots | no |
| `.worktrail/logs/` | audit trail and lifecycle events | no |
| `.worktrail/raw/` | imported or extracted source material | no |
| `.worktrail/index/` | rebuildable search or metadata indexes | no |
| `.worktrail/.cache/` | preview and other temporary artifacts | no |
| `.worktrail/exports/` | generated exports for sharing or inspection | no |

## Candidate Admission Rule

An item belongs in `candidates/` only when the answer to both questions is
clear:

1. What is the next action on this item?
2. Who or what is expected to take that action?

Examples of valid next actions:

- distill this evidence into semantic candidates
- review this pending semantic candidate
- promote or merge this confirmed candidate
- archive or discard this evidence after explicit confirmation
- inspect this operational draft because it may need durable handoff treatment

If an item has no concrete next action beyond "record that this happened", it
should not be a long-lived candidate.

This rule applies most strongly to the pending inbox. Non-pending evidence may
remain in the candidate store for traceability when an explicit lifecycle rule
requires it.

## Placement Rules By Content Type

### Semantic Knowledge Drafts

Keep in `candidates/` when pending review, promote, or merge.

Promote or merge into formal roots such as `decisions/`, `requirements/`,
`rules/`, `workflows/`, or `handoffs/` only after explicit confirmation.

### Transcript Evidence

Keep in `candidates/` while it is still needed for distillation, review
traceability, or evidence lifecycle decisions.

After downstream semantic candidates are resolved, evidence may remain in the
candidate store with non-pending lifecycle states such as archived or discarded,
but it should not pollute the default pending view.

### Durable Handoffs

If a handoff is intended to be reused across sessions, agents, or machines, it
belongs in the formal handoff root such as `.worktrail/handoffs/`.

Durable handoffs should include:

- current situation
- validated facts
- open risks or unknowns
- recommended next step

### Operational Handoff Drafts

Operational drafts may enter `candidates/` briefly when they still need a
decision such as:

- discard as noise
- rewrite into a durable handoff
- distill into semantic knowledge
- keep temporarily for active takeover

They should not remain pending indefinitely once that decision is no longer
active.

### Routine Stop And Session-End Artifacts

Routine stop/session-end capture should default to runtime and audit storage
first:

- snapshot current work in `state/`
- record lifecycle and provenance in `logs/`
- optionally preserve raw source in `raw/` when needed

Only create a candidate when the generated artifact appears to require a real
follow-up, such as explicit takeover, semantic distillation, or durable handoff
promotion.

### Preview, Export, And Index Outputs

Preview pages, exports, and indexes should never rely on candidate status to
justify their existence. They are derived outputs and belong in the derived
artifact layer.

Preview may still show operational candidates in a clearly labeled bucket for
inspection, but that visibility must not redefine what counts as the default
pending inbox for `review`, `context`, or maintenance hints.

## Operational Candidate Policy

Operational candidates such as `handoff` are valid in Worktrail, but they should
be treated as exceptions rather than the default destination for automation.

The intended policy is:

- allow operational candidates when they represent unresolved follow-up work
- hide them from default semantic review surfaces unless explicitly requested
- exclude them from evidence lifecycle workflows
- support explicit cleanup or archival once superseded or resolved

This keeps operational drafts available without letting them drown out the
semantic inbox.

## Failure Modes And Safeguards

### False Positive Candidate Creation

Risk: hooks generate an operational candidate for routine session noise.

Safeguard:

- prefer writing runtime/audit layers first
- require actionable summary content before retaining an operational candidate as
  pending
- allow later maintenance or explicit cleanup workflows to archive or discard
  superseded operational drafts

### False Negative Candidate Creation

Risk: a stop/session-end event that really needed takeover is written only to
`state/` or `logs/`, so no one notices it.

Safeguard:

- durable takeover guidance should still be promoted into formal handoffs
- context and maintenance surfaces should be able to report recent unresolved
  runtime/handoff signals
- hooks should be allowed to create an operational candidate when the summary is
  non-empty and clearly actionable

### Compatibility Drift

Risk: new layering rules conflict with evidence lifecycle or existing review
behavior.

Safeguard:

- keep archived/discarded evidence in the candidate store when required for
  traceability
- continue hiding operational candidates and transcript evidence from default
  semantic review unless explicitly requested
- treat operational-candidate cleanup as an explicit lifecycle or maintenance
  action, not a silent migration

## Handoff Split: Draft Vs Durable

Worktrail should distinguish two handoff classes:

- **Draft handoff:** a transitional item created during stop, compact, or
  session-end flows; this may remain in `state/` or briefly in `candidates/`
  while it awaits a keep/discard/promote decision.
- **Durable handoff:** a reusable Markdown knowledge document under
  `.worktrail/handoffs/` with enough structure to guide future work.

A draft handoff should move out of the pending candidate pool once one of the
following becomes true:

- it is rewritten into a durable handoff
- it is superseded by a newer draft or durable handoff
- it is determined to be routine noise and explicitly discarded or archived by a
  future operational-candidate cleanup workflow

## Design Constraints

- Hooks may generate candidates or update runtime state, but they must not
  directly promote, merge, or otherwise bypass explicit review.
- Formal knowledge roots must remain human-reviewable Markdown.
- Candidate lifecycle state must remain auditable even when items are hidden from
  default views.
- Cleanup should prefer explicit lifecycle transitions over silent deletion.
- Any future automation that creates operational candidates should justify why a
  runtime-layer write was insufficient.

## Impact Surface

This design affects:

- hook behavior for stop, compact, and session-end capture
- `review`, `review --all`, and maintenance surfaces that report pending work
- evidence lifecycle behavior and terminology
- any future operational-candidate cleanup flow
- preview and index views that currently enumerate candidates without stronger
  lifecycle bucketing
- documentation that explains `handoff`, `candidate`, `state`, and audit roles

## Implications For Current Behavior

This model implies the current system is directionally correct for:

- semantic pending candidates
- transcript evidence pending distillation
- explicit durable handoffs under `.worktrail/handoffs/`

But it likely overuses `candidates/` for:

- generic stop-hook drafts
- generic session-end drafts
- repeated "Draft handoff generated from the current Worktrail state" items
- operational items that no longer have a real pending decision attached

The correction is not "never create operational candidates". The correction is
"only create or retain operational candidates when they still carry unresolved
judgment."

## Compatibility And Migration Notes

- This document does not require deleting historical candidate files.
- Archived and discarded evidence may remain in the candidate store exactly as
  defined by the evidence lifecycle design.
- Existing pending operational candidates should be cleaned up through explicit
  lifecycle actions or maintenance workflows, not by silent removal.
- Hook changes implied by this design should be rollout-safe: first change where
  new artifacts land, then add cleanup for the old pending backlog.

## Acceptance Criteria

This design is successful when:

- users can treat pending candidates as a meaningful inbox
- default review remains focused on semantic work
- evidence continues to support traceability without becoming default noise
- routine stop/session-end automation primarily writes runtime and audit layers
- durable handoffs are easy to distinguish from transient operational drafts
- derived artifacts such as preview output are clearly separated from knowledge
  governance state

## Open Questions

- Should Worktrail add an explicit lifecycle for operational candidates similar
  to evidence archive/discard, or should routine cleanup be handled by a broader
  knowledge maintenance proposal workflow?
- Should hooks use a stricter gate before creating `handoff` candidates, for
  example requiring non-empty actionable summary content?
- Should durable handoff promotion become a first-class command, or remain a
  documented pattern implemented through existing candidate and formal knowledge
  flows?
