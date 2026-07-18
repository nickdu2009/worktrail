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

Requirement text for `REQ-SEM-001` … `REQ-SEM-011` lives only in
[worktrail-release-acceptance.md](worktrail-release-acceptance.md). This
checklist tracks evidence types and execution status:

| Requirement | Evidence type |
| --- | --- |
| `REQ-SEM-001` | Trusted-manifest / bundle identity tests; M1 verified vs M2–M5 experimental classification |
| `REQ-SEM-002` | Init network-free / `--semantic` / `--no-semantic` / no implicit generation tests |
| `REQ-SEM-003` | Loopback lifecycle, identity mismatch, `auto` degrade / `required` fail tests |
| `REQ-SEM-004` | Chunk, profile, sqlite-vec, lease, activation tests |
| `REQ-SEM-005` | Labeled retrieval + text/JSON v1 / JSON v2 fixtures |
| `REQ-SEM-006` | Context Pack section/evidence fixtures |
| `REQ-SEM-007` | Replaced-generation cleanup / no-rollback tests |
| `REQ-SEM-008` | Verified M1 physical report; experimental self-check coverage (no cross-chip fallback) |
| `REQ-SEM-009` | Fake/no-network CI + dated privacy-safe real-runtime evidence |
| `REQ-SEM-010` | Three semantic ADRs Accepted and conflict-free |
| `REQ-SEM-011` | M1 resource envelope (cold / warm P95 / peak RSS); experimental has no inherited claim |

Current engineering evidence index:
[worktrail-semantic-m1-release-evidence-2026-07-17.md](worktrail-semantic-m1-release-evidence-2026-07-17.md)
(dirty-tree; not a clean-checkout release record).

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
