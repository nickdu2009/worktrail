# Worktrail Semantic Table Hardening Evidence (2026-07-21)

Status: offline gates, production budget-matrix freeze, privacy-safe dogfood
(step 6), plus daemon ubatch fix required for HardMax-768 embed.
Not a clean-checkout M1 release record. Does not rewrite the 2026-07-17 index.

## Scope

- Offline integration of table retrieval gate, small refill capacity matrix,
  JSON v2 table golden checks, release-archive staging/reuse contracts, and
  dogfood runner wiring.
- Serial rebuild dogfood on two real projects that previously failed oversized
  table generation (aliases only in public evidence).
- Production budget-matrix freeze decision for `DefaultBudget` / `DefaultPolicy`.
- Daemon physical `--ubatch-size` raised to 1024 so HardMax-768 inputs embed.

## Bundle identity

- BundleID: `3625d27700727578d7694ab04d19291efce45095aa57daba66d692a37a51be58`
- Gate assets verified by SHA-256 before expensive runs (model/runtime/compressed
  runtime under operator gate cache; absolute paths omitted here).
- Chip / support: `m1` / `verified` after `worktrail init --semantic`.

## Offline gate

Commands (no network / fake fixtures):

```bash
go test ./internal/semantic/eval ./cmd/worktrail-semantic-eval -count=1
go test ./... -count=1
go vet ./...
go build ./...
git diff --check
bash scripts/semantic/run-offline-gate.sh
python3 -m unittest discover -s scripts/semantic -p 'test_release_record.py'
bash scripts/semantic/test-production-e2e-archive-retry.sh
```

Results: **PASS** (scripts + focused Go tests + archive retry contract).

## Budget-matrix freeze

Command form (production path, not `--fake`):

```bash
go run ./cmd/worktrail-semantic-eval budget-matrix \
  --fixture-root scripts/semantic/fixtures/production-e2e \
  --labels scripts/semantic/fixtures/production-e2e/labeled-queries.json \
  --budgets 384:640:80,512:768:80,512:768:128,640:768:80 \
  --gate-assets-root "$WORKTRAIL_SEMANTIC_GATE_ROOT" \
  --candidate-root "${TMPDIR:-/tmp}/worktrail-budget-matrix"
```

Selection order: blocking gates first, then table Evidence Recall@10, table
governed nDCG@10, fewer total chunks, smaller HardMax, then numeric
`(Target,HardMax,MinPayload)` tie-break.

- All four candidates: PASS
- Selected budget: `(640, 768, 80)` — not the initial `(512, 768, 80)`
- Rationale after tied evidence/nDCG: fewer total chunks (13 vs 14)
- DefaultPolicy changed: **yes** (`DefaultBudget` Target 640 / HardMax 768 /
  MinPayload 80; BundleID unchanged)
- Commit: `80e6b13`
- Public thresholds only; machine-readable report retained outside Git

## Refill capacity matrix

- Small offline matrix sizes `1000/2000/5000` prove forced 50→100→200 refill
  wiring under `--fake` (covered by `run-offline-gate.sh`).
- Full release-blocking `10k/50k/100k` matrix is a step-7 M1 gate must-run item.
  Do not treat the small matrix as capacity acceptance.

## Daemon ubatch fix (dogfood blocker)

Cold dogfood rebuilds initially failed with CLI `semantic_profile_stale` while
the underlying error was embed daemon HTTP 500:

- llama.app default physical ubatch is **512**
- production HardMax is **768**
- inputs above 512 tokens returned:
  `input (...) is too large to process. increase the physical batch size`

Fix: pin `--ubatch-size 1024` on daemon serve args (BundleID unchanged).
Commit: `aa97d6a`. After restart, HardMax-768 chunks embed and rebuild activates.

## Dogfood (privacy-safe)

Private artifacts (not in Git):

- `${XDG_DATA_HOME:-$HOME/.local/share}/worktrail/validation/<candidate-commit>/dogfood-manifest.json`
- matching `dogfood-manifest.key` (0600, 32-byte CSPRNG, create-once)

Runner commit under test: `aa97d6a` (includes ubatch fix + offline gates).
Public runner summary: `passed=true`.

| Alias | rebuild scopes | entry / chunk / table-group | max tokens | elapsed (runner warm) | db size | saturation / error | formal knowledge changed |
| --- | --- | --- | --- | --- | --- | --- | --- |
| coi-forge | all, project | 85 / 1883 / 121 | 768 | all 41957 ms; project 43635 ms | 18452480 B | none | no |
| delivery-experts | all, project | 113 / 2573 / 78 | 764 | all 13862 ms; project 12170 ms | 24100864 B | none | no |

Cold first-pass timings (pre-runner, same binary lineage after ubatch fix):

- coi-forge project (empty reuse): ~201 s
- coi-forge all (user+project): ~52 s
- delivery-experts all: ~171 s
- delivery-experts project: ~15 s

Additional privacy-safe checks:

- User refresh across the two `--scope all` runs: same `snapshot_hash`,
  `profile_id`, `bundle_id`, entry/chunk/max-token counts; generation IDs differ.
  User-scope search result IDs equivalent (`count=3`).
- Opaque query IDs: `q-coi-table-hardening-1`, `q-de-table-hardening-1` — both
  `passed=true` (`result_count=10` each).
- Manifest HMAC SHA-256:
  `cefc670036b43d32bb1be2cc3f420614b4e00b202e64c41470b2d4b686220e5a`
- Previously failing large-table path completed (table chunks present;
  coi-forge includes `table_cell_fragment` + `table_row_group`).
- Absolute paths, usernames, source text, and query originals are omitted.

## Commits in this worktree lane

1. `11996ee` — `chore(semantic): wire table-hardening offline gates and dogfood runner`
2. `80e6b13` — `fix(semantic): freeze DefaultBudget from production budget-matrix`
3. `aa97d6a` — `fix(semantic): raise llama ubatch-size above HardMax`
4. (this doc) — `docs(semantic): record table-hardening dogfood and offline gate evidence`

## Step 7 must-run leftovers

- Full refill capacity matrix at 10k / 50k / 100k (do not forge PASS)
- Clean-checkout `bash scripts/semantic/run-production-e2e-gate.sh all`

## JSON v1 baseline refresh (Go 1.26 + table fixture)

Step-7 offline gate failed on `search-json-v1.golden` for two independent
reasons; both are intentional contract/corpus updates, not gate-script bugs.

1. **Go 1.26 `omitempty` + zero `time.Time`**: encoding/json no longer omits
   zero-value `time.Time` under `omitempty` alone, so search JSON emitted
   `created_at` / `expires_at` as `0001-01-01T00:00:00Z`. Fix: tag
   `Entry.CreatedAt` / `Entry.ExpiresAt` with `omitempty,omitzero` so the v1
   shape stays `{}`-compatible (keys absent when zero). Same omission applied
   to JSON v2 table goldens under `internal/app/testdata/` and
   `scripts/semantic/fixtures/production-e2e/`.
2. **Lexical score drift from table fixture**:
   `architecture/table-hardening-matrix.md` is intentionally merged into the
   production-e2e project corpus via `install_fixtures`, which changes BM25
   IDF. Golden score refreshed `21.829…` → `22.995…` for the same single-hit
   needle `e2e-prod-gate-needle-zx9` (still one `[]index.Result`, no
   `chunk_matches` / v2 schema).
3. **Legacy handoff fixture blocked `worktrail context`**: Handoff V2 rejects
   root `handoffs/*.md` until migration. The production-e2e handoff-themed
   retrieval fixture moved to `workflows/latest-release-gate.md` (same
   `entry_id`, tags, body) so offline context smoke and labeled retrieval stay
   green without a runtime handoff migration step.
