---
name: worktrail-draft
description: Persist non-ADR semantic artifacts as pending Worktrail candidates without unnecessary standalone copies. Use when the user explicitly asks to save requirements, architecture, plans, rules, workflows, or other durable knowledge in Worktrail.
---

# Worktrail Draft

Use this skill to persist a non-ADR semantic artifact in a Worktrail-enabled project. Create a pending candidate first; formal knowledge still requires the separate `worktrail-review` confirmation flow.

## Guardrails

- If `.worktrail/` is absent at the workspace or repository root, stop; do not initialize Worktrail implicitly.
- Require explicit persistence intent. Draft-only, review-only, explain-only, and explicit no-write requests must not create candidates.
- Never directly edit formal `.worktrail` knowledge paths.
- Never promote, merge, discard, restore, retire, or commit automatically.
- Route ADRs and `decision` artifacts to `worktrail-adr`.
- Preserve existing source files. Never delete a user file merely because its content was persisted.

## Standalone-file policy

- If the user wants the artifact only as Worktrail knowledge, do not create `docs/`, `.plans/`, or another standalone copy. Send the complete artifact to `worktrail draft create` through stdin.
- If the user explicitly asks for both a normal file and Worktrail persistence, create the requested file and use `--from-file`.
- If the user supplies an existing file, use it as the source and leave it in place.
- If a temporary file is unavoidable, create it outside formal `.worktrail` knowledge and remove only that agent-created temporary file after candidate creation.

## Supported types and targets

Use a matching semantic type and target:

- `requirement` → `requirements/<name>.md`
- `architecture` → `architecture/<name>.md`
- `workflow` → `workflows/<name>.md`
- `rule` → `rules/<name>.md`
- `validation` → `validation/<name>.md`
- `integration` → `integrations/<name>.md`
- `glossary` → `glossary/<name>.md`
- `lesson` → `lessons/<name>.md`
- `prompt` → `prompts/<name>.md`
- `project` → `project.md`

Do not use this skill for `decision`, `adr`, `index`, evidence, runtime state, or handoffs. `candidate_type=handoff` is retired; explicit cross-chat or switch-agent recovery must use `worktrail-handoff`, which creates a local runtime record and optionally publishes an immutable team record.

## Formal-body metadata

The candidate body is the proposed formal document. Include Worktrail frontmatter so promotion preserves the stable id, topic, scope, type, and lifecycle:

```markdown
---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "<stable-id>",
  "scope": "project",
  "type": "<semantic-type>",
  "title": "<title>",
  "status": "active",
  "lifecycle": "current",
  "topic": "<stable-topic>"
}
---

# <title>

<artifact body>
```

Use `scope: user` in both the frontmatter and command only when the user explicitly requests user knowledge. Keep `topic` stable across related requirements, architecture, plans, and validations.

## Workflow

1. Confirm `.worktrail/`, explicit persistence intent, scope, semantic type, target path, stable id, and topic.
2. Confirm the artifact is complete enough to review. Do not silently invent missing business or design decisions.
3. Choose stdin, an explicitly requested file, or an existing file according to the standalone-file policy.
4. Create one pending semantic candidate with `operation=replace` unless the user explicitly requests a merge into an existing target.
5. Run `worktrail review plan --format json` with the same scope.
6. Report the generated candidate id, type, target, topic, source mode, warnings, and `next: worktrail-review`.

## Safe stdin

For content held in context, use a single-quoted heredoc delimiter so shell expansion is disabled:

```bash
worktrail draft create \
  --scope project \
  --topic "<stable-topic>" \
  --type "<semantic-type>" \
  --target "<matching-target-path>" \
  --title "<title>" \
  --summary "<summary>" \
  --operation replace \
  --format json <<'WORKTRAIL_DRAFT'
---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "<stable-id>",
  "scope": "project",
  "type": "<semantic-type>",
  "title": "<title>",
  "status": "active",
  "lifecycle": "current",
  "topic": "<stable-topic>"
}
---

# <title>

<artifact body>
WORKTRAIL_DRAFT
```

Do not use an unquoted heredoc delimiter.

## Existing-file input

Use `--from-file` only for a user-requested or pre-existing standalone artifact:

```bash
worktrail draft create \
  --scope project \
  --topic "<stable-topic>" \
  --type "<semantic-type>" \
  --target "<matching-target-path>" \
  --title "<title>" \
  --summary "<summary>" \
  --from-file "<existing-file>" \
  --format json
```

Before using `--from-file`, verify that the file includes compatible Worktrail frontmatter. Do not rewrite an existing file solely to add metadata without the user's authorization.

## Failure handling

- Missing persistence intent or ambiguous scope/type/target: stop and ask.
- Type and target mismatch: correct the mapping before creation.
- Missing or conflicting artifact content: return to the producing or review skill.
- CLI JSON with `ok: false`: report the error and do not continue.
- Candidate creation succeeds but review plan fails: preserve the candidate id and report the read-only follow-up failure.
- Review plan requires human review: report it; do not promote.

## Output

`[output: worktrail-draft | completed <confidence> | candidate_id:"..." type:"..." target:"..." topic:"..." source:"stdin|existing_file|requested_file" standalone_copy:"none|preserved|requested" | next:worktrail-review]`
