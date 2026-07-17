# Changelog

## v1.0.0 — Unreleased

### Added

- Opt-in local semantic recall. Install it only with `worktrail init --semantic`;
  plain `worktrail init` remains network-free by default, and
  `worktrail init --no-semantic` explicitly disables semantic installation.
- Explicit semantic index construction with
  `worktrail semantic rebuild --scope <user|project|all>` after installation.
- Pinned local llama.app/BGE-M3 runtime artifacts with integrity and
  installation-time self-checks.

### Runtime support

- Apple M1 is the only **verified** variant, based on the complete dated M1
  production gate.
- Apple M2, M3, M4, and M5 are **experimental** opt-in variants. They use
  chip-specific pinned artifacts and local self-checks, with no cross-chip
  fallback.

### Known limitations

- Experimental M2–M5 variants do not claim verified compatibility, performance,
  privacy, minimum macOS, or operational support.
- The current M1 evidence was collected from a dirty development tree and is
  not the final clean-checkout, commit-identified release record.
- Creating the v1.0.0 candidate commit and tag, rerunning release validation
  from a clean checkout, and publishing the release remain separate,
  user-authorized operations.
