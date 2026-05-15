# Distill and Review Dogfood Validation: 2026-05-15

## Repository Or Fixture

Fixture-based validation using synthetic Worktrail data in
`internal/testdata/distill/` plus app-level CLI tests. The fixtures do not use a
real transcript store, real user scope, private transcript body, or local
absolute paths.

## Worktrail Version

Working tree validation during backlog implementation on 2026-05-15.

## Commands Run

```bash
go test ./internal/app
```

## Scenarios Covered

- Transcript evidence proposal apply through `valid-basic`.
- KDD split-source proposal apply through `valid-split-source`.
- Proposal schema rejection through `invalid-schema`.
- Target path, confidence, source status, and source type validation failures.
- Blocked secret proposal item handling.
- Duplicate candidate id skip with warning code.
- Existing formal target warning codes.
- `worktrail review plan --format json` recommendations for promote, merge,
  discard, and needs-human-review.
- `worktrail evidence plan --format json` reference counting and
  active/archived/all status filters.

## Expected Result

- Source evidence remains a candidate.
- Distill apply creates pending semantic candidates only.
- Review plan and evidence plan are read-only.
- KDD split-source lessons are not recommended for direct promote, merge, or
  discard.
- No formal knowledge changes happen without explicit promote or merge.

## Actual Result

`go test ./internal/app` passed during implementation. The fixture tests compare
stable report fields: validity, created/skipped/blocked counts, item statuses,
warning codes, and error code text. Review and evidence plan tests verify JSON
schema names, recommended actions, source/reference status, and command safety.

## Candidate Counts And Apply Counts

Fixture coverage includes:

- Created: valid basic, valid split-source, and target-exists proposal cases.
- Skipped: duplicate-id proposal case.
- Blocked: blocked-secret proposal case.
- Errors: invalid schema, invalid target path, invalid confidence, missing
  source, non-pending source, and unexpected source type cases.

## Formal Knowledge Changes

None in fixture validation beyond temporary test repositories. Any formal target
files are synthetic fixtures copied into temporary `.worktrail/` roots.

## Cleanup

Temporary repositories are created by Go tests and cleaned up by the test
framework.

## Known Gaps

- This record is fixture-based, not a private real-transcript dogfood pass.
- Evidence archive/discard mutating commands are not implemented in v1; only
  the read-only `worktrail.evidence.plan.v1` contract is validated here.
