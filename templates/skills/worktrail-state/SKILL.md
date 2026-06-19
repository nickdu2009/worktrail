---
name: worktrail-state
description: Create, update, checkpoint, or inject the active Worktrail State Capsule. Use when the current task is long, risky, multi-step, or needs a saved checkpoint.
---

# Worktrail State

Use this skill when the active task is long, risky, multi-step, likely to compact, needs a checkpoint, or the user asks to record current state, update state, create a checkpoint, inject state, or save progress.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Use this skill for the current session's active state only.
- If the user wants to continue prior Worktrail work from the latest state or handoff in a new session, stop and use the installed `worktrail-resume` skill or `worktrail resume` instead of `worktrail state start` or `worktrail state inject`.
- Keep state factual: goal, constraints, evidence, decisions, work done, validation, open questions, and next step.

## Workflow

1. Choose the narrowest state command that fits the request:
   - Start: `worktrail state start "$ARGUMENTS"`
   - Update: `worktrail state update "$ARGUMENTS"`
   - Checkpoint: `worktrail state checkpoint --reason manual`
   - Inject: `worktrail state inject "$ARGUMENTS"`
2. Run the command for the current session.
3. Summarize the recorded state and the next step.

## Output

`[output: worktrail-state | completed <confidence> | action:"start|update|checkpoint|inject" validation:"..." | next:<action>]`
