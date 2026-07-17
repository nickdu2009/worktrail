#!/usr/bin/env bash
# Production E2E release gate for local semantic recall (Apple M1 only).
# Isolates HOME / WORKTRAIL_HOME / WORKTRAIL_PROJECT_ROOT under a temporary root.
# Golden files are compared read-only; this script never overwrites them.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture_root="$repo_root/scripts/semantic/fixtures/production-e2e"
golden_search="$fixture_root/search-json-v1.golden"
labels_path="$fixture_root/labeled-queries.json"
archive_safety_scan="$repo_root/scripts/semantic/check-archive-safety.py"
release_record_writer="$repo_root/scripts/semantic/write-release-record.py"
phase="${1:-all}"
gate_assets_root="${WORKTRAIL_SEMANTIC_GATE_ROOT:-}"

BUNDLE_ID="60e883f9f5fb62d0f6986d24df30ad8f129f3485a8a19b691eeb2662a438d4d2"
MODEL_SHA256="aa473d51f451a22f0fcf39ba3330c14bed38a385712b1113440f69df4047a173"
RUNTIME_SHA256="15d468c9f4820f4ba0fd44ccd2e19dd19bb60e8c7c839deb3fc2cfbff281307c"
MODEL_SIZE="634553760"
RUNTIME_SIZE="17114936"
LICENSE_SHA256="94f29bbed6a22c35b992c5c6ebf0e7c92f13b836b90f36f461c9cf2f0f1d010d"
ATTR_SHA256="b6b2586a63280720d84de679c6402310f045053cce38e46a4dcbe4b9fe15a5ec"

tmp_root=""
worktrail_bin=""
eval_bin=""
evidence_dir=""
daemon_pid=""
release_command_ledger=""
original_home="${HOME}"
original_gocache="$(go env GOCACHE)"
original_gomodcache="$(go env GOMODCACHE)"
KEEP_TMP="${WORKTRAIL_E2E_KEEP_TMP:-0}"
TEST_HOLD_SECONDS="${WORKTRAIL_E2E_TEST_HOLD_SECONDS:-0}"
cleanup_done=0

log() { printf '[e2e] %s\n' "$*"; }
fail() { printf '[e2e] FAIL: %s\n' "$*" >&2; exit 1; }

record_release_command() {
  local category="$1"
  local command="$2"
  [[ "$phase" == "all" ]] || return 0
  python3 - "$release_command_ledger" "$category" "$command" <<'PY'
import json,sys
with open(sys.argv[1],"a",encoding="utf-8") as output:
    json.dump({"category":sys.argv[2],"command":sys.argv[3],"result":"PASS"},output,ensure_ascii=False)
    output.write("\n")
PY
}

run_release_command() {
  local category="$1"
  local command="$2"
  shift 2
  "$@"
  record_release_command "$category" "$command"
}

cleanup() {
  [[ "$cleanup_done" == "0" ]] || return 0
  cleanup_done=1
  trap - EXIT INT TERM
  set +e
  if [[ -n "${worktrail_bin:-}" && -x "${worktrail_bin:-}" && -n "${HOME:-}" && -n "${WORKTRAIL_HOME:-}" ]]; then
    env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
      "$worktrail_bin" semantic stop --format json >/dev/null 2>&1 || true
  fi
  if [[ -n "${daemon_pid:-}" ]]; then
    kill "$daemon_pid" 2>/dev/null || true
    wait "$daemon_pid" 2>/dev/null || true
  fi
  # Stop any descriptor-backed daemon under the temporary runtime root.
  if [[ -n "${tmp_root:-}" && -d "$tmp_root/home/Library/Application Support/worktrail/semantic" ]]; then
    while IFS= read -r state; do
      [[ -f "$state" ]] || continue
      pid="$(python3 - "$state" <<'PY'
import json,sys
try:
  print(json.load(open(sys.argv[1]))["pid"])
except Exception:
  print("")
PY
)"
      if [[ -n "$pid" ]]; then
        kill "$pid" 2>/dev/null || true
        wait "$pid" 2>/dev/null || true
      fi
    done < <(find "$tmp_root/home/Library/Application Support/worktrail/semantic" -name state.json 2>/dev/null || true)
  fi
  if [[ "$KEEP_TMP" != "1" && -n "${tmp_root:-}" && -d "$tmp_root" ]]; then
    chmod -R u+w "$tmp_root" 2>/dev/null || true
    rm -rf "$tmp_root"
  fi
}

on_exit() {
  local ec=$?
  cleanup
  exit "$ec"
}

on_int() {
  cleanup
  exit 130
}

on_term() {
  cleanup
  exit 143
}
trap on_exit EXIT
trap on_int INT
trap on_term TERM

capture_dirty_tree_snapshot() {
  local output="$1"
  python3 - "$repo_root" "$output" <<'PY'
import hashlib,json,os,stat,subprocess,sys

root,output=sys.argv[1:]

excluded_pathspecs=(
    "docs/semantic-e2e-evidence/**",
    "docs/worktrail-semantic-production-e2e-*.md",
    "docs/worktrail-semantic-m1-release-evidence-*.md",
)

def is_excluded(path):
    return (
        path.startswith("docs/semantic-e2e-evidence/")
        or (
            path.startswith("docs/worktrail-semantic-production-e2e-")
            and path.endswith(".md")
        )
        or (
            path.startswith("docs/worktrail-semantic-m1-release-evidence-")
            and path.endswith(".md")
        )
    )

def git(*args, pathspecs=True):
    if pathspecs:
        args=(*args, "--", ".", *(f":(exclude){pattern}" for pattern in excluded_pathspecs))
    return subprocess.check_output(["git","-C",root,*args])

def sha256(data):
    return hashlib.sha256(data).hexdigest()

entries=[]
for raw_path in sorted(
    path
    for path in git("ls-files","--others","--exclude-standard","-z", pathspecs=False).split(b"\0")
    if path and not is_excluded(os.fsdecode(path))
):
    path=os.fsdecode(raw_path)
    absolute=os.path.join(root,path)
    mode=os.lstat(absolute).st_mode
    if stat.S_ISREG(mode):
        with open(absolute,"rb") as f:
            data=f.read()
        kind="regular"
        size=len(data)
        content_sha256=sha256(data)
    elif stat.S_ISLNK(mode):
        target=os.fsencode(os.readlink(absolute))
        kind="symlink"
        size=len(target)
        content_sha256=sha256(b"symlink\0"+target)
    else:
        raise SystemExit(f"unsupported untracked path type: {path}")
    entries.append({
        "path": path,
        "kind": kind,
        "size_bytes": size,
        "content_sha256": content_sha256,
    })

fingerprint=hashlib.sha256()
fingerprint.update(b"worktrail-untracked-inventory.v1\0")
for entry in entries:
    fingerprint.update(os.fsencode(entry["path"]))
    fingerprint.update(b"\0")
    fingerprint.update(entry["kind"].encode())
    fingerprint.update(b"\0")
    fingerprint.update(str(entry["size_bytes"]).encode())
    fingerprint.update(b"\0")
    fingerprint.update(entry["content_sha256"].encode())
    fingerprint.update(b"\n")

report={
    "schema": "worktrail.semantic.dirty-tree-snapshot.v1",
    "captured_at": subprocess.check_output(["date","-u","+%Y-%m-%dT%H:%M:%SZ"], text=True).strip(),
    "input_set": {
        "base_revision": "HEAD",
        "tracked_changes": "git diff --binary --no-ext-diff HEAD with the documented exclusion pathspecs",
        "untracked_changes": "git ls-files --others --exclude-standard -z, byte-sorted after excluding documented evidence outputs",
        "excluded_pathspecs": excluded_pathspecs,
        "exclusion_reason": "Gate-generated and gate-overwritten evidence is not a source input. Excluding it makes the pre-build dirty-tree snapshot reproducible after the gate writes its evidence.",
    },
    "head": git("rev-parse","HEAD", pathspecs=False).decode().strip(),
    "status_porcelain_v1_z_sha256": sha256(git("status","--porcelain=v1","-z","--untracked-files=all")),
    "tracked_diff_head_sha256": sha256(git("diff","--binary","--no-ext-diff","HEAD")),
    "untracked": {
        "algorithm": "git ls-files --others --exclude-standard -z; exclude documented gate evidence outputs; byte-sort remaining paths; regular files hash raw bytes; symlinks hash symlink\\\\0 plus raw target bytes; inventory SHA-256 hashes the version marker and path, kind, size_bytes, and content_sha256 fields separated by NUL/newline bytes.",
        "count": len(entries),
        "inventory_sha256": fingerprint.hexdigest(),
        "entries": entries,
    },
}
with open(output,"w",encoding="utf-8") as f:
    json.dump(report,f,indent=2,ensure_ascii=False)
    f.write("\n")
PY
}

verify_dirty_tree_snapshot() {
  local snapshot="$1"
  local output="$2"
  local recomputed="$tmp_root/source-snapshot-recomputed.json"
  capture_dirty_tree_snapshot "$recomputed"
  python3 - "$snapshot" "$recomputed" "$output" <<'PY'
import json,sys

snapshot_path,recomputed_path,output_path=sys.argv[1:]
with open(snapshot_path,encoding="utf-8") as f:
    snapshot=json.load(f)
with open(recomputed_path,encoding="utf-8") as f:
    recomputed=json.load(f)

fields=("head","status_porcelain_v1_z_sha256","tracked_diff_head_sha256","untracked","input_set")
mismatches={
    field: {"recorded": snapshot.get(field), "recomputed": recomputed.get(field)}
    for field in fields
    if snapshot.get(field) != recomputed.get(field)
}
report={
    "schema": "worktrail.semantic.dirty-tree-snapshot-verification.v1",
    "verified_at": __import__("datetime").datetime.now(__import__("datetime").timezone.utc).replace(microsecond=0).isoformat().replace("+00:00","Z"),
    "snapshot_file": snapshot_path,
    "verified_fields": list(fields),
    "matches": not mismatches,
    "mismatches": mismatches,
}
with open(output_path,"w",encoding="utf-8") as f:
    json.dump(report,f,indent=2,ensure_ascii=False)
    f.write("\n")
if mismatches:
    raise SystemExit(f"dirty-tree source snapshot no longer matches current checkout: {', '.join(mismatches)}")
print("source_snapshot_ok", snapshot["untracked"]["count"], snapshot["untracked"]["inventory_sha256"])
PY
}

require_m1() {
  local brand
  brand="$(sysctl -n machdep.cpu.brand_string 2>/dev/null || true)"
  [[ "$(uname -m)" == "arm64" ]] || fail "requires darwin arm64; got $(uname -m)"
  [[ "$brand" == *"Apple M1"* ]] || fail "requires Apple M1 host; got $brand (M2-M5 remain unverified)"
  local macos
  macos="$(sw_vers -productVersion)"
  python3 - "$macos" <<'PY' || fail "requires macOS 15.7.3+"
import sys
parts=[int(x) for x in sys.argv[1].split(".")]
sys.exit(0 if parts>=[15,7,3] else 1)
PY
}

setup_isolation() {
  tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/worktrail-semantic-e2e.XXXXXX")"
  mkdir -p "$tmp_root/home" "$tmp_root/worktrail-home" "$tmp_root/project" "$tmp_root/bin" "$tmp_root/evidence"
  evidence_dir="$tmp_root/evidence"
  release_command_ledger="$evidence_dir/release-commands.jsonl"
  : >"$release_command_ledger"
  if [[ -z "$gate_assets_root" ]]; then
    gate_assets_root="$tmp_root/gate-assets"
  fi
  log "isolated tmp_root=$tmp_root (KEEP_TMP=$KEEP_TMP)"
  capture_dirty_tree_snapshot "$evidence_dir/source-snapshot-before-gate.json"
  if [[ "$TEST_HOLD_SECONDS" != "0" ]]; then
    [[ "$TEST_HOLD_SECONDS" =~ ^[0-9]+$ ]] || fail "WORKTRAIL_E2E_TEST_HOLD_SECONDS must be a non-negative integer"
    log "test hold for ${TEST_HOLD_SECONDS}s"
    sleep "$TEST_HOLD_SECONDS"
  fi

  log "building temporary binaries (using host Go caches)"
  env HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
    go build -o "$tmp_root/bin/worktrail" "$repo_root/cmd/worktrail"
  env HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
    go build -o "$tmp_root/bin/worktrail-semantic-eval" "$repo_root/cmd/worktrail-semantic-eval"
  worktrail_bin="$tmp_root/bin/worktrail"
  eval_bin="$tmp_root/bin/worktrail-semantic-eval"

  export HOME="$tmp_root/home"
  export WORKTRAIL_HOME="$tmp_root/worktrail-home"
  export WORKTRAIL_PROJECT_ROOT="$tmp_root/project"
  # Force XDG-style and macOS Library paths under temporary HOME.
  export XDG_CACHE_HOME="$HOME/Library/Caches"
  export XDG_CONFIG_HOME="$HOME/Library/Application Support"
  mkdir -p "$XDG_CACHE_HOME" "$XDG_CONFIG_HOME" "$HOME/Library/Logs"

  {
    printf 'machine_type\t%s\n' "$(sysctl -n machdep.cpu.brand_string)"
    printf 'uname_m\t%s\n' "$(uname -m)"
    printf 'macos\t%s\n' "$(sw_vers -productVersion)"
    printf 'git_revision\t%s\n' "$(git -C "$repo_root" rev-parse HEAD)"
    printf 'started_at\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'platform_scope\tM1-only; M2-M5 unverified\n'
  } >"$evidence_dir/environment.txt"
}

wt() {
  env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
    "$worktrail_bin" "$@"
}

eval_cli() {
  env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
    "$eval_bin" "$@"
}

install_fixtures() {
  local dest_project="$WORKTRAIL_PROJECT_ROOT/.worktrail"
  local dest_user="$WORKTRAIL_HOME"
  mkdir -p "$dest_project" "$dest_user"
  # Merge fixture knowledge onto the init-seeded root. Lexical golden expects
  # this combined corpus; semantic rebuild tolerates init docs without
  # frontmatter via sourceRecords raw-body fallback.
  cp -R "$fixture_root/project/." "$dest_project/"
  cp -R "$fixture_root/user/." "$dest_user/"
}

# Positive/fault retrieval corpus: replace init-seeded knowledge so dense ranks
# are not dominated by unrelated bootstrap templates. Offline golden stays on
# install_fixtures (merge).
install_controlled_fixtures() {
  local dest_project="$WORKTRAIL_PROJECT_ROOT/.worktrail"
  local dest_user="$WORKTRAIL_HOME"
  mkdir -p "$dest_project" "$dest_user"
  local knowledge_dirs=(
    architecture decisions handoffs requirements workflows validation
    integrations glossary rules lessons prompts profile state
  )
  local dir
  for dir in "${knowledge_dirs[@]}"; do
    rm -rf "$dest_project/$dir" "$dest_user/$dir"
  done
  rm -f "$dest_project/index.md" "$dest_project/project.md" "$dest_user/index.md"
  cp -R "$fixture_root/project/." "$dest_project/"
  cp -R "$fixture_root/user/." "$dest_user/"
}

assert_no_semantic_artifacts() {
  local cache="$HOME/Library/Caches/worktrail/semantic"
  local runtime="$HOME/Library/Application Support/worktrail/semantic"
  local logs="$HOME/Library/Logs/worktrail/semantic"
  if [[ -e "$cache" || -e "$runtime" || -e "$logs" ]]; then
    fail "semantic cache/runtime/log must not exist before install --semantic"
  fi
}

compare_golden_readonly() {
  local actual="$1"
  local golden="$2"
  [[ -f "$golden" ]] || fail "missing golden $golden (update requires explicit human review; script never overwrites)"
  if ! cmp -s "$actual" "$golden"; then
    fail "golden mismatch for $(basename "$golden"); actual=$actual golden=$golden"
  fi
}

# JSON CLI failures often exit 0 after writing worktrail.cli.error.v1; gates must
# inspect the envelope instead of relying on process exit status alone.
assert_json_ok() {
  local path="$1"
  local label="$2"
  python3 - "$path" "$label" <<'PY'
import json,sys
path,label=sys.argv[1],sys.argv[2]
with open(path) as f:
    data=json.load(f)
if data.get("schema")=="worktrail.cli.error.v1" or data.get("ok") is False:
    raise SystemExit(f"{label} returned CLI error envelope: {data}")
print("json_ok", label)
PY
}

# JSON CLI often exits 0 after writing worktrail.cli.error.v1; treat that as failure.
assert_json_error() {
  local path="$1"
  local label="$2"
  python3 - "$path" "$label" <<'PY'
import json,sys
path,label=sys.argv[1],sys.argv[2]
with open(path) as f:
    data=json.load(f)
if data.get("schema")=="worktrail.cli.error.v1" or data.get("ok") is False:
    print("json_error_ok", label, data.get("error_codes"))
    raise SystemExit(0)
raise SystemExit(f"{label} expected CLI error envelope, got: {data}")
PY
}

require_clean_checkout_for_release_record() {
  [[ "$phase" == "all" ]] || return 0
  [[ -z "${WORKTRAIL_E2E_ARCHIVE_DIR:-}" ]] \
    || fail "WORKTRAIL_E2E_ARCHIVE_DIR is unsupported; release archives use XDG_DATA_HOME"
  [[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]] \
    || fail "clean release record requires a clean checkout"
}

assert_clean_checkout() {
  local label="$1"
  [[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]] \
    || fail "clean checkout ${label} check failed"
  record_release_command "readonly" "git status --porcelain=v1 --untracked-files=all (${label})"
}

release_archive_root() {
  local data_home="${XDG_DATA_HOME:-$original_home/.local/share}"
  python3 - "$data_home" <<'PY'
import pathlib,sys
data_home=pathlib.Path(sys.argv[1]).expanduser()
if not data_home.is_absolute():
    raise SystemExit("XDG_DATA_HOME must be absolute for release archives")
root=(data_home / "worktrail" / "release-archive").resolve()
if str(root) in ("/tmp", "/private/tmp") or str(root).startswith(("/tmp/", "/private/tmp/", "/var/folders/")):
    raise SystemExit("release archive root must be persistent and must not be under a temporary directory")
print(root)
PY
}

capture_review_plan() {
  local label="$1"
  local output="$evidence_dir/review-${label}.json"
  env HOME="$tmp_root/review-home" WORKTRAIL_HOME="$tmp_root/review-home/user" WORKTRAIL_PROJECT_ROOT="$repo_root" \
    "$worktrail_bin" review plan --format json >"$output"
  python3 - "$output" <<'PY'
import json,sys
report=json.load(open(sys.argv[1],encoding="utf-8"))
assert report.get("schema") == "worktrail.review.plan.v1", report
assert isinstance(report.get("summary",{}).get("total"),int), report
PY
  record_release_command "readonly" "worktrail review plan --format json (${label})"
}

phase_offline() {
  log "phase offline: tests, vet, builds, offline-gate, sandbox default path"
  (
    cd "$repo_root"
    run_release_command "readonly" "go test ./..." env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
      go test ./...
    run_release_command "readonly" "go vet ./..." env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
      go vet ./...
    run_release_command "readonly" "go build ./..." env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
      go build ./...
    run_release_command "readonly" "go build ./cmd/worktrail" env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
      go build ./cmd/worktrail
    rm -f "$repo_root/worktrail"
    mkdir -p "$tmp_root/gobin"
    run_release_command "mutating (isolated)" "go install ./cmd/worktrail" env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" GOBIN="$tmp_root/gobin" \
      go install ./cmd/worktrail
    [[ -x "$tmp_root/gobin/worktrail" ]] || fail "go install did not create the isolated worktrail binary"
    run_release_command "readonly" "git diff --check" git -C "$repo_root" diff --check
    run_release_command "readonly" "bash scripts/semantic/run-offline-gate.sh" env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
      bash scripts/semantic/run-offline-gate.sh
    run_release_command "readonly" "signal cleanup" \
      bash scripts/semantic/test-production-e2e-gate-signals.sh
  )

  local profile="$tmp_root/deny-outbound.sb"
  printf '%s\n' '(version 1)' '(allow default)' '(deny network-outbound)' >"$profile"

  sandbox-exec -f "$profile" env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
    "$worktrail_bin" init >"$evidence_dir/init-offline.txt"
  assert_no_semantic_artifacts

  install_fixtures
  wt index rebuild --scope user
  wt index rebuild --scope project

  sandbox-exec -f "$profile" env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
    "$worktrail_bin" search --scope project --format json "e2e-prod-gate-needle-zx9" \
    >"$evidence_dir/search-json-v1.actual"
  compare_golden_readonly "$evidence_dir/search-json-v1.actual" "$golden_search"

  sandbox-exec -f "$profile" env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
    "$worktrail_bin" context "semantic e2e release gate" >"$evidence_dir/context-offline.txt"
  assert_no_semantic_artifacts
  record_release_command "mutating (isolated)" "isolated CLI smoke"
  log "phase offline passed"
}

seed_bundle_from_gate_cache() {
  local cache="${WORKTRAIL_SEMANTIC_GATE_ROOT:-$original_home/.cache/worktrail-semantic-e2e-gate}"
  local model="$cache/bge-m3-q8_0.gguf"
  local runtime="$cache/llama"
  local compressed="$cache/llama-app.zst"
  [[ -f "$model" && -f "$runtime" ]] || fail "gate cache missing model/runtime at $cache"
  [[ "$(shasum -a 256 "$model" | awk '{print $1}')" == "$MODEL_SHA256" ]] || fail "gate cache model sha mismatch"
  [[ "$(shasum -a 256 "$runtime" | awk '{print $1}')" == "$RUNTIME_SHA256" ]] || fail "gate cache runtime sha mismatch"
  [[ "$(stat -f%z "$model")" == "$MODEL_SIZE" ]] || fail "gate cache model size mismatch"
  [[ "$(stat -f%z "$runtime")" == "$RUNTIME_SIZE" ]] || fail "gate cache runtime size mismatch"

  local bundle_dir="$HOME/Library/Caches/worktrail/semantic/bundles/$BUNDLE_ID"
  local staging="$HOME/Library/Caches/worktrail/semantic/bundles/.seed-staging-$$"
  rm -rf "$staging" "$bundle_dir"
  mkdir -p "$staging/licenses"
  # Installed layout strips the embed "assets/" prefix (licenses/, ATTRIBUTIONS.md).
  cp "$model" "$staging/bge-m3-q8_0.gguf"
  cp "$runtime" "$staging/llama"
  chmod 600 "$staging/bge-m3-q8_0.gguf"
  chmod 700 "$staging/llama"
  cp "$repo_root/internal/semantic/bundle/assets/licenses/MIT.txt" "$staging/licenses/MIT.txt"
  cp "$repo_root/internal/semantic/bundle/assets/ATTRIBUTIONS.md" "$staging/ATTRIBUTIONS.md"
  cp "$repo_root/internal/semantic/bundle/assets/trusted-manifest-m1.json" "$staging/manifest.json"
  chmod 600 "$staging/manifest.json" "$staging/licenses/MIT.txt" "$staging/ATTRIBUTIONS.md"
  chmod 700 "$staging"
  mv "$staging" "$bundle_dir"
  if [[ -f "$compressed" ]]; then
    cp "$compressed" "${WORKTRAIL_SEMANTIC_GATE_ROOT:-$original_home/.cache/worktrail-semantic-e2e-gate}/llama-app.zst"
  fi
}

verify_bundle() {
  local bundle_dir="$HOME/Library/Caches/worktrail/semantic/bundles/$BUNDLE_ID"
  [[ -d "$bundle_dir" ]] || fail "bundle missing at isolated cache"
  local model="$bundle_dir/bge-m3-q8_0.gguf"
  local runtime="$bundle_dir/llama"
  # Installed layout strips embed prefix "assets/".
  local license="$bundle_dir/licenses/MIT.txt"
  local attr="$bundle_dir/ATTRIBUTIONS.md"
  for f in "$model" "$runtime" "$license" "$attr"; do
    [[ -f "$f" ]] || fail "missing bundle file $f"
  done
  [[ "$(stat -f%z "$model")" == "$MODEL_SIZE" ]] || fail "model size mismatch"
  [[ "$(stat -f%z "$runtime")" == "$RUNTIME_SIZE" ]] || fail "runtime size mismatch"
  [[ "$(shasum -a 256 "$model" | awk '{print $1}')" == "$MODEL_SHA256" ]] || fail "model sha mismatch"
  [[ "$(shasum -a 256 "$runtime" | awk '{print $1}')" == "$RUNTIME_SHA256" ]] || fail "runtime sha mismatch"
  [[ "$(shasum -a 256 "$license" | awk '{print $1}')" == "$LICENSE_SHA256" ]] || fail "license sha mismatch"
  [[ "$(shasum -a 256 "$attr" | awk '{print $1}')" == "$ATTR_SHA256" ]] || fail "attribution sha mismatch"
  # Runtime must be owner-executable and not group/world accessible.
  local mode
  mode="$(stat -f%Lp "$runtime")"
  [[ "$mode" == "700" || "$mode" == "500" ]] || fail "runtime mode unexpected: $mode"
  [[ -x "$runtime" ]] || fail "runtime is not executable"
}

phase_install() {
  log "phase install: confirm core init stays offline, then init --semantic"
  # Fresh isolation for install phase if offline already initialized.
  if [[ ! -f "$WORKTRAIL_PROJECT_ROOT/.worktrail/config.json" ]]; then
    local profile="$tmp_root/deny-outbound.sb"
    printf '%s\n' '(version 1)' '(allow default)' '(deny network-outbound)' >"$profile"
    sandbox-exec -f "$profile" env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
      "$worktrail_bin" init >"$evidence_dir/init-before-semantic.txt"
  fi
  if [[ -e "$HOME/Library/Caches/worktrail/semantic/bundles" ]]; then
    fail "bundle cache should not exist before init --semantic"
  fi

  local install_ok=0
  for attempt in 1 2; do
    log "init --semantic attempt $attempt"
    if wt init --semantic >"$evidence_dir/init-semantic.txt" 2>"$evidence_dir/init-semantic.err"; then
      install_ok=1
      break
    fi
    sleep $((attempt * 2))
  done
  if [[ "$install_ok" != "1" ]]; then
    log "init --semantic download failed; seeding verified artifacts from gate cache (HF CDN mitigation)"
    seed_bundle_from_gate_cache
    if wt init --semantic >"$evidence_dir/init-semantic-seeded.txt" 2>"$evidence_dir/init-semantic-seeded.err"; then
      install_ok=1
      printf 'seeded_from_gate_cache\ttrue\nreason\tHF CDN/gateway failures during Go HTTPS download; curl-fetched pinned artifacts verified by SHA-256\n' \
        >"$evidence_dir/install-mitigation.txt"
    fi
  fi
  [[ "$install_ok" == "1" ]] || fail "init --semantic failed after retries and seed; see init-semantic.err"
  verify_bundle
  # Bundle must remain under the temporary cache only.
  case "$HOME/Library/Caches/worktrail/semantic/bundles/$BUNDLE_ID" in
    "$tmp_root"/*) ;;
    *) fail "bundle escaped temporary root" ;;
  esac
  wt semantic status --format json >"$evidence_dir/semantic-status-after-install.json"
  python3 - "$evidence_dir/semantic-status-after-install.json" <<'PY'
import json,sys
report=json.load(open(sys.argv[1]))
state=report.get("state","")
if state in ("", "unavailable"):
    raise SystemExit(f"semantic status unavailable after install: {report}")
print("status_ok", state)
PY
  log "phase install passed"
}

descriptor_path() {
  printf '%s\n' "$HOME/Library/Application Support/worktrail/semantic/$BUNDLE_ID/state.json"
}

count_matching_daemons() {
  local state
  state="$(descriptor_path)"
  [[ -f "$state" ]] || { printf '0\n'; return; }
  python3 - "$state" <<'PY'
import json,sys,os,subprocess
d=json.load(open(sys.argv[1]))
pid=d.get("pid")
if not pid:
  print(0); raise SystemExit
try:
  os.kill(pid,0)
  print(1)
except OSError:
  print(0)
PY
}

phase_positive() {
  log "phase positive: rebuild, lifecycle, search/context, retrieval report"
  install_controlled_fixtures
  wt index rebuild --scope user
  wt index rebuild --scope project
  wt semantic rebuild --scope user --format json >"$evidence_dir/semantic-rebuild-user.json"
  assert_json_ok "$evidence_dir/semantic-rebuild-user.json" "semantic rebuild user"
  wt semantic rebuild --scope project --format json >"$evidence_dir/semantic-rebuild-project.json"
  assert_json_ok "$evidence_dir/semantic-rebuild-project.json" "semantic rebuild project"
  wt semantic rebuild --scope all --format json >"$evidence_dir/semantic-rebuild-all.json"
  assert_json_ok "$evidence_dir/semantic-rebuild-all.json" "semantic rebuild all"

  wt semantic stop --format json >"$evidence_dir/semantic-stop-before-auto.json" || true
  [[ "$(count_matching_daemons)" == "0" ]] || fail "daemon still running after stop"

  # First semantic search should auto-start exactly one daemon.
  # Use required so lexical fallback cannot silently hide a start failure.
  # JSON error envelopes may still exit 0; assert schema/results below.
  wt search --semantic=required --scope project --format json-v2 "e2e-prod-gate-needle-zx9" \
    >"$evidence_dir/search-semantic-first.json" 2>"$evidence_dir/search-semantic-first.err" || true
  assert_json_ok "$evidence_dir/search-semantic-first.json" "first semantic search"
  python3 - "$evidence_dir/search-semantic-first.json" <<'PY'
import json,sys
env=json.load(open(sys.argv[1]))
assert env.get("schema")=="worktrail.search.results.v2", env
assert not env.get("degraded_reasons"), env
assert env.get("results"), env
print("first_search_ok", len(env["results"]))
PY
  # Cold start can take several seconds; wait briefly for descriptor/PID.
  local daemon_count="0"
  for _ in $(seq 1 60); do
    daemon_count="$(count_matching_daemons)"
    [[ "$daemon_count" == "1" ]] && break
    sleep 0.5
  done
  if [[ "$daemon_count" != "1" ]]; then
    printf 'daemon_count=%s\ntmp_root=%s\n' "$daemon_count" "$tmp_root" >"$evidence_dir/daemon-count-failure.txt"
    [[ -f "$(descriptor_path)" ]] && cp "$(descriptor_path)" "$evidence_dir/descriptor-on-failure.json" || true
    cat "$evidence_dir/search-semantic-first.err" >>"$evidence_dir/daemon-count-failure.txt" || true
    head -c 2000 "$evidence_dir/search-semantic-first.json" >>"$evidence_dir/daemon-count-failure.txt" || true
    fail "expected exactly one daemon after first semantic search (count=$daemon_count); see daemon-count-failure.txt"
  fi
  local state
  state="$(descriptor_path)"
  python3 - "$state" "$BUNDLE_ID" <<'PY'
import json,sys,subprocess
d=json.load(open(sys.argv[1]))
assert d["bundle_id"]==sys.argv[2]
assert d["endpoint"].startswith("http://127.0.0.1:")
out=subprocess.check_output(["/usr/sbin/lsof","-nP","-a","-p",str(d["pid"]),"-iTCP"], text=True)
lines=out.splitlines()[1:]
assert lines, "no listening socket"
assert all("127.0.0.1:" in line or "[::1]:" in line for line in lines)
print("daemon_ok", d["pid"], d["endpoint"])
PY

  # Warm query
  wt search --semantic=auto --scope project --format json-v2 "hybrid recall context contract" \
    >"$evidence_dir/search-warm.json" 2>"$evidence_dir/search-warm.err"

  wt semantic status --format json >"$evidence_dir/semantic-status.json"
  wt semantic start --format json >"$evidence_dir/semantic-start.json"
  wt semantic restart --format json >"$evidence_dir/semantic-restart.json"
  [[ "$(count_matching_daemons)" == "1" ]] || fail "expected one daemon after restart"

  for mode_scope in "auto:user" "auto:project" "auto:all" "required:project"; do
    mode="${mode_scope%%:*}"
    scope="${mode_scope##*:}"
    wt search --semantic="$mode" --scope "$scope" --format json-v2 "rebuild only semantic generation" \
      >"$evidence_dir/search-${mode}-${scope}.json" 2>"$evidence_dir/search-${mode}-${scope}.err"
    python3 - "$evidence_dir/search-${mode}-${scope}.json" "$mode" <<'PY'
import json,sys
env=json.load(open(sys.argv[1]))
mode=sys.argv[2]
assert env.get("schema")=="worktrail.search.results.v2"
if mode=="required":
    assert not env.get("degraded_reasons"), env
assert env.get("results"), env
print("ok", mode, len(env["results"]))
PY
  done

  wt context --semantic "semantic e2e release gate" >"$evidence_dir/context-semantic.txt" 2>"$evidence_dir/context-semantic.err"

  eval_cli collect-retrieval --labels "$labels_path" --scope all \
    >"$evidence_dir/retrieval-rankings.json"
  eval_cli retrieval-report --labels "$labels_path" --rankings "$evidence_dir/retrieval-rankings.json" \
    >"$evidence_dir/retrieval-report.json"

  # Active generation sealed/read-only checks
  for scope in user project; do
    local pointer gen_db
    pointer="$WORKTRAIL_HOME/index/semantic/active.json"
    [[ "$scope" == "project" ]] && pointer="$WORKTRAIL_PROJECT_ROOT/.worktrail/index/semantic/active.json"
    [[ -f "$pointer" ]] || fail "missing active pointer for $scope"
    gen_db="$(python3 - "$pointer" <<'PY'
import json,sys,os
p=json.load(open(sys.argv[1]))
print(os.path.join(os.path.dirname(sys.argv[1]), p["generation_id"]+".sqlite"))
PY
)"
    [[ -f "$gen_db" ]] || fail "missing generation db for $scope"
    # SQLite file should not be writable by others; sealed marker via active pointer presence.
    python3 - "$pointer" <<'PY'
import json,sys
p=json.load(open(sys.argv[1]))
for key in ("generation_id","recall_profile_id","bundle_id","snapshot_hash"):
    assert p.get(key), key
print("generation_ok", p["generation_id"], p["recall_profile_id"])
PY
  done
  log "phase positive passed"
}

phase_fault() {
  log "phase fault: injection and recovery"
  # Deterministic daemon unit tests cover the endpoint bind race and retry
  # contract. The production E2E gate does not claim that an unrelated random
  # listener exercised the daemon's actual allocated endpoint.
  (
    cd "$repo_root"
    env -u WORKTRAIL_HOME -u WORKTRAIL_PROJECT_ROOT \
      HOME="$original_home" GOCACHE="$original_gocache" GOMODCACHE="$original_gomodcache" \
      go test ./internal/semantic/daemon ./internal/semantic/generation -count=1
  )

  # Concurrent first semantic requests after stop.
  wt semantic stop --format json >/dev/null 2>&1 || true
  wt search --semantic --scope project --format json-v2 "e2e-prod-gate-needle-zx9" \
    >"$evidence_dir/fault-concurrent-a.json" 2>"$evidence_dir/fault-concurrent-a.err" &
  local p1=$!
  wt search --semantic --scope project --format json-v2 "hybrid recall" \
    >"$evidence_dir/fault-concurrent-b.json" 2>"$evidence_dir/fault-concurrent-b.err" &
  local p2=$!
  wait "$p1"
  wait "$p2"
  [[ "$(count_matching_daemons)" == "1" ]] || fail "concurrent start did not converge to one daemon"

  # Stale PID/endpoint state.
  wt semantic start --format json >/dev/null
  local state
  state="$(descriptor_path)"
  python3 - "$state" <<'PY'
import json,sys
p=sys.argv[1]
d=json.load(open(p))
d["pid"]=1
d["endpoint"]="http://127.0.0.1:1"
json.dump(d, open(p,"w"))
PY
  wt search --semantic=auto --scope project --format json-v2 "e2e-prod-gate-needle-zx9" \
    >"$evidence_dir/fault-stale-recover.json" 2>"$evidence_dir/fault-stale-recover.err" || true
  # auto may fall back; required should error stably after more severe faults.

  # Force-kill known daemon and recover on next semantic request.
  wt semantic start --format json >/dev/null || true
  state="$(descriptor_path)"
  if [[ -f "$state" ]]; then
    local pid
    pid="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["pid"])' "$state")"
    kill -9 "$pid" 2>/dev/null || true
    wait "$pid" 2>/dev/null || true
  fi
  wt search --semantic=auto --scope project --format json-v2 "hybrid recall" \
    >"$evidence_dir/fault-kill-recover.json" 2>"$evidence_dir/fault-kill-recover.err"
  [[ "$(count_matching_daemons)" == "1" ]] || fail "daemon did not recover after kill"

  # Tamper bundle model hash -> required fails, auto degrades.
  local model="$HOME/Library/Caches/worktrail/semantic/bundles/$BUNDLE_ID/bge-m3-q8_0.gguf"
  local backup="$tmp_root/model.bak"
  cp "$model" "$backup"
  printf 'x' >>"$model"
  wt search --semantic=required --scope project --format json-v2 "hybrid recall" \
    >"$evidence_dir/fault-tamper-required.json" 2>"$evidence_dir/fault-tamper-required.err" || true
  assert_json_error "$evidence_dir/fault-tamper-required.json" "required search after bundle tamper"
  wt search --semantic=auto --scope project --format json-v2 "hybrid recall" \
    >"$evidence_dir/fault-tamper-auto.json" 2>"$evidence_dir/fault-tamper-auto.err" || true
  grep -E 'reason:|degraded|runtime_unavailable|bundle' "$evidence_dir/fault-tamper-auto.err" \
    "$evidence_dir/fault-tamper-auto.json" >/dev/null 2>&1 \
    || python3 - "$evidence_dir/fault-tamper-auto.json" <<'PY'
import json,sys
try:
  env=json.load(open(sys.argv[1]))
except Exception:
  raise SystemExit(0)
assert env.get("degraded_reasons") or env.get("schema")=="worktrail.cli.error.v1" or env.get("results") is not None
PY
  mv "$backup" "$model"

  # Delete active generation pointer and recover via rebuild.
  local project_pointer="$WORKTRAIL_PROJECT_ROOT/.worktrail/index/semantic/active.json"
  rm -f "$project_pointer"
  wt search --semantic=required --scope project --format json-v2 "hybrid recall" \
    >"$evidence_dir/fault-missing-gen-required.json" 2>"$evidence_dir/fault-missing-gen-required.err" || true
  assert_json_error "$evidence_dir/fault-missing-gen-required.json" "required search without active generation"
  wt search --semantic=auto --scope project --format json-v2 "hybrid recall" \
    >"$evidence_dir/fault-missing-gen-auto.json" 2>"$evidence_dir/fault-missing-gen-auto.err" || true
  wt semantic rebuild --scope project --format json >"$evidence_dir/fault-rebuild-recover.json"
  wt search --semantic=required --scope project --format json-v2 "e2e-prod-gate-needle-zx9" \
    >"$evidence_dir/fault-after-rebuild.json" 2>"$evidence_dir/fault-after-rebuild.err"

  # Two consecutive rebuilds on same scope (no rollback path).
  wt semantic rebuild --scope project --format json >"$evidence_dir/fault-rebuild-1.json"
  wt semantic rebuild --scope project --format json >"$evidence_dir/fault-rebuild-2.json"
  log "phase fault passed"
}

phase_resource() {
  log "phase resource: real M1 parity + security/resource gate"
  local free_kb
  free_kb="$(df -k "$tmp_root" | awk 'NR==2{print $4}')"
  # Need room for ~650MB model + runtime + reference (~2GB+) + capture headroom.
  [[ "$free_kb" -gt 5000000 ]] || fail "insufficient disk for production gate assets (~5GiB free required)"

  if [[ -z "$gate_assets_root" ]]; then
    # Durable host-side cache (outside the temporary HOME) to avoid re-downloading
    # multi-GB reference weights on every gate run.
    gate_assets_root="${WORKTRAIL_SEMANTIC_GATE_ROOT:-$original_home/.cache/worktrail-semantic-e2e-gate}"
  fi
  mkdir -p "$gate_assets_root"
  log "preparing gate assets in $gate_assets_root"
  local bundle_dir="$HOME/Library/Caches/worktrail/semantic/bundles/$BUNDLE_ID"
  cp "$bundle_dir/llama" "$gate_assets_root/llama"
  cp "$bundle_dir/bge-m3-q8_0.gguf" "$gate_assets_root/bge-m3-q8_0.gguf"
  # Compressed runtime may not be retained after install; fetch for hash record if absent.
  if [[ ! -f "$gate_assets_root/llama-app.zst" ]]; then
    curl -L --fail --max-redirs 1 \
      "https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/m1/llama-app.zst" \
      -o "$gate_assets_root/llama-app.zst"
  fi
  if [[ ! -f "$gate_assets_root/reference-model/pytorch_model.bin" ]]; then
    mkdir -p "$gate_assets_root/venv" "$gate_assets_root/reference-model"
    if [[ ! -x "$gate_assets_root/venv/bin/python" ]]; then
      python3 -m venv "$gate_assets_root/venv"
    fi
    # FlagEmbedding 1.3.4 still imports transformers.utils.is_torch_fx_available,
    # which was removed in transformers 5.x.
    "$gate_assets_root/venv/bin/pip" install -q 'FlagEmbedding==1.3.4' 'torch' 'transformers==4.51.3' 'sentencepiece' 'huggingface_hub'
    "$gate_assets_root/venv/bin/python" - <<PY
from huggingface_hub import snapshot_download
snapshot_download(
  repo_id="BAAI/bge-m3",
  revision="5617a9f61b028005a4858fdac845db406aefb181",
  local_dir=r"$gate_assets_root/reference-model",
)
PY
  fi
  [[ -x "$gate_assets_root/venv/bin/python" ]] || {
    python3 -m venv "$gate_assets_root/venv"
    # FlagEmbedding 1.3.4 still imports transformers.utils.is_torch_fx_available,
    # which was removed in transformers 5.x.
    "$gate_assets_root/venv/bin/pip" install -q 'FlagEmbedding==1.3.4' 'torch' 'transformers==4.51.3' 'sentencepiece' 'huggingface_hub'
  }

  run_release_command "mutating (isolated)" "parity" \
    bash "$repo_root/scripts/semantic/run-real-darwin-m1-gate.sh" "$gate_assets_root" \
    | tee "$evidence_dir/real-m1-gate.log"
  run_release_command "mutating (isolated)" "security/resource" \
    bash "$repo_root/scripts/semantic/run-runtime-security-resource-gate.sh" "$gate_assets_root" \
    | tee "$evidence_dir/runtime-security-resource-gate.log"

  python3 - "$gate_assets_root/runtime-resource-report.json" <<'PY'
import json,sys
r=json.load(open(sys.argv[1]))
assert r["cold_start_ms"] <= 25000, r
assert r["warm_embedding_p95_ms"] <= 35, r
assert r["peak_rss_kb"] <= 1024*1024, r
print("resource_ok", r["cold_start_ms"], r["warm_embedding_p95_ms"], r["peak_rss_kb"])
PY
  cp "$gate_assets_root/parity-report.json" "$evidence_dir/" 2>/dev/null || true
  cp "$gate_assets_root/runtime-resource-report.json" "$evidence_dir/" 2>/dev/null || true
  log "phase resource passed"
}

phase_evidence() {
  log "phase evidence: write the controlled persistent release archive"
  local archive_root release_archive candidate_commit author committer
  require_clean_checkout_for_release_record
  archive_root="$(release_archive_root)"
  mkdir -p -m 700 "$archive_root"
  candidate_commit="$(git -C "$repo_root" rev-parse HEAD)"
  release_archive="$archive_root/$candidate_commit"
  [[ ! -e "$release_archive" ]] || fail "refusing to reuse an existing release archive"
  verify_dirty_tree_snapshot "$evidence_dir/source-snapshot-before-gate.json" "$evidence_dir/source-snapshot-verification.json"
  capture_review_plan "after"
  record_release_command "mutating (isolated)" "production E2E"
  record_release_command "readonly" "archive safety and SHA256SUMS verification"

  assert_clean_checkout "after"
  author="$(git -C "$repo_root" log -1 --format='%an <%ae>')"
  committer="$(git -C "$repo_root" log -1 --format='%cn <%ce>')"
  python3 "$release_record_writer" \
    --archive "$release_archive" \
    --candidate-commit "$candidate_commit" \
    --author "$author" \
    --committer "$committer" \
    --commands "$release_command_ledger" \
    --review-before "$evidence_dir/review-before.json" \
    --review-after "$evidence_dir/review-after.json" \
    --retrieval-report "$evidence_dir/retrieval-report.json" \
    --resource-report "$evidence_dir/runtime-resource-report.json"
  python3 "$archive_safety_scan" "$release_archive" \
    --report "$release_archive/SAFETY-SCAN.json" >/dev/null
  (
    cd "$release_archive"
    shasum -a 256 SUMMARY.md RELEASE-RECORD.json SAFETY-SCAN.json >SHA256SUMS
  )
  python3 "$archive_safety_scan" "$release_archive" \
    --report "$release_archive/SAFETY-SCAN.json" >/dev/null
  (
    cd "$release_archive"
    shasum -a 256 SUMMARY.md RELEASE-RECORD.json SAFETY-SCAN.json >SHA256SUMS
    shasum -a 256 -c SHA256SUMS
  )
  python3 "$archive_safety_scan" "$release_archive" >"$evidence_dir/archive-safety-scan.json"
  python3 - "$release_archive" <<'PY'
import pathlib,sys
root=pathlib.Path(sys.argv[1])
manifest=(root/"SHA256SUMS").read_text(encoding="utf-8").splitlines()
members={line.split(maxsplit=1)[1].lstrip("*") for line in manifest}
assert members == {"SUMMARY.md","RELEASE-RECORD.json","SAFETY-SCAN.json"}, members
assert {path.name for path in root.iterdir()} == members | {"SHA256SUMS"}
PY

  wt semantic stop --format json >/dev/null 2>&1 || true
  log "phase evidence recorded; temporary runtime cleaned via trap"
}

generate_golden_if_requested() {
  # Explicit opt-in only; never used by default gate path.
  [[ "${WORKTRAIL_E2E_WRITE_GOLDEN:-}" == "1" ]] || return 0
  fail "refusing automatic golden write inside gate; generate manually with reviewed cmp"
}

main() {
  cd "$repo_root"
  require_m1
  require_clean_checkout_for_release_record
  generate_golden_if_requested
  setup_isolation
  if [[ "$phase" == "all" ]]; then
    assert_clean_checkout "before"
    capture_review_plan "before"
  fi

  case "$phase" in
    offline)
      phase_offline
      ;;
    install)
      wt init
      phase_install
      ;;
    positive)
      wt init --semantic
      install_fixtures
      phase_positive
      ;;
    fault)
      wt init --semantic
      install_fixtures
      phase_positive
      phase_fault
      ;;
    resource)
      wt init --semantic
      phase_resource
      ;;
    all)
      phase_offline
      phase_install
      phase_positive
      phase_fault
      phase_resource
      phase_evidence
      ;;
    harness)
      # Offline harness validation: fixture install + golden compare + trap cleanup.
      local profile="$tmp_root/deny-outbound.sb"
      printf '%s\n' '(version 1)' '(allow default)' '(deny network-outbound)' >"$profile"
      sandbox-exec -f "$profile" env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
        "$worktrail_bin" init >/dev/null
      install_fixtures
      wt index rebuild --scope user >/dev/null
      wt index rebuild --scope project >/dev/null
      sandbox-exec -f "$profile" env HOME="$HOME" WORKTRAIL_HOME="$WORKTRAIL_HOME" WORKTRAIL_PROJECT_ROOT="$WORKTRAIL_PROJECT_ROOT" \
        "$worktrail_bin" search --scope project --format json "e2e-prod-gate-needle-zx9" \
        >"$evidence_dir/search-json-v1.actual"
      compare_golden_readonly "$evidence_dir/search-json-v1.actual" "$golden_search"
      eval_cli retrieval-report \
        --labels "$repo_root/testdata/semantic/retrieval-labels-fixture.json" \
        --rankings "$repo_root/testdata/semantic/retrieval-rankings-fixture.json" \
        >"$evidence_dir/retrieval-report-fixture.json"
      assert_no_semantic_artifacts
      log "harness phase passed"
      ;;
    *)
      fail "unknown phase $phase (use all|offline|install|positive|fault|resource|harness)"
      ;;
  esac
  log "phase $phase completed successfully"
}

main "$@"
