# Review Plan Automation Requirements

Last updated: 2026-05-15

Status: proposed

## Summary

Worktrail should add review-stage automation that lets Codex and Claude Code
read pending candidates, classify review risk, and produce a recommended action
plan without changing candidate state. The plan is an agent contract first and a
human-readable summary second. Formal knowledge changes still require explicit
human confirmation before `promote`, `merge`, or `discard` runs.

## Goals

- Give agents a stable, machine-readable review contract.
- Keep the default review path conservative and auditable.
- Recommend `promote`, `merge`, `discard`, or `needs_human_review` for pending
  semantic candidates.
- Expose source traceability through `source_candidate_ids` and source health
  details.
- Preserve the boundary that `worktrail review` and `worktrail review plan` do
  not mutate knowledge or candidate state.
- Leave evidence cleanup and batch execution as later explicit phases.

## Non-Goals

- Do not auto-run `promote`, `merge`, or `discard` from `worktrail review`.
- Do not add an LLM provider or semantic similarity engine.
- Do not clean up `transcript_notes` evidence in the first phase.
- Do not include operational candidates such as `handoff` in the first version
  of the default review plan.
- Do not make text output the contract that agents parse.

## Primary Users

- Codex and Claude Code agents need structured JSON for grouping, explaining,
  and asking the user for confirmation.
- Human reviewers need concise text that explains the same plan without having
  to inspect raw candidate files first.

## Phase 1: Read-Only Review Plan

Add:

```bash
worktrail review plan [--scope project|user] [--format text|json]
```

Behavior:

- Read pending semantic candidates only.
- Build one review plan model.
- Render both text and JSON from that same model.
- Update `/worktrail-review` so Codex and Claude Code run
  `worktrail review plan --format json` before summarizing pending semantic
  candidates. Phase 1 updates the skill template; user-level installation
  verification is a post-implementation acceptance step.
- Do not change candidate status.
- Do not promote, merge, discard, restore, retire, delete, or replace formal
  knowledge.

The existing `worktrail review` command remains the lightweight human review
surface. The agent contract lives in `worktrail review plan`.

## JSON Contract

Schema name:

```json
"worktrail.review.plan.v1"
```

Top-level shape:

```json
{
  "schema": "worktrail.review.plan.v1",
  "generated_at": "2026-05-15T00:00:00Z",
  "scope": "project",
  "summary": {
    "total": 0,
    "promote": 0,
    "merge": 0,
    "discard": 0,
    "needs_human_review": 0
  },
  "items": []
}
```

Item shape:

```json
{
  "candidate_id": "distill-rule-example",
  "candidate_type": "rule",
  "status": "pending",
  "operation": "replace",
  "target_path": "rules/example.md",
  "target_exists": false,
  "source_candidate_ids": ["note-1"],
  "source_statuses": [
    {
      "candidate_id": "note-1",
      "candidate_type": "transcript_notes",
      "status": "pending",
      "redaction_status": "clean",
      "exists": true,
      "is_split_source": false
    }
  ],
  "warnings": [],
  "reason_codes": [
    "semantic_candidate",
    "new_target",
    "source_pending",
    "candidate_redaction_clean"
  ],
  "recommended_action": "promote",
  "commands": [
    "worktrail candidates diff distill-rule-example",
    "worktrail promote distill-rule-example"
  ],
  "snapshot": {
    "candidate_status": "pending",
    "candidate_operation": "replace",
    "candidate_target_path": "rules/example.md",
    "candidate_redaction_status": "clean",
    "candidate_created_at": "2026-05-15T00:00:00Z",
    "candidate_updated_at": "",
    "candidate_body_hash": "sha256:...",
    "candidate_metadata_hash": "sha256:...",
    "target_exists": false,
    "source_candidate_ids_hash": "sha256:..."
  }
}
```

`warnings` contains advisory warning codes from existing review analysis, such
as `target_exists`, `same_target_pending:N`, `merge_target_missing`, and
`replace_target_exists`. Decision explanations belong in `reason_codes`; agents
should not infer recommendation logic from `warnings` alone.

When a source id is missing, it must still be represented in
`source_statuses`:

```json
{
  "candidate_id": "missing-note",
  "candidate_type": "",
  "status": "missing",
  "redaction_status": "",
  "exists": false,
  "is_split_source": false
}
```

## Recommendation Rules

Action selection is deterministic. Apply rules in this order:

1. KDD split-source deferral check.
2. Structural discard checks:
   - empty or whitespace-only candidate body
   - later duplicate in a same-target, same-body pending group
3. Source, target, redaction, and operation checks for `promote` or `merge`.
4. Fallback to `needs_human_review`.

This precedence means an empty-body semantic candidate can be recommended for
`discard` even when it has no sources, except when the candidate is a KDD
split-source. KDD split-source candidates are classified before structural
discard checks and are never recommended for `promote`, `merge`, or `discard` in
v1 review plan.

### Promote

Recommend `promote` only when all conditions hold:

- Candidate is pending.
- Candidate type is semantic.
- Operation is `replace`.
- Target does not exist.
- No same-target pending conflict exists.
- `source_candidate_ids` is non-empty.
- Every source exists, is pending, and is an allowed evidence source.
- Candidate redaction status is `clean`.

### Merge

Recommend `merge` only when all conditions hold:

- Candidate is pending.
- Candidate type is semantic.
- Operation is `merge`.
- Target exists.
- No same-target pending conflict exists.
- `source_candidate_ids` is non-empty.
- Every source exists, is pending, and is an allowed evidence source.
- Candidate redaction status is `clean`.

### Discard

Version 1 enables `discard`, but only for structurally safe cases inside the
default semantic candidate set:

- Same-target pending candidate whose body hash is identical to an older pending
  candidate body.
- Semantic candidate with an empty or whitespace-only body.

Do not recommend `discard` for low confidence, missing source, target conflict,
same target with different body, redaction uncertainty, merge target missing, or
KDD split-source lessons. Those cases require human review or a later evidence
lifecycle workflow.

Duplicate body ordering is deterministic: among pending candidates with the
same scope, `target_path`, and normalized body hash, keep the first record
sorted by `created_at ASC, id ASC`; recommend `discard` only for later records.
The first record in a duplicate group is not automatically safe to promote or
merge while same-target duplicates are still pending; it should receive
`needs_human_review` with `older_duplicate_retained` until the later duplicates
are discarded.

### Needs Human Review

Recommend `needs_human_review` for every uncertain or risky case, including:

- Source is missing.
- Source is not pending.
- Source type is not transcript evidence or allowed split-source evidence.
- `source_candidate_ids` is empty.
- Candidate redaction is `blocked`, `redacted`, `unreviewed`, or unknown.
- Same-target pending candidates have different bodies.
- `replace` target already exists.
- `merge` target is missing.
- Candidate is a KDD split-source lesson.
- Any rule cannot make a conservative recommendation.

Non-semantic candidates are outside the default v1 input set. Future `--all`
support may classify them, but v1 must not scan operational candidates such as
`handoff` for default review plan recommendations.

## Source Handling

For each source id:

- Resolve `source_candidate_ids` only inside the same plan `scope`. Cross-scope
  sources are treated as missing in v1.
- If it exists, include candidate type, status, redaction status, and whether it
  is a split source.
- If it is missing, include a `source_statuses` item with `exists=false` and
  `status="missing"`, plus `source_missing`.
- If `source_candidate_ids` is empty, do not fail the plan. Add
  `source_candidate_ids_empty` and recommend `needs_human_review`.

Allowed evidence sources:

- Pending `transcript_notes`.
- Pending KDD split-source `lesson`.

KDD split-source detection:

- `target_path == "lessons/kdd-active-knowledge-log.md"`
- or tags include `split-source`
- or summary/body contains `Do not promote directly`

KDD split-source candidates are evidence sources, not formal knowledge targets.
In v1 review plan they should receive `needs_human_review` with
`kdd_split_source_not_promotable` and `defer_evidence_cleanup`. Their cleanup is
owned by the later evidence lifecycle flow, after the semantic candidates that
reference them have been promoted, merged, discarded, or otherwise resolved.

Source redaction is reported separately from candidate redaction. Source
redaction statuses are traceability signals; `source_redaction_unreviewed` alone
does not force `needs_human_review` when the source is pending and otherwise an
allowed evidence source. Candidate redaction affects the recommended action
because the candidate body is what would become formal knowledge.

## Reason Codes

Initial reason code set:

- `semantic_candidate`
- `non_semantic_candidate` (reserved for future `--all`)
- `new_target`
- `target_exists`
- `replace_target_exists`
- `merge_target_exists`
- `merge_target_missing`
- `same_target_pending`
- `duplicate_body_same_target`
- `older_duplicate_retained`
- `newer_duplicate_discardable`
- `source_missing`
- `source_candidate_ids_empty`
- `source_pending`
- `source_not_pending`
- `source_type_transcript_notes`
- `source_type_split_source`
- `source_type_unexpected`
- `source_redaction_clean`
- `source_redaction_redacted`
- `source_redaction_unreviewed`
- `source_redaction_blocked`
- `candidate_redaction_clean`
- `candidate_redaction_redacted`
- `candidate_redaction_unreviewed`
- `candidate_redaction_blocked`
- `empty_candidate_body`
- `kdd_split_source_not_promotable`
- `defer_evidence_cleanup`
- `conservative_discard_only`
- `needs_human_confirmation`

Reason codes are the stable interface. Human text may change, but agents should
not need to parse prose to understand the plan.

`worktrail review` may render compact human-facing warning codes such as
`source_missing:<id>` or `source_not_evidence:<id>`. `worktrail review plan`
should not require agents to parse ids out of warning strings. It should expose
source ids through `source_statuses` and use reason codes such as
`source_missing`, `source_not_pending`, and `source_type_unexpected`.

Human review warnings and review plan reason codes are related but not the same
interface. New human warnings should either map to an existing reason code or
add a reason code here before they are used by agent-facing review plan output.

## Hashes and Snapshots

Hashes use lowercase hex SHA-256 prefixed with `sha256:`.

Body hash canonical input:

- Trim trailing whitespace from the end of the whole candidate body, not from
  every line.
- Add exactly one trailing newline.
- Hash the resulting UTF-8 bytes.

`source_candidate_ids_hash` canonical input:

- Trim each source id.
- Keep the candidate metadata order.
- Encode the resulting string array as compact JSON.
- Hash the resulting UTF-8 bytes.

`candidate_metadata_hash` canonical input is compact JSON with keys sorted
lexicographically and RFC3339 UTC timestamps. It includes exactly these fields:

- `id`
- `scope`
- `candidate_type`
- `status`
- `operation`
- `target_path`
- `source_candidate_ids`
- `redaction_status`
- `tags`
- `evidence_label`
- `confidence`
- `created_at`
- `updated_at`

Missing optional values use their JSON zero value: empty string for strings,
empty array for arrays, and `0` for confidence. Zero time values encode as an
empty string. Non-zero time values encode as RFC3339 UTC strings. This keeps
future `apply-plan` stale checks deterministic.

## Text Rendering

Text output should be grouped by recommended action:

- Recommended promote
- Recommended merge
- Recommended discard
- Needs human review

Each item should show:

- Candidate id, type, operation, and target path.
- Source summary.
- Warning and reason codes.
- Suggested next command, starting with `worktrail candidates diff <id>`.

The text renderer must use the same review plan model as JSON output.

`commands` must always start with `worktrail candidates diff <id>`. Only items
whose `recommended_action` is `promote`, `merge`, or `discard` may include the
corresponding state-changing command. Items with `needs_human_review` must not
include `promote`, `merge`, or `discard` commands.

## Later Phases

### Phase 2: Documentation Examples

- README should show the review plan command and a minimal JSON example.
- Add a short agent workflow example showing how Codex or Claude Code summarizes
  the JSON plan by recommended action and asks for explicit confirmation before
  any state change.

### Phase 3: Review Plan Fixtures and Dogfood

- Add fixtures for clean promote, clean merge, target conflicts, missing source,
  split-source deferral, duplicate body, and empty body.
- Add validation documentation for KDD dogfood and review plan behavior.

### Phase 4: Confirmed Apply Plan

Future command:

```bash
worktrail review apply-plan <plan.json> --confirm
```

Requirements:

- Refuse to run without `--confirm`.
- Validate schema and scope.
- Validate candidate snapshots to detect stale plans.
- Treat a plan as stale when status, operation, target path, target existence,
  source ids, candidate redaction status, metadata hash, or body hash no longer
  matches the current candidate state.
- Support partial reports for promote, merge, and discard.
- Never make `worktrail review` itself mutate candidate state.

### Phase 5: Evidence Lifecycle

Future command:

```bash
worktrail evidence plan [--scope project|user] [--format text|json]
```

Requirements:

- Analyze `transcript_notes` and KDD split-source evidence.
- Report evidence already referenced by semantic candidates.
- Report evidence already covered by promoted or merged knowledge.
- Recommend keep, archive, or discard only through an explicit evidence
  lifecycle workflow.

## Acceptance Criteria

- `worktrail review plan --format json` emits
  `worktrail.review.plan.v1`.
- JSON contains summary counts and stable item fields.
- Text output is generated from the same plan model.
- `/worktrail-review` defaults to `worktrail review plan --format json` before
  summarizing pending semantic candidates. The template must be updated in Phase
  1; user-level install verification is required for release acceptance, not for
  normal unit tests.
- Clean replace candidate is recommended for `promote`.
- Clean merge candidate is recommended for `merge`.
- Duplicate same-target body after the first deterministic record and empty body
  can be recommended for `discard`.
- Missing source, empty `source_candidate_ids`, candidate redaction uncertainty,
  target conflict, same target with different body, and KDD split-source lessons
  are recommended for `needs_human_review`.
- Running review plan does not mutate any candidate or formal knowledge file.
- Existing `worktrail review`, `worktrail review --evidence`, and
  `worktrail review --all` behavior remains compatible.
