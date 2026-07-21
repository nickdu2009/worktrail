---worktrail
{
  "id": "e2e-architecture-table-hardening-matrix",
  "scope": "project",
  "type": "architecture",
  "title": "Table Hardening Budget Matrix",
  "topic": "semantic-recall",
  "source_of_truth": true,
  "status": "active",
  "updated_at": "2026-07-21T00:00:00Z",
  "tags": ["semantic", "table", "budget-matrix", "e2e-prod-gate"]
}
---

# Table Hardening Budget Matrix

This architecture note captures the freeze-time chunk budget candidates for
table-aware semantic recall. Adjacent rows stay in one structural group so
neighbor expansion can recover nearby evidence without crossing into prose.

## Candidate Matrix

| Candidate | Target | HardMax | MinPayload | Notes |
| --- | ---: | ---: | ---: | --- |
| A | 384 | 640 | 80 | tighter denser row groups for short architecture tables and noisy headers |
| B | 512 | 768 | 80 | initial production candidate used before budget-matrix freeze comparison |
| C | 512 | 768 | 128 | higher floor reduces tiny tails while keeping the same hard maximum |
| D | 640 | 768 | 80 | larger target with the same HardMax for fewer total structural chunks |
| E | 384 | 640 | 80 | duplicate-shaped control row for neighbor coverage and occupancy checks |
| F | 512 | 768 | 80 | paraphrase neighbor for exact row-key B with shared header context terms |
| G | 512 | 768 | 128 | aggregate wording row describing whole-table comparison without wrong-row leakage |
| H | 640 | 768 | 80 | hard-cap pressure row that stays inside the matrix structural group boundary |
| I | 384 | 640 | 80 | cross-group negative control must not be satisfied by adjacent prose chunks |
| J | 512 | 768 | 80 | scope-all tie-break row keeping project architecture ahead of user lesson noise |

## Freeze Order

Selection compares only candidates that clear every blocking quality gate, then
prefers higher table Evidence Recall@10, higher table governed nDCG@10, fewer
total chunks, smaller HardMax, and finally numeric `(Target,HardMax,MinPayload)`
order. Bundle identity stays pinned by the trusted manifest and must not change
only because a budget candidate wins.
