# Knowledge Maintenance Proposal Workflow

Last updated: 2026-05-26

Status: implemented

## Summary

Long-lived Worktrail knowledge will accumulate duplicates, stale documents,
conflicting decisions, orphaned entries, and direct formal edits. Worktrail
should not solve that by embedding an LLM or by letting agents freely rewrite
formal knowledge. The maintenance model should separate deterministic governance
from semantic drafting.

The chosen workflow is:

```text
worktrail maintain knowledge --format json
        -> agent reads the report and drafts a proposal
        -> worktrail maintain validate proposal.json
        -> user confirms
        -> worktrail maintain apply proposal.json --confirm
```

## Goals

- Keep formal knowledge writable only through controlled Worktrail operations.
- Let agents help with semantic cleanup without directly editing formal
  `.worktrail` documents.
- Make long-term knowledge maintenance repeatable, reviewable, and auditable.
- Preserve Worktrail's local-first, no-daemon, no-built-in-LLM boundary.

## Non-Goals

- Do not add an embedded LLM provider, model selection, embeddings, or vector
  database.
- Do not add a daemon, watcher, TUI, Web UI, or background cleanup service.
- Do not allow agents to apply semantic cleanup without explicit user
  confirmation.
- Do not make deterministic scanner output depend on natural-language model
  behavior.

## Design Decision

Worktrail owns deterministic maintenance. The agent owns semantic proposal
drafting. The user owns approval. Worktrail owns execution and audit logging.

This rejects two alternatives:

- **Agent edits formal knowledge directly.** This is fast, but it loses review
  boundaries, source traceability, and event history.
- **Worktrail embeds an LLM.** This would make the CLI harder to install,
  configure, test, and trust, and it conflicts with Worktrail's local-first
  deterministic boundary.

## Workflow

### 1. Deterministic Scan

`worktrail maintain knowledge --format json` is read-only. It scans formal
knowledge and emits a machine-readable report.

It can detect structural issues such as:

- missing or invalid frontmatter
- multiple `source_of_truth` documents for the same topic
- `supersedes` references to missing files
- retired documents still referenced by `index.md` or `project.md`
- formal knowledge files with no candidate / promote / merge trail
- untracked or modified formal knowledge files
- stale pending candidates or unresolved evidence

It does not decide semantic correctness and does not rewrite documents.

### 2. Agent Proposal Drafting

An agent reads the maintenance report and the referenced formal documents. It may
use LLM judgment to decide whether documents are actually duplicates, whether a
decision supersedes another, and how a merged document should read.

The agent writes a proposal, not formal knowledge:

```json
{
  "schema": "worktrail.knowledge.maintenance.proposal.v1",
  "actions": [
    {
      "action": "merge",
      "source_paths": [
        "architecture/old-plan.md",
        "architecture/current-direction.md"
      ],
      "target_path": "architecture/current-direction.md",
      "body": "...merged markdown...",
      "retire_sources": ["architecture/old-plan.md"],
      "reason": "Current direction supersedes the old implementation plan."
    }
  ]
}
```

The agent must not directly edit formal paths such as `architecture/`,
`decisions/`, `requirements/`, `workflows/`, `validation/`, `integrations/`,
`glossary/`, `rules/`, `project.md`, or `index.md`.

### 3. Proposal Validation

`worktrail maintain validate proposal.json` is read-only. It validates that:

- the proposal schema is supported
- all source files exist
- target paths are under allowed Worktrail knowledge roots
- every formal write is represented as an explicit action
- destructive actions require a reason
- raw transcript bodies, local absolute paths, and obvious secrets are rejected
- the proposal does not mutate runtime-only files such as logs, state, raw
  transcript metadata, or index caches unless a supported action explicitly owns
  that path

### 4. User Confirmation

The agent presents the validated proposal summary to the user. Worktrail must not
apply it until the user explicitly confirms.

### 5. Controlled Apply

`worktrail maintain apply proposal.json --confirm` converts approved proposal
actions into controlled Worktrail operations.

Allowed action families:

- create pending candidate
- promote confirmed candidate
- merge confirmed candidate into formal knowledge
- retire superseded formal knowledge
- archive resolved evidence or stale operational candidates
- update `index.md` / `project.md` through explicit proposal actions
- rebuild derived indexes
- write audit events and backups

Apply must not run implicit semantic cleanup. It only executes actions already
declared in the validated proposal.

## Acceptance

This workflow is accepted when:

- maintenance scanning can run without an LLM and without writing files
- agents can draft a proposal from scanner output without touching formal
  knowledge
- validation catches unsafe paths, missing sources, and unsupported destructive
  actions
- validate and apply report items expose machine-readable `error_codes` for
  semantic text safety failures; apply copies manager failures into the same
  field on failed items
- apply writes audit events for every mutation
- user confirmation remains the boundary for all formal knowledge changes
