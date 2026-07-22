#!/usr/bin/env bash
# Focused real-launchd acceptance gate for the user-scoped semantic Host.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bundle_id="3625d27700727578d7694ab04d19291efce45095aa57daba66d692a37a51be58"
uid="$(id -u)"
# Keep the root short: Darwin limits Unix-domain socket paths to roughly 104
# bytes, and the semantic runtime suffix is intentionally descriptive.
tmp_root="$(mktemp -d /tmp/wt-lh.XXXXXX)"
test_label="com.nickdu2009.worktrail.semantic.test.${uid}.$$"
test_home="$tmp_root/home"
test_binary="$tmp_root/bin/worktrail"
source_bundle="${WORKTRAIL_LAUNCHD_GATE_BUNDLE_SOURCE:-$HOME/Library/Caches/worktrail/semantic/bundles/$bundle_id}"
keep_tmp="${WORKTRAIL_LAUNCHD_GATE_KEEP_TMP:-0}"

cleanup() {
	local exit_code="${1:-$?}"
  trap - EXIT INT TERM
  /bin/launchctl bootout "gui/$uid/$test_label" >/dev/null 2>&1 || true
  if [[ "$keep_tmp" != "1" ]]; then
    chmod -R u+w "$tmp_root" 2>/dev/null || true
    rm -rf "$tmp_root"
  else
    printf '[launchd-gate] retained tmp_root=%s\n' "$tmp_root"
  fi
  exit "$exit_code"
}
trap 'cleanup $?' EXIT
trap 'cleanup 130' INT
trap 'cleanup 143' TERM

printf '[launchd-gate] initialized tmp_root=%s label=%s\n' "$tmp_root" "$test_label"
if [[ "${WORKTRAIL_LAUNCHD_GATE_TEST_HOLD_SECONDS:-0}" != "0" ]]; then
	printf '[launchd-gate] controlled test hold=%ss\n' "$WORKTRAIL_LAUNCHD_GATE_TEST_HOLD_SECONDS"
	hold_deadline=$((SECONDS + WORKTRAIL_LAUNCHD_GATE_TEST_HOLD_SECONDS))
	while (( SECONDS < hold_deadline )); do
		sleep 0.1
	done
fi

[[ "$(uname -s)" == "Darwin" ]] || { printf 'launchd Host gate requires macOS\n' >&2; exit 1; }
[[ "$(uname -m)" == "arm64" ]] || { printf 'launchd Host gate requires Apple silicon\n' >&2; exit 1; }
[[ -d "$source_bundle" ]] || {
  printf 'verified semantic bundle source is missing: %s\n' "$source_bundle" >&2
  printf 'set WORKTRAIL_LAUNCHD_GATE_BUNDLE_SOURCE to an installed M1 bundle\n' >&2
  exit 1
}

mkdir -p "$tmp_root/bin" "$test_home/Library/Caches/worktrail/semantic/bundles"
mkdir -p "$test_home/Library/Application Support/worktrail/semantic"
if ! cp -cR "$source_bundle" "$test_home/Library/Caches/worktrail/semantic/bundles/$bundle_id" 2>/dev/null; then
  cp -R "$source_bundle" "$test_home/Library/Caches/worktrail/semantic/bundles/$bundle_id"
fi
chmod 700 "$test_home/Library/Application Support/worktrail/semantic"
printf '%s\n' '{"schema":"worktrail.semantic.service-config.v1","idle_timeout":"1m"}' \
  >"$test_home/Library/Application Support/worktrail/semantic/service.json"
chmod 600 "$test_home/Library/Application Support/worktrail/semantic/service.json"

(
  cd "$repo_root"
  go build -o "$test_binary" ./cmd/worktrail
  WORKTRAIL_LAUNCHD_GATE=1 \
  WORKTRAIL_LAUNCHD_GATE_HOME="$test_home" \
  WORKTRAIL_LAUNCHD_GATE_BINARY="$test_binary" \
  WORKTRAIL_LAUNCHD_GATE_LABEL="$test_label" \
  WORKTRAIL_LAUNCHD_GATE_BUNDLE_ID="$bundle_id" \
  go test -tags=launchdgate ./internal/semantic/service -run '^TestLaunchdHostGate$' -count=1 -v
)

printf '[launchd-gate] PASS label=%s\n' "$test_label"
