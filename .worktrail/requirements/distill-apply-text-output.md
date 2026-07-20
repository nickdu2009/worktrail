---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "distill-apply-text-output-contract",
  "scope": "project",
  "type": "requirement",
  "title": "Distill Apply Text Output Contract",
  "status": "active",
  "lifecycle": "current",
  "topic": "distillation"
}
---

# Distill Apply Text Output Contract

## Purpose

Default text output for `worktrail distill validate` and `worktrail distill apply` must give operators and agents a compact, deterministic summary without replacing the JSON machine contract.

## Requirements

- JSON remains the stable machine-readable contract; text is rendered from the same report model.
- Text distinguishes created, skipped, blocked, and error proposal items and always includes a summary count line.
- The status line is one of `success`, `partial success`, `completed with issues`, `no changes`, or `failed`, determined by created, skipped, blocked, and error counts rather than warnings alone.
- Item details identify the proposal index, candidate id when known, target path, candidate type, operation, and non-empty warning or error codes.
- Empty item sections are omitted.
- Text output never prints candidate bodies, local absolute proposal paths, or sensitive redaction details.
- The output contract does not change proposal validation semantics, candidate creation, redaction, id generation, or governed lifecycle actions.

## Validation

Cover mixed partial-success output, fatal input failures, warning visibility, JSON compatibility, and absence of candidate bodies or local paths.

## Migration provenance

Distilled from `docs/distill-apply-text-output.md`. The source remains in `docs/` until this candidate is promoted and inbound references are reviewed.
