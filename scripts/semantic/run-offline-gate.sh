#!/usr/bin/env bash
# Default no-network semantic gate for table-hardening offline checks.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

log() { printf '[offline-gate] %s\n' "$*"; }

log "focused semantic eval packages"
go test ./internal/semantic/eval ./cmd/worktrail-semantic-eval -count=1

bash scripts/semantic/test-archive-safety-scan.sh

log "parity fixture"
go run ./cmd/worktrail-semantic-eval parity \
  --cases testdata/semantic/parity-corpus-fixture.json \
  --reference testdata/semantic/parity-reference-fixture.json \
  --candidate testdata/semantic/parity-candidate-fixture.json

log "synthetic sqlite-vec"
go run ./cmd/worktrail-semantic-eval vec \
  --count 1000 \
  --dimension 1024 \
  --queries 10 \
  --limit 10

log "table hardening retrieval gate (fixture rankings)"
go run ./cmd/worktrail-semantic-eval retrieval-report \
  --labels testdata/semantic/table-retrieval-labels-fixture.json \
  --rankings testdata/semantic/table-retrieval-rankings-fixture.json \
  >/tmp/worktrail-offline-table-retrieval-report.json

python3 - <<'PY'
import json
from pathlib import Path
report=json.loads(Path("/tmp/worktrail-offline-table-retrieval-report.json").read_text(encoding="utf-8"))
assert report.get("passed") is True, report
assert report.get("evidence",{}).get("evidence_recall_at_k",0) >= 0.9, report
print("table_hardening_retrieval_gate PASS")
PY

log "small refill capacity matrix (logic proof; full 10k/50k/100k is step-7 M1)"
for size in 1000 2000 5000; do
  go run ./cmd/worktrail-semantic-eval refill-benchmark \
    --corpus-size "$size" \
    --queries 20 \
    --warmup 3 \
    --fake \
    >"/tmp/worktrail-offline-refill-${size}.json"
done

python3 - <<'PY'
import json
from pathlib import Path
for size in (1000, 2000, 5000):
    report=json.loads(Path(f"/tmp/worktrail-offline-refill-{size}.json").read_text(encoding="utf-8"))
    assert report.get("passed") is True, report
    assert all(lane.get("forced_refill_rounds", 0) >= 3 for lane in report.get("lanes", [])), report
print("refill_capacity_matrix_small PASS sizes=1000,2000,5000")
PY

log "JSON v2 table golden presence + schema"
python3 - <<'PY'
import json
from pathlib import Path
for path in (
    Path("internal/app/testdata/search-json-v2-table.golden"),
    Path("scripts/semantic/fixtures/production-e2e/search-json-v2-table.golden"),
):
    data=json.loads(path.read_text(encoding="utf-8"))
    assert data.get("schema") == "worktrail.search.results.v2", path
    assert data.get("results"), path
print("json_v2_table_golden PASS")
PY

log "offline gate PASS"
