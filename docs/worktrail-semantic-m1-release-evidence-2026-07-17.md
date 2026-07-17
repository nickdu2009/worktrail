# Apple M1 Semantic Recall Engineering Evidence — 2026-07-17

## Result and release boundary

**PASS (engineering evidence only)** — the complete production E2E gate passed
on an Apple M1 Pro (`darwin-arm64`, macOS 15.7.3) against the uncommitted source
snapshot rooted at commit `91907b1a7a6bdce0f8fd6f3ca3c3d91205f8eac4`.

This was deliberately a dirty-tree run. It verifies the M1 runtime behavior but
is **not** the clean-checkout, commit-identified v1.0.0 release record. It does
not close `REQ-REL-001`, authorize a commit or tag, or authorize publication.

## Reproducible dirty-tree source snapshot

The gate captured its defined source input at `2026-07-17T04:38:07Z` before
building temporary binaries. The complete 156-entry untracked inventory, every
entry's content digest, and the algorithm are in
`docs/semantic-e2e-evidence/m1-release-gate-2026-07-17-source-snapshot.json`.
The gate then recomputed every input field from the current checkout after
writing evidence; the successful result is in
`docs/semantic-e2e-evidence/m1-release-gate-2026-07-17-source-snapshot-verification.json`.

| Snapshot field | Value |
| --- | --- |
| Status porcelain v1 NUL-stream SHA-256 | `14dceaa4428375de839594ebc04c3dd844b1606da6bef7ad7573a055447e5e2a` |
| `git diff HEAD --binary --no-ext-diff` SHA-256 | `827ff09422a112f4e05464a1d86b74c16f7105f426cf81a172ba8d4bfd14459f` |
| Untracked inventory SHA-256 | `088acd8f92f793bb29cdf4167d5063b059a2adf6b4f0a7f3792fd845ca06f26d` |
| Untracked file count | 156 |

The source set is exactly `HEAD`, the working-tree binary diff from `HEAD`, and
untracked files. The gate excludes its own generated or overwritten evidence
from all three snapshot components:
`docs/semantic-e2e-evidence/**`,
`docs/worktrail-semantic-production-e2e-*.md`, and
`docs/worktrail-semantic-m1-release-evidence-*.md`.

The inventory algorithm is versioned as `worktrail-untracked-inventory.v1`:
obtain paths with `git ls-files --others --exclude-standard -z`, remove those
exclusions, sort bytewise, record each regular file's raw-byte SHA-256 (or each
symlink target's SHA-256), then hash a NUL/newline-delimited stream of the
version marker, path, kind, size, and content digest. The same exclusions apply
to the porcelain status digest and binary diff. This makes every recorded input
hash reproducible without treating later-created evidence as source input.

## Commands actually run

| Command or gate | Result |
| --- | --- |
| `WORKTRAIL_E2E_KEEP_TMP=0 bash scripts/semantic/run-production-e2e-gate.sh all` | PASS: offline, install, positive, fault, resource, and evidence phases |
| `go test ./...` | PASS |
| `go vet ./...` | PASS |
| `go build ./...` | PASS |
| `git diff --check` | PASS |
| `bash scripts/semantic/test-production-e2e-gate-signals.sh` | PASS: INT exits 130, TERM exits 143, temporary root removed |
| `go test ./internal/semantic/daemon -run TestSupervisorStartRecoversFromEndpointBindRaceAndSavesAuthenticatedDescriptor -count=1` | PASS |

The full E2E run exercised explicit `init --semantic`, rebuild/search/context
lifecycle, authenticated loopback runtime, sealed generations, concurrent
start, stale/force-killed daemon, bundle-tamper, and missing-generation recovery
paths. `go build ./...` ran in the E2E offline phase. Endpoint bind-race recovery
is covered by the controlled daemon unit test named above; this E2E record does
not claim that an unrelated random port-holder occupied the daemon's actual
ephemeral endpoint. The E2E trap removed the isolated temporary root, including
its runtime, model, daemon descriptor, and any temporary port files.

## Retrieval gates

The raw report is `docs/semantic-e2e-evidence/retrieval-report.json`. RRF met
its Recall@10/MRR/nDCG@10 floors and matched or exceeded entry FTS on all three
metrics. Governed retrieval met only its specified gate: Recall@10 at least
0.9 and no worse than entry FTS. Its MRR (`0.6759`) and nDCG@10 (`0.7547`) are
reported diagnostic values, not governed release thresholds.

## M1 resource envelope

| Measurement | Observed | Release limit | Result |
| --- | ---: | ---: | --- |
| Cold readiness | 1086.924708 ms | 25000 ms | PASS |
| Warm single-input embedding P95 | 12.265667 ms | 35 ms | PASS |
| Peak RSS | 829568 KiB | 1048576 KiB | PASS |

The full dated summary and copied raw reports are
`docs/worktrail-semantic-production-e2e-2026-07-17.md` and
`docs/semantic-e2e-evidence/`.

## Variant classification and remaining release work

M1 is **verified** by this evidence. M2, M3, M4, and M5 are **experimental**:
they retain their pinned artifacts and installation-time self-check coverage,
but no hardware report was required or produced. This record makes no
cross-chip compatibility, performance, privacy, minimum-macOS, or
operational-support claim for M2–M5.

Only user-authorized release operations remain: commit the reviewed candidate,
validate that exact commit in a clean checkout with clean before/after status,
create and validate the v1.0.0 tag, then perform any separate publication step.
