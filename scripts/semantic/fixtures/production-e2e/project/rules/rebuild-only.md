---worktrail
{
  "id": "e2e-rule-rebuild-only",
  "scope": "project",
  "type": "rule",
  "title": "Rebuild Only Semantic Generation",
  "topic": "semantic-generation",
  "source_of_truth": true,
  "updated_at": "2026-07-15T01:00:00Z",
  "tags": ["semantic", "rebuild-only", "e2e-prod-gate"]
}
---

语义索引 generations are rebuild-only and immutable after seal.
Never mutate an active generation in place; activate a new sealed generation instead.
This rule covers rebuild-only immutable generation recovery for the production E2E gate.
