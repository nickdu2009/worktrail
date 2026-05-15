---
name: worktrail-maintain
description: Summarize Worktrail maintenance state and guide explicit user-confirmed distill, review, and evidence lifecycle actions.
---

# Worktrail Maintain

Use this skill when the user asks to clean up or advance Worktrail knowledge with minimal intervention.

1. Run `worktrail context "maintenance"` and summarize only maintenance counts and next steps.
2. Run `worktrail distill --pending --summary` to check pending transcript evidence. If there is no pending evidence, report that as a no-op.
3. Run `worktrail review plan --format json` and group pending semantic candidates by `recommended_action`.
4. Run `worktrail evidence plan --format json` and group evidence lifecycle items by `recommended_action`.
5. Present one concise maintenance summary: pending evidence, pending review, evidence lifecycle actions, blockers, and commands that would be safe after confirmation.
6. Ask the user which lane to run: distill proposal workflow, review apply-plan workflow, single candidate command, evidence archive/discard, or no action.
7. Execute state-changing commands only after explicit user confirmation that identifies the exact candidate id, evidence id, or saved plan file.
8. After any confirmed action, rerun the relevant read-only plan command and summarize the new counts.

Do not automatically commit git changes.

Do not paste transcript bodies, local absolute paths, session ids, usernames, or temporary pack/proposal paths into the chat or durable docs.

Do not promote, merge, discard, archive, restore, retire, or apply a saved review plan unless the user explicitly confirms the exact action.
