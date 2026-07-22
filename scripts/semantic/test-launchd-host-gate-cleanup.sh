#!/usr/bin/env bash
# Static and signal-focused safety test for the launchd gate cleanup boundary.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
gate="$repo_root/scripts/semantic/run-launchd-host-gate.sh"

python3 - "$gate" <<'PY'
import os,pathlib,re,signal,subprocess,sys,tempfile,time
gate=pathlib.Path(sys.argv[1])
source=gate.read_text(encoding="utf-8")
assert 'com.nickdu2009.worktrail.semantic.test.' in source
assert '/bin/launchctl bootout "gui/$uid/$test_label"' in source
assert 'rm -rf "$tmp_root"' in source
assert 'com.nickdu2009.worktrail.semantic"' not in source
assert 'WORKTRAIL_LAUNCHD_GATE_BUNDLE_SOURCE' in source
assert "-tags=launchdgate" in source

with tempfile.TemporaryDirectory(prefix="worktrail-launchd-cleanup-input.") as bundle:
    process=subprocess.Popen(
        ["bash", str(gate)],
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        text=True,
        env=os.environ | {
            "WORKTRAIL_LAUNCHD_GATE_BUNDLE_SOURCE": bundle,
            "WORKTRAIL_LAUNCHD_GATE_TEST_HOLD_SECONDS": "30",
        },
        start_new_session=True,
    )
    try:
        output=[]
        tmp_root=None
        deadline=time.monotonic()+10
        while time.monotonic() < deadline:
            line=process.stdout.readline()
            output.append(line)
            match=re.search(r"initialized tmp_root=(.+) label=", line)
            if match:
                tmp_root=match.group(1)
            if "controlled test hold" in line:
                break
        if not tmp_root:
            raise AssertionError("gate did not expose controlled cleanup root\n"+"".join(output))
        os.killpg(process.pid, signal.SIGTERM)
        exit_code=process.wait(timeout=10)
        if exit_code != 143:
            raise AssertionError(f"TERM exit={exit_code}, want 143\n{''.join(output)}")
        if os.path.exists(tmp_root):
            raise AssertionError(f"temporary root remains after TERM: {tmp_root}")
    finally:
        if process.poll() is None:
            os.killpg(process.pid, signal.SIGKILL)
            process.wait()
print("launchd Host gate cleanup boundary: PASS")
PY
