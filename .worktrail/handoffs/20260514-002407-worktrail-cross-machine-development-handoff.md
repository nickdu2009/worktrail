# Handoff: Worktrail cross-machine development handoff

Use this prompt to continue Worktrail development on another machine with an AI agent.

```text
You will continue developing the Worktrail project.

Repository:
- Repo: git@github.com:nickdu2009/worktrail.git
- Go module: github.com/nickdu2009/worktrail
- CLI command: worktrail
- Main branch: main

Bootstrap:
1. git clone git@github.com:nickdu2009/worktrail.git
2. cd worktrail
3. go test ./...
4. go build ./cmd/worktrail
5. go install ./cmd/worktrail

Important recent commits:
- e751055 Add Worktrail handoff knowledge
- c644fb6 Improve transcript import and distill UX
- c285a42 Add agent-native transcript distillation
- f4626a0 Import current project Codex transcripts

Completed capabilities:
- `worktrail import codex` scans Codex transcripts related to the current project. It dry-runs by default; `--all` syncs and extracts.
- Import/extract creates pending `transcript_notes` evidence candidates only. It does not write formal knowledge.
- `worktrail distill` prepares transcript evidence for semantic distillation by the current AI agent. It does not call external LLM APIs.
- `worktrail distill --pending --all --write-pack <file>` writes a large evidence pack to disk without flooding terminal or chat context.
- `worktrail distill --pending --summary`, `--json`, `--limit`, and `--offset` support compact and paged bulk workflows.
- `worktrail candidates create --help` shows parameters and examples.
- `worktrail candidates list --semantic` shows `rule`, `decision`, `lesson`, `prompt`, and `workflow` candidates.
- `worktrail candidates list --evidence` shows `transcript_notes` evidence candidates.
- `worktrail review` hides transcript evidence by default and reports how many evidence candidates were hidden; `worktrail review --evidence` inspects them.
- `transcript_notes` candidates cannot be promoted or merged. They must first be distilled into semantic candidates.
- Codex skill `/worktrail-import` is updated to import, distill all evidence, create semantic pending candidates, and then review.

Safety boundaries:
- import, extract, distill, and candidates create can only create pending candidates.
- Hooks must never promote, merge, discard, delete, or replace knowledge.
- MCP must not expose promote, merge, discard, delete, or replace by default.
- Formal knowledge writes require explicit user confirmation and CLI promote/merge.

Validation on the original machine:
- `go test ./...` passed with 43 tests.
- `go build ./cmd/worktrail` passed.
- Remote origin is `git@github.com:nickdu2009/worktrail.git`.

Known local state from the original machine:
- `.worktrail/` contains local runtime/project knowledge files. Some initialization files were intentionally left untracked.
- The handoff knowledge file is tracked at `.worktrail/handoffs/20260514-002407-worktrail-cross-machine-development-handoff.md`.

Suggested next steps:
1. Confirm the clone has the latest `origin/main`.
2. Run `go test ./...` and `go build ./cmd/worktrail`.
3. Run `go install ./cmd/worktrail`.
4. Run `worktrail install codex` for user-level Codex skills.
5. In any target project, run `worktrail init-project` to install project `.worktrail/` and `.codex/hooks.json`.
6. Smoke test a real project:
   - `worktrail import codex`
   - `worktrail import codex --all`
   - `worktrail distill --pending --summary`
   - `worktrail distill --pending --all --write-pack worktrail-distill.md`
   - `worktrail candidates list --semantic`
   - `worktrail review`
7. Possible future improvements: evidence de-duplication, transcript clustering, and better semantic candidate drafting while keeping all outputs pending.
```
