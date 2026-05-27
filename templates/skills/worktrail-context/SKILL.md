---
name: worktrail-context
description: Generate a Worktrail Context Pack when starting, continuing, or loading project context for a task.
---

# Worktrail Context

Use this skill when starting substantial project work, continuing work after explicitly asking for context, loading project memory, or when the user asks to start work, load context, or load project context.

1. If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
2. Run `worktrail context "$ARGUMENTS"`.
3. Read the Context Pack into the current conversation.
4. Follow any active state, constraints, and next steps in the pack.
5. If the user wants to continue a previous session from the latest state or handoff, stop and use `/worktrail-resume` instead of `worktrail context`.
6. If the task is long or risky after loading context, start or update state with `/worktrail-state`.
