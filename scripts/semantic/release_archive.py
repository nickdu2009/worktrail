#!/usr/bin/env python3
"""Shared release-archive allowlist, validation, and staging helpers."""

from __future__ import annotations

import hashlib
import json
import os
import shutil
import tempfile
from pathlib import Path

ALLOWED_MEMBERS = frozenset(
    {
        "SUMMARY.md",
        "RELEASE-RECORD.json",
        "SAFETY-SCAN.json",
        "SHA256SUMS",
    }
)
PAYLOAD_MEMBERS = frozenset(
    {
        "SUMMARY.md",
        "RELEASE-RECORD.json",
        "SAFETY-SCAN.json",
    }
)


class ArchiveError(ValueError):
    """Raised when an archive is present but not safely reusable."""


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def list_regular_members(archive: Path) -> set[str]:
    if not archive.is_dir() or archive.is_symlink():
        raise ArchiveError(f"archive must be a real directory: {archive}")
    members: set[str] = set()
    for path in archive.iterdir():
        if path.is_symlink() or not path.is_file():
            raise ArchiveError(f"illegal archive member type: {path.name}")
        if path.name not in ALLOWED_MEMBERS:
            raise ArchiveError(f"unexpected archive member: {path.name}")
        members.add(path.name)
    return members


def verify_sha256sums(archive: Path) -> None:
    manifest = archive / "SHA256SUMS"
    if not manifest.is_file() or manifest.is_symlink():
        raise ArchiveError("SHA256SUMS missing")
    lines = manifest.read_text(encoding="utf-8").splitlines()
    seen: set[str] = set()
    for line in lines:
        if not line.strip():
            continue
        digest, name = line.split(maxsplit=1)
        name = name.lstrip("*")
        if name not in PAYLOAD_MEMBERS:
            raise ArchiveError(f"SHA256SUMS covers unexpected member: {name}")
        path = archive / name
        if not path.is_file():
            raise ArchiveError(f"SHA256SUMS member missing: {name}")
        if sha256_file(path) != digest:
            raise ArchiveError(f"SHA256SUMS mismatch: {name}")
        seen.add(name)
    if seen != PAYLOAD_MEMBERS:
        raise ArchiveError(f"SHA256SUMS incomplete: {sorted(seen)}")


def validate_reusable_archive(archive: Path, candidate_commit: str) -> str:
    """Return 'missing' or 'reusable'; raise ArchiveError when illegal."""
    if not archive.exists():
        return "missing"
    members = list_regular_members(archive)
    if members != ALLOWED_MEMBERS:
        raise ArchiveError(f"archive member set incomplete or illegal: {sorted(members)}")
    verify_sha256sums(archive)
    record = json.loads((archive / "RELEASE-RECORD.json").read_text(encoding="utf-8"))
    if record.get("result") != "PASS":
        raise ArchiveError("existing archive result is not PASS")
    candidate = record.get("candidate")
    if not isinstance(candidate, dict) or candidate.get("commit") != candidate_commit:
        raise ArchiveError("existing archive candidate commit mismatch")
    safety = json.loads((archive / "SAFETY-SCAN.json").read_text(encoding="utf-8"))
    if safety.get("result") != "PASS":
        raise ArchiveError("existing archive SAFETY-SCAN is not PASS")
    return "reusable"


def staging_dir(archive_root: Path, candidate_commit: str) -> Path:
    archive_root.mkdir(mode=0o700, parents=True, exist_ok=True)
    staging = Path(
        tempfile.mkdtemp(
            prefix=f".staging-{candidate_commit}-",
            dir=str(archive_root),
        )
    )
    os.chmod(staging, 0o700)
    return staging


def atomic_promote(staging: Path, final_archive: Path) -> None:
    if final_archive.exists():
        raise ArchiveError(f"refusing to clobber existing archive: {final_archive}")
    os.rename(staging, final_archive)


def cleanup_staging(staging: Path | None) -> None:
    if staging is None:
        return
    if staging.exists() and staging.is_dir() and not staging.is_symlink():
        shutil.rmtree(staging)
