# Worktrail

`worktrail` is a local-first AI coding session knowledge and state layer for Codex, Claude Code, and Cursor.

The command name is `worktrail`; the Go module path is `github.com/nickdu2009/worktrail`.

Hard boundaries:

- no TUI
- no Web UI or dashboard
- no HTTP MCP server
- no daemon, watcher, or background service
- no embedding or vector database
- no custom external command provider
- no default MCP promote, merge, discard, restore, retire, delete, or replace tools

Formal knowledge is Markdown with JSON frontmatter. Local indexes are rebuildable acceleration data, not source of truth.

## Minimal workflow

```bash
worktrail init
worktrail context "current task"
worktrail candidates create --type rule --target rules/example.md --title "Example Rule" "Rule body."
worktrail review
worktrail review plan --format json
worktrail candidates diff <candidate-id>
worktrail promote <candidate-id>
worktrail index rebuild
worktrail context "current task"
```

`worktrail review` shows pending semantic candidates by default and reports hidden transcript evidence plus non-semantic operational candidates such as handoffs. Semantic candidates include `source_candidate_ids` details when present, source health warnings, target/duplicate warnings, and a focused `worktrail candidates diff <id>` next step. Use `worktrail review --evidence` for raw transcript evidence, or `worktrail review --all` when operational candidates also need inspection.

`worktrail review plan --format json` emits the read-only agent contract `worktrail.review.plan.v1`. It groups pending semantic candidates into deterministic recommendations: `promote`, `merge`, `discard`, or `needs_human_review`. The command never changes candidate state or formal knowledge; state-changing commands still require explicit user confirmation.

`worktrail context <task>` hides pending transcript evidence by default and reports how many evidence candidates are hidden. Use `worktrail context --evidence <task>` when the raw transcript notes themselves need to be included in the Pending Candidates section.

## Agent integration support

Worktrail supports multiple local agent surfaces in the same repository. `worktrail install all` installs the supported local integrations: Codex, Claude Code, and Cursor. Use explicit targets such as `worktrail install codex` or `worktrail install claude` when only a subset should be installed.

Current capability matrix:

- Codex: install, doctor, uninstall, skills, hooks, MCP config, current-project `import codex` discovery, and explicit transcript `sync`/`extract`.
- Claude Code: install, doctor, uninstall, skills, hooks/settings, and explicit transcript `sync claude <file>` / `extract --source claude`. There is no automatic `import claude` discovery yet.
- Cursor: install, doctor, uninstall, MCP, rules, hooks, Cursor-visible skills, safe observed transcript metadata, and `import cursor` from explicit `--file` paths or Worktrail-observed transcript metadata. Cursor import does not scan undocumented private Cursor directories.

Cursor can see Worktrail skills through compatible roots such as `.agents/skills`, `.codex/skills`, and `.claude/skills`. `install cursor` reuses visible managed skills by default and does not duplicate them into `.cursor/skills` unless no visible copy exists; `doctor cursor` reports duplicate visible skills as warnings, not failures.

## Low-intervention maintenance

`worktrail context <task>` includes a `maintenance` object in JSON output and a short text section when pending maintenance exists. The hints are scope-aware, so user-scope evidence is suggested with commands such as:

```bash
worktrail distill --pending --summary --scope user
worktrail evidence plan --format json --scope user
```

For routine upkeep, use the installed `/worktrail-maintain` skill. It chains `context "maintenance"`, `distill --pending --summary`, `review plan --format json`, and `evidence plan --format json`, then waits for explicit confirmation before any state-changing command.

Saved review plans can be applied only with confirmation:

```bash
worktrail review apply-plan review-plan.json --confirm
```

`apply-plan` validates the `worktrail.review.plan.v1` schema and candidate snapshots before running `promote`, `merge`, or `discard`. It skips `needs_human_review`, reports `applied`, `skipped`, `stale`, and `failed`, and does not clean up evidence.

## Distillation proposals

Transcript evidence and KDD split sources can be distilled into semantic pending candidates with an agent-authored proposal:

```bash
worktrail distill --pending --all --write-pack worktrail-distill.md
worktrail distill validate proposal.json
worktrail distill apply proposal.json
worktrail review
```

Proposal JSON uses schema `worktrail.distill.proposal.v1`:

```json
{
  "schema": "worktrail.distill.proposal.v1",
  "source_candidate_ids": ["codex-01-user"],
  "candidates": [
    {
      "candidate_type": "rule",
      "title": "Review Before Promote",
      "summary": "Imported evidence should be reviewed before formal knowledge changes.",
      "target_path": "rules/review-before-promote.md",
      "operation": "replace",
      "evidence_label": "Pending Verification",
      "confidence": 0.7,
      "tags": ["distilled"],
      "body": "# Review Before Promote\n\nKeep imported evidence pending until it is reviewed."
    }
  ]
}
```

`distill apply` creates pending semantic candidates only. It never promotes, merges, discards, restores, or retires knowledge. JSON output remains the stable machine contract; default text output summarizes created, skipped, blocked, and error items without printing candidate bodies or local proposal paths.

Copyable proposal examples live under `docs/examples/distill/`. Test fixtures live under `internal/testdata/distill/` and use only synthetic candidates.

## Evidence lifecycle

Evidence candidates remain available for source traceability after distillation:

```bash
worktrail evidence plan --format json
worktrail evidence plan --status archived
worktrail evidence plan --status all --format json
worktrail evidence archive <candidate-id> --confirm --reason "covered by applied knowledge"
worktrail evidence discard <candidate-id> --confirm --reason "empty duplicate evidence"
```

`worktrail evidence plan` emits the v1 contract `worktrail.evidence.plan.v1`. It reports `transcript_notes` and KDD split-source `lesson` evidence, counts pending and applied semantic references, and recommends `keep`, `archive`, `discard`, or `needs_human_review`. Archive and discard require `--confirm`, keep candidate files for audit, and only run when the current evidence plan recommends the same action.

## KDD import

Existing `docs/knowledge-driven-development/` project knowledge can be migrated into Worktrail as pending candidates:

```bash
worktrail import kdd
worktrail import kdd --all
worktrail review
```

`worktrail import kdd` is a dry-run by default. `--all` creates pending semantic candidates only; it does not promote or merge formal knowledge. Candidate `target_path` values are relative to `.worktrail/`, for example `architecture/system.md`.

`docs/knowledge-driven-development/local/**` is skipped by default because it may contain current-developer paths, temporary IDs, or private environment context. Category README files such as `project/architecture/README.md` are also skipped by default because they are usually directory guidance rather than durable project knowledge. `project/active-knowledge-log.md` is imported only as a pending split source and should not be promoted directly.

See [docs/kdd-import-dogfood-acceptance.md](docs/kdd-import-dogfood-acceptance.md) for the delivery-experts dogfood acceptance record.

## Restore vs retire

`worktrail review` warns when an applied candidate says a formal target exists but the target file is missing.

- Use `worktrail restore <candidate-id>` when the formal target was deleted by mistake and should be recreated from the applied candidate body.
- Use `worktrail retire <candidate-id> --reason <text>` when the formal target was intentionally deleted and Worktrail should stop warning about it.
