---
name: worktrail-context
description: Generate a Worktrail Context Pack. Use when starting substantial project work or loading project context for a task.
---

# Worktrail Context

Use this skill when starting substantial project work, continuing work after explicitly asking for context, loading project memory, or when the user asks to start work, load context, or load project context.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Run `worktrail context "$ARGUMENTS"` as the starting command for substantial Worktrail-enabled project work.
- If the user wants to continue a previous session from the latest state or handoff, stop and use `/worktrail-resume` instead of `worktrail context`.
- If the task is long or risky after loading context, start or update state with `/worktrail-state`.

## Workflow

1. Run `worktrail context "$ARGUMENTS"`.
2. Read the Context Pack into the current conversation.
3. Follow any active state, constraints, and next steps in the pack.

## Output

`[output: worktrail-context | completed <confidence> | task:"..." validation:"..." | next:<action>]`
