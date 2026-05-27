---
name: worktrail-maintain
description: Maintain, clean up, advance, or summarize Worktrail knowledge through read-only discovery and explicit user-confirmed distill, review, and evidence lifecycle actions.
---

# Worktrail Maintain

Use this skill when the user asks to maintain, clean up, advance, summarize, or do low-intervention upkeep for Worktrail knowledge, pending evidence, review candidates, or evidence lifecycle actions.

1. If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
2. Run the read-only discovery chain without asking for confirmation first.
3. Start with `worktrail context "maintenance"` and summarize only maintenance counts and next steps.
4. Follow the exact scope-aware commands from `maintenance.next_steps`. Treat those generated commands as authoritative. If the context suggests `--scope user`, keep that scope.
5. Run `worktrail distill --pending --summary` or its scoped variant to check pending evidence that still needs distillation. If it reports that another scope has work, rerun the exact suggested `--scope` command. If there is no pending evidence for a scope, report that as a no-op.
6. Run `worktrail review plan --format json` or its scoped variant and group pending semantic candidates by `recommended_action`.
7. Run `worktrail evidence plan --format json` or its scoped variant and group evidence lifecycle items by `recommended_action`.
8. Present one concise maintenance summary: pending evidence, pending review, evidence lifecycle actions, blockers, and commands that would be safe after confirmation. Treat this as the default inbox summary: operational candidates may still exist in preview or `worktrail review --all`, but they should not be counted as default maintenance work unless the user explicitly asks to inspect them.
9. When the user asks to capture a confirmed finding directly into Worktrail knowledge, use `worktrail note add --type <type> --target <path> --title <title> --summary <summary> --evidence-label <label> ...` to create a pending semantic candidate. Do not edit formal `.worktrail` knowledge files directly.
10. Ask the user which lane to run: note capture, distill proposal workflow, review apply-plan workflow, review apply-candidates batch command, single candidate command, evidence archive/discard, or no action.
11. Execute review batch commands only after explicit user confirmation that identifies the action, exact candidate id list, and scope for `worktrail review apply-candidates --promote|--merge|--discard <id...> [--scope ...]`.
12. Execute other state-changing commands only after explicit user confirmation that identifies the exact candidate id, evidence id, saved plan file, and scope.
13. After any confirmed action, rerun the relevant read-only plan command and summarize the new counts.

Do not automatically commit git changes.

Do not paste transcript bodies, local absolute paths, session ids, usernames, or temporary pack/proposal paths into the chat or durable docs.

Do not promote, merge, discard, archive, restore, retire, or apply a saved review plan unless the user explicitly confirms the exact action.
