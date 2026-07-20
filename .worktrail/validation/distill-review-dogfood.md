---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "distill-review-dogfood-validation",
  "scope": "project",
  "type": "validation",
  "title": "Distill and Review Dogfood Validation",
  "status": "active",
  "lifecycle": "current",
  "topic": "knowledge-validation"
}
---

# Distill and Review Dogfood Validation

## Purpose

Validate the real evidence-to-knowledge workflow without committing private project data or treating dated acceptance output as durable formal knowledge.

## Required scenarios

1. **Transcript evidence distillation:** create or import pending transcript evidence, validate and apply a proposal, then confirm pending semantic candidates preserve source identifiers and formal knowledge remains unchanged before explicit review action.
2. **KDD migration-source distillation:** use a temporary or disposable workspace to create `migration_source` evidence, derive semantic candidates, and confirm the source itself is not eligible for direct semantic promotion.
3. **Review aid and review plan:** exercise clean promote and merge, conflicts, missing sources, duplicates, empty bodies, and split-source deferrals; text and JSON recommendations must be conservative, deterministic, and read-only.
4. **Promote and merge smoke:** inspect each selected candidate diff, obtain explicit confirmation, perform one replace and one merge, then confirm context loads the applied knowledge and evidence remains in its separate lifecycle lane.

## Record contract

Each dated dogfood record records the date, repository or fixture description, version under test, commands and outcomes, expected versus actual result, candidate counts, created/skipped/blocked/warning counts, selected ids, formal-write status, cleanup, and known gaps. It excludes raw transcript bodies and local absolute paths.

## Acceptance

Before treating the workflow as release-ready, record real transcript and KDD dogfood passes, validate source traceability and review-plan automation, and ensure disposable acceptance output is removed or explicitly marked as such.

## Migration provenance

Distilled from `docs/distill-review-dogfood-validation.md`. The source remains in `docs/` until this candidate is promoted and inbound references are reviewed.
