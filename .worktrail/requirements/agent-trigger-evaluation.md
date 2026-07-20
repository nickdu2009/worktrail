---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "agent-trigger-evaluation",
  "scope": "project",
  "type": "requirement",
  "title": "Agent Trigger Evaluation",
  "status": "active",
  "lifecycle": "current",
  "topic": "agent-integrations"
}
---

# Agent Trigger Evaluation

## Purpose

Measure whether real agents follow installed Worktrail trigger guidance using hard evidence, rather than only testing that template instructions exist. Phase 1 targets Codex; Claude Code and Cursor use compatible adapters after the harness proves useful.

## Evaluation flow

```text
case corpus -> isolated Worktrail install -> real agent prompt run -> evidence collection -> deterministic scoring -> optional LLM review -> redacted trigger-rate report
```

## Case contract

The corpus defines the expected skill and behavior, expected commands or artifacts, forbidden patterns, negative-case marker, and confirmation sensitivity. Every installed Worktrail skill receives at least two positive and one negative case. Intent is human-authored ground truth; Phase 1 scoring does not infer it from prompt prose.

## Harness and safety

The harness creates a disposable temporary repository with isolated `HOME`, `WORKTRAIL_HOME`, and project root; it initializes and validates the applicable installation, invokes an explicitly configured Codex command, collects structured artifacts, and writes local-only ignored output. Missing runner availability or authentication skips a real run with a clear reason rather than failing default tests.

It must not run against the user's working tree, forward unrelated secrets or credentials, use unbounded environments, or commit raw transcripts, absolute paths, usernames, keys, or private runtime payloads. Every case has a timeout.

## Evidence and scoring

Structured transcript/tool records, command logs, candidates, and Worktrail events are primary evidence. Per-case behavior is `hit`, `miss`, `text_only_failure`, `forbidden_hit`, `false_positive`, or `skipped`; evidence strength is `strong`, `weak`, or `none`. Hard command or artifact evidence overrides assistant claims.

Confirmation-sensitive cases expect read-only planning and an explicit confirmation request; unconfirmed lifecycle mutations are forbidden hits. Reports include command hit, text-only failure, forbidden hit, false-positive, per-skill hit, and skip rates with redacted per-case summaries.

An optional LLM judge may inspect redacted structured evidence but never replaces deterministic scoring, mutates the evaluated run, or runs in the same session; disagreement requires human review.

## Phase-1 acceptance

Provide the case corpus, configurable Codex harness, fake-runner tests, deterministic report generation, and focused coverage for all behavior states and report aggregation. Real Codex runs remain explicit and are skipped by default when unavailable. The first real report is a baseline, not a release gate.

## Migration provenance

Distilled from `docs/trigger-eval.md`. The source remains in `docs/` until this candidate is promoted and inbound references are reviewed.
