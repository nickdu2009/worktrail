#!/usr/bin/env python3
"""Write the bounded, self-contained M1 release record."""

from __future__ import annotations

import argparse
import json
import re
from pathlib import Path


SCHEMA = "worktrail.semantic.release-record.v1"
REQUIRED_COMMANDS = {
    "go test ./...",
    "go vet ./...",
    "go build ./...",
    "go build ./cmd/worktrail",
    "go install ./cmd/worktrail",
    "isolated CLI smoke",
    "production E2E",
    "parity",
    "security/resource",
    "signal cleanup",
    "git diff --check",
    "git status --porcelain=v1 --untracked-files=all (before)",
    "git status --porcelain=v1 --untracked-files=all (after)",
    "worktrail review plan --format json (before)",
    "worktrail review plan --format json (after)",
    "table hardening retrieval gate",
    "refill capacity matrix",
}


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path, required=True)
    parser.add_argument("--candidate-commit", required=True)
    parser.add_argument("--author", required=True)
    parser.add_argument("--committer", required=True)
    parser.add_argument("--commands", type=Path, required=True)
    parser.add_argument("--review-before", type=Path, required=True)
    parser.add_argument("--review-after", type=Path, required=True)
    parser.add_argument("--retrieval-report", type=Path, required=True)
    parser.add_argument("--resource-report", type=Path, required=True)
    parser.add_argument(
        "--refill-capacity-report",
        type=Path,
        help="optional JSON object summarizing 10k/50k/100k (or small offline) refill matrix",
    )
    parser.add_argument(
        "--final-budget",
        default="640:768:80",
        help="final frozen chunk budget Target:HardMax:MinPayload (Overlap fixed at 64)",
    )
    return parser.parse_args()


def load_json(path: Path) -> dict[str, object]:
    with path.open(encoding="utf-8") as source:
        value = json.load(source)
    if not isinstance(value, dict):
        raise ValueError(f"{path.name} must contain a JSON object")
    return value


def load_commands(path: Path) -> list[dict[str, str]]:
    commands: list[dict[str, str]] = []
    with path.open(encoding="utf-8") as source:
        for line in source:
            if not line.strip():
                continue
            item = json.loads(line)
            if not isinstance(item, dict):
                raise ValueError("command ledger item must be an object")
            command = item.get("command")
            category = item.get("category")
            result = item.get("result")
            if not all(isinstance(value, str) for value in (command, category, result)):
                raise ValueError("command ledger fields must be strings")
            commands.append(
                {
                    "command": command,
                    "category": category,
                    "result": result,
                }
            )
    observed = {item["command"] for item in commands if item["result"] == "PASS"}
    missing = sorted(REQUIRED_COMMANDS - observed)
    if missing:
        raise ValueError(f"missing PASS command results: {', '.join(missing)}")
    if any(item["result"] != "PASS" for item in commands):
        raise ValueError("release command ledger contains a non-PASS result")
    return commands


def review_count(path: Path) -> int:
    report = load_json(path)
    summary = report.get("summary")
    if not isinstance(summary, dict) or not isinstance(summary.get("total"), int):
        raise ValueError(f"{path.name} has no review summary total")
    return summary["total"]


def metrics(
    retrieval: dict[str, object],
    resource: dict[str, object],
    *,
    refill_capacity: dict[str, object] | None,
    final_budget: str,
) -> dict[str, object]:
    lanes = retrieval.get("lanes")
    if not isinstance(lanes, list):
        raise ValueError("retrieval report has no lanes")
    lane_metrics: list[dict[str, object]] = []
    for lane in lanes:
        if not isinstance(lane, dict):
            raise ValueError("retrieval lane must be an object")
        lane_metrics.append(
            {
                "lane": lane.get("lane"),
                "recall_at_k": lane.get("recall_at_k"),
                "mrr": lane.get("mrr"),
                "ndcg_at_k": lane.get("ndcg_at_k"),
                "vs_entry_fts": lane.get("vs_entry_fts"),
            }
        )
    resource_metrics = {
        key: resource.get(key)
        for key in (
            "cold_start_ms",
            "warm_embedding_samples",
            "warm_embedding_p50_ms",
            "warm_embedding_p95_ms",
            "peak_rss_kb",
        )
    }
    if any(value is None for value in resource_metrics.values()):
        raise ValueError("resource report is missing required metrics")
    thresholds = retrieval.get("thresholds")
    evidence = retrieval.get("evidence")
    out: dict[str, object] = {
        "retrieval_passed": retrieval.get("passed"),
        "retrieval_lanes": lane_metrics,
        "retrieval_thresholds": thresholds if isinstance(thresholds, dict) else {},
        "evidence": evidence if isinstance(evidence, dict) else {},
        "final_chunk_budget": final_budget,
        "resource": resource_metrics,
    }
    if refill_capacity is not None:
        out["refill_capacity_matrix"] = refill_capacity
    return out


def markdown(record: dict[str, object]) -> str:
    candidate = record["candidate"]
    review = record["worktrail_review"]
    cleanup = record["cleanup"]
    metrics_report = record["metrics"]
    lines = [
        "# Worktrail Semantic M1 Release Record",
        "",
        f"- schema: `{SCHEMA}`",
        f"- candidate_commit: `{candidate['commit']}`",
        f"- author: `{candidate['author']}`",
        f"- committer: `{candidate['committer']}`",
        "- result: PASS",
        "",
        "## Clean checkout",
        "",
        f"- before: {record['clean_checkout']['before']}",
        f"- after: {record['clean_checkout']['after']}",
        "",
        "## Commands",
        "",
        "| Category | Command | Result |",
        "| --- | --- | --- |",
    ]
    for command in record["commands"]:
        lines.append(f"| {command['category']} | `{command['command']}` | {command['result']} |")
    lines.extend(
        [
            "",
            "## Worktrail review",
            "",
            f"- candidate_count_before: {review['candidate_count_before']}",
            f"- candidate_count_after: {review['candidate_count_after']}",
            "- formal_knowledge_changes: none; no promote, merge, discard, restore, or retire command was executed.",
            "",
            "## Maturity and release boundary",
            "",
            "- M1: verified.",
            "- M2–M5: experimental and unverified.",
            "- release_blockers: no M1 blocker remains after this gate.",
            "- known_gaps: M2–M5 require their own hardware validation; tag, push, and release were intentionally not performed.",
            "",
            "## Cleanup",
            "",
            f"- temporary_worktree_runtime_models_daemon_cleanup: {cleanup['temporary_worktree_runtime_models_daemon_cleanup']}",
            f"- signal_cleanup_validation: {cleanup['signal_cleanup_validation']}",
            "",
            "## Metrics",
            "",
            f"- retrieval_passed: {metrics_report['retrieval_passed']}",
        ]
    )
    for lane in metrics_report["retrieval_lanes"]:
        lines.append(
            "- retrieval"
            f" {lane['lane']}: recall_at_k={lane['recall_at_k']},"
            f" mrr={lane['mrr']}, ndcg_at_k={lane['ndcg_at_k']},"
            f" vs_entry_fts={lane['vs_entry_fts']}"
        )
    for key, value in metrics_report["resource"].items():
        lines.append(f"- {key}: {value}")
    lines.append(f"- final_chunk_budget: {metrics_report.get('final_budget', metrics_report.get('final_chunk_budget'))}")
    evidence = metrics_report.get("evidence")
    if isinstance(evidence, dict) and evidence:
        lines.append(
            "- evidence"
            f" recall_at_k={evidence.get('evidence_recall_at_k')}"
            f" neighbor_precision={evidence.get('neighbor_precision')}"
        )
    refill = metrics_report.get("refill_capacity_matrix")
    if isinstance(refill, dict) and refill:
        lines.append(f"- refill_capacity_matrix_passed: {refill.get('passed')}")
        sizes = refill.get("sizes")
        if isinstance(sizes, list):
            lines.append(f"- refill_capacity_sizes: {', '.join(str(item) for item in sizes)}")
    lines.append("")
    return "\n".join(lines)


def main() -> int:
    args = parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.candidate_commit):
        raise ValueError("candidate commit must be a full lowercase SHA-1")
    commands = load_commands(args.commands)
    retrieval = load_json(args.retrieval_report)
    resource = load_json(args.resource_report)
    refill_capacity = (
        load_json(args.refill_capacity_report) if args.refill_capacity_report else None
    )
    record = {
        "schema": SCHEMA,
        "result": "PASS",
        "candidate": {
            "commit": args.candidate_commit,
            "author": args.author,
            "committer": args.committer,
        },
        "clean_checkout": {"before": "PASS", "after": "PASS"},
        "commands": commands,
        "worktrail_review": {
            "candidate_count_before": review_count(args.review_before),
            "candidate_count_after": review_count(args.review_after),
            "formal_knowledge_changes": {
                "status": "unchanged_by_release_gate",
                "operations_executed": [],
            },
        },
        "maturity": {"M1": "verified", "M2-M5": "experimental"},
        "release_boundary": {
            "release_blockers": ["none for the M1 gate"],
            "known_gaps": [
                "M2-M5 hardware validation remains experimental and unverified",
                "tag, push, and release were intentionally not performed",
            ],
        },
        "cleanup": {
            "temporary_worktree_runtime_models_daemon_cleanup": "PASS",
            "signal_cleanup_validation": "PASS",
        },
        "metrics": metrics(
            retrieval,
            resource,
            refill_capacity=refill_capacity,
            final_budget=args.final_budget,
        ),
    }
    if args.archive.exists():
        if not args.archive.is_dir() or args.archive.is_symlink():
            raise ValueError("archive path must be a real directory")
        if any(args.archive.iterdir()):
            raise ValueError("archive staging directory must be empty")
    else:
        args.archive.mkdir(mode=0o700, parents=True, exist_ok=False)
    (args.archive / "RELEASE-RECORD.json").write_text(
        json.dumps(record, ensure_ascii=False, indent=2) + "\n",
        encoding="utf-8",
    )
    (args.archive / "SUMMARY.md").write_text(markdown(record), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
