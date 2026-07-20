---worktrail
{
  "schema": "worktrail.knowledge.v1",
  "id": "post-release-dogfood-feedback",
  "scope": "project",
  "type": "workflow",
  "title": "Post-Release Dogfood Feedback",
  "status": "active",
  "lifecycle": "current",
  "topic": "post-release-dogfood"
}
---

# Post-Release Dogfood Feedback

## Purpose

Turn real post-release maintenance use into privacy-safe, triaged feedback and convert repeated or blocking problems into independently reviewable requirements rather than speculative implementation work.

## Cadence

Run at least three real maintenance passes: a mostly read-only/no-op pass, a pass with pending evidence, and a pass with pending semantic candidates or evidence lifecycle work. Start from the read-only maintenance chain: context, distill summary, review plan, and evidence plan; follow generated scoped commands only when appropriate.

## Record contract

Create one dated record per pass with scenario, starting state, scoped commands and outcomes, queue counts, required user intervention, knowledge-quality observations, scope UX, privacy check, findings, and decision. Do not commit transcript bodies, local paths, session ids, usernames, or temporary pack/proposal contents.

## Triage

Classify every finding as `release-blocking`, `post-release`, `polish`, or `no-action`. Create a numbered requirement only when friction repeats, safe lane selection is unclear, scope causes an unsafe command attempt, semantic drafts repeatedly need the same rewrite, or read-only maintenance hides a meaningful blocker.

A requirement records priority, problem, verifiable acceptance, and its dogfood evidence. Raw dogfood notes do not authorize implementation directly.

## Boundaries

This workflow does not add automatic mutation, daemon behavior, UI, vector search, embedded LLM behavior, or unbounded/private record capture. Candidate follow-up areas are observations only, not approved delivery scope.

## Completion

The loop is active when three dated records exist, all findings are triaged, private content is absent, repeated findings are converted to `REQ-POST-*` requirements, and the backlog links the current requirements.

## Migration provenance

Distilled from `docs/post-release-dogfood-feedback.md`. The source remains in `docs/` until this candidate is promoted and inbound references are reviewed.
