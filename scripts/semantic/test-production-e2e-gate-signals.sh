#!/usr/bin/env bash
# Verifies the production gate preserves interrupt exit status and removes its
# isolated runtime root before any phase can produce durable evidence.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gate="$repo_root/scripts/semantic/run-production-e2e-gate.sh"

python3 - "$gate" <<'PY'
import os,re,signal,subprocess,sys,tempfile,time

gate=sys.argv[1]

def run_case(name, sig, expected_exit):
    with tempfile.NamedTemporaryFile(prefix="worktrail-e2e-signal-test.", delete=False) as log:
        log_path=log.name
    env=os.environ | {
        "WORKTRAIL_E2E_KEEP_TMP": "0",
        "WORKTRAIL_E2E_TEST_HOLD_SECONDS": "30",
    }
    process=None
    try:
        with open(log_path, "w") as log:
            process=subprocess.Popen(
                ["bash", gate, "harness"],
                stdout=log,
                stderr=subprocess.STDOUT,
                env=env,
                start_new_session=True,
            )
            root=None
            deadline=time.monotonic()+15
            while time.monotonic() < deadline:
                contents=open(log_path, encoding="utf-8").read()
                match=re.search(r"isolated tmp_root=(.+?) \(KEEP_TMP=", contents)
                if match and "test hold for 30s" in contents:
                    root=match.group(1)
                    break
                time.sleep(0.1)
            if not root:
                raise AssertionError(f"{name}: gate did not reach controlled hold\n{contents}")
            os.killpg(process.pid, sig)
            exit_code=process.wait(timeout=15)
        if exit_code != expected_exit:
            raise AssertionError(
                f"{name}: gate exit was {exit_code}, want {expected_exit}\n"
                f"{open(log_path, encoding='utf-8').read()}"
            )
        if os.path.exists(root):
            raise AssertionError(f"{name}: temporary root remains: {root}")
    finally:
        if process and process.poll() is None:
            process.kill()
            process.wait()
        os.unlink(log_path)

run_case("INT", signal.SIGINT, 130)
run_case("TERM", signal.SIGTERM, 143)

source=open(gate, encoding="utf-8").read()
assert 'XDG_DATA_HOME' in source
assert 'release-archive' in source
assert 'require_clean_checkout_for_release_record' in source
assert 'release archive precheck: reusable PASS archive for HEAD' in source
assert 'validate_reusable_archive' in source
assert 'check-archive-safety.py' in source
assert 'write-release-record.py' in source
assert 'RELEASE-RECORD.json' in source
assert 'SAFETY-SCAN.json' in source
assert 'SHA256SUMS' in source
assert 'go install ./cmd/worktrail' in source
assert 'assert_clean_checkout "before"' in source
assert 'assert_clean_checkout "after"' in source
assert 'capture_review_plan "before"' in source
assert 'capture_review_plan "after"' in source
subprocess.run(["bash", os.path.join(os.path.dirname(gate), "test-archive-safety-scan.sh")], check=True)
print("production E2E gate signal handling: PASS")
PY
