---
name: worktrail-handoff
description: Create and optionally publish a task-scoped Worktrail handoff. Use when the user explicitly wants to hand off work, continue later in another chat, or switch agents with preserved recovery context.
---

# Worktrail Handoff

Use this skill when the user explicitly wants a handoff, wants to continue later in another chat, wants to switch agents, asks to end the current chat with durable recovery context, or otherwise asks to hand off work.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Prefer `worktrail state close --to handoff --next-step "<action>" "<summary>"` when an active explicit state exists so the handoff is tied to the latest state and that state is archived atomically.
- When no active explicit state exists, use `worktrail handoff create --next-step "<action>" "<summary>"`; use `--complete` only when no follow-up remains.
- Local is the default. Do not publish a team handoff unless the user explicitly asks to share or publish it.
- Do not only output a copyable text handoff when the user asked for a Worktrail handoff.
- Do not create a handoff just because a normal reply finished, a subtask completed, or compacting happened without an explicit handoff boundary.
- Treat handoffs as runtime recovery records, not formal knowledge or review candidates. Never create `candidate_type=handoff`; that legacy candidate type is retired.
- Hooks never create or publish handoffs.
- Never run `git add`, `git commit`, or `git push` as part of handoff publish.

## Workflow

1. Summarize the active state, current diff intent, validation, risks, open questions, and next step. Do not embed a full state snapshot, raw transcript, diff, secret, or absolute local path.
2. If an active explicit state exists, run `worktrail state close --to handoff --next-step "<action>" "<summary>"`. This transaction archives the state and creates a private local record under `.worktrail/handoffs/local/`.
3. Otherwise run `worktrail handoff create --next-step "<action>" "<summary>"`. Use `--complete` instead only when no follow-up remains.
4. If the user explicitly requests team sharing, publish the returned local id with `worktrail handoff publish <local-id>`.
5. If publish reports a dirty worktree, stop by default. Use `--allow-dirty --confirm` only when the user explicitly accepts that the team record will mark code unavailable.
6. If multiple team heads exist, ask for or derive an explicit reconciliation set and publish with `--supersedes <team-id,...>`. Never edit or close a team record in place.
7. Report the local/team visibility, task id, record id, relative path, code availability, and that Git staging/commit/push was not performed.

## Safety and recovery

- Local content uses the local safety profile and may be redacted; blocked secret material still fails.
- Team publish uses the stricter team profile and rejects redactable secrets or PII, absolute paths, raw transcript-style content, and diffs.
- Run `worktrail handoff doctor` for read-only diagnostics. `worktrail handoff repair` is dry-run by default; only `--apply --confirm` quarantines malformed local handoffs or repairs other local records, and team records remain immutable.
- Use `worktrail doctor recovery` for the read-only malformed state/runtime plan, then `worktrail doctor recovery --apply --confirm` to quarantine repairable records. The obsolete recovery `--repair` flag is not accepted.
- For legacy root handoffs or retired handoff candidates, run `worktrail migrate handoff-v2` first. Apply only with `--apply --confirm`; the verified default backup stays outside `.worktrail` under the precisely ignored `/.worktrail-handoff-v2-backups/`, and a successful apply rebuilds the project index.

## Output

`[output: worktrail-handoff | completed <confidence> | summary:"..." validation:"..." | next:<action>]`
