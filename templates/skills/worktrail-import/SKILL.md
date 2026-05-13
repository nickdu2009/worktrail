---
name: worktrail-import
description: Sync and extract pending Worktrail candidates from one explicit Codex or Claude transcript file. Do not use for requests to find, scan, or import all current-project or historical conversations.
---

# Worktrail Import

Use this skill only when the user provides one explicit transcript file path to extract reusable knowledge from an existing AI coding conversation.

Hard stop: if the request asks to find, scan, sync, import, or extract all historical conversations, all current-project conversations, or any transcript set without exact file paths, explain that bulk transcript discovery/import is not implemented yet. Do not search transcript history directories or improvise a bulk import.

1. Ask for one explicit transcript file path if one was not provided.
2. Identify the source as `codex`, `claude`, or `manual`.
3. Run `worktrail sync <source> <transcript-file>`.
4. Run `worktrail extract --source <source> --session latest`.
5. Run `worktrail candidates list --format json` and count only newly created pending candidates if possible.
6. If zero pending candidates were created, say that no reviewable candidates were produced and do not call the import complete.
7. Summarize created pending candidates and their target paths.
8. Hand off review to `/worktrail-review`.

Do not promote, merge, discard, or write formal knowledge from this skill.
