#!/usr/bin/env python3
"""Unit tests for release-record required commands and dogfood HMAC helpers."""

from __future__ import annotations

import hashlib
import hmac
import importlib.util
import json
import os
import secrets
import stat
import tempfile
import unittest
from pathlib import Path


def _load(name: str, filename: str):
    path = Path(__file__).with_name(filename)
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    assert spec.loader is not None
    spec.loader.exec_module(module)
    return module


wrr = _load("write_release_record", "write-release-record.py")
archive = _load("release_archive", "release_archive.py")


def canonical_json_bytes(value: object) -> bytes:
    return json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


class ReleaseRecordTests(unittest.TestCase):
    def test_required_commands_include_table_hardening_gates(self) -> None:
        self.assertIn("table hardening retrieval gate", wrr.REQUIRED_COMMANDS)
        self.assertIn("refill capacity matrix", wrr.REQUIRED_COMMANDS)

    def test_load_commands_rejects_missing_required(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "commands.jsonl"
            path.write_text(
                json.dumps(
                    {
                        "category": "readonly",
                        "command": "go test ./...",
                        "result": "PASS",
                    }
                )
                + "\n",
                encoding="utf-8",
            )
            with self.assertRaises(ValueError) as ctx:
                wrr.load_commands(path)
            self.assertIn("missing PASS command results", str(ctx.exception))
            self.assertIn("table hardening retrieval gate", str(ctx.exception))
            self.assertIn("refill capacity matrix", str(ctx.exception))

    def test_load_commands_accepts_complete_ledger(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            path = Path(tmp) / "commands.jsonl"
            with path.open("w", encoding="utf-8") as handle:
                for command in sorted(wrr.REQUIRED_COMMANDS):
                    handle.write(
                        json.dumps(
                            {
                                "category": "readonly",
                                "command": command,
                                "result": "PASS",
                            },
                            ensure_ascii=False,
                        )
                        + "\n"
                    )
            loaded = wrr.load_commands(path)
            self.assertEqual(len(loaded), len(wrr.REQUIRED_COMMANDS))

    def test_metrics_embed_budget_and_optional_refill(self) -> None:
        retrieval = {
            "passed": True,
            "lanes": [
                {
                    "lane": "rrf",
                    "recall_at_k": 1.0,
                    "mrr": 1.0,
                    "ndcg_at_k": 1.0,
                    "vs_entry_fts": "ge",
                }
            ],
            "thresholds": {"k": 10},
            "evidence": {"evidence_recall_at_k": 1.0, "neighbor_precision": 1.0},
        }
        resource = {
            "cold_start_ms": 1,
            "warm_embedding_samples": 1,
            "warm_embedding_p50_ms": 1,
            "warm_embedding_p95_ms": 1,
            "peak_rss_kb": 1,
        }
        refill = {"passed": True, "sizes": [1000, 2000, 5000], "path": "fake-deterministic"}
        report = wrr.metrics(
            retrieval,
            resource,
            refill_capacity=refill,
            final_budget="512:768:80",
        )
        self.assertEqual(report["final_chunk_budget"], "512:768:80")
        self.assertEqual(report["refill_capacity_matrix"]["sizes"], [1000, 2000, 5000])
        self.assertEqual(report["evidence"]["evidence_recall_at_k"], 1.0)


class DogfoodManifestCryptoTests(unittest.TestCase):
    def test_canonical_json_is_stable_and_key_sorted(self) -> None:
        payload = {"b": 2, "a": {"z": 1, "y": [3, 2, 1]}, "schema": "x"}
        first = canonical_json_bytes(payload)
        second = canonical_json_bytes(payload)
        self.assertEqual(first, second)
        self.assertEqual(first, b'{"a":{"y":[3,2,1],"z":1},"b":2,"schema":"x"}')
        self.assertFalse(first.endswith(b"\n"))

    def test_hmac_stable_for_same_key_and_manifest(self) -> None:
        key = b"\x11" * 32
        manifest = {
            "schema": "worktrail.semantic.dogfood-manifest.v1",
            "candidate_commit": "a" * 40,
            "worktrail_binary_sha256": "b" * 64,
            "projects": [],
            "queries": [],
        }
        first = hmac.new(key, canonical_json_bytes(manifest), hashlib.sha256).hexdigest()
        second = hmac.new(key, canonical_json_bytes(manifest), hashlib.sha256).hexdigest()
        self.assertEqual(first, second)
        self.assertEqual(len(first), 64)

    def test_key_create_once_reuse_and_reject_symlink(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            key_path = root / "dogfood-manifest.key"
            fd = os.open(str(key_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            try:
                os.write(fd, secrets.token_bytes(32))
            finally:
                os.close(fd)
            first = key_path.read_bytes()
            self.assertEqual(len(first), 32)
            mode = key_path.stat().st_mode
            self.assertEqual(stat.S_IMODE(mode), 0o600)

            # Reuse path must not recreate.
            with self.assertRaises(FileExistsError):
                os.open(str(key_path), os.O_CREAT | os.O_EXCL | os.O_WRONLY, 0o600)
            self.assertEqual(key_path.read_bytes(), first)

            link = root / "dogfood-manifest.key.link"
            link.symlink_to(key_path)
            self.assertTrue(link.is_symlink())


class ReleaseArchiveHelperTests(unittest.TestCase):
    def _write_valid_archive(self, archive_dir: Path, commit: str) -> None:
        archive_dir.mkdir(parents=True, exist_ok=True)
        record = {
            "schema": "worktrail.semantic.release-record.v1",
            "result": "PASS",
            "candidate": {"commit": commit, "author": "nick.du <nickdu2009@gmail.com>", "committer": "nick.du <nickdu2009@gmail.com>"},
        }
        (archive_dir / "RELEASE-RECORD.json").write_text(
            json.dumps(record, indent=2) + "\n", encoding="utf-8"
        )
        (archive_dir / "SUMMARY.md").write_text("# summary\nresult: PASS\n", encoding="utf-8")
        (archive_dir / "SAFETY-SCAN.json").write_text(
            json.dumps({"result": "PASS", "files_scanned": sorted(archive.ALLOWED_MEMBERS)}) + "\n",
            encoding="utf-8",
        )
        lines = []
        for name in ("SUMMARY.md", "RELEASE-RECORD.json", "SAFETY-SCAN.json"):
            digest = archive.sha256_file(archive_dir / name)
            lines.append(f"{digest}  {name}")
        (archive_dir / "SHA256SUMS").write_text("\n".join(lines) + "\n", encoding="utf-8")

    def test_validate_reusable_and_reject_illegal(self) -> None:
        commit = "c" * 40
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            missing = root / "missing"
            self.assertEqual(archive.validate_reusable_archive(missing, commit), "missing")

            good = root / "good"
            self._write_valid_archive(good, commit)
            self.assertEqual(archive.validate_reusable_archive(good, commit), "reusable")

            bad = root / "bad"
            self._write_valid_archive(bad, commit)
            (bad / "extra.log").write_text("nope\n", encoding="utf-8")
            with self.assertRaises(archive.ArchiveError):
                archive.validate_reusable_archive(bad, commit)

    def test_atomic_promote_no_clobber(self) -> None:
        commit = "d" * 40
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            final = root / commit
            staging = archive.staging_dir(root, commit)
            (staging / "marker").write_text("ok\n", encoding="utf-8")
            archive.atomic_promote(staging, final)
            self.assertTrue(final.exists())
            staging2 = archive.staging_dir(root, commit)
            (staging2 / "marker").write_text("other\n", encoding="utf-8")
            with self.assertRaises(archive.ArchiveError):
                archive.atomic_promote(staging2, final)
            archive.cleanup_staging(staging2)


if __name__ == "__main__":
    unittest.main()
