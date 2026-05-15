Use Worktrail at the start and end of coding tasks:

- Run `/worktrail-context` or `worktrail context "<task>"` before starting substantial work.
- Keep `/worktrail-state` current for long or risky sessions.
- Run `/worktrail-handoff` or `worktrail handoff "<summary>"` before switching tools, compacting, or ending a session.
- Use `/worktrail-import` only for explicit transcript files that should become pending candidates.
- Use `/worktrail-review` or `worktrail review` to inspect candidates before any promote, merge, discard, restore, or retire action.

Worktrail is the only project knowledge route. Use Worktrail context, review, and handoff flows for durable knowledge; do not read from or write to legacy KDD directories.

{{WORKTRAIL_SKILL_TRIGGER_ROUTING}}

Worktrail hooks may create state, checkpoints, candidates, and event logs. Hooks must never promote, merge, discard, restore, retire, delete, or replace knowledge.
