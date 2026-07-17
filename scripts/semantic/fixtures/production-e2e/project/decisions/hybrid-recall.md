---worktrail
{
  "id": "e2e-decision-hybrid-recall",
  "scope": "project",
  "type": "decision",
  "title": "Hybrid Recall Context Contract",
  "stage": "decision",
  "topic": "semantic-recall",
  "source_of_truth": true,
  "status": "accepted",
  "updated_at": "2026-07-15T00:00:00Z",
  "tags": ["semantic", "hybrid", "e2e-prod-gate"]
}
---

Use explicit hybrid recall that fuses lexical entry FTS with dense chunk retrieval.
The unique marker e2e-prod-gate-needle-zx9 identifies this decision in production E2E gates.
Local semantic search must preserve Worktrail contracts while ranking fused candidates.
