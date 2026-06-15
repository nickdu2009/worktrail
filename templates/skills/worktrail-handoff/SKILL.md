---
name: worktrail-handoff
description: Create a durable Worktrail handoff. Use when the user explicitly wants to hand off work, continue later in another chat, or switch agents with preserved recovery context.
---

# Worktrail Handoff

Use this skill when the user explicitly wants a handoff, wants to continue later in another chat, wants to switch agents, asks to end the current chat with durable recovery context, or otherwise asks to hand off work.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Prefer `worktrail state close --to handoff "<summary>"` when an active explicit state exists so the handoff is tied to the latest state and that state is archived atomically.
- Use bare `worktrail handoff "<summary>"` only when no active explicit state exists or the user explicitly needs a handoff-only record.
- Do not only output a copyable text handoff when the user asked for a Worktrail handoff.
- Do not create a durable handoff just because a normal reply finished, a subtask completed, or compacting happened without an explicit handoff boundary.
- Do not promote, merge, discard, restore, or retire without explicit user confirmation.

## Workflow

1. Summarize the active state, current diff intent, validation, risks, open questions, and next step.
2. If an active explicit state exists, run `worktrail state close --to handoff "<summary>"`.
3. Otherwise run `worktrail handoff "<summary>"`.
4. Remember that `worktrail handoff` writes a durable record under `.worktrail/handoffs/`; stop/session-end hooks now keep their output in runtime records instead of creating pending handoff drafts by default.

## Output

`[output: worktrail-handoff | completed <confidence> | summary:"..." validation:"..." | next:<action>]`
