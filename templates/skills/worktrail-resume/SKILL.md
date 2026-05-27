---
name: worktrail-resume
description: Continue prior Worktrail work in a brand-new session. Use the `worktrail resume` command — never `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, or `worktrail state show` — whenever the user wants to resume the latest Worktrail session, pick up where they left off, continue the previous work, load prior Worktrail context for a new chat, recover from the most recent state or handoff, or start a fresh session that should inherit the previous task. Triggers include phrases like "resume my Worktrail session", "continue the previous Worktrail work", "pick up where I left off", "load my previous Worktrail context for this new chat", "resume the latest state", or "continue the last handoff".
---

# Worktrail Resume

Use this skill when a new session should continue prior Worktrail work from the latest active state and/or durable handoff.

## Rules

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Use `worktrail resume` as the first recovery command when the intent is to continue prior work in a new session.
- Do not replace `worktrail resume` with `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, or `worktrail state show`.
- Read the resumed state output and linked records before continuing risky or stateful work.
- Keep subsequent state updates factual: goal, constraints, evidence, decisions, work done, validation, open questions, and next step.

## Workflow

1. Confirm that the user wants to continue prior Worktrail work in a new session.
2. Run `worktrail resume "$ARGUMENTS"` or `worktrail resume` when no new task title is needed before using any fallback state/context command.
3. Read the resumed state output and note the linked state and/or handoff records.
4. Continue work from those linked records instead of reconstructing context manually.
5. If the resumed task becomes long or risky again, keep using `/worktrail-state` for ongoing progress updates.

## Output

`[output: worktrail-resume | completed <confidence> | task:"..." validation:"..." | next:<action>]`
