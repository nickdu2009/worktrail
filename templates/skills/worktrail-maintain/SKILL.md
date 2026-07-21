---
name: worktrail-maintain
description: Maintain Worktrail knowledge through read-only discovery and user-confirmed follow-up actions. Use when the user wants cleanup, summaries, pending review triage, or evidence lifecycle handling.
---

# Worktrail Maintain

Use this skill when the user asks to maintain, clean up, advance, summarize, or do low-intervention upkeep for Worktrail knowledge, pending evidence, review candidates, or evidence lifecycle actions.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Run the read-only discovery chain without asking for confirmation first.
- Follow the exact scope-aware commands from `maintenance.next_steps`. Treat those generated commands as authoritative. If the context suggests `--scope user`, keep that scope.
- Use `worktrail note add --type <type> --target <path> --title <title> --summary <summary> --evidence-label <label> ...` when the user wants to capture a confirmed finding directly into Worktrail knowledge. Do not edit formal `.worktrail` knowledge files directly.
- Do not paste transcript bodies, local absolute paths, session ids, usernames, or temporary pack/proposal paths into the chat or durable docs.
- Do not promote, merge, discard, archive, restore, retire, or apply a saved review plan unless the user explicitly confirms the exact action.

Do not automatically commit git changes.

## Workflow

1. Start with `worktrail context --semantic=auto "maintenance"` and summarize only maintenance counts and next steps.
2. Run `worktrail distill --pending --summary`, `worktrail review plan --format json`, and `worktrail evidence plan --format json` or their scoped variants. If a command reports that another scope has work, rerun the exact suggested `--scope` command. If there is no work for a scope, report that as a no-op.
3. Present one concise maintenance summary: pending evidence, pending review, evidence lifecycle actions, blockers, and commands that would be safe after confirmation. Treat this as the default inbox summary: operational candidates may still exist in preview or `worktrail review --all`, but they should not be counted as default maintenance work unless the user explicitly asks to inspect them.
4. Ask the user which lane to run: note capture, distill proposal workflow, review apply-plan workflow, review apply-candidates batch command, single candidate command, evidence archive/discard, or no action.
5. Execute `worktrail review apply-candidates --promote|--merge|--discard <id...> [--scope ...]` only after explicit user confirmation that identifies the action, exact candidate id list, and scope.
6. Execute other state-changing commands only after explicit user confirmation that identifies the exact candidate id, evidence id, saved plan file, and scope.
7. After any confirmed action, rerun the relevant read-only plan command and summarize the new counts.

## Operational maintenance

- Runtime cleanup: run `worktrail runtime prune` first; delete only after explicit confirmation with `worktrail runtime prune --apply --confirm`.
- Malformed state/runtime recovery: run `worktrail doctor recovery` first; quarantine repairable malformed state and runtime records only with `worktrail doctor recovery --apply --confirm`. The obsolete `--repair` flag is not accepted.
- Operation health: run `worktrail doctor ops status`; replay pending operations or remove a recoverable stale lock only after explicit confirmation with `worktrail doctor ops repair --confirm`.
- Handoff health: run `worktrail handoff doctor`, then `worktrail handoff repair` for a dry-run plan; quarantine malformed local handoffs or apply other reviewed local repairs only with `worktrail handoff repair --apply --confirm`. Team handoffs remain immutable.
- These commands do not replace handoff creation. An unfinished handoff must still use `--next-step "<action>"`; completed work must use `--complete`.
