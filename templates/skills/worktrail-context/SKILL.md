---
name: worktrail-context
description: Generate a Worktrail Context Pack when starting, resuming, continuing, or loading project context for a task.
---

# Worktrail Context

Use this skill when starting substantial project work, resuming an old task, continuing previous work, loading project memory, or when the user asks to start work, continue a previous task, load context, or load project context.

1. If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
2. Run `worktrail context "$ARGUMENTS"`.
3. Read the Context Pack into the current conversation.
4. Follow any active state, constraints, and next steps in the pack.
5. If the task is long or risky, start or update state with `/worktrail-state`.
