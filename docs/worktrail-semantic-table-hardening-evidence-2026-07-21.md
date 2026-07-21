# Worktrail Semantic Table Hardening Evidence (2026-07-21)

Status: offline gates and privacy-safe dogfood evidence (step 6).
Not a clean-checkout M1 release record. Does not rewrite the 2026-07-17 index.

## Scope

- Offline integration of table retrieval gate, small refill capacity matrix,
  JSON v2 table golden checks, release-archive staging/reuse contracts, and
  dogfood runner wiring.
- Serial rebuild dogfood on two real projects that previously failed oversized
  table generation (aliases only in public evidence).
- Production budget-matrix freeze decision for `DefaultBudget` / `DefaultPolicy`.

## Bundle identity

- BundleID: `3625d27700727578d7694ab04d19291efce45095aa57daba66d905a37a51be58`
- Gate assets verified by SHA-256 before expensive runs (model/runtime/compressed
  runtime under operator gate cache; absolute paths omitted here).

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

Results: filled after the step-6 validation pass.

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

- Selected budget: pending production run
- DefaultPolicy changed: pending production run
- Public thresholds only; machine-readable report retained outside Git

## Refill capacity matrix

- Small offline matrix sizes `1000/2000/5000` prove forced 50→100→200 refill
  wiring under `--fake`.
- Full release-blocking `10k/50k/100k` matrix is a step-7 M1 gate must-run item.
  Do not treat the small matrix as capacity acceptance.

## Dogfood (privacy-safe)

Private artifacts (not in Git):

- `${XDG_DATA_HOME:-$HOME/.local/share}/worktrail/validation/<candidate-commit>/dogfood-manifest.json`
- matching `dogfood-manifest.key` (0600, 32-byte CSPRNG, create-once)

Public summary fields only:

| Alias | rebuild scopes | entry/chunk/table-group | max tokens | elapsed | db size | saturation/error | formal knowledge changed |
| --- | --- | --- | --- | --- | --- | --- | --- |
| pending | pending | pending | pending | pending | pending | pending | pending |

- User refresh equivalence across the two project `--scope all` runs: pending
- Manifest HMAC / opaque query IDs: pending
- Absolute paths, usernames, source text, and query originals are omitted

## Step 7 must-run leftovers

- Full refill capacity matrix at 10k / 50k / 100k
- Clean-checkout `bash scripts/semantic/run-production-e2e-gate.sh all`
- Any DefaultPolicy identity follow-up if production budget-matrix selects a
  non-initial candidate after step-6 freeze handling
