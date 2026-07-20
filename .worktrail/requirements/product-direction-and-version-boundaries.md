---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "product-direction-and-version-boundaries",
  "scope": "project",
  "type": "requirement",
  "title": "Product Direction and Version Boundaries",
  "status": "active",
  "lifecycle": "current",
  "topic": "product-direction"
}
---

# Product Direction and Version Boundaries

## Product purpose

Worktrail is a local-first AI coding knowledge memory layer. It helps agents discover, distill, review, and maintain durable knowledge with minimal user intervention while keeping every durable change explainable, auditable, reversible, and privacy-safe.

## Stable v1.0.0 boundary

The stable local workflow is:

```text
context -> evidence -> distill -> review plan -> confirmed candidate action -> evidence lifecycle -> maintenance hints and skills
```

v1.0.0 commits to local-first CLI and agent-skill workflows, explicit scopes, hidden-by-default transcript evidence, evidence-to-semantic-candidate distillation, read-only review planning, confirmed knowledge mutations, evidence lifecycle planning and cleanup, privacy-safe validation, and no automatic Git commit or knowledge lifecycle action.

Local semantic recall is an explicit conditional capability: it is available only after its separate release gate passes, uses a user-installed loopback-only llama.app runtime, BGE-M3 Q8_0 embeddings, rebuild-only local sqlite-vec generations, explicit semantic modes, visible lexical degradation, and an M1-specific verified claim. M2–M5 remain experimental without that claim.

v1.0.0 does not commit to a general daemon, watcher, scheduler, UI/TUI/dashboard, embedded LLM provider, cloud embeddings, standalone vector database, implicit indexing, semantic-quality auto-scoring, cross-machine sync, team collaboration, or fully automatic maintenance.

## Long-term invariants

- Transcript content is evidence, not formal knowledge.
- Candidates are reviewable proposals, not facts.
- Formal knowledge is durable project or user memory.
- Every formal change has source traceability.
- Agents may perform proactive read-only discovery.
- Mutations use an explicit, risk-aware safety model.
- Private local content does not enter committed validation artifacts.

## Product evolution constraints

Future evolution may explore continuous maintenance, risk-aware confirmation, stronger knowledge-quality signals, cross-project or cross-machine workflows, richer interaction surfaces, and more mature release operations. These are directions, not pre-approved implementation scope.

Each direction must preserve the v1 safety model and address its own trust boundary: background behavior introduces daemon and notification risk; risk classification can misclassify destructive work; semantic quality automation can create false confidence; synchronization adds identity, permissions, conflict, and privacy concerns; UI must not replace CLI contracts; operations must improve confidence without unnecessary ceremony.

## Evidence-led delivery

New work follows this sequence:

```text
real dogfood observation -> triaged finding -> numbered requirement -> implementation plan -> focused validation
```

Work is selected for demonstrated failure modes, repeated friction, or a clear knowledge-workflow gap, not speculative breadth.

## Version direction

v1.x improves the existing local workflow: dogfood templates, release notes, review auditability, low-noise maintenance summaries, knowledge-quality examples, and scope-preserving commands.

v2.0 is reserved for a meaningful expansion of the product contract, such as background maintenance, risk-aware semi-automation, cross-machine or team knowledge workflows, richer UI surfaces, or formal compatibility and migration policy. These changes require their own requirements, design, validation, and explicit review; they are not part of the v1.0.0 commitment.

## Migration provenance

Distilled from `docs/worktrail-long-term-vision-discussion.md`. The source remains in `docs/` until this candidate is promoted and its inbound references are repaired.
