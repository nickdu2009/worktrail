---
name: worktrail-import
description: Discover, sync, and extract pending Worktrail candidates from existing Codex or Claude transcript files, including all current-project Codex conversations when requested.
---

# Worktrail Import

Use this skill when the user wants to extract reusable knowledge from existing AI coding conversations.

For all current-project Codex conversations:

1. Run `worktrail import codex` first and show the dry-run count.
2. If the user asked to proceed or already asked for all conversations, run `worktrail import codex --all`.
3. Summarize matched sessions, synced transcripts, extracted pending transcript evidence candidates, and skipped duplicates.
4. Distill all transcript evidence before review. Prefer `worktrail distill --pending --all --write-pack worktrail-distill.md`; for chat-sized batches, process every batch with `worktrail distill --pending --limit 5 --offset <N>` until all `transcript_notes` are covered.
5. As the current AI agent, summarize every evidence pack into semantic pending candidates with `worktrail candidates create`.
6. Hand off review to `/worktrail-review`.

For one explicit transcript file:

1. Identify the source as `codex`, `claude`, or `manual`.
2. Run `worktrail sync <source> <transcript-file>`.
3. Run `worktrail extract --source <source> --session latest`.
4. Summarize created pending candidates and their target paths.
5. Hand off review to `/worktrail-review`.

Do not promote, merge, discard, or write formal knowledge from this skill. Import, extract, distill, and candidate creation produce pending candidates only. Transcript notes are evidence and must be distilled into semantic candidates before review or promotion.
