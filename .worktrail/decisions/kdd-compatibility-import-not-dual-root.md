# KDD Compatibility Uses Pending Import Instead Of A Second Knowledge Root

## Decision

Worktrail should inherit KDD capabilities through a compatibility import and distillation workflow, not by keeping `docs/knowledge-driven-development/` as a second long-term knowledge root.

## Rationale

The real transcript shows that KDD and Worktrail share the same core lifecycle: reusable project knowledge is captured as candidates, reviewed, and only then promoted into formal knowledge. Worktrail already owns the stronger primitives for this lifecycle: candidate metadata, redaction, review, promote/merge/discard, context loading, hooks, and handoff.

Keeping both `.worktrail/` and `docs/knowledge-driven-development/` as active sources of truth would make context loading, promotion, and cleanup ambiguous. KDD migration should therefore create pending semantic candidates and preserve human review before any formal knowledge change.

## Operating Boundary

- KDD source documents are migration inputs, not a durable runtime context source.
- Imported KDD active logs are split sources and should not be promoted directly.
- Formal project knowledge remains under `.worktrail/` after explicit review and promotion.
- Local-only KDD material must not be promoted into shared project knowledge without review.

## Evidence

Distilled from a real Codex transcript where the user repeatedly reviewed and approved the KDD-to-Worktrail compatibility-import direction before implementation.
