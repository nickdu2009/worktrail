#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
scanner="$repo_root/scripts/semantic/check-archive-safety.py"
tmp_root="$(mktemp -d "${TMPDIR:-/tmp}/worktrail-archive-safety.XXXXXX")"
trap 'rm -rf "$tmp_root"' EXIT

safe_archive="$tmp_root/safe"
mkdir -p "$safe_archive"
printf '%s\n' '# Safe release summary' 'result: PASS' >"$safe_archive/SUMMARY.md"
printf '%s\n' '{"schema":"worktrail.semantic.release-record.v1","result":"PASS"}' >"$safe_archive/RELEASE-RECORD.json"
python3 "$scanner" "$safe_archive" --report "$safe_archive/SAFETY-SCAN.json" >/dev/null
(
  cd "$safe_archive"
  shasum -a 256 SUMMARY.md RELEASE-RECORD.json SAFETY-SCAN.json >SHA256SUMS
)
python3 "$scanner" "$safe_archive" --report "$safe_archive/SAFETY-SCAN.json" >/dev/null
(
  cd "$safe_archive"
  shasum -a 256 SUMMARY.md RELEASE-RECORD.json SAFETY-SCAN.json >SHA256SUMS
  shasum -a 256 -c SHA256SUMS >/dev/null
)
python3 "$scanner" "$safe_archive" >/dev/null
python3 - "$safe_archive/SAFETY-SCAN.json" <<'PY'
import json,sys
report=json.load(open(sys.argv[1]))
assert report["result"] == "PASS", report
assert report["files_scanned"] == ["RELEASE-RECORD.json", "SAFETY-SCAN.json", "SHA256SUMS", "SUMMARY.md"], report
PY

for local_path in '/Users/example/private.log' '/var/folders/example/runtime.log'; do
  unsafe_path_archive="$tmp_root/unsafe-path-${local_path##*/}"
  mkdir -p "$unsafe_path_archive"
  printf 'raw log: %s\n' "$local_path" >"$unsafe_path_archive/raw.log"
  if python3 "$scanner" "$unsafe_path_archive" >/dev/null 2>&1; then
    printf '%s\n' 'expected absolute-path archive scan failure' >&2
    exit 1
  fi
done

unsafe_credential_archive="$tmp_root/unsafe-credential"
mkdir -p "$unsafe_credential_archive"
printf '%s\n' 'api_key=not-a-real-secret-value' >"$unsafe_credential_archive/raw.log"
if python3 "$scanner" "$unsafe_credential_archive" >/dev/null 2>&1; then
  printf '%s\n' 'expected credential archive scan failure' >&2
  exit 1
fi

unsafe_member_archive="$tmp_root/unsafe-member"
mkdir -p "$unsafe_member_archive"
printf '%s\n' '# Safe release summary' >"$unsafe_member_archive/SUMMARY.md"
printf '%s\n' 'raw execution log' >"$unsafe_member_archive/raw.log"
if python3 "$scanner" "$unsafe_member_archive" >/dev/null 2>&1; then
  printf '%s\n' 'expected unexpected archive member scan failure' >&2
  exit 1
fi

non_utf8_archive="$tmp_root/non-utf8"
mkdir -p "$non_utf8_archive"
printf '\377' >"$non_utf8_archive/SUMMARY.md"
if python3 "$scanner" "$non_utf8_archive" >/dev/null 2>&1; then
  printf '%s\n' 'expected non-UTF-8 archive scan failure' >&2
  exit 1
fi

symlink_archive="$tmp_root/symlink"
mkdir -p "$symlink_archive"
ln -s /etc/hosts "$symlink_archive/SUMMARY.md"
if python3 "$scanner" "$symlink_archive" >/dev/null 2>&1; then
  printf '%s\n' 'expected symlink archive scan failure' >&2
  exit 1
fi

printf '%s\n' 'archive safety scan: PASS'
