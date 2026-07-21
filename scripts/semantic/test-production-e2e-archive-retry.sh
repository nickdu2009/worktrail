#!/usr/bin/env bash
# Covers release-archive precheck/reuse/staging/no-clobber contracts without the full M1 gate.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

python3 - <<'PY'
from __future__ import annotations

import importlib.util
import json
import tempfile
from pathlib import Path

repo = Path(".").resolve()
gate = repo / "scripts/semantic/run-production-e2e-gate.sh"
helper_path = repo / "scripts/semantic/release_archive.py"
spec = importlib.util.spec_from_file_location("release_archive", helper_path)
archive = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(archive)
gate_src = gate.read_text(encoding="utf-8")

assert "release_archive" in gate_src or "validate_reusable_archive" in gate_src
assert "staging" in gate_src
assert "atomic" in gate_src or "os.rename" in gate_src or "mv " in gate_src
assert "table hardening retrieval gate" in gate_src
assert "refill capacity matrix" in gate_src
assert "WORKTRAIL_E2E_REUSE_ARCHIVE" in gate_src or "reuse" in gate_src.lower()


def write_valid(archive_dir: Path, commit: str) -> None:
    archive_dir.mkdir(parents=True, exist_ok=True)
    record = {
        "schema": "worktrail.semantic.release-record.v1",
        "result": "PASS",
        "candidate": {
            "commit": commit,
            "author": "nick.du <nickdu2009@gmail.com>",
            "committer": "nick.du <nickdu2009@gmail.com>",
        },
    }
    (archive_dir / "RELEASE-RECORD.json").write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    (archive_dir / "SUMMARY.md").write_text("# summary\nresult: PASS\n", encoding="utf-8")
    (archive_dir / "SAFETY-SCAN.json").write_text(
        json.dumps({"result": "PASS", "files_scanned": sorted(archive.ALLOWED_MEMBERS)}) + "\n",
        encoding="utf-8",
    )
    lines = []
    for name in ("SUMMARY.md", "RELEASE-RECORD.json", "SAFETY-SCAN.json"):
        lines.append(f"{archive.sha256_file(archive_dir / name)}  {name}")
    (archive_dir / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")


with tempfile.TemporaryDirectory(prefix="worktrail-archive-retry.") as tmp:
    tmp_path = Path(tmp)
    commit = "a" * 40
    data_home = tmp_path / "xdg-data"
    archive_root = data_home / "worktrail" / "release-archive"
    archive_root.mkdir(parents=True)

    # missing -> reusable after write
    final = archive_root / commit
    assert archive.validate_reusable_archive(final, commit) == "missing"
    write_valid(final, commit)
    assert archive.validate_reusable_archive(final, commit) == "reusable"

    # dirty/SHA mismatch
    mismatched = archive_root / ("b" * 40)
    write_valid(mismatched, "c" * 40)
    try:
        archive.validate_reusable_archive(mismatched, "b" * 40)
        raise SystemExit("expected commit mismatch failure")
    except archive.ArchiveError:
        pass

    # illegal existing directory (extra member) must fail and never be treated reusable
    illegal = archive_root / ("d" * 40)
    write_valid(illegal, "d" * 40)
    (illegal / "raw.log").write_text("leak\n", encoding="utf-8")
    try:
        archive.validate_reusable_archive(illegal, "d" * 40)
        raise SystemExit("expected illegal member failure")
    except archive.ArchiveError:
        pass

    # concurrent no-clobber: second promote fails
    target = archive_root / ("e" * 40)
    staging1 = archive.staging_dir(archive_root, "e" * 40)
    (staging1 / "SUMMARY.md").write_text("one\n", encoding="utf-8")
    archive.atomic_promote(staging1, target)
    staging2 = archive.staging_dir(archive_root, "e" * 40)
    (staging2 / "SUMMARY.md").write_text("two\n", encoding="utf-8")
    try:
        archive.atomic_promote(staging2, target)
        raise SystemExit("expected no-clobber failure")
    except archive.ArchiveError:
        pass
    archive.cleanup_staging(staging2)

    # interrupt cleans only staging created by the helper process
    staging3 = archive.staging_dir(archive_root, "f" * 40)
    (staging3 / "partial.json").write_text("{}\n", encoding="utf-8")
    assert staging3.exists()
    archive.cleanup_staging(staging3)
    assert not staging3.exists()
    assert target.exists()  # final archive untouched

print("production E2E archive retry contracts: PASS")
PY

# Source-level contract: gate must precheck before expensive resource phase and use staging promote.
python3 - <<'PY'
from pathlib import Path
src = Path("scripts/semantic/run-production-e2e-gate.sh").read_text(encoding="utf-8")
for needle in (
    "precheck_release_archive",
    "phase_resource",
    "validate_reusable_archive",
    "staging_dir",
    "atomic_promote",
    "table hardening retrieval gate",
    "refill capacity matrix",
    "search-json-v2-table.golden",
):
    assert needle in src, needle
print("production E2E archive retry source contracts: PASS")
PY
