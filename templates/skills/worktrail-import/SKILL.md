---
name: worktrail-import
description: Discover, sync, and extract pending Worktrail candidates from Codex, Claude, or Cursor transcript files, including all current-project Codex conversations and observed Cursor transcripts when requested.
---

# Worktrail Import

Use this skill when the user wants to extract reusable knowledge from existing AI coding conversations.

For an existing `docs/knowledge-driven-development/` project knowledge base:

1. Run `worktrail import kdd` first and show the dry-run matched, blocked, skipped, and local-skipped counts.
2. If the user asked to proceed, run `worktrail import kdd --all`.
3. Summarize created pending semantic candidates and their target paths.
4. Explain that `local/**` and category README files are skipped by default.
5. Treat `project/active-knowledge-log.md` as a pending split source, not as a candidate to promote directly.
6. If active-log split-source knowledge needs promotion, run `worktrail distill <candidate-id>` or `worktrail distill --pending --split-sources`, write a `worktrail.distill.proposal.v1` JSON proposal, then run `worktrail distill validate <proposal.json>` and `worktrail distill apply <proposal.json>`.
7. Hand off review to `/worktrail-review`.

For all current-project Codex conversations:

1. Run `worktrail import codex` first and show the dry-run count.
2. If the user asked to proceed or already asked for all conversations, run `worktrail import codex --all`.
3. Summarize matched sessions, synced transcripts, extracted pending transcript evidence candidates, and skipped duplicates.
4. Distill all transcript evidence before review. Prefer `worktrail distill --pending --all --write-pack worktrail-distill.md`; for chat-sized batches, process every batch with `worktrail distill --pending --limit 5 --offset <N>` until all `transcript_notes` are covered.
5. As the current AI agent, summarize every evidence pack into a `worktrail.distill.proposal.v1` JSON proposal.
6. Run `worktrail distill validate <proposal.json>`.
7. If validation reports useful valid items, run `worktrail distill apply <proposal.json>` to create pending semantic candidates.
8. Hand off review to `/worktrail-review`.

For observed Cursor conversations:

1. Run `worktrail import cursor` first and show the dry-run observed, matched, skipped, and blocked counts.
2. Explain that Cursor import uses explicit `--file` paths or Worktrail-observed `observed-*.metadata.json` registry entries. It does not scan undocumented private Cursor directories.
3. If the user asked to proceed or already asked for observed Cursor conversations, run `worktrail import cursor --all`.
4. Summarize observed, synced, extracted, skipped, and blocked counts.
5. Distill transcript evidence before review, then hand off review to `/worktrail-review`.

For one explicit transcript file:

1. Identify the source as `codex`, `claude`, `cursor`, or `manual`.
2. Run `worktrail sync <source> <transcript-file>`.
3. Run `worktrail extract --source <source> --session latest`.
4. Summarize created pending candidates and their target paths.
5. Hand off review to `/worktrail-review`.

Claude Code currently supports explicit transcript files through `sync claude <file>` and `extract --source claude`; it does not yet have automatic `worktrail import claude` discovery.

Do not promote, merge, discard, restore, retire, or write formal knowledge from this skill. Import, extract, distill, proposal apply, and candidate creation produce pending candidates only. Transcript notes are evidence and must be distilled into semantic candidates before review or promotion.
