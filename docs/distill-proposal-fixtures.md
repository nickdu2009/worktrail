# Distill Proposal Fixtures Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail should include proposal schema examples and fixtures for
`worktrail.distill.proposal.v1` so agents can author proposals consistently and
tests can cover validation, apply, redaction, duplicate, and split-source
behavior.

The fixtures should be small, explicit, and safe to run in temporary Worktrail
repositories.

## Goals

- Provide copyable proposal examples for humans and agents.
- Provide test fixtures that exercise both valid and invalid proposal paths.
- Cover KDD split-source and transcript evidence source usage.
- Keep fixture data free of real secrets, real local paths, and private project
  identifiers.
- Make expected outcomes clear enough for CLI and package tests.

## Non-Goals

- Do not add large transcript corpora.
- Do not depend on the user's real Codex or Claude transcript store.
- Do not include live project knowledge from external repositories.
- Do not change the proposal schema in this documentation task.

## Fixture Placement

Recommended layout:

```text
docs/examples/distill/
  valid-basic-proposal.json
  valid-split-source-proposal.json
  invalid-schema-proposal.json
  invalid-target-path-proposal.json
  invalid-confidence-proposal.json
  blocked-secret-proposal.json
  duplicate-id-proposal.json

internal/testdata/distill/
  README.md
  valid-basic/
    proposal.json
    seed-candidates/
    expected-report.json
  valid-split-source/
    proposal.json
    seed-candidates/
    expected-report.json
  invalid-schema/
    proposal.json
    expected-report.json
  invalid-target-path/
    proposal.json
    seed-candidates/
    expected-report.json
  invalid-confidence/
    proposal.json
    seed-candidates/
    expected-report.json
  blocked-secret/
    proposal.json
    seed-candidates/
    expected-report.json
  duplicate-id/
    proposal.json
    seed-candidates/
    existing-candidates/
    expected-report.json
  target-exists/
    proposal.json
    seed-candidates/
    formal-targets/
    expected-report.json
```

`docs/examples` is optimized for users and agents. `internal/testdata` is
optimized for automated tests. The two sets may share content, but tests should
not depend on prose examples that may change for readability.

## Test Fixture Contract

Each `internal/testdata/distill/<case>/` fixture should describe both the
proposal and the Worktrail state required to reproduce the scenario.

Required files:

- `proposal.json`: the proposal passed to `worktrail distill validate` or
  `worktrail distill apply`.
- `expected-report.json`: the expected stable report fields for validation or
  apply. It should include counts, item statuses, warning codes, and error
  codes, but should avoid timestamp-sensitive fields unless tests normalize
  them.

Optional state directories:

- `seed-candidates/`: source candidates that must exist before the command
  runs, such as pending `transcript_notes` or pending KDD split-source `lesson`
  candidates.
- `existing-candidates/`: candidates that already exist before apply, used for
  duplicate-id and skipped-item scenarios.
- `formal-targets/`: existing formal knowledge files under the Worktrail scope
  root, used for target-exists and merge-target scenarios.
- `expected-candidates/`: candidate files expected after apply when full file
  comparison is useful.

Tests should load each fixture into a temporary Worktrail repository, run the
command under test, and compare the generated report to `expected-report.json`.
They should not rely on the developer's real user scope, transcript store, or
current repository candidates.

## Fixture File Format

Fixture state directories should mirror the files that will be copied into a
temporary Worktrail repository. They are not custom JSON formats.

Candidate fixture files:

```text
seed-candidates/project/note-1.md
seed-candidates/project/kdd-active-log.md
existing-candidates/project/distill-rule-example.md
```

The test loader copies candidate files into:

```text
.worktrail/candidates/project/<file>
```

For user-scope fixtures, use:

```text
seed-candidates/user/note-1.md
existing-candidates/user/distill-rule-example.md
```

The test loader copies those files into the user candidate store for the
temporary test home. Tests should not read or write the real user Worktrail
scope.

Formal target fixture files:

```text
formal-targets/project/rules/example.md
formal-targets/project/project.md
formal-targets/user/rules/example.md
```

The test loader copies project formal targets into the temporary repository
Worktrail root and user formal targets into the temporary user Worktrail root.
The scope prefix is not part of the final target path.

Copy rules:

- `formal-targets/project/<target_path>` -> temporary repo
  `.worktrail/<target_path>`
- `formal-targets/user/<target_path>` -> temporary user Worktrail root
  `<target_path>`

For example:

- `formal-targets/project/rules/example.md` copies to
  `.worktrail/rules/example.md`
- `formal-targets/project/project.md` copies to `.worktrail/project.md`
- `formal-targets/user/rules/example.md` copies to the temporary user
  Worktrail root at `rules/example.md`

`expected-candidates/` uses the same scope-prefixed layout as
`seed-candidates/`:

```text
expected-candidates/project/distill-rule-example.md
expected-candidates/user/distill-rule-example.md
```

All candidate fixture files should use the same Markdown plus JSON frontmatter
format as real Worktrail candidates. This keeps fixture behavior aligned with
the production candidate parser and avoids a second test-only schema.

## Required Fixture Coverage

Valid fixtures:

- Basic transcript evidence to semantic candidate.
- KDD active-log split source to multiple semantic candidates.
- Proposal item using top-level `source_candidate_ids`.
- Proposal item overriding `source_candidate_ids`.
- Redacted-but-allowed item where candidate creation succeeds with existing
  redaction behavior.

Invalid fixtures:

- Unknown schema.
- Unknown candidate type.
- Unsupported operation for proposal items.
- Invalid `evidence_label`.
- Explicit `confidence: 0`.
- `target_path` with `.worktrail/` prefix.
- Path traversal outside the scope root.
- Candidate type and target directory mismatch.
- Missing source candidate.
- Source candidate that is not pending.
- Source candidate that is not an allowed evidence type.

Apply behavior fixtures:

- Duplicate candidate id is skipped.
- Duplicate id with different body emits
  `duplicate_id_existing_body_may_differ`.
- Blocked secret item is counted as blocked, emits
  `body_blocked_sensitive_material`, and does not create a candidate.
- Transcript-style bodies emit `body_raw_transcript_style_conversation`.
- Local absolute paths emit `{field}_local_absolute_path`.
- Redactable secret/PII patterns emit `{field}_redactable_secret_or_pii`.
- Distill proposal load/schema preflight failures in `--format json` return
  `worktrail.cli.error.v1` on stdout with `ok: false`.
- Mixed proposal creates valid items while reporting item errors for invalid
  items.

## Example Standards

Fixture examples should:

- Use synthetic ids such as `note-1` and `kdd-active-log`.
- Use stable titles and target paths.
- Avoid absolute local paths.
- Avoid real credentials, hostnames, account ids, and customer data.
- Prefer short bodies that still show Markdown frontmatter and content behavior.

## Acceptance Criteria

- Documentation includes at least one minimal valid proposal and one invalid
  proposal example.
- Testdata covers the required validation and apply paths listed above.
- Fixture names make the expected behavior obvious.
- `go test ./...` uses the test fixtures where practical.
- `git diff --check` passes after adding fixture files.
