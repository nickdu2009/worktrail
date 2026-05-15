# Low-Intervention Maintenance Validation 2026-05-15

Status: passed

Scope: Phase 3 maintenance workflow validation for
`docs/low-intervention-knowledge-workflow.md`.

## Workflow Coverage

The `/worktrail-maintain` skill guides an agent through these read-only checks,
using scoped variants from `maintenance.next_steps` when needed:

- `worktrail context "maintenance"`
- `worktrail distill --pending --summary`
- `worktrail review plan --format json`
- `worktrail evidence plan --format json`

The workflow then asks the user which lane to run before any state-changing
command. It does not automatically commit git changes.

## Fixture-Based Validation

Commands:

- `go test ./internal/integrations`
- `go test ./...`
- `git diff --check`

Expected and actual result:

- Codex and Claude user installs include `/worktrail-maintain`.
- Installed skill template includes the maintenance command chain.
- Installed skill template tells agents to preserve scope from
  `maintenance.next_steps`.
- Installed skill template states explicit confirmation is required before
  mutating commands.

## Real CLI Dogfood

An isolated disposable Worktrail environment was seeded with one redacted
transcript evidence candidate and one semantic review candidate.

Commands:

- `worktrail context "maintenance"`
- `worktrail distill --pending --summary`
- `worktrail review plan --format json`
- `worktrail evidence plan --format json`
- `worktrail install codex`
- `worktrail doctor codex`

Actual result:

- The maintenance commands completed and produced only counts, plans, and
  redacted fixture data.
- A real project smoke found pending evidence in user scope. The context hint
  correctly pointed to `--scope user`, and the scoped distill summary found the
  pending evidence.
- An isolated `review apply-plan` smoke confirmed that missing `--confirm` is
  rejected, confirmed apply reports `applied` and `skipped`, and reusing the same
  plan reports a `stale` item.
- No formal knowledge was changed by the read-only maintenance checks.
- No transcript body, local absolute path, session id, username, or temporary
  pack/proposal path is recorded in this document.
