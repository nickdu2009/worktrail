# Agent Text and CLI Write Boundary

Last updated: 2026-06-06

Status: implemented

Related contract: [semantic-text-safety-contract.md](semantic-text-safety-contract.md)

## Summary

Worktrail should allow the current coding agent to draft knowledge text, but the
`worktrail` CLI must remain the write boundary for candidate creation and formal
knowledge mutation.

This document defines that boundary so implementation reviews can distinguish:

- acceptable agent-authored text drafting
- required CLI-owned persistence and state changes
- weak workflow compliance versus strong enforcement

## Goal

The goal is to preserve the useful part of AI assistance without letting the
agent bypass Worktrail's lifecycle controls.

Desired split of responsibility:

- the agent understands evidence, summarizes it, and drafts text
- the CLI validates, records, applies, and mutates Worktrail state
- the user confirms state-changing actions at the appropriate boundary

## Non-Goals

- Do not forbid agents from generating proposal text, summaries, or candidate
  bodies.
- Do not require a separate UI, daemon, or background process.
- Do not require every read-only helper command to pass through an additional
  review layer.
- Do not redefine Worktrail's evidence, candidate, and formal knowledge
  lifecycle.

## Boundary Definition

### Agent Responsibility

The coding agent may author text such as:

- proposal JSON bodies
- candidate `title`
- candidate `summary`
- candidate `body`
- proposal `reason`
- review summaries and confirmation prompts

The agent may also assemble temporary pack or proposal files for later CLI
validation.

### CLI Responsibility

The `worktrail` CLI owns all persistence that changes Worktrail-managed state.

This includes:

- creating pending evidence candidates
- creating pending semantic candidates
- promoting a candidate into formal knowledge
- merging a candidate into formal knowledge
- discarding, restoring, retiring, archiving, or otherwise changing candidate
  lifecycle state
- rebuilding derived indexes after state-changing operations when required

## Required Rules

### 1. Candidate Creation Must Go Through CLI

Any persistent candidate record must be created through a `worktrail` CLI
command.

Accepted examples:

- `worktrail note add`
- `worktrail import ...`
- `worktrail distill apply ...`
- `worktrail candidates create`
- `worktrail maintain apply ...` with a `create_candidate` action

Rejected pattern:

- the agent writes `.worktrail/candidates/...` files directly as the primary
  workflow

### 2. Formal Knowledge Mutation Must Go Through CLI

Any mutation of formal knowledge must be executed by a `worktrail` CLI command.

Accepted examples:

- `worktrail promote`
- `worktrail merge`
- `worktrail review apply-plan --confirm`
- `worktrail review apply-candidates ...`
- `worktrail maintain apply ...` for supported formal actions

Rejected pattern:

- the agent edits formal knowledge files directly under Worktrail knowledge roots
  as the primary workflow

Formal knowledge roots include paths such as:

- `.worktrail/rules/`
- `.worktrail/workflows/`
- `.worktrail/decisions/`
- `.worktrail/architecture/`
- `.worktrail/lessons/`
- `.worktrail/prompts/`
- `.worktrail/project.md`
- `.worktrail/index.md`

### 3. Agent Direct Editing Is Limited to Temporary Artifacts

In the recommended workflow, the agent may directly edit only temporary or
intermediate artifacts such as:

- temporary distill packs
- temporary proposal files
- review notes prepared for user confirmation

The agent should not directly edit formal knowledge or candidate storage as the
main write path.

### 4. Validate and Apply Must Form a Real Boundary

When a workflow uses proposal-based mutation, the split must be:

- the agent drafts proposal content
- `validate` checks schema, path safety, and content safety
- `apply` performs the actual persistent mutation

If `validate` does not reject unsafe proposal content that the design claims it
should reject, then this requirement is only partially satisfied.

### 5. State-Changing Actions Must Stay Traceable

Every formal write should be traceable to:

- the CLI action that executed it
- the candidate or proposal that authorized it
- the source evidence or source candidates when applicable

Workflows that bypass this traceability do not satisfy the intended boundary,
even if the final file happens to be written by CLI code.

## Compliance Levels

### Weak Compliance

A workflow is weakly compliant when:

- the agent drafts text
- the normal product path uses CLI commands to persist candidates and formal
  knowledge

This is enough to say the architecture follows the intended split at a workflow
level.

### Strong Compliance

A workflow is strongly compliant when all of the following hold:

- candidate creation is CLI-only in the supported workflow
- formal knowledge mutation is CLI-only in the supported workflow
- the agent does not directly edit formal knowledge paths in the recommended
  workflow
- proposal `validate` commands enforce the documented safety checks
- direct CLI write-path failures in `--format json` return
  `worktrail.cli.error.v1` on stdout with `ok: false`; agents must inspect the
  envelope instead of relying on process exit codes or Go `Run()` errors
- state-changing operations preserve traceability and the intended review
  boundary

Strong compliance is the preferred acceptance level for review-sensitive
features.

## Acceptance Checklist

A feature or CLI flow satisfies this boundary when:

- agent-authored text is accepted as input
- persistent candidate creation is performed by CLI code
- persistent formal knowledge mutation is performed by CLI code
- direct agent editing of formal knowledge is not the intended workflow
- any proposal workflow has a meaningful validate/apply split
- state-changing writes remain reviewable and traceable

## Current Review Heuristic

When reviewing an implementation against this boundary, classify findings as:

- `blocking` when the workflow lets formal knowledge or candidate persistence
  bypass the CLI boundary, or when a documented validate step fails to enforce a
  claimed safety guarantee
- `warning` when the main boundary exists but consistency, traceability, or
  post-apply behavior is incomplete
- `future work` when stronger enforcement is desired but the current documented
  contract does not yet require it

## Examples

### Compliant Example

1. The agent reads transcript evidence.
2. The agent drafts a `worktrail.distill.proposal.v1` JSON file.
3. `worktrail distill validate proposal.json` checks the proposal.
4. `worktrail distill apply proposal.json` creates pending semantic candidates.
5. `worktrail review plan --format json` summarizes recommended actions.
6. After confirmation, `worktrail promote` or `worktrail merge` writes formal
   knowledge.

### Non-Compliant Example

1. The agent reads transcript evidence.
2. The agent directly edits `.worktrail/rules/example.md`.
3. No candidate, proposal, review, or CLI mutation path records how that formal
   knowledge was created.

This bypasses the intended boundary even if the resulting Markdown looks valid.
