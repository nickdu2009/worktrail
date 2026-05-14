# KDD Import Dogfood Acceptance

This record captures the real-project acceptance pass for importing legacy
`docs/knowledge-driven-development/` knowledge into Worktrail pending candidates.

## Fixture

- Repository: delivery-experts checkout supplied as the local fixture
- Source root: `docs/knowledge-driven-development/`
- Destination: `.worktrail/`
- Rule: the imported `.worktrail/` tree was treated as an acceptance artifact and removed after validation.

## Findings

The first dogfood pass exposed a real candidate-id collision when long KDD paths
shared the same truncated prefix. Worktrail now appends a stable hash suffix when
creating KDD candidate ids, so repeated imports stay deterministic without
colliding.

The review pass also showed two content-quality issues:

- Category README files were directory guidance, not durable project knowledge.
- `project/active-knowledge-log.md` was too coarse to promote directly.

The importer now skips category README files and imports the active log only as a
pending split source marked `Pending Verification`.

## Final Dry Run

After the refinements:

- `matched`: 37
- `skipped`: 7
- `local_skipped`: 7
- `blocked`: 0

Skipped files were the root overview and six category README files. Local files
under `local/**` were reported separately and not imported.

## Import Verification

`worktrail import kdd --all --format json` created 37 pending candidates.
Running the same import again created 0 new candidates and counted all duplicates
as skipped.

Semantic distribution:

- `architecture`: 11
- `decision`: 12
- `glossary`: 1
- `integration`: 3
- `lesson`: 1
- `project`: 1
- `validation`: 2
- `workflow`: 6

All candidate metadata `target_path` values were relative to the Worktrail scope
root, with no `.worktrail/` prefix.

## Promote Workflow Verification

The acceptance pass promoted one imported decision candidate and merged the
imported project README candidate. After that:

- pending count dropped from 37 to 35
- promoted and merged candidates no longer appeared in pending review output
- `worktrail context` loaded the promoted decision and merged project knowledge
- review output did not report missing-target warnings

This confirms the KDD import remains a pending-candidate bridge into the existing
review, promote, merge, and context lifecycle. It does not create a long-term
dual knowledge root.
