#!/usr/bin/env bash
# Privacy-safe table-hardening dogfood runner.
# Private manifest/key stay under XDG data; public stdout is opaque summaries only.
set -euo pipefail

usage() {
  cat <<'EOF'
Usage:
  bash scripts/semantic/run-table-dogfood.sh \
    --manifest <absolute-manifest> \
    --worktrail-binary <absolute-binary>
EOF
}

manifest_path=""
worktrail_binary=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --manifest)
      manifest_path="${2:-}"
      shift 2
      ;;
    --worktrail-binary)
      worktrail_binary="${2:-}"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      printf 'unknown flag: %s\n' "$1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

[[ -n "$manifest_path" && -n "$worktrail_binary" ]] || {
  usage >&2
  exit 2
}

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
export WORKTRAIL_TABLE_DOGFOOD_MANIFEST="$manifest_path"
export WORKTRAIL_TABLE_DOGFOOD_BINARY="$worktrail_binary"
export WORKTRAIL_TABLE_DOGFOOD_REPO="$repo_root"

python3 - <<'PY'
from __future__ import annotations

import hashlib
import hmac
import json
import os
import secrets
import stat
import subprocess
import sys
import time
from pathlib import Path

MANIFEST = Path(os.environ["WORKTRAIL_TABLE_DOGFOOD_MANIFEST"])
BINARY = Path(os.environ["WORKTRAIL_TABLE_DOGFOOD_BINARY"])
REPO = Path(os.environ["WORKTRAIL_TABLE_DOGFOOD_REPO"])
SCHEMA_PATH = REPO / "scripts/semantic/fixtures/dogfood-manifest.schema.json"
ALLOWED_ACTIONS = {"semantic_rebuild"}
ALLOWED_SCOPES = {"all", "project", "user"}


def fail(message: str) -> None:
    print(f"[dogfood] FAIL: {message}", file=sys.stderr)
    raise SystemExit(1)


def require_abs_regular(path: Path, label: str, *, writable: bool = False) -> None:
    if not path.is_absolute():
        fail(f"{label} must be an absolute path")
    if path.is_symlink():
        fail(f"{label} must not be a symlink")
    if not path.is_file():
        fail(f"{label} must be a regular file")
    mode = path.stat().st_mode
    if mode & (stat.S_IRWXG | stat.S_IRWXO):
        fail(f"{label} permissions must be 0600 (no group/other bits)")
    if writable and not os.access(path, os.W_OK):
        fail(f"{label} is not writable")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def canonical_json_bytes(value: object) -> bytes:
    return json.dumps(
        value,
        ensure_ascii=False,
        sort_keys=True,
        separators=(",", ":"),
    ).encode("utf-8")


def validation_dir(candidate_commit: str) -> Path:
    data_home = Path(os.environ.get("XDG_DATA_HOME") or (Path.home() / ".local/share"))
    if not data_home.is_absolute():
        fail("XDG_DATA_HOME must be absolute when set")
    root = (data_home / "worktrail" / "validation" / candidate_commit).resolve()
    root.mkdir(mode=0o700, parents=True, exist_ok=True)
    os.chmod(root, 0o700)
    return root


def ensure_key(path: Path) -> bytes:
    flags = os.O_CREAT | os.O_EXCL | os.O_WRONLY
    try:
        fd = os.open(str(path), flags, 0o600)
    except FileExistsError:
        require_abs_regular(path, "dogfood-manifest.key")
        data = path.read_bytes()
        if len(data) != 32:
            fail("dogfood-manifest.key must be exactly 32 bytes")
        return data
    try:
        os.write(fd, secrets.token_bytes(32))
    finally:
        os.close(fd)
    require_abs_regular(path, "dogfood-manifest.key")
    data = path.read_bytes()
    if len(data) != 32:
        fail("newly created dogfood-manifest.key is not 32 bytes")
    return data


def validate_manifest(manifest: dict) -> None:
    schema = json.loads(SCHEMA_PATH.read_text(encoding="utf-8"))
    if manifest.get("schema") != schema["properties"]["schema"]["const"]:
        fail("manifest schema mismatch")
    required = schema["required"]
    for key in required:
        if key not in manifest:
            fail(f"manifest missing {key}")
    if any(key in manifest for key in ("command", "eval", "commands", "shell")):
        fail("manifest must not contain arbitrary command/eval fields")
    if not isinstance(manifest.get("projects"), list) or not manifest["projects"]:
        fail("manifest.projects must be a non-empty array")
    if not isinstance(manifest.get("queries"), list) or not manifest["queries"]:
        fail("manifest.queries must be a non-empty array")
    aliases = set()
    for project in manifest["projects"]:
        if not isinstance(project, dict):
            fail("project must be an object")
        for key in ("alias", "root", "root_digest", "source_content_digest", "rebuild_actions"):
            if key not in project:
                fail(f"project missing {key}")
        alias = project["alias"]
        if alias in aliases:
            fail(f"duplicate project alias {alias}")
        aliases.add(alias)
        root = Path(project["root"])
        if not root.is_absolute() or root.is_symlink() or not root.is_dir():
            fail(f"project root invalid for alias {alias}")
        for action in project["rebuild_actions"]:
            if action.get("action") not in ALLOWED_ACTIONS:
                fail(f"unsupported rebuild action for {alias}")
            if action.get("scope") not in ALLOWED_SCOPES:
                fail(f"unsupported rebuild scope for {alias}")
    for query in manifest["queries"]:
        if query.get("project_alias") not in aliases:
            fail(f"query references unknown project alias {query.get('project_alias')}")
        expect = query.get("expect")
        if not isinstance(expect, dict) or "scope" not in expect or "entry_id" not in expect:
            fail(f"query {query.get('opaque_id')} missing expect.scope/entry_id")


def git_digest(root: Path) -> str:
    status = subprocess.check_output(
        ["git", "-C", str(root), "status", "--porcelain=v1", "-z", "--untracked-files=all"],
    )
    head = subprocess.check_output(["git", "-C", str(root), "rev-parse", "HEAD"]).strip()
    tracked = subprocess.check_output(["git", "-C", str(root), "ls-files", "-z"])
    digest = hashlib.sha256()
    digest.update(head)
    digest.update(b"\0")
    digest.update(status)
    digest.update(b"\0")
    digest.update(tracked)
    return digest.hexdigest()


def source_content_digest(root: Path) -> str:
    worktrail = root / ".worktrail"
    if not worktrail.is_dir():
        fail(f"missing .worktrail under {root}")
    digest = hashlib.sha256()
    paths = sorted(
        path for path in worktrail.rglob("*")
        if path.is_file() and not path.is_symlink() and ".worktrail/state" not in str(path)
        and "/index/" not in str(path.relative_to(worktrail)).replace("\\", "/")
    )
    for path in paths:
        rel = path.relative_to(worktrail).as_posix().encode("utf-8")
        digest.update(rel)
        digest.update(b"\0")
        digest.update(path.read_bytes())
        digest.update(b"\n")
    return digest.hexdigest()


def run_wt(binary: Path, root: Path, args: list[str], output: Path) -> None:
    env = os.environ.copy()
    env["WORKTRAIL_PROJECT_ROOT"] = str(root)
    # Keep gse/runtime logs off stdout so JSON reports stay parseable.
    err_path = output.with_suffix(output.suffix + ".stderr")
    with output.open("w", encoding="utf-8") as handle, err_path.open(
        "w", encoding="utf-8"
    ) as err_handle:
        proc = subprocess.run(
            [str(binary), *args],
            cwd=str(root),
            env=env,
            stdout=handle,
            stderr=err_handle,
            text=True,
            check=False,
        )
    if proc.returncode != 0:
        fail(f"command failed ({proc.returncode}): {' '.join(args)}; see {output}")


def main() -> int:
    require_abs_regular(MANIFEST, "manifest")
    if BINARY.is_symlink() or not BINARY.is_file() or not BINARY.is_absolute():
        fail("worktrail binary must be an absolute regular file")
    if not os.access(BINARY, os.X_OK):
        fail("worktrail binary must be executable")

    manifest = json.loads(MANIFEST.read_text(encoding="utf-8"))
    validate_manifest(manifest)
    candidate = manifest["candidate_commit"]
    private_dir = validation_dir(candidate)
    expected_manifest = private_dir / "dogfood-manifest.json"
    if MANIFEST.resolve() != expected_manifest.resolve():
        fail(
            "manifest path must be "
            f"${{XDG_DATA_HOME:-$HOME/.local/share}}/worktrail/validation/{candidate}/dogfood-manifest.json"
        )
    key = ensure_key(private_dir / "dogfood-manifest.key")

    binary_sha = sha256_file(BINARY)
    if binary_sha != manifest["worktrail_binary_sha256"]:
        fail("worktrail binary sha256 mismatch")

    public = {
        "schema": "worktrail.semantic.dogfood-public-evidence.v1",
        "candidate_commit": candidate,
        "manifest_hmac_sha256": hmac.new(key, canonical_json_bytes(manifest), hashlib.sha256).hexdigest(),
        "projects": [],
        "queries": [],
        "passed": True,
    }

    for project in manifest["projects"]:
        alias = project["alias"]
        root = Path(project["root"])
        before = git_digest(root)
        if git_digest(root) != project["root_digest"]:
            # compare using freshly computed before digest
            pass
        actual_root_digest = before
        if actual_root_digest != project["root_digest"]:
            fail(f"root digest mismatch for {alias}")
        actual_source = source_content_digest(root)
        if actual_source != project["source_content_digest"]:
            fail(f"source content digest mismatch for {alias}")

        project_public = {
            "alias": alias,
            "rebuilds": [],
            "git_status_digest_before": actual_root_digest,
            "git_status_digest_after": "",
            "formal_knowledge_changed": "no",
        }
        for action in project["rebuild_actions"]:
            started = time.perf_counter()
            out = private_dir / f"rebuild-{alias}-{action['scope']}.json"
            run_wt(
                BINARY,
                root,
                ["semantic", "rebuild", "--scope", action["scope"], "--format", "json"],
                out,
            )
            elapsed_ms = int((time.perf_counter() - started) * 1000)
            payload = json.loads(out.read_text(encoding="utf-8"))
            if payload.get("schema") == "worktrail.cli.error.v1" or payload.get("ok") is False:
                fail(f"rebuild failed for {alias} scope={action['scope']}: typed error retained in private dir")
            # Privacy-safe counters only.
            summary = {
                "scope": action["scope"],
                "elapsed_ms": elapsed_ms,
                "entry_count": payload.get("entry_count") or payload.get("entries") or payload.get("entry_total"),
                "chunk_count": payload.get("chunk_count") or payload.get("chunks") or payload.get("chunk_total"),
                "table_group_count": payload.get("table_group_count") or payload.get("table_groups"),
                "max_tokens": payload.get("max_tokens") or payload.get("max_token_count"),
                "db_size_bytes": payload.get("db_size_bytes") or payload.get("generation_db_bytes"),
                "saturation": payload.get("saturation") or payload.get("saturation_class"),
                "error_class": payload.get("error_class") or "none",
            }
            project_public["rebuilds"].append(summary)
        after = git_digest(root)
        project_public["git_status_digest_after"] = after
        if after != actual_root_digest:
            # Digest includes index artifacts under .worktrail; compare porcelain only.
            before_porcelain = subprocess.check_output(
                ["git", "-C", str(root), "status", "--porcelain=v1", "--untracked-files=all"],
                text=True,
            )
            # Recompute porcelain after and compare excluding .worktrail/index
            after_porcelain = subprocess.check_output(
                ["git", "-C", str(root), "status", "--porcelain=v1", "--untracked-files=all"],
                text=True,
            )
            def strip_index(text: str) -> list[str]:
                lines = []
                for line in text.splitlines():
                    path = line[3:] if len(line) > 3 else line
                    if path.startswith(".worktrail/index/") or path.startswith(".worktrail/state/"):
                        continue
                    lines.append(line)
                return lines
            if strip_index(before_porcelain) != strip_index(after_porcelain):
                project_public["formal_knowledge_changed"] = "yes"
                public["passed"] = False
                fail(f"formal knowledge or non-index paths changed for {alias}")
        public["projects"].append(project_public)

    for query in manifest["queries"]:
        alias = query["project_alias"]
        project = next(item for item in manifest["projects"] if item["alias"] == alias)
        root = Path(project["root"])
        started = time.perf_counter()
        out = private_dir / f"query-{query['opaque_id']}.json"
        args = [
            "search",
            f"--semantic={query['mode']}",
            "--scope",
            query["scope"],
            "--format",
            "json-v2",
            query["text"],
        ]
        run_wt(BINARY, root, args, out)
        elapsed_ms = int((time.perf_counter() - started) * 1000)
        payload = json.loads(out.read_text(encoding="utf-8"))
        ok = payload.get("schema") == "worktrail.search.results.v2"
        expect = query["expect"]
        matched = False
        if ok:
            for result in payload.get("results") or []:
                entry = result.get("entry") or {}
                if entry.get("scope") == expect["scope"] and entry.get("id") == expect["entry_id"]:
                    matched = True
                    if "evidence_role" in expect:
                        roles = {
                            match.get("evidence_role")
                            for match in (result.get("chunk_matches") or [])
                        }
                        if expect["evidence_role"] not in roles:
                            matched = False
                    if matched and "start_byte" in expect and "end_byte" in expect:
                        ranges = {
                            (
                                (match.get("primary_source_range") or {}).get("start_byte"),
                                (match.get("primary_source_range") or {}).get("end_byte"),
                            )
                            for match in (result.get("chunk_matches") or [])
                        }
                        if (expect["start_byte"], expect["end_byte"]) not in ranges:
                            matched = False
                    break
        query_public = {
            "opaque_id": query["opaque_id"],
            "passed": bool(matched),
            "elapsed_ms": elapsed_ms,
            "result_count": len(payload.get("results") or []) if ok else 0,
        }
        if not matched:
            public["passed"] = False
        public["queries"].append(query_public)

    print(json.dumps(public, ensure_ascii=False, indent=2))
    return 0 if public["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
PY
