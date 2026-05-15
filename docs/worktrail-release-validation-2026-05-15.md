# Worktrail Release Validation 2026-05-15

Status: passed

Baseline commit recorded before implementation: `448b133`

Requirements:
`docs/worktrail-release-acceptance.md`

Checklist:
`docs/worktrail-release-validation-checklist.md`

## Reproducible Release Gate

All commands were run from the implementation checkout. The release smoke used a
temporary `worktrail` binary and disposable `HOME`, `WORKTRAIL_HOME`, and
`WORKTRAIL_PROJECT_ROOT` values, so it did not depend on private local state.

| Command | Mode | Result |
| --- | --- | --- |
| `go test ./internal/app ./internal/contextpack ./internal/integrations` | read-only | passed |
| `go test ./...` | read-only | passed |
| `git diff --check` | read-only | passed |
| `go build ./cmd/worktrail` | read-only | passed |
| `go install ./cmd/worktrail` | mutates Go install target | passed |
| isolated `go build -o <tmp>/worktrail ./cmd/worktrail` | writes disposable binary | passed |

## Isolated CLI Smoke

The isolated smoke covered:

- `worktrail init`
- `worktrail install codex` and `worktrail doctor codex`
- `worktrail install claude` and `worktrail doctor claude`
- user-scope synthetic evidence creation
- default project-scope `distill --pending --summary` returning non-zero with a
  concrete `--scope user` next step
- scoped `distill --pending --summary`
- scoped distill pack, proposal validate, and proposal apply
- scoped `review plan --format json`
- `review apply-plan --confirm --scope project` rejecting a user-scope plan
- `review apply-plan --confirm` using the plan scope when `--scope` is omitted
- scoped `evidence plan --format json`
- default project-scope `evidence plan` suggesting `--scope user`
- `context --format json maintenance`

Result categories:

- apply-plan report: `applied=1`, `failed=0`
- evidence plan: `archive=1`
- maintenance next steps: `2`
- formal knowledge changed only inside disposable state
- cleanup: disposable state only, no project or user Worktrail state changed

## Supplemental Real Local Dogfood

The real local dogfood pass used the read-only `/worktrail-maintain` command
chain equivalent and recorded only counts:

- `worktrail context --format json maintenance`: read-only
- `worktrail distill --pending --summary`: read-only, returned non-zero because
  the default project scope had no pending transcript evidence while another
  scope did
- `worktrail review plan --format json`: read-only
- `worktrail evidence plan --format json`: read-only

Counts and categories:

- pending evidence candidates: `3`
- pending semantic candidates: `0`
- evidence lifecycle candidates from aggregate maintenance: `1`
- review plan total: `0`
- review recommendations: `promote=0`, `merge=0`, `discard=0`,
  `needs_human_review=0`
- default-scope evidence plan total: `0`
- maintenance next steps: `2`
- scope hint present for distill: yes
- formal knowledge changed: no

## Privacy And Safety

This record does not include transcript bodies, local absolute paths, session
ids, usernames, temporary pack/proposal content, or private candidate bodies.

All mutating release-smoke actions ran only in disposable state. The real local
dogfood pass was read-only. No automatic git commit was performed.

## Requirement Coverage

- `REQ-REL-*`: covered by targeted tests, full Go tests, build/install, isolated
  CLI smoke, and this dated validation record.
- `REQ-SCOPE-*`: covered by scope-default help text, user-scope commands in
  generated plans, focused distill/evidence scope hint tests, isolated smoke,
  and maintenance skill guidance.
- `REQ-MAINT-*`: covered by `/worktrail-maintain` template assertions and the
  read-only real local dogfood pass.
- `REQ-KQ-*`: covered by distill/review skill guidance, source traceability
  review-plan behavior, and `docs/examples/distill/knowledge-quality.md`.
- `REQ-APPLY-*`: covered by apply-plan confirmation, stale snapshot, skipped
  human-review, partial report, and explicit scope mismatch tests plus isolated
  smoke.
- `REQ-DOG-*`: covered by this dogfood record and privacy notes.

Known gaps: none blocking for the current release acceptance scope.
