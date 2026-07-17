# Worktrail Release Validation Checklist

Last updated: 2026-07-17

Status: in progress

Previous baseline commit: `448b133` (passed on 2026-05-15)

This checklist maps the release acceptance requirements in
`docs/worktrail-release-acceptance.md` to validation evidence. The dated release
validation record should update the Evidence column with exact commands and
results.

The May 2026 result remains historical evidence for the lexical knowledge
workflow. It does not validate the July 2026 semantic-recall scope expansion.
This checklist returns to `passed` only after the semantic requirements below
have dated evidence.

## Current Release Provenance Status

The 2026-07-17 M1 production-gate record is **dirty-tree engineering
evidence**, not the final reproducible release record. It verifies the M1
runtime path only; M2–M5 remain explicitly experimental. It cannot satisfy
`REQ-REL-001` because its source snapshot identifies an uncommitted working
tree rather than a candidate commit in a clean checkout.

The following release gates remain blocked until a user authorizes the release
operations:

- commit the reviewed release candidate;
- run the complete validation sequence from a clean checkout of that commit and
  record clean before/after status;
- create and validate the intended v1.0.0 tag, including ordinary tag/binary
  integrity evidence;
- perform any publish or release action separately.

Neither the dirty-tree record nor this checklist authorizes a commit, tag,
push, or release.

## Requirement Map

| Requirement | Validation evidence |
| --- | --- |
| `REQ-REL-001` | Clean checkout record with commit, `go test ./...`, `git diff --check`, `go build ./cmd/worktrail`, `go install ./cmd/worktrail`, and before/after worktree status. |
| `REQ-REL-002` | Isolated Codex and Claude install/doctor smoke using disposable `HOME`, `WORKTRAIL_HOME`, and `WORKTRAIL_PROJECT_ROOT`. |
| `REQ-REL-003` | Synthetic lifecycle smoke: init, evidence, distill validate/apply, review plan, confirmed apply-plan or explicit candidate action, evidence plan, context. |
| `REQ-REL-004` | Dated release validation record with commands, read-only/mutating classification, counts, result categories, formal knowledge change status, cleanup, known gaps, and privacy notes. |
| `REQ-REL-005` | Supplemental real local `/worktrail-maintain` dogfood record with private content redacted and findings triaged. |
| `REQ-SCOPE-001` | Help text or README coverage for command scope defaults plus user/project maintenance hint tests. |
| `REQ-SCOPE-002` | Context maintenance tests showing user-scope next steps and both-scope command emission where applicable. |
| `REQ-SCOPE-003` | `maintenance.next_steps` JSON/text behavior and skill guidance that treats generated commands as authoritative. |
| `REQ-SCOPE-004` | Focused tests or CLI smoke for `distill --pending --summary` and `evidence plan` suggesting the matching scope when another scope has work. |
| `REQ-MAINT-001` | `/worktrail-maintain` skill install test proving read-only discovery and scope preservation guidance. |
| `REQ-MAINT-002` | Dogfood record with a redacted maintenance summary description and no transcript body, local path, session id, or temporary artifact path. |
| `REQ-MAINT-003` | `/worktrail-maintain` skill text and install test covering distill, review apply-plan, single candidate, evidence lifecycle, and no-op lanes. |
| `REQ-MAINT-004` | `/worktrail-maintain` skill text and validation record confirming no automatic git activity. |
| `REQ-MAINT-005` | Zero-count context behavior and no-op maintenance dogfood note, unless listed as a non-blocking gap. |
| `REQ-KQ-001` | Distill skill text plus docs example or validation record showing high-quality semantic knowledge and a low-quality pattern rejected by agent/user review, not CLI semantic scoring. |
| `REQ-KQ-002` | Review and review-plan tests for `source_candidate_ids`, source statuses, and missing-source human review. |
| `REQ-KQ-003` | Docs example or validation note covering stable, type-appropriate, privacy-safe target paths. |
| `REQ-KQ-004` | Review/review-plan/evidence-plan validation showing redaction status is visible and unsafe redaction blocks automatic promote/merge. |
| `REQ-KQ-005` | Review-plan tests for empty candidates, duplicate candidates, and conservative same-target handling. |
| `REQ-APPLY-001` | Apply-plan without `--confirm` focused test or CLI smoke. |
| `REQ-APPLY-002` | Apply-plan schema and malformed JSON rejection tests. |
| `REQ-APPLY-002A` | Apply-plan explicit scope mismatch rejection tests for project-plan/user-scope and user-plan/project-scope. |
| `REQ-APPLY-003` | Stale snapshot tests covering status, operation, target path, source ids, body hash, metadata hash, and smoke for reused old plan. |
| `REQ-APPLY-004` | Apply-plan tests proving only promote/merge/discard run, human-review and unknown actions are skipped, and evidence cleanup is not performed. |
| `REQ-APPLY-005` | Apply-plan partial report tests and CLI smoke covering applied, skipped, stale, and failed groups. |
| `REQ-DOG-001` | Dated dogfood record with required fields and counts before/after mutating actions. |
| `REQ-DOG-002` | Dogfood privacy notes confirming no transcript body, local absolute paths, session ids, usernames, or temporary proposal/pack contents. |
| `REQ-DOG-003` | Dogfood findings classified as release-blocking, post-release, non-blocking polish, or no findings. |

## Semantic Recall Requirement Map

- `REQ-SEM-001`: canonical immutable manifest embedded in the formal Worktrail
  release as the sole v1 runtime trust root; content-addressed bundle ID;
  M1-verified and M2-M5-experimental variant classification; and startup/reuse
  rejection tests for manifest identity, the selected local-chip
  model/runtime/license/attribution artifacts, sizes, SHA-256 values, and chip
  variant. Ordinary release tag and binary-distribution integrity remains
  independently evidenced.
- `REQ-SEM-002`: init tests proving plain core init remains network-free,
  `--semantic` is the only semantic-download entry point, `--no-semantic`
  explicitly disables it, semantic-install failure preserves core init, no
  implicit generation occurs, and rebuild guidance is shown.
- `REQ-SEM-003`: authenticated loopback runtime lifecycle, endpoint-race,
  unknown-process, offline, logging, and permission tests; plus visible warning
  and stable-reason coverage for bundle/profile/generation/daemon identity
  mismatches, with `auto` lexical degradation and `required` failure.
- `REQ-SEM-004`: chunk golden fixtures, profile hashes, sqlite-vec smoke,
  source catch-up, read-only active database, lease, and atomic activation tests.
- `REQ-SEM-005`: labeled retrieval evaluation plus text/JSON v1 regression and
  JSON v2 schema fixtures.
- `REQ-SEM-006`: Context Pack fixtures proving deterministic sections and
  evidence controls remain unchanged.
- `REQ-SEM-007`: activation/lease tests proving automatic replaced-generation
  deletion and absence of rollback behavior.
- `REQ-SEM-008`: one physical report per verified Apple chip variant (M1);
  M2-M5 each have a pinned official artifact and installation-time local
  self-check coverage for integrity, authenticated loopback, alias,
  tokenization, embedding shape, CLS pooling, and L2 normalization, with no
  cross-chip fallback. Pre-release per-chip self-check reports are not a gate
  for experimental variants; evidence must not imply compatible or verified,
  performance, privacy, minimum-macOS, or operational-support guarantees.
  Unsupported behavior is covered for A18, non-Darwin, and other unlisted
  targets.
- `REQ-SEM-009`: fake/no-network default CI plus bounded, explicit real-runtime
  workflow and privacy-safe dated evidence.
- `REQ-SEM-010`: active-set evidence showing all three semantic ADRs are current,
  Accepted, and conflict-free before production integration or release.
- `REQ-SEM-011`: verified M1 host test on macOS 15.7.3 or later, recording cold
  readiness, warm single-input embedding P95, and peak RSS against the
  25 s / 35 ms / 1 GiB release envelope; experimental variants have no
  inherited resource or minimum-macOS claim.

## Release Gate Commands

Run these from a clean checkout:

```bash
go test ./...
git diff --check
go build ./cmd/worktrail
go install ./cmd/worktrail
```

For v1.0.0, record the exact candidate commit and the intended tag in the
clean-checkout validation record. A dirty-tree M1 gate can be retained as
supplemental runtime evidence but must not be relabeled as the release record.

Run isolated CLI smoke with disposable environment variables and a temporary
binary:

```bash
go build -o "$TMPDIR/worktrail-release-smoke/worktrail" ./cmd/worktrail
HOME="$TMPDIR/worktrail-release-smoke/home" \
WORKTRAIL_HOME="$TMPDIR/worktrail-release-smoke/worktrail-home" \
WORKTRAIL_PROJECT_ROOT="$TMPDIR/worktrail-release-smoke/project" \
"$TMPDIR/worktrail-release-smoke/worktrail" init
```

The dated validation record should include the full smoke sequence and results,
not private transcript content or local machine paths.
