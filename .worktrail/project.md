---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "project",
  "scope": "project",
  "type": "project",
  "title": "Project",
  "status": "active",
  "lifecycle": "current",
  "topic": "knowledge-governance"
}
---

# Project

**worktrail** is a local-first knowledge base, work log, and task-recovery tool
for AI coding sessions in Cursor, Codex, Claude Code, and ZCode Agent.

## Scope

This `.worktrail/` tree holds project-scoped knowledge and runtime records for
the Go CLI in this repository. User-scoped knowledge remains separate.

## Stack

- Go CLI under `cmd/worktrail`
- Markdown with Worktrail JSON frontmatter as the formal knowledge source of
  truth
- SQLite FTS5 with `gse` tokenization at `.worktrail/index/index.sqlite` as a
  rebuildable search index
- Optional local semantic recall governed by accepted project decisions

## Knowledge conventions

- Formal reusable knowledge may live under approved roots such as `decisions/`,
  `architecture/`, `requirements/`, `rules/`, `workflows/`, `validation/`,
  `integrations/`, `glossary/`, `lessons/`, and `prompts/`, plus `project.md`
  and `index.md`.
- Pending candidates are staging and audit records. They are not formal
  knowledge and do not become authoritative until explicit review and a
  confirmed Worktrail CLI promote or merge operation succeeds.
- Candidate creation and all formal knowledge mutations use supported
  `worktrail` CLI operations. Agents may draft text but must not directly edit
  formal knowledge or candidate storage as the normal workflow.
- Handoff V2 local and team records are task-scoped runtime recovery records.
  They are not formal knowledge and are not part of default semantic candidate
  review.
- State, checkpoints, runtime records, logs, raw imports, operation journals,
  receipts, and bindings are operational or audit material, not formal reusable
  knowledge.
- Index databases, caches, previews, and exports are derived artifacts and must
  remain rebuildable.
- Documentation manuals, engineering evidence, examples, and archived records
  remain under `docs/` unless concise semantic knowledge is explicitly drafted,
  reviewed, and promoted through Worktrail.

## Hook conventions

- Cursor and Codex hooks use the local Worktrail CLI and host-native protocols.
- Hooks may write only bounded runtime, checkpoint, receipt, binding, terminal,
  validation, and reason-code audit effects.
- Hooks do not modify explicit active state and never create takeover, handoff,
  candidate, or formal knowledge records.
- Hooks never promote, merge, discard, restore, retire, archive, prune, or
  publish knowledge or handoffs.
- Hook records must not persist complete prompts, thoughts, transcripts, raw
  tool input or output, secrets, or absolute transcript paths.

## Review and maintenance conventions

- Read-only context, search, review-plan, evidence-plan, validation, doctor, and
  preview operations may run without mutation confirmation.
- Governed mutations require explicit user approval and the command-specific
  confirmation gate when the command defines one. This includes formal writes,
  evidence archive or discard, proposal apply, runtime pruning or repair,
  handoff publishing, and remote operations. Supported direct candidate
  creation creates pending, non-authoritative knowledge.
- Default review and context surfaces remain focused; evidence and operational
  records require explicit inspection paths.
- Maintenance separates deterministic discovery, agent-authored proposals, CLI
  validation, user confirmation, controlled apply, and post-action verification.
- Hooks and implicit automation never perform candidate review decisions or
  formal knowledge lifecycle actions.

## Current architecture anchors

- [Cursor and Codex local hooks architecture](architecture/cursor-codex-local-hooks.md)
- [Cursor and Codex local hooks implementation workflow](workflows/cursor-codex-local-hooks-implementation.md)
- [Hybrid recall context contract](decisions/ADR-20260715-hybrid-recall-context-contract.md)
- [Local semantic runtime bundle](decisions/ADR-20260715-local-semantic-runtime-bundle.md)
- [Rebuild-only semantic generations](decisions/ADR-20260715-rebuild-only-semantic-generation.md)
- [KDD compatibility import boundary](decisions/kdd-compatibility-import-not-dual-root.md)

## Current milestones

- SQLite plus GSE is the lexical search and context baseline; Markdown remains
  the recovery source of truth.
- Local semantic recall decisions preserve lexical compatibility and explicit
  degradation behavior.
- Cursor and Codex local hook architecture defines the current runtime and write
  boundary; older handoff- and candidate-producing hook conventions are not
  authoritative.

## Alignment references

This project profile aligns with conventions recorded in:

- `.worktrail/index.md`
- `.worktrail/architecture/cursor-codex-local-hooks.md`
- `.worktrail/workflows/cursor-codex-local-hooks-implementation.md`
- `.worktrail/index.md`
