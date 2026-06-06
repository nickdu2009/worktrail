# Project

**worktrail** — local-first knowledge base, work log, and handoff tool for AI coding sessions (Cursor, Codex, Claude Code).

## Scope

This `.worktrail/` tree holds **project-scoped** knowledge for the Go CLI in this repository.

## Stack

- Go CLI (`cmd/worktrail`)
- Markdown + JSON frontmatter as source of truth
- SQLite FTS5 + `gse` tokenization at `.worktrail/index/index.sqlite`

## Conventions

- Formal knowledge lives under `rules/`, `decisions/`, `prompts/`, `handoffs/`, etc.
- Hooks may create state, checkpoints, and pending candidates — never promote knowledge automatically.
- Durable handoffs are written to `handoffs/`; pending duplicates in `candidates/` should be discarded after review.

## Recent milestone

SQLite + GSE index backend shipped; legacy `index.db` / `manifest.json` removed. See handoff `20260606-000909`.
