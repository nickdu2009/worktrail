---
name: worktrail-resume
description: Resume exactly one prior Worktrail task with `worktrail resume`. Use when a new chat should continue task-scoped state, a local handoff, or a team durable handoff instead of starting fresh.
---

# Worktrail Resume

Use this skill when a new session should continue exactly one prior Worktrail task from task-bound handoff, explicit state/checkpoint, or degraded runtime material.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Use `worktrail resume` as the first recovery command when the intent is to continue prior work in a new session.
- Do not replace `worktrail resume` with `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, or `worktrail state show`.
- Read the resumed state output and linked records before continuing risky or stateful work.
- Keep subsequent state updates factual: goal, constraints, evidence, decisions, work done, validation, open questions, and next step.
- Never combine records from different `project_id` or `task_id` values.
- Interpret the latest state only inside the selected task, never as a repository-global winner.
- Recovery priority is local handoff, team handoff, explicit state, explicit checkpoint, runtime checkpoint, then runtime session. Runtime fallback is degraded.

## Workflow

1. Confirm that the user wants to continue prior Worktrail work in a new session.
2. Prefer an explicit selector when known:
   - `worktrail resume --task-id <id>`
   - `worktrail resume --task-title "<exact title>"`
   - `worktrail resume --ref [scope:]kind:id`
3. Run bare `worktrail resume` only when automatic selection is appropriate. If multiple tasks are recoverable, stop on the structured ambiguity error, show the candidate task ids/titles, and ask the user to choose; do not guess the newest file.
4. Read the selected source and `supporting_refs`. An explicit `checkpoint:<id>` ref may select a checkpoint even when the same task still has an active state.
5. If a team handoff reports code unavailable, tell the user to fetch or restore the referenced revision before implementation.
6. Continue work from the new explicit state instead of reconstructing or mixing context manually.
7. If the resumed task becomes long or risky again, keep using the installed `worktrail-state` skill or `worktrail state ...` for ongoing progress updates.

## Output

`[output: worktrail-resume | completed <confidence> | task:"..." validation:"..." | next:<action>]`
