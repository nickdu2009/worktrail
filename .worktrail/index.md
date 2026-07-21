---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "index",
  "scope": "project",
  "type": "index",
  "title": "Worktrail Project Index",
  "status": "active",
  "lifecycle": "current",
  "topic": "knowledge-governance"
}
---

# Worktrail Project Index

Formal knowledge entry points for the **worktrail** CLI repository.

## Entry points

- [Project overview](project.md)

## Architecture

- [Cursor and Codex local hooks](architecture/cursor-codex-local-hooks.md)
- [Local semantic recall](architecture/local-semantic-recall.md)
- [M1-M5 trusted manifest runtime boundary](architecture/semantic-m1-trusted-manifest-boundary.md)
- [SQLite and GSE index design](architecture/sqlite-gse-index-design.md)

## Requirements

- [Product direction and version boundaries](requirements/product-direction-and-version-boundaries.md)
- [Release acceptance](requirements/release-acceptance.md)

## Workflows

- [Low-Intervention Knowledge Lifecycle](workflows/low-intervention-knowledge-lifecycle.md)
- [Cursor and Codex local hooks implementation](workflows/cursor-codex-local-hooks-implementation.md)

## Validation

- [Release validation checklist](validation/release-validation-checklist.md)

## Rules

- [Knowledge Boundaries and Write Safety](rules/knowledge-boundaries-and-write-safety.md)
- [Coding rules](rules/coding-rules.md)
- [Security rules](rules/security-rules.md)
- [Testing rules](rules/testing-rules.md)

## Prompts

- [Project review](prompts/project-review.md)
- [Generate config draft](prompts/generate-config-draft.md)

## Decisions

- [Hybrid recall context contract](decisions/ADR-20260715-hybrid-recall-context-contract.md)
- [Local semantic runtime bundle](decisions/ADR-20260715-local-semantic-runtime-bundle.md)
- [Rebuild-only semantic generations](decisions/ADR-20260715-rebuild-only-semantic-generation.md)
- [KDD compatibility import](decisions/kdd-compatibility-import-not-dual-root.md)

## Task recovery

Task recovery uses `worktrail resume` with an explicit task or runtime-record
selector. State and Handoff V2 local/team records are runtime recovery material,
not formal knowledge entry points and not default semantic review candidates.

## Operations

- Search: `worktrail search "<keyword>"`
- Preview: `worktrail preview --scope project`
- Resume a known task: `worktrail resume --task-id <task-id>`
- Rebuild derived search index: `worktrail index rebuild --scope project`
