# Semantic Validation Scripts

This directory contains release-engineering helpers for the bounded semantic
runtime gate. It is not part of normal `worktrail` execution and must never
publish, rehost, or install artifacts into a user's Worktrail cache.

`run-offline-gate.sh` is the default no-network check. It uses only committed
fixture captures and deterministic synthetic vectors, plus the table-hardening
fixture retrieval gate, a small fake refill capacity matrix (1000/2000/5000),
and JSON v2 table golden schema checks. Full 10k/50k/100k refill capacity and
clean-checkout M1 E2E remain step-7 release gates.

```bash
bash scripts/semantic/run-offline-gate.sh
python3 -m unittest discover -s scripts/semantic -p 'test_release_record.py'
bash scripts/semantic/test-production-e2e-archive-retry.sh
```

`run-production-e2e-gate.sh` is the isolated M1 production E2E release gate. It
builds temporary `worktrail` / `worktrail-semantic-eval` binaries, sets temporary
`HOME`, `WORKTRAIL_HOME`, and `WORKTRAIL_PROJECT_ROOT`, and never touches the
operator's real Worktrail cache or knowledge roots. Golden files under
`fixtures/production-e2e/` are compared read-only; the script never overwrites
them. Phases: `harness`, `offline`, `install`, `positive`, `fault`, `resource`,
or `all` (default).

At startup, the gate writes a complete dirty-tree source snapshot to its
temporary evidence root and the `all` phase copies it to
`evidence/semantic-m1/reports/m1-release-gate-2026-07-17-source-snapshot.json`.
Its input set is exactly: `HEAD`, the working-tree binary diff from `HEAD`, and
all untracked files. It excludes gate-generated or gate-overwritten evidence:
`evidence/semantic-m1/reports/**`,
`docs/worktrail-semantic-production-e2e-*.md`, and
`docs/worktrail-semantic-m1-release-evidence-*.md`. The same exclusions apply
to the porcelain status digest, tracked diff, and untracked inventory.

The snapshot's untracked inventory is reproducible: it obtains untracked paths
with `git ls-files --others --exclude-standard -z`, removes those exclusions,
byte-sorts the remaining paths, records each regular file's raw-byte SHA-256
(or each symlink's target-byte SHA-256), then SHA-256s a versioned
NUL-delimited sequence of path, kind, size, and content digest. Before the
gate completes, it re-computes the snapshot against the current checkout and
records the result in
`evidence/semantic-m1/reports/m1-release-gate-2026-07-17-source-snapshot-verification.json`.
The snapshot identifies the dirty gate input; it is not a clean-checkout
release record.

```bash
bash scripts/semantic/run-production-e2e-gate.sh harness   # offline harness only
bash scripts/semantic/run-production-e2e-gate.sh all       # full M1 gate
bash scripts/semantic/run-launchd-host-gate.sh             # real launchd Host gate
bash scripts/semantic/test-launchd-host-gate-cleanup.sh    # label/cleanup safety
bash scripts/semantic/test-production-e2e-gate-signals.sh  # EXIT/INT/TERM cleanup
bash scripts/semantic/test-archive-safety-scan.sh          # archive privacy checks
bash scripts/semantic/test-production-e2e-archive-retry.sh # staging/reuse/no-clobber
```

`run-launchd-host-gate.sh` 是独立的真实 macOS `launchd` 验收门槛。它默认创建
唯一 test label 和 `/tmp` 下的临时 HOME/cache/runtime/log roots，不操作正式
label；cleanup 只 bootout 本次 test label 并删除本次临时目录。它复用一份已验证
的 M1 bundle（默认读取当前用户 cache，也可显式设置
`WORKTRAIL_LAUNCHD_GATE_BUNDLE_SOURCE`），验证 20 个并发客户端只产生一个
Host/worker、跨 CLI 复用、1 分钟 idle 退出与冷恢复、worker/Host crash 恢复、
blocked outbound 下的 tokenize、以及 install/remove 幂等。真实 gate 会加载模型
并等待 idle timeout；普通本地校验只运行 cleanup 静态测试和 build-tag 编译检查。

The `all` phase prechecks the commit-named release archive under
`${XDG_DATA_HOME:-~/.local/share}/worktrail/release-archive/<commit>/` before
expensive resource work. A complete PASS archive with matching HEAD, allowlist,
and SHA256SUMS may be reused; an incomplete or illegal existing directory fails
immediately and is never overwritten. New archives are written to a 0700
staging directory, safety-scanned, checksummed, then atomically renamed.
Process traps remove only that staging directory.

The offline phase records `table hardening retrieval gate` and
`refill capacity matrix` into the release command ledger, validates
`search-json-v2-table.golden`, and runs `go test ./...`, `go vet ./...`,
`go build ./...`, and `git diff --check`. The fault phase executes the
controlled daemon unit tests;
`TestSupervisorStartRecoversFromEndpointBindRaceAndSavesAuthenticatedDescriptor`
exercises a deterministic endpoint-bind race and recovery. The E2E gate does
not claim that an unrelated random port-holder occupied the daemon's actual
ephemeral endpoint.

`run-table-dogfood.sh` drives privacy-safe rebuild/query dogfood from a private
manifest and 32-byte HMAC key under
`${XDG_DATA_HOME:-~/.local/share}/worktrail/validation/<candidate-commit>/`.
Neither the manifest nor the key is committed. Public evidence emits only opaque
query IDs, the manifest HMAC, pass/count/timing, and project aliases.

```bash
bash scripts/semantic/run-table-dogfood.sh \
  --manifest "$HOME/.local/share/worktrail/validation/<commit>/dogfood-manifest.json" \
  --worktrail-binary /absolute/path/to/worktrail
```

The `all` clean-checkout release gate writes one commit-named archive to
`${XDG_DATA_HOME:-~/.local/share}/worktrail/release-archive/<commit>/`; it
rejects temporary archive roots and a dirty checkout. A complete PASS archive
for the same HEAD may be reused; incomplete or illegal existing directories fail
without overwrite. The archive contains only `SUMMARY.md`,
`RELEASE-RECORD.json`, `SAFETY-SCAN.json`, and `SHA256SUMS`; it never exports
raw logs, API-key files, environment dumps, or full reports. The archive safety
scan rejects unexpected members, local absolute-path prefixes, common credential
patterns, non-UTF-8 files, and symlinks. `SHA256SUMS` covers every structured
payload record (the manifest itself is validated as a UTF-8 allowlisted member),
and the gate verifies every manifest entry before its final archive scan.

Release-engineering retrieval reports use:

```bash
go run ./cmd/worktrail-semantic-eval retrieval-report \
  --labels scripts/semantic/fixtures/production-e2e/labeled-queries.json \
  --rankings /path/to/explicit-rankings.json
```

Lane rankings must be collected explicitly (`collect-retrieval`); they must not
be inferred from public search JSON.

`retrieval-report` has lane-specific gates. RRF must meet the configured
Recall@K, MRR, and nDCG@K floors and, by default, match or beat entry FTS on all
three metrics. Governed results must meet the governed Recall@K floor and, by
default, preserve Recall@K relative to entry FTS. Governed MRR and nDCG@K are
always reported but intentionally are not release gates because governance can
promote current source-of-truth records.

The fixture providers and 4-dimensional vectors are deliberately synthetic.
They validate schemas, comparison math, CLI behavior, sqlite-vec cosine DDL,
and workflow wiring; they are not BGE-M3 quality evidence.

`run-real-darwin-m1-gate.sh` is the separately authorized M1 candidate gate. It
expects a caller-provided temporary directory containing the fixed Q8 GGUF,
reference model, virtual environment, and verified llama.app runtime:

```bash
bash scripts/semantic/run-real-darwin-m1-gate.sh "$TMPDIR/worktrail-semantic-gate"
```

It verifies the pinned candidate files, starts an authenticated loopback-only
runtime, captures FlagEmbedding and local API vectors, runs the parity
comparison, records the Python environment, and terminates the child process.
It does not publish, install a Worktrail bundle, modify `PATH`, or retain a
generation. The 2026-07-15 M1 result is summarized in the
[M1 engineering evidence index](../../evidence/semantic-m1/release-evidence-2026-07-17.md);
the detailed process spike remains recoverable from Git history.

`run-runtime-security-resource-gate.sh` validates the fixed local M1 runtime
under macOS `sandbox-exec` with outbound networking denied. It records cold
readiness, warm embedding latency, peak RSS, loopback-only sockets, and Mach-O
minimum-version metadata:

```bash
bash scripts/semantic/run-runtime-security-resource-gate.sh "$TMPDIR/worktrail-semantic-gate"
```

It is a release-validation harness, not production process supervision. The
2026-07-16 result is summarized in the
[M1 engineering evidence index](../../evidence/semantic-m1/release-evidence-2026-07-17.md);
the detailed process spike remains recoverable from Git history.
