#!/usr/bin/env python3
"""Reject unsafe content from a release-evidence archive."""

from __future__ import annotations

import argparse
import json
import re
import sys
from pathlib import Path


ABSOLUTE_PATH = re.compile(
    r"(?<![A-Za-z0-9_.-])/(?:Users|var|private|tmp|home|etc|opt|Volumes)/[^\s\"'`<>]*"
)
COMMON_CREDENTIALS = (
    ("aws_access_key", re.compile(r"\b(?:AKIA|ASIA)[A-Z0-9]{16}\b")),
    ("github_token", re.compile(r"\bgh[pousr]_[A-Za-z0-9_]{20,}\b")),
    ("openai_key", re.compile(r"\bsk-[A-Za-z0-9_-]{20,}\b")),
    (
        "assigned_secret",
        re.compile(
            r"(?i)\b(?:api[_-]?key|(?:access|auth|github|aws|openai)[_-]?token|token|password|secret|authorization)"
            r"\s*(?:=|:)\s*(?:bearer\s+)?[^\s\"'`<>]{8,}"
        ),
    ),
)
ALLOWED_ARCHIVE_FILES = {
    "SUMMARY.md",
    "RELEASE-RECORD.json",
    "SAFETY-SCAN.json",
    "SHA256SUMS",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("archive", type=Path)
    parser.add_argument("--report", type=Path)
    return parser.parse_args()


def regular_files(root: Path) -> list[Path]:
    files: list[Path] = []
    for path in sorted(root.rglob("*")):
        if path.is_symlink():
            raise ValueError(f"symlink is not permitted: {path.relative_to(root)}")
        if path.is_file():
            files.append(path)
    return files


def findings_for(path: Path, root: Path) -> list[dict[str, object]]:
    relative = str(path.relative_to(root))
    findings: list[dict[str, object]] = []
    if relative not in ALLOWED_ARCHIVE_FILES:
        findings.append({"file": relative, "kind": "unexpected_archive_member"})
    try:
        text = path.read_text(encoding="utf-8")
    except UnicodeDecodeError:
        return findings + [{"file": relative, "kind": "non_utf8_content"}]

    for line_number, line in enumerate(text.splitlines(), start=1):
        if ABSOLUTE_PATH.search(line):
            findings.append(
                {
                    "file": str(path.relative_to(root)),
                    "line": line_number,
                    "kind": "absolute_path",
                }
            )
        for kind, pattern in COMMON_CREDENTIALS:
            if pattern.search(line):
                findings.append(
                    {
                        "file": str(path.relative_to(root)),
                        "line": line_number,
                        "kind": kind,
                    }
                )
    return findings


def main() -> int:
    args = parse_args()
    root = args.archive.resolve()
    if not root.is_dir():
        print("archive safety scan requires an existing directory", file=sys.stderr)
        return 2

    try:
        files = regular_files(root)
    except ValueError as err:
        print(f"archive safety scan failed: {err}", file=sys.stderr)
        return 1

    findings = [finding for path in files for finding in findings_for(path, root)]
    report = {
        "schema": "worktrail.semantic.archive-safety-scan.v1",
        "result": "PASS" if not findings else "FAIL",
        "files_scanned": [str(path.relative_to(root)) for path in files],
        "findings": findings,
        "checks": [
            "only SUMMARY.md, RELEASE-RECORD.json, SAFETY-SCAN.json, and SHA256SUMS are permitted",
            "no local absolute-path prefixes",
            "no common AWS, GitHub, OpenAI, or assigned-secret credential patterns",
            "UTF-8 regular files only; no symlinks",
        ],
    }
    if args.report:
        args.report.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(report, sort_keys=True))
    return 0 if not findings else 1


if __name__ == "__main__":
    raise SystemExit(main())
