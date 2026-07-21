---
name: worktrail-context
description: Generate a Worktrail Context Pack. Use when starting substantial project work or loading project context for a task.
---

# Worktrail Context

Use this skill when starting substantial project work, continuing work after explicitly asking for context, loading project memory, or when the user asks to start work, load context, or load project context.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Run `worktrail context --semantic=auto "$ARGUMENTS"` as the starting command for substantial Worktrail-enabled project work. Prefer `auto` over `required` so missing or mismatched semantic generations visibly degrade instead of blocking the Context Pack.
- If stderr reports `semantic context fallback (auto)`, continue with the pack and note the reason; do not silently treat the run as full semantic selection.
- If the user wants to continue a previous session from the latest state or handoff, stop and use the installed `worktrail-resume` skill or `worktrail resume` instead of `worktrail context`.
- If the task is long or risky after loading context, start or update state with the installed `worktrail-state` skill or the matching `worktrail state ...` command.
- Treat the Task Recovery Summary as task-scoped. Never merge state, handoffs, checkpoints, or runtime records from different task ids.
- Local/team handoffs are runtime recovery records, not formal knowledge or pending review candidates.

## Workflow

1. Run `worktrail context --semantic=auto "$ARGUMENTS"`.
2. Read the Context Pack into the current conversation.
3. Read each task recovery summary by `task_id`, source kind, priority, and structured ref. If more than one task could match the user's continuation intent, stop and use `worktrail resume --task-id <id>` or `--ref [scope:]kind:id` instead of choosing implicitly.
4. Treat hook runtime session/checkpoint material as degraded fallback. Runtime records expire after 14 days and recovery reads expose at most the latest five valid records per task.
5. Follow the selected task's constraints and next steps without importing another task's recovery records.

## Output

`[output: worktrail-context | completed <confidence> | task:"..." validation:"..." | next:<action>]`
