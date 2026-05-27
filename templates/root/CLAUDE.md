Use Worktrail at the start and end of coding tasks only when the current project has opted in:

- Treat the project as Worktrail-enabled only when `.worktrail/` exists at the current workspace or repository root.
- If `.worktrail/` is absent, do not automatically run Worktrail context, preview, search, state, resume, handoff, import, review, maintain, distill, or note workflows.
- This gate does not block explicit user requests to initialize, install, inspect, or repair Worktrail itself.

- Run `/worktrail-context` or `worktrail context "<task>"` before starting substantial work.
- Use `/worktrail-doc-preview` or `worktrail preview` when you need to browse Worktrail knowledge.
- Use `/worktrail-search` or `worktrail search "<keyword>"` when you need to pinpoint Worktrail knowledge.
- Keep `/worktrail-state` current for long or risky sessions.
- Run `/worktrail-handoff` or `worktrail handoff "<summary>"` before switching tools, compacting, or ending a session.
- Use `/worktrail-resume` or `worktrail resume "<task>"` at the start of a new session when you need to continue prior work from the latest state and handoff.
- Use `/worktrail-import` only for explicit transcript files that should become pending candidates.
- Use `/worktrail-review` or `worktrail review` to inspect candidates before any promote, merge, discard, restore, or retire action.

Worktrail is the only project knowledge route. Use Worktrail context, review, and handoff flows for durable knowledge; do not read from or write to legacy KDD directories.

## Worktrail command picker

Use this picker before reaching for shell commands or adjacent Worktrail subcommands. Match the user intent to the first row that fits, run the listed command, and avoid the substitutes.

| User intent | Use this command | Do NOT use these substitutes |
| --- | --- | --- |
| Look up a Worktrail rule, lesson, decision, workflow, or note by keyword/term/phrase ("find", "search", "look up", "where is X documented") | `worktrail search "<keyword>"` (skill `/worktrail-search`) | `rg`, `grep`, `find`, `cat`, `worktrail context`, `worktrail preview` |
| Continue prior Worktrail work in a new session ("resume", "pick up where I left off", "continue previous session", "load my previous Worktrail context for this new chat") | `worktrail resume [<task>]` (skill `/worktrail-resume`) | `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, `worktrail state show` |
| Browse the rendered Worktrail knowledge site for a doc, candidate, handoff, workflow, profile, rule, or lesson | `worktrail preview <path|--candidate id> --scope <scope> --open` (skill `/worktrail-doc-preview`) | `worktrail search`, target project's dev server, ad hoc Markdown viewers |
| Start substantial project work or load project memory for a new task | `worktrail context "<task>"` (skill `/worktrail-context`) | `worktrail resume`, `worktrail search`, `worktrail preview` |
| Record current state, checkpoint, or update progress for the active session | `worktrail state start|update|checkpoint|inject` (skill `/worktrail-state`) | `worktrail resume`, `worktrail handoff` (until ending the session) |
| End the session, compact context, switch tools, hand off | `worktrail handoff "<summary>"` (skill `/worktrail-handoff`) | `worktrail state checkpoint` alone, copy-pasted text summaries |

If two rows seem to fit, pick the more specific intent (search/resume win over context/preview/state when keyword lookup or session recovery is the primary goal).

{{WORKTRAIL_SKILL_TRIGGER_ROUTING}}

Worktrail hooks may create state, checkpoints, pending handoff candidates, and event logs. Hooks must never promote, merge, discard, restore, retire, delete, or replace knowledge.
