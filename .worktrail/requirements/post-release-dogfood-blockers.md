---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "post-release-dogfood-blocking-requirements",
  "scope": "project",
  "type": "requirement",
  "title": "Post-Release Dogfood Blocking Requirements",
  "status": "active",
  "lifecycle": "current",
  "topic": "post-release-dogfood"
}
---

# Post-Release Dogfood Blocking Requirements

## Purpose

The P0 dogfood findings define these durable safeguards for ordinary long-session Worktrail use. They preserve the review boundary and do not permit automatic knowledge lifecycle mutation.

## Requirements

### REQ-POST-001: Hook State Captures Task Context

Hook-generated state derives a real task signal when available; event-only payloads do not overwrite useful active state. Generated state separates durable task facts from runtime telemetry and records goal, completed work, validation, open questions, and next step. Tests cover meaningful and event-only payloads.

### REQ-POST-002: Pre-Compact Checkpoints Support Recovery

When transcript context is available, a bounded recovery summary identifies the active goal, decisions, work completed, validation, and next action without retaining full transcript content. When unavailable, the checkpoint says so. Tests cover both cases and runtime-field redaction.

### REQ-POST-003: Context Surfaces Importable Project Transcripts

Context reports scoped, bounded hints for relevant unimported project transcripts and suggests explicit import commands. It distinguishes evidence intake from semantic candidate promotion. Tests cover absent, importable, and already imported sessions.

### REQ-POST-004: Low-Friction Knowledge Capture

Provide a concise path that creates pending candidates from validated findings without hand-written proposal JSON. It records type, target, summary, confidence, and evidence label; it never promotes or exposes private transcript bodies by default. Documentation and tests cover creation, duplicate targets, missing fields, and the no-auto-promote boundary.

### REQ-POST-005: Consistent Subcommand Help

Public top-level and nested commands accept `--help` and `help`, successful help exits zero, and unknown subcommands return actionable failures. CLI smoke coverage includes context, state, handoff, import, review, distill, and evidence.

### REQ-POST-006: Handoff Diagnoses Sandbox Write Boundaries

Permission or sandbox failures identify the target directory and likely configuration action. Relevant doctor checks detect common write-boundary mismatches, and guidance lists required writable directories. Handoff remains explicit; tests cover success, permission diagnostics, and doctor output.

### REQ-POST-007: Detect Formal Knowledge Write Escapes

`worktrail doctor knowledge` read-only reports formal knowledge that lacks a matching lifecycle trail, distinguishes imported/promoted content where possible, identifies modified or untracked formal paths, and recommends recovery. It covers formal roots and excludes runtime files; tests cover promoted, direct-edit, untracked, deleted, and runtime scenarios.

## Completion boundary

The P0 set is complete only when every item is implemented or explicitly deferred, preserves the no-daemon/no-Web-UI/no-vector-store/no-auto-promotion boundary, and receives CLI tests plus a dogfood smoke pass.

## Migration provenance

Distilled from `docs/post-release-dogfood-release-blocking-requirements.md`. The source remains in `docs/` until this candidate is promoted and inbound references are reviewed.
