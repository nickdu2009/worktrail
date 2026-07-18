# Changelog

## v1.0.0 — Unreleased

### Added

- Opt-in local semantic recall via `worktrail init --semantic` and
  `worktrail semantic rebuild --scope <user|project|all>`.
- Pinned local llama.app / BGE-M3 artifacts with installation-time self-checks.

### Runtime support

- Apple M1: **verified** (dated M1 production gate).
- Apple M2–M5: **experimental** opt-in (chip-specific pinned artifacts; no
  cross-chip fallback).

### Known limitations

- Experimental M2–M5 do not claim verified compatibility, performance,
  privacy, minimum macOS, or operational support.
- Current M1 evidence is dirty-tree engineering evidence, not a clean-checkout
  release record. Tagging and publishing v1.0.0 remain separate authorized
  steps. See `docs/worktrail-release-acceptance.md`.
