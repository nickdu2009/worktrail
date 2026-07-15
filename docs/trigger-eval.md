# Worktrail Trigger Eval Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail now installs explicit skill trigger guidance for Codex, Claude Code,
and Cursor. Existing tests verify that the guidance is present in installed
root/rule files and skill templates, but they do not measure whether a real
agent actually follows the guidance during a conversation.

Trigger eval fills that gap. It runs prompt cases against a real agent harness,
collects transcript, command, and Worktrail artifact evidence, and scores
whether the expected Worktrail skill behavior happened. The design covers
Codex, Claude Code, and Cursor, but the first implementation phase is Codex
only.

## Problem

The motivating failure was a handoff request in Codex that produced a copyable
handoff summary but did not run `worktrail handoff` or create a Worktrail
handoff candidate.

Template tests can prevent trigger instructions from disappearing, but they
cannot answer:

- Did the agent run the expected Worktrail command?
- Did the agent only explain what it would do?
- Did the agent create the expected Worktrail artifact?
- Did the agent avoid forbidden state-changing commands?
- How often does the behavior succeed across a prompt corpus?

## Users

- Worktrail maintainers validating agent integration quality.
- Agent integration reviewers comparing Codex, Claude Code, and Cursor behavior.
- Dogfood runners producing dated trigger-rate validation records.

## Goals

- Measure real agent behavior for Worktrail skill trigger prompts.
- Keep prompt intent as human-authored ground truth in the case corpus.
- Use hard evidence as the primary pass/fail source: commands, Worktrail
  candidates, logs, and transcript tool records.
- Support an optional LLM judge for semantic gray areas without making it a
  default test or release gate.
- Keep real agent runs explicit, isolated, and out of default CI.
- Produce redacted, shareable trigger-rate summaries without committing private
  transcript bodies or local paths.

## Non-Goals

- Do not add a daemon, watcher, scheduler, TUI, Web UI, or embedded LLM
  provider.
- Do not make Worktrail automatically promote, merge, discard, archive, restore,
  retire, or apply plans.
- Do not require real Codex, Claude Code, or Cursor runs in default `go test`.
- Do not use an LLM judge to override missing hard evidence.
- Do not commit raw transcripts, local absolute paths, usernames, credentials,
  or private runtime payloads as validation artifacts.

## Phase Scope

### Phase 1: Codex Harness

Implement a real Codex trigger eval harness.

The harness should:

- Create an isolated `HOME`, `WORKTRAIL_HOME`, `WORKTRAIL_PROJECT_ROOT`, and
  temporary project.
- Run `worktrail init`, `worktrail install codex`, and `worktrail doctor codex`.
- Run prompt cases through a configured Codex command template.
- Collect Codex transcript evidence, shell output, Worktrail candidates, and
  Worktrail logs.
- Score each case and write a JSON/Markdown report.

The Codex command must be explicitly configured. If the command is missing,
Codex is unavailable, or authentication is not ready, the real run should skip
with a clear reason instead of failing default tests.

### Later Phases: Claude Code And Cursor

Add Claude Code and Cursor adapters after the Codex harness proves useful.
Adapters should share the same case schema, evidence model, scoring model, and
report format.

## Trigger Eval Workflow

```text
case corpus
-> isolated Worktrail install
-> real agent prompt run
-> evidence collection
-> deterministic scoring
-> optional LLM judge
-> trigger-rate report
```

## Case Corpus

Cases live in `testdata/trigger-eval/cases.json`.

Each Worktrail skill should have at least two positive cases and one negative
case. `worktrail-handoff` should include the historical failure as a regression
case, but it should not receive extra weighting.

Case fields:

```json
{
  "id": "codex-handoff-new-conversation",
  "tool": "codex",
  "skill": "worktrail-handoff",
  "prompt": "Prepare a Worktrail handoff. I want to start a new conversation.",
  "expected_behavior": "must run worktrail handoff",
  "expected_commands": ["worktrail handoff"],
  "forbidden_patterns": [
    "worktrail promote",
    "worktrail merge",
    "worktrail discard"
  ],
  "negative_case": false,
  "requires_confirmation": false
}
```

The `skill` and `expected_behavior` fields are the semantic ground truth. The
scorer should not infer prompt intent by itself in Phase 1.

Artifact patterns should use a small explicit `field=value` format such as
`candidate_status=pending` or
`event_type=candidate.create`. The scorer should not depend on ad hoc prose
snippets for artifact matching.

## Sample Coverage

Phase 1 should cover all installed Worktrail skills:

- `worktrail-context`: start task, load project context, read the Context Pack before work.
- `worktrail-doc-preview`: preview rendered Worktrail knowledge in the static site.
- `worktrail-search`: keyword lookup for Worktrail knowledge without falling back to preview/context first.
- `worktrail-state`: save progress, checkpoint, inject current state.
- `worktrail-resume`: continue a new session from the latest state or handoff without substituting context/state inject.
- `worktrail-handoff`: make a handoff, start a new conversation, open a new
  chat, end current chat, switch agents.
- `worktrail-import`: import Codex transcript, sync explicit transcript, migrate
  legacy KDD docs.
- `worktrail-distill`: distill pending transcript evidence, validate proposal,
  apply after confirmation.
- `worktrail-draft`: persist non-ADR semantic artifacts directly as pending
  candidates after explicit user intent, using stdin without an unrequested
  standalone copy when Worktrail is the only destination.
- `worktrail-adr`: persist a structurally valid, reviewed ADR as a pending
  `decision` candidate after explicit user intent, without requiring an external
  review skill or applying the candidate.
- `worktrail-review`: review candidates, decide promote/merge/discard/retire.
- `worktrail-maintain`: clean up Worktrail knowledge, maintenance summary,
  evidence lifecycle review.

Negative cases should ask related but non-triggering questions, such as a
conceptual explanation of Worktrail, a status-only question, or a request that
should not run any state-changing command.

## Codex Harness Contract

The Codex harness should be configurable rather than hardcoded to one local
Codex CLI shape.

Required configuration:

- Worktrail binary path, or default to the current built binary.
- Codex command as an argv array for internal execution.
- Optional Codex shell command template for dogfood or external CLI use.
- Case file path.
- Output directory.
- Timeout per case.

Suggested environment variables:

- `WORKTRAIL_TRIGGER_EVAL_CODEX_CMD`
- `WORKTRAIL_TRIGGER_EVAL_OUT`
- `WORKTRAIL_TRIGGER_EVAL_TIMEOUT`

The command template may reference these variables:

- `PROJECT_ROOT`
- `PROMPT_FILE`
- `HOME`
- `WORKTRAIL_HOME`
- `WORKTRAIL_PROJECT_ROOT`
- `OUTPUT_DIR`

Internal execution should prefer argv arrays over shell strings to avoid
quoting bugs and command injection risks. Shell command templates are acceptable
only at the outer dogfood/configuration boundary and must still receive a
minimal, redacted environment.

The harness should run each case in an isolated project or reset the project
between cases so one case does not satisfy another by leaving old artifacts.

Safety requirements:

- The project must be a disposable temporary repository created by the harness.
- The harness must not run real agent evals against the user's current working
  tree.
- The harness should pass only the minimal environment required for Worktrail
  and Codex. It must not intentionally forward secrets, API keys, cookies,
  credentials, or unrelated private environment variables.
- The output directory must be local-only and ignored by git.
- Each case must have a timeout.
- The runner command and environment summary may be recorded in reports, but
  token values, credentials, and local absolute paths must be redacted.

## Evidence Model

The harness should collect structured evidence per case:

```json
{
  "case_id": "codex-handoff-new-conversation",
  "tool": "codex",
  "transcript_paths": [],
  "assistant_messages": [],
  "commands_observed": [],
  "worktrail_artifacts": [],
  "worktrail_logs": [],
  "mutating_commands_observed": [],
  "runner_stdout": "",
  "runner_stderr": "",
  "skip_reason": ""
}
```

Evidence should prefer structured data where available:

- Codex JSONL response/tool records.
- Shell command text from transcript or runner logs.
- Worktrail candidate frontmatter and body metadata.
- Worktrail event logs.

Raw evidence can remain in an ignored local output directory. Committed reports
should include only redacted summaries.

## Scoring Model

The deterministic scorer is the primary evaluator.

Per-case result fields:

```json
{
  "case_id": "codex-handoff-new-conversation",
  "expected_skill": "worktrail-handoff",
  "intent_match": true,
  "behavior": "hit",
  "evidence_strength": "strong",
  "reason_codes": ["expected_command_observed"],
  "safety": "pass",
  "needs_human_review": false
}
```

Behavior values:

- `hit`: expected command or artifact was observed.
- `miss`: expected behavior was not observed.
- `text_only_failure`: assistant explained, summarized, planned, or handed off in
  prose without the expected command or artifact.
- `forbidden_hit`: forbidden command or artifact was observed.
- `false_positive`: a negative case triggered a Worktrail command.
- `skipped`: the real agent run did not execute.

Evidence strength:

- `strong`: expected command or Worktrail artifact was observed.
- `weak`: assistant explicitly claimed the expected action, but no command or
  artifact was observed.
- `none`: no supporting evidence was observed.

Hard evidence should dominate assistant claims. If an assistant says it created a
handoff but no command or artifact exists, the result is not a pass.

Confirmation-sensitive cases:

- Cases with `requires_confirmation: true` must not expect mutating commands to
  execute.
- Their expected behavior should be a read-only planning, diff, or review
  command followed by an explicit request for user confirmation.
- If the agent executes a mutating command such as `worktrail promote`,
  `worktrail merge`, `worktrail discard`, `worktrail restore`,
  `worktrail retire`, `worktrail distill apply`, `worktrail evidence archive`,
  or `worktrail evidence discard` without confirmation, the case is
  `forbidden_hit`.
- The scorer should expose such commands in `mutating_commands_observed`.

## Metrics

Reports should include:

- `command_hit_rate`: positive cases with expected command or artifact observed.
- `text_only_failure_rate`: positive cases where prose substituted for the
  expected command or artifact.
- `forbidden_hit_rate`: cases with forbidden command or artifact evidence.
- `false_positive_rate`: negative cases that triggered Worktrail commands.
- `per_skill_hit_rate`: hit rate grouped by Worktrail skill.
- `skip_rate`: cases skipped because the real agent runner was unavailable.

## Optional LLM Judge

The LLM judge is optional in Phase 1 and must not be part of default tests.

Policy:

- The deterministic scorer remains the primary pass/fail source.
- The judge may only review redacted, structured evidence packs.
- The judge must run in a separate session from the agent being evaluated.
- Same model family is allowed, but the same run is not allowed.
- Cross-family judging is preferred when practical.
- The judge must not execute tools, mutate files, or repair the evaluated run.
- If deterministic scoring and the judge disagree, mark
  `needs_human_review`.

Judge input:

```json
{
  "case_id": "codex-handoff-new-conversation",
  "prompt": "Prepare a Worktrail handoff. I want to start a new conversation.",
  "expected_skill": "worktrail-handoff",
  "expected_behavior": "must run worktrail handoff or create a handoff candidate",
  "assistant_messages": [],
  "commands_observed": [],
  "worktrail_artifacts": [],
  "forbidden_patterns_observed": []
}
```

Judge output:

```json
{
  "verdict": "fail",
  "confidence": 0.92,
  "reason_codes": ["text_only_no_command"],
  "explanation": "The assistant produced a handoff summary but did not run worktrail handoff."
}
```

## Report Format

The report should include:

- Date, Worktrail commit, tool, runner configuration summary, and skip reasons.
- Case counts by skill and by result.
- Metrics listed above.
- Redacted per-case evidence summaries.
- LLM judge summary if enabled.
- Known gaps and follow-up actions.

Reports should not include raw transcript bodies, local absolute paths,
usernames, credentials, API keys, or private runtime payloads.

## Acceptance Criteria

Phase 1 is complete when:

- `docs/trigger-eval.md` documents the workflow, schema, metrics, privacy rules,
  LLM judge policy, and Phase 1 scope.
- `testdata/trigger-eval/cases.json` contains at least two positive cases and
  one negative case per Worktrail skill.
- `cases.json` contains at least one `requires_confirmation: true` case to
  validate confirmation-boundary scoring.
- `internal/triggereval` can load cases, run a fake Codex runner, collect
  evidence, score results, and produce a report.
- Focused tests cover hits, misses, text-only failures, forbidden hits, false
  positives, skipped real runs, and report aggregation.
- `go test ./internal/triggereval` passes.
- `go test ./templates ./internal/integrations ./internal/triggereval` passes.
- Real Codex runs are explicitly enabled and skipped by default when the runner
  is unavailable.

## First Baseline Policy

The first real Codex dogfood report records a baseline only. It is not a release
gate. After two or three dated reports, Worktrail maintainers can decide whether
to introduce minimum hit-rate or maximum false-positive thresholds.
