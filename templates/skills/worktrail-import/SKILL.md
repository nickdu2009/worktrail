---
name: worktrail-import
description: Sync and extract pending Worktrail candidates from an existing Codex or Claude transcript file.
---

# Worktrail Import

Use this skill when the user wants to extract reusable knowledge from an existing AI coding conversation.

1. Ask for an explicit transcript file path if one was not provided.
2. Identify the source as `codex`, `claude`, or `manual`.
3. Run `worktrail sync <source> <transcript-file>`.
4. Run `worktrail extract --source <source> --session latest`.
5. Summarize created pending candidates and their target paths.
6. Hand off review to `/worktrail-review`.

Do not scan all historical conversations by default. Do not promote, merge, discard, or write formal knowledge from this skill.
