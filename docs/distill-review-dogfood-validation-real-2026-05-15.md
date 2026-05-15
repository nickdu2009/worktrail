# Real Transcript Dogfood Validation: 2026-05-15

## Repository Or Scope

Real Codex transcript validation for the Worktrail repository, isolated to
Worktrail user scope so private transcript evidence and generated candidates do
not enter the project repository.

No transcript body, transcript file path, or local absolute path is recorded in
this document.

## Commands Run

```bash
go run ./cmd/worktrail import codex --scope user --format json
go run ./cmd/worktrail import codex --scope user --all --format json
go run ./cmd/worktrail distill --pending --summary --scope user
go run ./cmd/worktrail distill validate <temporary-proposal.json> --scope user --format json
go run ./cmd/worktrail distill apply <temporary-proposal.json> --scope user --format json
go run ./cmd/worktrail review plan --scope user --format json
go run ./cmd/worktrail evidence plan --scope user --format json
go run ./cmd/worktrail distill apply <temporary-proposal.json> --scope user
go run ./cmd/worktrail discard --scope user <temporary-validation-candidate-id>
```

The installed `worktrail` binary on this machine was older than the current
source during validation, so the dogfood pass used `go run ./cmd/worktrail`.

## Results

- Import dry run matched 3 real Codex transcript sessions.
- User-scope import synced 3 sessions and created 3 pending
  `transcript_notes` evidence candidates.
- Distill summary reported 3 pending evidence candidates without printing
  transcript body.
- A temporary proposal referencing one real evidence candidate validated
  successfully.
- `distill apply --format json` created 1 pending semantic `validation`
  candidate with `source_candidate_ids` pointing to the real evidence candidate.
- `review plan --scope user --format json` emitted
  `worktrail.review.plan.v1` and recommended `promote` for the temporary
  semantic candidate.
- `evidence plan --scope user --format json` emitted
  `worktrail.evidence.plan.v1`; the referenced evidence was recommended
  `keep` with 1 pending semantic reference.
- Re-running `distill apply` in text mode produced `Distill apply: no changes`
  and a skipped duplicate item.

## Formal Knowledge Changes

None. No promote or merge command was run.

## Cleanup

- The temporary proposal file was deleted.
- The temporary semantic validation candidate in user scope was discarded.
- Imported user-scope transcript evidence was left pending for future local
  review; it is outside the project repository and was not committed.

## Known Gaps

- This pass validated the mechanics of importing, referencing, applying,
  reviewing, and evidence planning real transcript evidence. It did not promote
  or merge the temporary semantic candidate into formal knowledge.
