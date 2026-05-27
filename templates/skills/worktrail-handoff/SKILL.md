---
name: worktrail-handoff
description: Create a Worktrail handoff before ending, compacting, switching agents, opening a new chat, opening a new conversation, or requests to end current chat.
---

# Worktrail Handoff

Use this skill before ending a session, compacting context, switching tools or agents, opening a new chat or new conversation, handing off work, requests to end current chat, or when the user says handoff, compact, switch chat, or switch agent.

1. If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
2. Run `worktrail handoff`; do not only output a copyable text handoff when the user asked for a Worktrail handoff.
3. Summarize the active state, current diff intent, validation, and next step.
4. `worktrail handoff` writes a durable record under `.worktrail/handoffs/`; only hook-generated handoff drafts stay pending for `/worktrail-review`.
5. Do not promote, merge, discard, restore, or retire without explicit user confirmation.
