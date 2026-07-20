# Documentation Governance Index

This directory has four distinct responsibilities:

1. `manual/` and `user-guide.md` are user-facing product documentation.
2. Active specifications describe current behavior, constraints, and operational
   acceptance criteria.
3. Evidence, examples, and fixtures support verification but are not formal
   Worktrail knowledge.
4. `archive/` retains dated or superseded engineering records.

Formal reusable project knowledge belongs under `.worktrail/` only after a
pending candidate has completed explicit review. A pending candidate is never
authoritative. After a rule or workflow is promoted and inbound references are
updated, a matching source document may remain as design evidence with a
formal-path backlink or be deleted as a superseded duplicate. Deleted sources
must remain recoverable from Git history, and the formal document must retain
migration provenance.

## Governance labels

- `user_manual`: end-user documentation; keep in `docs/`.
- `active_spec`: current design, requirement, contract, or operational
  checklist; keep in `docs/` unless a reviewed semantic extraction replaces
  its normative role.
- `distill_source`: source for a concise pending Worktrail candidate; do not
  promote the source document verbatim.
- `evidence_run`: human-readable validation or dogfood evidence.
- `evidence_machine`: machine-readable evidence, manifests, logs, or
  environment snapshots.
- `example_fixture`: examples and fixtures; never treat as authority.
- `historical`: completed plans, audits, prompts, or superseded designs.
- `superseded_by_worktrail`: source whose normative content exists in accepted
  Worktrail knowledge; retain it as evidence or delete it after provenance and
  reference checks pass.

The actions below are governance decisions, not automatic mutations:

- `keep_docs`: remain in this documentation tree.
- `archive`: retain under `docs/archive/`.
- `distill_to_rule` / `distill_to_workflow`: prepare a pending candidate and
  stop at Worktrail review.
- `delete_after_migrate`: remove a superseded duplicate only after promotion,
  reference repair, and provenance checks.
- `compare_before_migrate`: compare against accepted decisions before creating
  any candidate.
- `distill_lesson_only`: extract only durable lessons; keep the evidence here.

## Inventory

This inventory covers the 29 documentation files retained after migration
cleanup. Each file has one primary label and one action.

### Governance metadata

- [`README.md`](README.md) — governance index; `active_spec`; `keep_docs`.

### User manual

- [`user-guide.md`](user-guide.md) — stable thin entry; `user_manual`;
  `keep_docs`.
- [`manual/README.md`](manual/README.md) — canonical manual home;
  `user_manual`; `keep_docs`.
- [`manual/QUICK-START.md`](manual/QUICK-START.md) — quick start;
  `user_manual`; `keep_docs`.
- [`manual/INSTALLATION.md`](manual/INSTALLATION.md) — installation;
  `user_manual`; `keep_docs`.
- [`manual/DESIGN-PHILOSOPHY.md`](manual/DESIGN-PHILOSOPHY.md) — product
  philosophy; `user_manual`; `keep_docs`.
- [`manual/AUTOMATION.md`](manual/AUTOMATION.md) — automation boundaries;
  `user_manual`; `keep_docs`.
- [`manual/COMMON-WORKFLOWS.md`](manual/COMMON-WORKFLOWS.md) — user workflows;
  `user_manual`; `keep_docs`.
- [`manual/TROUBLESHOOTING.md`](manual/TROUBLESHOOTING.md) — troubleshooting;
  `user_manual`; `keep_docs`.
- [`manual/FAQ.md`](manual/FAQ.md) — frequently asked questions;
  `user_manual`; `keep_docs`.
- [`manual/_sidebar.md`](manual/_sidebar.md) — Docsify navigation;
  `user_manual`; `keep_docs`.
- [`manual/index.html`](manual/index.html) — Docsify shell; `user_manual`;
  `keep_docs`.

### Active specifications and operational documents

- [`worktrail-local-semantic-recall-architecture.md`](worktrail-local-semantic-recall-architecture.md)
  — semantic-recall implementation architecture governed by accepted semantic
  ADRs; `active_spec`; `keep_docs`.
- [`worktrail-release-acceptance.md`](worktrail-release-acceptance.md) —
  active release requirements; `active_spec`; `keep_docs`.
- [`worktrail-release-validation-checklist.md`](worktrail-release-validation-checklist.md)
  — release checklist; `active_spec`; `keep_docs`.
- [`worktrail-semantic-m1-trusted-manifest-boundary.md`](worktrail-semantic-m1-trusted-manifest-boundary.md)
  — trusted-manifest operational and release boundary governed by the accepted
  runtime-bundle ADR; `active_spec`; `keep_docs`.
- [`worktrail-sqlite-gse-index-design.md`](worktrail-sqlite-gse-index-design.md)
  — lexical-index design referenced by accepted decisions; `active_spec`;
  `keep_docs`.
- [`worktrail-backlog.md`](worktrail-backlog.md) — current implementation
  status and backlog; `active_spec`; `keep_docs`.

### Top-level evidence

- [`worktrail-semantic-m1-release-evidence-2026-07-17.md`](worktrail-semantic-m1-release-evidence-2026-07-17.md)
  — semantic M1 evidence index; `evidence_run`; `keep_docs`.
- [`worktrail-semantic-production-e2e-2026-07-17.md`](worktrail-semantic-production-e2e-2026-07-17.md)
  — semantic production E2E summary; `evidence_run`; `keep_docs`.

### Machine-readable evidence

- [`semantic-e2e-evidence/retrieval-report.json`](semantic-e2e-evidence/retrieval-report.json)
  — retrieval report; `evidence_machine`; `keep_docs`.
- [`semantic-e2e-evidence/runtime-resource-report.json`](semantic-e2e-evidence/runtime-resource-report.json)
  — runtime resource report; `evidence_machine`; `keep_docs`.
- [`semantic-e2e-evidence/m1-release-gate-2026-07-17.json`](semantic-e2e-evidence/m1-release-gate-2026-07-17.json)
  — release-gate result; `evidence_machine`; `keep_docs`.
- [`semantic-e2e-evidence/m1-release-gate-2026-07-17-source-snapshot.json`](semantic-e2e-evidence/m1-release-gate-2026-07-17-source-snapshot.json)
  — source snapshot; `evidence_machine`; `keep_docs`.
- [`semantic-e2e-evidence/m1-release-gate-2026-07-17-source-snapshot-verification.json`](semantic-e2e-evidence/m1-release-gate-2026-07-17-source-snapshot-verification.json)
  — snapshot verification; `evidence_machine`; `keep_docs`.

### Examples and fixtures

- [`examples/distill/knowledge-quality.md`](examples/distill/knowledge-quality.md)
  — semantic-quality review example required by release acceptance;
  `example_fixture`; `keep_docs`.
- [`examples/distill/valid-basic-proposal.json`](examples/distill/valid-basic-proposal.json)
  — valid proposal example; `example_fixture`; `keep_docs`.
- [`examples/distill/valid-split-source-proposal.json`](examples/distill/valid-split-source-proposal.json)
  — split-source proposal example; `example_fixture`; `keep_docs`.
- [`examples/distill/invalid-target-path-proposal.json`](examples/distill/invalid-target-path-proposal.json)
  — invalid-target example; `example_fixture`; `keep_docs`.

## Migration status

The first Worktrail migration batch is complete:

1. [Knowledge Boundaries and Write Safety](../.worktrail/rules/knowledge-boundaries-and-write-safety.md)
2. [Low-Intervention Knowledge Lifecycle](../.worktrail/workflows/low-intervention-knowledge-lifecycle.md)

Their eight superseded source documents were removed after promotion, reference
repair, and provenance checks. A second cleanup removed 21 completed plans,
dated validation outputs, redundant machine artifacts, and superseded records;
their original content remains recoverable from Git history.

A third batch promoted six active contracts and the product-direction requirement,
then removed their seven superseded source documents after reference repair and
provenance checks. Release acceptance, manuals, the current M1 evidence package,
copyable examples, and remaining active implementation specifications stay in
`docs/`. The three accepted semantic ADRs now reference the consolidated M1
evidence and preserve the removed process spikes as explicit Git-history
provenance.

The two `compare_before_migrate` documents were compared with accepted semantic
ADRs and remain active implementation or release specifications. The two
`distill_lesson_only` sources were also resolved: the quality rubric remains a
required example, while the KDD dogfood record was deleted because its durable
lessons already exist in accepted Worktrail knowledge.
