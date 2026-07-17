---worktrail
{
  "id": "ADR-20260715-hybrid-recall-context-contract",
  "lifecycle": "current",
  "schema": "worktrail.knowledge.v1",
  "scope": "project",
  "stage": "decision",
  "status": "accepted",
  "title": "Preserve Worktrail Contracts with Explicit Hybrid Recall",
  "type": "decision"
}
---

# ADR-20260715-hybrid-recall-context-contract: Preserve Worktrail Contracts with Explicit Hybrid Recall

- Status: Accepted
- Date: 2026-07-15

## Context

Worktrail needs paraphrase and cross-language recall without weakening exact command, path, identifier, metadata, lifecycle, supersession, or source-of-truth behavior. Existing search text, JSON v1, and Context Pack deterministic sections are public compatibility boundaries. Semantic failure must not masquerade as successful hybrid retrieval.

## Decision

Use an explicit hybrid recall facade with independent chunk FTS and dense vector lanes. It generates one query vector for the exact active recall profile, retrieves bounded candidates, combines lexical and semantic ranks with reciprocal rank fusion, then applies versioned Worktrail governance, scope, lifecycle, graph, diversity, and bounded neighbor-expansion rules. Similarity never overrides authority or mutates knowledge.

Default `worktrail search` remains the existing lexical path. Semantic mode accepts only `--semantic`, `--semantic=auto`, or `--semantic=required`; the ambiguous space-separated value form is rejected. Existing text and `--format json` output remain v1-compatible. Opt-in JSON v2 reports entry and chunk citations, profile and policy versions, lanes, ranks, degraded reasons, and repair next steps. Auto mode visibly degrades to lexical behavior; required mode returns a stable typed error.

Semantic Context Pack selection may affect only knowledge sections. Active state, up to two current handoffs, recovery, maintenance, and evidence controls retain deterministic assembly and cannot compete for semantic budget. With an explicit topic, matching requirements are pinned in existing order and remaining requirement slots may use cross-topic semantic fill; other knowledge sections remain topic-filtered. Without a topic, all requirements participate in semantic selection. Semantic indexing still keeps only the latest valid handoff per scope.

Missing, stale, corrupt, incompatible, cross-scope-mismatched, unsupported, or unavailable semantic capability must be checked before unnecessary runtime startup. Every degraded result reports stable reason codes and an actionable install or rebuild command.

The M1 model/runtime Gate confirms the candidate embedding profile retains the committed parity corpus retrieval ordering. This does not approve production hybrid retrieval: labeled Worktrail evaluation, lexical regression fixtures, JSON v2 schema, governance ordering, Context Pack deterministic fixtures, and failure/degradation contracts remain required before semantic enablement.

## Consequences

### Positive

- Exact lexical behavior and governance remain authoritative.
- Semantic results are source-cited, inspectable, and machine-readable.
- Context Pack continuity sections cannot disappear because of low similarity.
- Users can distinguish normal lexical mode, visible degradation, and strict semantic failure.

### Negative

- The recall facade must maintain multiple lanes, diagnostics, and versioned policies.
- JSON v2 and explain output add a new compatibility surface.
- Labeled Worktrail-specific evaluation is required before tuning RRF and diversity values.
- Context Pack requirement/topic behavior needs dedicated regression fixtures.

## Links

- Related: docs/worktrail-local-semantic-recall-architecture.md
- Related: docs/worktrail-release-acceptance.md
- Evidence: docs/worktrail-semantic-parity-spike-2026-07-15.md
