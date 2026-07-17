# Worktrail

`worktrail` is a local-first knowledge base, work log, and handoff tool for AI coding sessions in Codex, Claude Code, Cursor, and ZCode Agent.

The command name is `worktrail`; the Go module path is `github.com/nickdu2009/worktrail`.

Hard boundaries:

- no TUI
- no Web UI or dashboard
- no MCP server
- no general daemon, watcher, scheduler, or public background service
- no cloud embedding provider or standalone vector database
- no custom external command provider
- no hidden background write surface

Formal knowledge is Markdown with JSON frontmatter. Local indexes are rebuildable acceleration data, not source of truth.

The v1.0.0 release scope includes an opt-in local semantic-recall capability,
subject to the gates in
[`docs/worktrail-local-semantic-recall-architecture.md`](docs/worktrail-local-semantic-recall-architecture.md).
Its only runtime exception is an explicitly installed, authenticated,
loopback-only llama.app process started for semantic operations. Its sqlite-vec
generations are local derived data. M1 is the only `verified` runtime variant.
M2–M5 are opt-in `experimental` variants: each uses its own pinned official
artifact and must pass installation-time local self-checks before activation,
without fallback to a different chip artifact. Experimental variants do not
claim compatibility or verification, performance, privacy, minimum-macOS, or
operational support guarantees; they do not require pre-release per-chip
self-check reports. The lexical SQLite + FTS5 path remains available whenever
semantic recall is not installed or cannot run.

Install local semantic recall only with the explicit
`worktrail init --semantic` flag. Plain `worktrail init` performs core
initialization without network access by default; `worktrail init --no-semantic`
explicitly disables semantic installation. A successful semantic installation
does not create an index: run `worktrail semantic rebuild --scope all`
explicitly when you want to build one.

Search and context use a SQLite + FTS5 index at `.worktrail/index/index.sqlite` (or the user-scope equivalent) with application-layer Chinese tokenization via `gse`. Markdown remains the only source of truth; delete a corrupted `index.sqlite` and run `worktrail index rebuild` when search or context reports index health issues.

## User Manual

- [`docs/manual/README.md`](docs/manual/README.md) - `Worktrail 使用手册`
- [`docs/manual/QUICK-START.md`](docs/manual/QUICK-START.md) - `快速开始`
- [`docs/manual/DESIGN-PHILOSOPHY.md`](docs/manual/DESIGN-PHILOSOPHY.md) - `设计理念`
- [`docs/manual/AUTOMATION.md`](docs/manual/AUTOMATION.md) - `自动化`
- [`docs/manual/FAQ.md`](docs/manual/FAQ.md) - `常见问题`
- [`docs/user-guide.md`](docs/user-guide.md) - 用户指南入口

Preview the manual locally with:

```bash
make docs-manual-serve
```

This preview uses `npx docsify-cli`, so Node.js/npm must be available.

Preview the overall Worktrail knowledge library with:

```bash
worktrail preview
worktrail preview --scope user
```

`worktrail preview` renders a local multi-page static site for the selected scope and opens the entry page in the browser. The entry path is stable under `.worktrail/.cache/preview/index.html`, and the command does not start a long-running HTTP preview service.

If you already installed Worktrail-managed skills or rules for Cursor, Codex, Claude, or ZCode, rerun `worktrail install <tool> --user` (and `--project` where applicable) after upgrading so agents pick up the new preview contract.

## Main workflow

```bash
worktrail init
worktrail context "current task"
worktrail preview
worktrail search "keyword"
worktrail state start "task title"
worktrail state update "progress, validation, and next step"
worktrail state close --to handoff "summary, validation, risks, and next step"
worktrail resume "continue the task"
worktrail doctor knowledge
```

`worktrail note add` is the low-friction path for capturing a confirmed finding
as a pending semantic candidate. It does not edit formal knowledge or promote
candidates; use `review`, `promote`, or `merge` for the explicit review step.

`worktrail adr create` is the specialized path for a reviewed Architecture Decision Record. It accepts a standard ADR from `--from-file` or explicit `--stdin`, validates the ADR ID/status/required sections, and creates a pending `decision` candidate. ADR document status (`Proposed`, `Accepted`, `Deprecated`, `Superseded`), candidate status (`pending`, `promoted`, `merged`, `archived`), and formal lifecycle (`current`, `historical`, `retired`) remain independent. Only Accepted ADRs may write `supersedes` graph metadata; retiring the old decision still requires the explicit maintenance/review flow.

`worktrail review` now behaves as a semantic draft review surface by default. It still keeps the legacy `candidate` terminology for compatibility, but the default inbox is only pending semantic drafts. Hidden evidence items are reported separately, and operational drafts stay out of the default review table unless you opt into `--all`. Semantic drafts include `source_candidate_ids` details when present, source health warnings, target/duplicate warnings, and a focused `worktrail candidates diff <id>` next step. Use `worktrail review --evidence` for evidence items such as `transcript_notes` and `migration_source`, or `worktrail review --all` when operational drafts also need inspection.

`worktrail review plan --format json` emits the read-only agent contract `worktrail.review.plan.v1`. It groups pending semantic candidates into deterministic recommendations: `promote`, `merge`, `discard`, or `needs_human_review`. The command never changes candidate state or formal knowledge; state-changing commands still require explicit user confirmation.

`worktrail search` tokenizes queries with `gse`, retrieves candidates through SQLite FTS5, and reranks with Worktrail semantics (`source_of_truth`, `active`, lifecycle, recency). Filter with `--type`, `--topic`, `--tag`, and `--scope`; pass `--include-content` only when the body is needed.

`worktrail context <task>` is the read-only navigation entry for a task. It now prioritizes fresh explicit state first, then current durable handoffs, hides pending evidence by default, reports how many evidence candidates are hidden, and skips stale indexed entries instead of surfacing deleted or outdated cached content. Use `worktrail context --evidence <task>` when evidence items need to be included in the pending section, and use `worktrail index diff` or `worktrail index rebuild` when context reports a stale index. Context and search refresh the SQLite index incrementally before reads; `worktrail index rebuild` remains the full-recovery oracle.

`worktrail state close --to handoff "<summary>"` is the primary durable handoff path when an explicit state exists: it binds the handoff to the latest explicit state and closes that state in one flow. Bare `worktrail handoff "<summary>"` remains available as an exception path when no active explicit state exists or when a handoff-only record is intentionally needed. Stop and session-end hooks now keep their output in runtime records (`state/` plus checkpoints and audit logs) instead of creating pending handoff candidates by default, so routine hook residue no longer accumulates in the default review inbox.

`worktrail resume [<task>]` creates a fresh active state from the best available recovery source, preferring fresh explicit state over handoffs and using runtime artifacts only as degraded fallback, so a new session can continue without manually stitching together `context`, `state show`, and handoff files.

Project knowledge can use `.worktrail/requirements/` for PRDs, user goals, persona or primary-user scope, workflow problems, MVP boundaries, out-of-scope notes, requirement-level acceptance criteria, business capability requirements, failure exits, and requirement-stage open questions. This complements, rather than replaces, `architecture/`, `decisions/`, `validation/`, and `workflows/`.

Formal Markdown frontmatter can optionally declare governance metadata:

```json
{
  "stage": "requirements",
  "topic": "delivery-expert-workbench",
  "source_of_truth": true,
  "supersedes": ["architecture/old-doc.md"]
}
```

Allowed stages are `requirements`, `design`, `decision`, `implementation`, `validation`, `historical`, and `retired`. Existing documents without this metadata continue to work. Use `worktrail context --stage requirements <task>`, `--stage design`, or `--stage implementation` to bias context section order and item priority for the current work stage. Historical and retired knowledge is now treated as lifecycle metadata and is excluded from default context; include it explicitly with `worktrail context --include-lifecycle historical <task>` or `--include-lifecycle historical,retired`. The legacy aliases `worktrail context --stage historical <task>` and `--stage retired <task>` continue to work during the transition. In practice, `topic` is most useful for threadable knowledge such as requirements, architecture, workflows, integrations, validation, glossary, and lessons. Bootstrap docs (`project.md`, `index.md`), durable handoffs, global rules/prompts, decisions, and logs can stay current without a topic.

`worktrail doctor knowledge` checks for knowledge governance drift such as requirements content in `architecture/` or `decisions/`, decisions without a clear Decision section, multiple `source_of_truth` documents for the same topic, superseded documents still referenced from `index.md` or `project.md`, stale index entries, and invalid stage metadata. It also detects direct formal edits that bypass the candidate/review flow, but suppresses low-signal noise from Worktrail-owned bootstrap docs and durable handoff records. Findings now include concrete hints and, when appropriate, the exact `worktrail index rebuild --scope ...` command to recover.

`worktrail doctor delete <path>` is a read-only preflight for formal knowledge deletion. It reports blocking structured references such as pending candidate targets, `supersedes` / `superseded_by` relationships, and Markdown links from starter docs, plus warning-only plain-text mentions from governance files or candidate bodies.

## Agent integration support

Worktrail supports multiple local agent surfaces in the same repository. Install scope is explicit:

- User scope installs the agent capabilities that should follow the human across repositories. The bundled `templates/root/**` and `templates/skills/**` files are user-level instructions, rules, and skills.
- Project scope installs runtime integration config for the current repository only, such as hooks, tool settings files, and the managed `.gitignore` entries needed for those runtime files. `--project` does not install project-level rules or project-level skills from `templates/root/**` or `templates/skills/**`.
- User-level instructions and skills are gated by project opt-in: agents should only run automatic Worktrail workflows when `.worktrail/` exists at the current workspace or repository root. In directories without that marker, Worktrail remains available only for explicit init, install, inspect, or repair requests.

Install the Worktrail CLI before installing agent integrations. Installed skills
and hooks invoke the `worktrail` command, so `worktrail` must be
available in `PATH`; from this repository, use `go install ./cmd/worktrail` or a
packaged binary.

By default, `worktrail install <tool>` installs user scope only. For a complete setup, run both scopes in one command:

```bash
worktrail install <tool> --user --project
```

You can also install the scopes separately:

```bash
worktrail install <tool> --user
worktrail install <tool> --project
```

When Worktrail updates bundled rules, skills, or hook/settings templates, rerun `worktrail install <tool> --user --project` for the affected tool so the managed integration files pick up the new contract. This includes `worktrail-draft`, which persists non-ADR semantic artifacts directly as pending candidates when Worktrail is the only requested destination, and `worktrail-adr`, which does not require an external design-review skill.

Use `worktrail install all --user --project` to set up Codex, Claude Code, Cursor, and ZCode Agent together, or explicit targets such as `worktrail install codex --user --project` and `worktrail install zcode --user` when only a subset should be installed. ZCode Agent currently uses only the user-level installation surface.

Current capability matrix:

- Codex: user instructions and skills, project hooks/runtime config, doctor, uninstall, current-project `import codex` discovery, and explicit transcript `sync`/`extract`.
- Claude Code: user instructions and skills, project hooks/settings runtime config, doctor, uninstall, and explicit transcript `sync claude <file>` / `extract --source claude`. There is no automatic `import claude` discovery yet.
- Cursor: user-level rule and skills, project hooks runtime config, doctor, uninstall, safe observed transcript metadata, and `import cursor` from explicit `--file` paths or Worktrail-observed transcript metadata. Cursor user install manages the Cursor-visible Worktrail rule and skills; Cursor project install does not create project rules or project skills. Cursor import does not scan undocumented private Cursor directories.
- ZCode Agent: user instructions and skills via `~/.zcode/AGENTS.md` and `~/.zcode/skills`, doctor, and uninstall. ZCode Agent does not currently have Worktrail-managed project hooks/runtime config or transcript import support.

For ZCode Agent, Worktrail automation is semantic rather than hook-driven: the installed `AGENTS.md` guidance routes the agent toward Worktrail skills and direct `worktrail` CLI commands when the workspace has opted in with `.worktrail/`.

Cursor may also see user-level Worktrail skills through compatible roots such as `$HOME/.agents/skills`, `$HOME/.codex/skills`, and `$HOME/.claude/skills`. Cursor user install always writes managed skills to `$HOME/.cursor/skills` so they appear in Cursor directly; `doctor cursor --user` reports duplicate visible skills as warnings, not failures.

## Low-intervention maintenance

`worktrail context <task>` includes a `maintenance` object in JSON output and a short text section when pending maintenance exists. The hints are scope-aware, so user-scope evidence is suggested with commands such as:

```bash
worktrail distill --pending --summary --scope user
worktrail evidence plan --format json --scope user
```

For routine upkeep, use the installed `worktrail-maintain` skill. It chains `context "maintenance"`, `distill --pending --summary`, `review plan --format json`, `evidence plan --format json`, and `maintain knowledge --format json` when a proposal workflow is needed, then waits for explicit confirmation before any state-changing command. Maintenance counts track the default pending inbox (semantic review + evidence lanes); operational candidates remain inspectable through `worktrail review --all` and preview, but do not count as default review work.

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

`worktrail evidence plan` emits the v1 contract `worktrail.evidence.plan.v1`. It reports `transcript_notes`, `migration_source`, and legacy KDD split-source evidence, counts pending and applied semantic references, and recommends `keep`, `archive`, `discard`, or `needs_human_review`. Archive and discard require `--confirm`, keep candidate files for audit, and only run when the current evidence plan recommends the same action.

## KDD migration

Existing `docs/knowledge-driven-development/` project knowledge can be migrated into Worktrail as pending candidates:

```bash
worktrail migrate kdd
worktrail migrate kdd --write-candidates
worktrail doctor migration
worktrail review
```

`worktrail migrate kdd` is a dry-run by default. `--write-candidates` creates pending candidates only; it does not promote or merge formal knowledge. Candidate `target_path` values are relative to the Worktrail scope root, for example `architecture/system.md` for project knowledge.

`docs/knowledge-driven-development/local/**` migrates to user-scope pending candidates only. Category README files such as `project/architecture/README.md` are skipped by default because they are usually directory guidance rather than durable project knowledge. `active-knowledge-log.md` files migrate as `migration_source` evidence and must be distilled before formal knowledge is promoted or merged. Migration is complete only after `worktrail doctor migration` passes and the legacy KDD root is removed with explicit cleanup.

See [docs/kdd-import-dogfood-acceptance.md](docs/kdd-import-dogfood-acceptance.md) for the delivery-experts dogfood acceptance record.

## Restore vs retire

`worktrail review` warns when an applied candidate says a formal target exists but the target file is missing.

- Use `worktrail restore <candidate-id>` when the formal target was deleted by mistake and should be recreated from the applied candidate body.
- Use `worktrail retire <candidate-id> --reason <text>` when the formal target was intentionally deleted and Worktrail should stop warning about it.
