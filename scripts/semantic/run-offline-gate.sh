#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

go test ./internal/semantic/eval ./cmd/worktrail-semantic-eval

bash scripts/semantic/test-archive-safety-scan.sh

go run ./cmd/worktrail-semantic-eval parity \
  --cases testdata/semantic/parity-corpus-fixture.json \
  --reference testdata/semantic/parity-reference-fixture.json \
  --candidate testdata/semantic/parity-candidate-fixture.json

go run ./cmd/worktrail-semantic-eval vec \
  --count 1000 \
  --dimension 1024 \
  --queries 10 \
  --limit 10
