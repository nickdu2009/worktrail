---
name: worktrail-handoff
description: Create a durable Worktrail handoff. Use when ending a session, compacting context, switching tools or agents, or moving work to a new chat or conversation.
---

# Worktrail Handoff

Use this skill before ending a session, compacting context, switching tools or agents, opening a new chat or new conversation, handing off work, requests to end current chat, or when the user says handoff, compact, switch chat, or switch agent.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Run `worktrail handoff`; do not only output a copyable text handoff when the user asked for a Worktrail handoff.
- Do not promote, merge, discard, restore, or retire without explicit user confirmation.

## Workflow

1. Summarize the active state, current diff intent, validation, and next step.
2. Run `worktrail handoff`.
3. Remember that `worktrail handoff` writes a durable record under `.worktrail/handoffs/`; stop/session-end hooks now keep their output in runtime records instead of creating pending handoff drafts by default.

## Output

`[output: worktrail-handoff | completed <confidence> | summary:"..." validation:"..." | next:<action>]`
