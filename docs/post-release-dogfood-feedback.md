# Post-Release Dogfood Feedback Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail release acceptance is complete for the current CLI and skill workflow.
The next iteration should be driven by repeated real usage instead of speculative
feature expansion.

This document defines the post-release dogfood loop: how to run real maintenance
passes, what to record, how to triage findings, and when feedback becomes a new
implementation requirement.

## Goals

- Validate that users can rely on `/worktrail-maintain` with low intervention.
- Capture scope surprises, confirmation friction, and knowledge quality issues
  as structured feedback.
- Keep dogfood records useful without committing private transcript content or
  local machine details.
- Convert repeated or blocking findings into clear follow-up requirements.

## Non-Goals

- Do not add automatic mutation, daemon behavior, UI, vector search, or embedded
  LLM behavior.
- Do not make the CLI judge semantic quality beyond structural validation and
  safety checks.
- Do not require private transcripts, temporary packs, temporary proposals,
  session ids, usernames, or local absolute paths in committed records.
- Do not reopen completed release acceptance unless dogfood finds a regression
  against its requirements.

## Dogfood Cadence

Run at least three real maintenance passes after the release acceptance commit:

1. A no-op or mostly read-only pass where Worktrail has little pending work.
2. A pass with pending evidence that exercises distillation guidance.
3. A pass with pending semantic candidates or evidence lifecycle work that
   exercises review and confirmation choices.

Each pass should start with the read-only `/worktrail-maintain` chain or its CLI
equivalent:

```bash
worktrail context --format json maintenance
worktrail distill --pending --summary
worktrail review plan --format json
worktrail evidence plan --format json
```

Follow Worktrail-generated scoped commands when the output suggests them.

## Observation Template

Create one dated dogfood record per pass. The record should use this structure:

```markdown
# Worktrail Dogfood YYYY-MM-DD

Status: draft|complete

Commit under test: <short sha>

## Scenario

- Goal:
- Starting state:
- Scope(s) involved:

## Commands

| Command | Read-only or mutating | Expected | Actual |
| --- | --- | --- | --- |
| `worktrail context --format json maintenance` | read-only |  |  |

## Counts

- pending evidence:
- pending semantic candidates:
- review plan total:
- review groups:
- evidence lifecycle total:
- evidence actions:
- stale/skipped/blocked/failed:
- formal knowledge changed: yes|no

## User Intervention

- Read-only steps that required no confirmation:
- Confirmation prompts that felt necessary:
- Confirmation prompts that felt repetitive or unclear:
- Exact lane chosen:

## Knowledge Quality

- High-quality candidate patterns observed:
- Low-quality candidate patterns rejected or rewritten:
- Missing source traceability:
- Target path issues:

## Scope UX

- Scope hints followed:
- Scope surprises:
- Commands that should have preserved or suggested scope better:

## Privacy Check

- Transcript bodies included: no
- Local absolute paths included: no
- Session ids included: no
- Usernames included: no
- Temporary pack/proposal contents included: no

## Findings

| Finding | Category | Severity | Proposed follow-up |
| --- | --- | --- | --- |
|  | release-blocking|post-release|polish|no-action |  |  |

## Decision

- Release blocker: yes|no
- New requirement needed: yes|no
- Follow-up document or issue:
```

## Triage Rules

Use these categories for every finding:

- `release-blocking`: the current workflow is unsafe, loses traceability,
  mutates unexpectedly, leaks private content, or cannot complete a primary
  maintenance pass.
- `post-release`: the workflow completes safely, but repeated friction or
  ambiguity should become a requirement.
- `polish`: wording, formatting, or convenience improvements that do not change
  safety or core workflow success.
- `no-action`: expected behavior, local data issue, or finding already covered
  by existing documentation.

Promote a finding into a new requirement when at least one of these is true:

- The same friction appears in two or more dogfood passes.
- A user cannot confidently choose a safe lane without extra explanation.
- Scope behavior causes the wrong command to be attempted.
- Distilled knowledge repeatedly needs rewriting for the same reason.
- A read-only maintenance pass hides a blocker that should be visible.

## Requirement Intake

When a dogfood finding becomes a requirement, write it in this format:

```markdown
#### REQ-POST-XXX: Short Name

Priority: MUST|SHOULD|MAY

Problem:

Acceptance:

- 

Evidence:

- Dogfood record:
```

Do not implement new behavior directly from a raw dogfood note. First convert it
into a requirement with acceptance criteria and safety boundaries.

## Candidate Follow-Up Areas

These are likely areas to watch, not pre-approved implementation scope:

- `review apply-plan` dry-run or richer audit output.
- More concise maintenance summaries when counts are zero.
- Better scope-preserving command generation across all review and evidence
  lanes.
- A reusable dogfood record generator or docs template.
- Stronger skill examples for rejecting transcript summaries as semantic
  knowledge.

## Acceptance

This post-release dogfood loop is ready when:

- At least three dated dogfood records exist.
- Every finding is triaged with one of the defined categories.
- Private content is absent from committed records.
- Repeated findings are converted into numbered `REQ-POST-*` requirements.
- The backlog links to the active post-release requirement document.
