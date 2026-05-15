# Distill Knowledge Quality Examples

Last updated: 2026-05-15

These examples guide agent and human review of distilled semantic candidates.
They are workflow guidance, not CLI semantic validation rules.

## High-Quality Semantic Candidate

```json
{
  "candidate_type": "workflow",
  "title": "Run Release Smoke With Disposable State",
  "summary": "Release validation must prove the CLI works without relying on local user or project state.",
  "target_path": "workflows/release-smoke.md",
  "operation": "replace",
  "source_candidate_ids": ["evidence-release-smoke"],
  "body": "# Run Release Smoke With Disposable State\n\nBuild a temporary `worktrail` binary and run release smoke commands with disposable `HOME`, `WORKTRAIL_HOME`, and `WORKTRAIL_PROJECT_ROOT` so installed tools or private local candidates cannot affect the result."
}
```

Why this is acceptable:

- The body states a durable workflow, not a summary of a transcript.
- The summary explains reusable value.
- The target path is stable, type-appropriate, and contains no machine-local
  detail.
- `source_candidate_ids` preserves traceability to evidence.

## Low-Quality Pattern To Reject

```json
{
  "candidate_type": "lesson",
  "title": "What Happened In The Chat",
  "summary": "The transcript talked about release validation.",
  "target_path": "lessons/tmp-local-chat.md",
  "operation": "replace",
  "source_candidate_ids": [],
  "body": "# What Happened\n\nThe user asked about release acceptance and the agent ran some commands."
}
```

Why an agent or reviewer should reject or rewrite this:

- It summarizes a chat instead of preserving a durable rule, decision,
  validation, workflow, glossary entry, project fact, prompt, or lesson.
- The target path is temporary-looking and not tied to a reusable knowledge
  category.
- It lacks `source_candidate_ids`, so later review cannot trace the candidate
  back to evidence.
- The summary describes the transcript rather than the durable value.

The Worktrail CLI may still accept syntactically valid proposal JSON. Semantic
quality remains an agent and user review responsibility.
