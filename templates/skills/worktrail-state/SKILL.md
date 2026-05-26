---
name: worktrail-state
description: Create, update, checkpoint, or inject the active Worktrail State Capsule for long, risky, or compactable work.
---

# Worktrail State

Use this skill when the active task is long, risky, multi-step, likely to compact, needs a checkpoint, or the user asks to record current state, update state, create a checkpoint, inject state, or save progress.

If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.

- Start: `worktrail state start "$ARGUMENTS"`
- Update: `worktrail state update --session latest`
- Checkpoint: `worktrail state checkpoint --reason manual`
- Inject: `worktrail state inject "$ARGUMENTS"`

Keep state factual: goal, constraints, evidence, decisions, work done, validation, open questions, and next step.
