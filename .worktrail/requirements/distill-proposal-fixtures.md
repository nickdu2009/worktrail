---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "distill-proposal-fixture-contract",
  "scope": "project",
  "type": "requirement",
  "title": "Distill Proposal Fixture Contract",
  "status": "active",
  "lifecycle": "current",
  "topic": "distillation"
}
---

# Distill Proposal Fixture Contract

## Purpose

Provide copyable, privacy-safe proposal examples for users and agents and isolated fixtures for deterministic validation and apply tests of `worktrail.distill.proposal.v1`.

## Requirements

- Keep user-facing examples under `examples/distill/` and automated fixtures under `internal/testdata/distill/`; tests must not depend on prose examples.
- All fixture data is synthetic and excludes real transcripts, credentials, local paths, private identifiers, and external project knowledge.
- Each automated case contains its proposal and expected stable report fields; optional seed candidates, existing candidates, formal targets, and expected candidates reproduce the required initial state.
- Fixture layouts mirror real candidate and formal-knowledge paths in a temporary Worktrail repository, rather than adding test-only schemas.
- Coverage includes valid transcript and KDD split-source proposals; schema, type, path, evidence-label, confidence, source, and operation failures; duplicate and target-exists behavior; blocked sensitive bodies; redaction warnings; and mixed partial results.
- JSON-mode load and schema failures return the CLI JSON error envelope.
- Fixture names and expected outcomes remain explicit, stable, and safe to run in temporary repositories.

## Validation

Run focused distill fixture tests and the repository test suite; user examples retain at least one minimal valid and one invalid proposal.

## Migration provenance

Distilled from `docs/distill-proposal-fixtures.md`. The source remains in `docs/` until this candidate is promoted and inbound references are reviewed.
