---
name: worktrail-import
description: Import Worktrail evidence or pending candidates from transcripts, external agent sessions, or legacy KDD docs. Use when the user wants to import, sync, extract, migrate, or reuse knowledge from Codex, Claude, Cursor, transcript files, current-project conversations, or legacy KDD content.
---

# Worktrail Import

Use this skill when the user wants to import, sync, extract, migrate, or reuse knowledge from Codex, Claude, Cursor, transcript files, all current-project conversations, observed Cursor conversations, or legacy KDD docs.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Start with a bounded dry-run and show the counts before any state-changing import.
- Import, extract, distill, proposal apply, and candidate creation produce pending candidates only.
- Do not promote, merge, discard, restore, retire, or write formal knowledge from this skill.
- Transcript notes are evidence and must be distilled into semantic candidates before review or promotion.

## Workflow

Choose the lane that matches the source.

### Legacy handoffs and handoff candidates

1. Run `worktrail migrate handoff-v2` first. This is a complete read-only plan that validates every generated V2 record before any backup or target write.
2. Report all `invalid`, `conflict`, and unresolved items. Do not apply while either invalid or conflict counts are non-zero.
3. After the user confirms the reviewed plan, run `worktrail migrate handoff-v2 --apply --confirm`.
4. Verify that migrated sources left the legacy handoff and candidate surfaces, terminal candidate lifecycle is preserved, the external backup manifest is complete, and the project index rebuild succeeded.

### Legacy KDD knowledge base

1. Run `worktrail migrate kdd` first and show the dry-run matched, blocked, skipped, project item, and local item counts.
2. If the user asked to proceed, run `worktrail migrate kdd --write-candidates`.
3. Summarize created pending candidates, scopes, and target paths.
4. Explain that `local/**` migrates to user-scope candidates only and category README files are skipped by default.
5. Treat `active-knowledge-log.md` files as pending `migration_source` evidence, not as candidates to promote directly.
6. If active-log migration source knowledge needs promotion, run `worktrail distill <candidate-id>` or `worktrail distill --pending --split-sources`, write a `worktrail.distill.proposal.v1` JSON proposal, then run `worktrail distill validate <proposal.json>` and `worktrail distill apply <proposal.json>`.
7. Run `worktrail doctor migration` after review and only then use explicit cleanup for the legacy root.
8. Hand off review to the installed `worktrail-review` skill or the equivalent `worktrail review` CLI flow.

### Current-project Codex conversations

1. Run a bounded dry-run first and show the count. Prefer the exact command from `worktrail context --semantic=auto "maintenance"` when present, such as `worktrail import codex --since 14d`, otherwise use `worktrail import codex --limit 20`.
2. If the user asked to proceed or already asked for all conversations, run the same bounded command with `--all`, for example `worktrail import codex --since 14d --all`.
3. Summarize matched sessions, synced transcripts, extracted pending transcript evidence candidates, and skipped duplicates.
4. Distill all transcript evidence before review. Prefer `worktrail distill --pending --all --write-pack worktrail-distill.md`; for chat-sized batches, process every batch with `worktrail distill --pending --limit 5 --offset <N>` until all `transcript_notes` are covered.
5. As the current AI agent, summarize every evidence pack into a `worktrail.distill.proposal.v1` JSON proposal.
6. Run `worktrail distill validate <proposal.json>`.
7. If validation reports useful valid items, run `worktrail distill apply <proposal.json>` to create pending semantic candidates.
8. Hand off review to the installed `worktrail-review` skill or the equivalent `worktrail review` CLI flow.

### Observed Cursor conversations

1. Run a bounded dry-run first and show the observed, matched, skipped, and blocked counts. Prefer the exact command from `worktrail context --semantic=auto "maintenance"` when present, such as `worktrail import cursor --limit 20`.
2. Explain that Cursor import uses explicit `--file` paths or Worktrail-observed `observed-*.metadata.json` registry entries. It does not scan undocumented private Cursor directories.
3. If the user asked to proceed or already asked for observed Cursor conversations, run the same bounded command with `--all`, for example `worktrail import cursor --limit 20 --all`.
4. Summarize observed, synced, extracted, skipped, and blocked counts.
5. Distill transcript evidence before review, then hand off review to the installed `worktrail-review` skill or the equivalent `worktrail review` CLI flow.

### One explicit transcript file

1. Identify the source as `codex`, `claude`, `cursor`, or `manual`.
2. Run `worktrail sync <source> <transcript-file>`.
3. Run `worktrail extract --source <source> --session latest`.
4. Summarize created pending candidates and their target paths.
5. Hand off review to the installed `worktrail-review` skill or the equivalent `worktrail review` CLI flow.

Claude Code currently supports explicit transcript files through `sync claude <file>` and `extract --source claude`; it does not yet have automatic `worktrail import claude` discovery.
