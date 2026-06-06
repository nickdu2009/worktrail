# Semantic Text Safety Contract

Last updated: 2026-06-06

Status: implemented

## Summary

Worktrail semantic knowledge text must pass shared safety checks before pending
candidates are created or formal knowledge is written. This document defines the
stable code taxonomy and the machine-readable output fields agents should parse.

## Code Layers

| Field | Meaning | Examples |
| --- | --- | --- |
| `error_codes` | Validation or execution failure | `summary_redactable_secret_or_pii`, `cli_usage_error` |
| `reason_codes` | Read-only recommendation or apply-flow explanation | `target_exists`, `apply_failed` |
| `warning_codes` | Non-blocking hints | `target_exists`, `same_target_pending:2` |

Do not treat `reason_codes` as validation failures. Do not treat `warning_codes`
as blocking errors.

## Semantic Issue Naming

Semantic issues use:

```text
{field}_{suffix}
```

Supported suffixes:

| Suffix | Meaning |
| --- | --- |
| `blocked_sensitive_material` | Hard-blocked secret or credential material |
| `redactable_secret_or_pii` | Detected secret/PII pattern that should be redacted |
| `local_absolute_path` | Local absolute path such as `/Users/...` |
| `raw_transcript_style_conversation` | Raw user/assistant transcript-style text |
| `required` | Required field missing |

Examples:

- `title_blocked_sensitive_material`
- `summary_redactable_secret_or_pii`
- `body_raw_transcript_style_conversation`
- `reason_required`

## Field Check Matrix

| Field | CheckBlocked | CheckTranscript | Notes |
| --- | --- | --- | --- |
| `title` | yes | no | create/validate paths |
| `summary` | yes | yes | create/validate paths |
| `body` create/validate | yes | yes | proposal/note/draft/maintain validate |
| `body` formal write | via `redact.Scan` | yes | promote/merge/restore last-line check |
| `reason` | yes | no | retire/evidence archive/discard/maintain actions |

## CLI Error Envelope

Direct command failures in `--format json` mode return:

```json
{
  "schema": "worktrail.cli.error.v1",
  "ok": false,
  "command": "worktrail note add",
  "message": "summary contains redactable secret or PII pattern",
  "error_codes": ["summary_redactable_secret_or_pii"],
  "issues": [
    {
      "field": "summary",
      "code": "summary_redactable_secret_or_pii",
      "message": "summary contains redactable secret or PII pattern"
    }
  ]
}
```

Contract:

- stdout contains the envelope
- library callers receive `Run() == nil`
- agents must inspect `ok == false` and `error_codes`
- text mode continues to return a normal Go error

Common CLI codes:

- `cli_usage_error`
- `cli_scope_mismatch`
- `cli_file_load_failed`
- `cli_confirmation_required`
- `cli_candidate_not_found`
- `cli_command_failed`

## Command Output Map

| Command | reason_codes | error_codes | warning_codes |
| --- | --- | --- | --- |
| `review plan` | yes | no | text warnings |
| `evidence plan` | yes | no | no |
| `distill validate/apply` | no | yes | yes |
| `maintain validate/apply` | no | yes | no |
| `review apply-plan/candidates` | yes | yes on failed items | no |
| `note add` / `draft create` / `candidates create` | no | envelope on JSON failure | no |
| `promote` / `merge` / `restore` / `retire` | no | envelope on JSON failure | no |
| `evidence archive` / `evidence discard` | no | envelope on JSON failure | no |

## Related Docs

- [Agent Text and CLI Write Boundary](agent-cli-write-boundary.md)
- [Review Plan Automation](review-plan-automation.md)
- [Knowledge Maintenance Proposal Workflow](knowledge-maintenance-proposal-workflow.md)
