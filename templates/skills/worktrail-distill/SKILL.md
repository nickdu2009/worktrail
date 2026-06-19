---
name: worktrail-distill
description: Distill pending Worktrail evidence into semantic candidates. Use when `transcript_notes`, `migration_source`, legacy KDD split-source, or imported evidence should become reusable knowledge candidates.
---

# Worktrail Distill

Use this skill when pending `transcript_notes`, `migration_source`, legacy KDD split-source evidence, or imported evidence should become semantic Worktrail candidates.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Preserve the exact suggested `--scope` from discovery for validate/apply. Use `worktrail distill --pending --split-sources --summary` when `migration_source` or legacy KDD split-source evidence is intentionally in scope.
- Temporary pack or proposal filenames must not include transcript ids, session ids, local usernames, or local absolute paths.
- Proposal bodies must be durable semantic knowledge, not transcript summaries. Candidate summaries should explain reusable value, `target_path` values must be stable and type-appropriate, and distilled semantic candidates must keep `source_candidate_ids`.
- Store the proposal in a temporary file. Prefer a system temporary directory or an ignored workspace-local file. Do not commit the pack or proposal unless the user explicitly asks.
- Wait for explicit user confirmation before applying. Confirmation must identify the proposal file and scope.

Never promote, merge, discard, archive, evidence-discard, restore, retire, delete, or replace from this skill.

Do not automatically commit git changes.

Never promote or merge `transcript_notes`; they are evidence and must become semantic candidates first.

Never promote, merge, or discard `migration_source` or legacy KDD split-source candidates directly. They are evidence-like sources.

## Workflow

1. Run `worktrail distill --pending --summary` to discover pending transcript evidence. If another scope has work, rerun the exact suggested `--scope` command and preserve that scope. Use `worktrail distill --pending --split-sources --summary` when split-source evidence is intentionally in scope.
2. If useful evidence exists, create a temporary pack with `worktrail distill --pending --all --write-pack <temporary-file>` or the scoped equivalent.
3. Read the pack and draft a `worktrail.distill.proposal.v1` proposal.
4. Run `worktrail distill validate <proposal.json>` and summarize candidate ids, target paths, candidate types, operations, warning codes, and error codes. Do not paste transcript evidence bodies.
5. After confirmation, run `worktrail distill apply <proposal.json>` with the same scope used for validation.
6. Delete temporary pack and proposal files unless the user explicitly asks to keep them.
7. Run `worktrail review plan --format json` and hand off review to the installed `worktrail-review` skill or the equivalent `worktrail review` CLI flow.
