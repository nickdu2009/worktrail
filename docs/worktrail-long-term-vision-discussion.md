# Worktrail Long-Term Vision Discussion

Last updated: 2026-05-15

Status: discussion result

## Summary

Worktrail `v1.0.0` should mean the local-first CLI and agent-skill knowledge
workflow is stable, safe, and useful. It should not mean Worktrail has reached
its final product form.

The long-term vision is broader: Worktrail should become a reliable AI coding
knowledge memory layer that helps agents discover, distill, review, and maintain
durable knowledge with minimal user intervention while keeping every long-term
knowledge change explainable, auditable, and reversible.

## V1.0.0 Boundary

The current `v1.0.0` boundary is a stable local workflow:

```text
context
-> evidence
-> distill
-> review plan
-> confirmed apply-plan or explicit candidate action
-> evidence lifecycle
-> maintenance hints and skills
```

This is enough for `v1.0.0` because Worktrail can now be used as a real local
knowledge workflow, not just a proof of concept.

`v1.0.0` commits to:

- local-first operation
- CLI and agent-skill workflows
- explicit scope behavior
- transcript evidence hidden by default
- evidence-to-semantic-candidate distillation
- read-only review planning
- confirmed knowledge mutation
- evidence lifecycle planning and confirmed cleanup
- privacy-safe validation records
- no automatic git commit
- no automatic promote, merge, discard, archive, restore, retire, or apply-plan

`v1.0.0` does not commit to:

- daemon or scheduler behavior
- UI, TUI, dashboard, or IDE sidebar
- embedded LLM provider
- vector search
- semantic quality auto-scoring
- cross-machine sync
- team collaboration
- fully automatic knowledge maintenance

## Long-Term Product Shape

The desired long-term shape is:

```text
transcript evidence is raw material
candidate knowledge is a reviewable proposal
formal knowledge is durable project or user memory
maintenance is agent-assisted and low-noise
mutation is risk-aware, explainable, and auditable
```

Worktrail should not become a system that blindly turns every chat into
documentation. That would create low-quality knowledge and make the memory layer
less trustworthy over time.

The product should preserve these invariants:

- Transcript content is evidence, not formal knowledge.
- Candidates are proposals, not facts.
- Formal knowledge is the durable source used by future agents.
- Every formal knowledge change has source traceability.
- Agents may do read-only discovery proactively.
- Mutating actions require a clear safety model.
- Private local content must not leak into committed validation artifacts.

## Major Gaps Between V1.0.0 And End State

### Continuous Maintenance

Current state:

- Maintenance is triggered by the user or agent.
- `/worktrail-maintain` chains read-only commands and asks for confirmation
  before state changes.

End-state direction:

- Worktrail can detect maintenance opportunities continuously or periodically.
- Users receive low-noise summaries instead of needing to ask what to do next.
- The system distinguishes urgent blockers, routine cleanup, and no-op states.

Design risk:

- Background behavior introduces daemon, scheduler, notification, and trust
  boundaries. These remain outside `v1.0.0`.

### Risk-Aware Confirmation

Current state:

- All mutating actions require explicit confirmation.

End-state direction:

- Confirmation can become risk-aware.
- Low-risk cleanup may be easier to approve in batches.
- Formal knowledge changes remain highly visible.
- Dangerous or ambiguous actions remain manual.

Possible risk classes:

- Low risk: archive evidence already covered by applied semantic knowledge.
- Medium risk: discard obviously empty or duplicate candidates.
- High risk: promote or merge durable formal knowledge.
- Always manual: destructive, ambiguous, privacy-sensitive, or cross-scope
  actions.

Design risk:

- If risk classification is wrong, Worktrail can pollute or lose knowledge.

### Knowledge Quality Governance

Current state:

- The CLI validates structure, safety, and source traceability.
- Semantic quality is handled by agent and user review.

End-state direction:

- Worktrail provides stronger quality signals without becoming the semantic
  judge.
- Agents receive better rubrics, examples, and review queues.
- The system can surface likely transcript summaries, duplicate knowledge,
  stale knowledge, and contradictory rules or decisions.

Design risk:

- Over-automating semantic judgment may reject useful knowledge or bless bad
  knowledge with false confidence.

### Cross-Project And Cross-Machine Knowledge

Current state:

- Knowledge is local-first with user and project scopes.
- Scope boundaries are explicit.

End-state direction:

- Personal knowledge can move across machines.
- Project knowledge can be shared safely.
- Teams can review and merge knowledge changes.
- Conflicts between user, project, and team knowledge can be detected and
  resolved.

Design risk:

- Sync and collaboration introduce identity, permissions, conflict resolution,
  privacy, and migration complexity.

### Richer Interaction Surfaces

Current state:

- The primary interface is CLI plus installed agent skills.

End-state direction:

- Worktrail may expose review queues, evidence graphs, knowledge diffs,
  maintenance inboxes, or dashboards.
- UI should improve comprehension, not replace the underlying CLI contracts.

Design risk:

- UI work can distract from stabilizing the knowledge model and safety
  invariants.

### Release And Operations Maturity

Current state:

- Release acceptance, validation checklist, isolated smoke, and dogfood records
  exist.

End-state direction:

- Worktrail has changelogs, release notes, migration guides, compatibility
  policies, schema lifecycle rules, fixture corpora, and release automation.

Design risk:

- Operational process should support product confidence without becoming heavy
  ceremony.

## Evolution Principle

Future work should not be chosen because it sounds impressive. It should be
chosen because dogfood shows a real failure mode, repeated friction, or a clear
gap in the knowledge workflow.

The preferred sequence is:

```text
real dogfood observation
-> triaged finding
-> numbered requirement
-> implementation plan
-> focused validation
```

This keeps Worktrail grounded in actual agent/user behavior.

## Potential Version Direction

### V1.x

Likely `v1.x` work should improve the current local workflow without changing
the fundamental product boundary:

- better dogfood record templates
- clearer release notes
- review apply-plan dry-run or richer audit output
- lower-noise maintenance summaries
- stronger skill examples for knowledge quality
- more scope-preserving generated commands

### V2.0

`v2.0` should be reserved for a meaningful expansion of the product contract,
such as:

- background maintenance
- risk-aware semi-automation
- cross-machine or team knowledge workflows
- richer UI surfaces
- formal compatibility and migration policy

These should not be rushed into `v1.0.0`.

## Conclusion

Current Worktrail can qualify as `v1.0.0` because it has a complete, safe,
validated local knowledge workflow.

Worktrail is not yet complete in the long-term product sense. The end state is a
trusted AI coding memory layer where agents can actively maintain knowledge, but
durable facts remain source-traceable, reviewable, and controlled by a clear
safety model.

The next step after `v1.0.0` should be post-release dogfood, not immediate
feature expansion.
