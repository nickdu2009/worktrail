---
name: worktrail-maintain
description: Maintain, clean up, advance, or summarize Worktrail knowledge through read-only discovery and explicit user-confirmed distill, review, and evidence lifecycle actions.
---

# Worktrail Maintain

Use this skill when the user asks to maintain, clean up, advance, summarize, or do low-intervention upkeep for Worktrail knowledge, pending evidence, review candidates, or evidence lifecycle actions.

1. Run the read-only discovery chain without asking for confirmation first.
2. Start with `worktrail context "maintenance"` and summarize only maintenance counts and next steps.
3. Follow the exact scope-aware commands from `maintenance.next_steps`. Treat those generated commands as authoritative. If the context suggests `--scope user`, keep that scope.
4. Run `worktrail distill --pending --summary` or its scoped variant to check pending transcript evidence. If it reports that another scope has work, rerun the exact suggested `--scope` command. If there is no pending evidence for a scope, report that as a no-op.
5. Run `worktrail review plan --format json` or its scoped variant and group pending semantic candidates by `recommended_action`.
6. Run `worktrail evidence plan --format json` or its scoped variant and group evidence lifecycle items by `recommended_action`.
7. Present one concise maintenance summary: pending evidence, pending review, evidence lifecycle actions, blockers, and commands that would be safe after confirmation.
8. Ask the user which lane to run: distill proposal workflow, review apply-plan workflow, single candidate command, evidence archive/discard, or no action.
9. Execute state-changing commands only after explicit user confirmation that identifies the exact candidate id, evidence id, saved plan file, and scope.
10. After any confirmed action, rerun the relevant read-only plan command and summarize the new counts.

Do not automatically commit git changes.

Do not paste transcript bodies, local absolute paths, session ids, usernames, or temporary pack/proposal paths into the chat or durable docs.

Do not promote, merge, discard, archive, restore, retire, or apply a saved review plan unless the user explicitly confirms the exact action.
