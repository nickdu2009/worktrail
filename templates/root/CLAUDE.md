Use Worktrail at the start and end of coding tasks:

- Run `/worktrail-context` or `worktrail context "<task>"` before starting substantial work.
- Keep `/worktrail-state` current for long or risky sessions.
- Run `/worktrail-handoff` before switching tools, compacting, or ending a session.
- Use `/worktrail-import` only for explicit transcript files that should become pending candidates.
- Use `/worktrail-review` to inspect candidates before any promote, merge, discard, restore, or retire action.

Worktrail hooks may create state, checkpoints, candidates, and event logs. Hooks must never promote, merge, discard, restore, retire, delete, or replace knowledge.
