---
name: worktrail-adr
description: Persist reviewed Architecture Decision Records as pending Worktrail decision candidates. Use when the user explicitly asks to save, land, or persist an ADR in Worktrail.
---

# Worktrail ADR

Use this skill only when the user explicitly asks to persist an ADR in a Worktrail-enabled project.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root, stop; do not initialize Worktrail implicitly.
- ADR content review and Worktrail persistence are separate. This skill creates a pending candidate only.
- Never directly edit `.worktrail/decisions/`.
- Never promote, merge, discard, restore, retire, or commit automatically.
- If the user only asks to explain, draft, or review an ADR, do not run a Worktrail write command.
- If the user says not to persist yet, stop after returning the portable ADR.

## Persistence gate

Proceed only when both conditions are true:

1. The user explicitly requests Worktrail persistence.
2. ADR content readiness is established by either:
   - a compatible review result with `type: adr-rfc` and `review_result: clean` or `clean_with_assumptions`; or
   - a structurally valid ADR plus the user's explicit confirmation that content review is complete and the ADR is ready to persist.

Do not require `design-review-loop` or any other external skill to be installed. A compatible result is optional evidence, not a runtime dependency.

If no compatible review result exists:

1. Verify the ADR structure below.
2. Report any missing or conflicting content.
3. Ask the user to confirm: `ADR content review is complete and this document is ready to persist as a pending Worktrail candidate.`
4. Stop if confirmation is absent.

## Minimum ADR structure

- Heading: `# <ADR-ID>: <title>`
- ID: `ADR-NNNN` or `ADR-YYYYMMDD-<slug>`
- Status: `Proposed`, `Accepted`, `Deprecated`, or `Superseded`
- Non-empty sections: `Context`, `Decision`, and `Consequences`
- Use `Considered Alternatives` when multiple realistic options existed.
- Optional relationship keys under `Links`:
  - Accepted: `- Supersedes: <ADR-ID>`
  - Proposed: `- Proposes to supersede: <ADR-ID>`
  - Informational: `- Related: <ADR-ID>`

ADR document status, candidate status, and formal knowledge lifecycle are independent. Candidate promotion must not change ADR status.

## Workflow

1. Confirm the persistence gate and `.worktrail/`.
2. Parse the ADR ID, title, status, and `Links`.
3. For each Accepted `Supersedes` ID:
   - Run `worktrail search --type decision --format json "<ADR-ID>"`.
   - Accept only an exact ADR ID match under `decisions/` that is formal and current.
   - Treat an omitted/empty lifecycle as current, which is Worktrail's canonical search projection; reject `historical` or `retired`.
   - Stop on zero or multiple matches.
   - Collect the matched formal decision path.
4. Proposed ADRs may retain `Proposes to supersede` in Markdown but must not pass `--supersedes`.
5. Create one pending decision candidate.
6. Run `worktrail review plan --format json` with the same scope.
7. Report the candidate id, target path, review gate evidence, and `next: worktrail-review`.

## Safe input

Prefer an existing file:

```bash
worktrail adr create "<title>" \
  --from-file "<adr-file>" \
  --decision-status "<status>" \
  --format json
```

Add `--scope user` only for user-scope knowledge. Add resolved formal paths with:

```bash
--supersedes "decisions/old-one.md,decisions/old-two.md"
```

For ADR content held only in context, use explicit stdin and a single-quoted heredoc delimiter so shell expansion is disabled:

```bash
worktrail adr create "<title>" --stdin --format json <<'WORKTRAIL_ADR'
# ADR-0001: Example

- Status: Proposed
- Date: 2026-07-14

## Context

Context.

## Decision

Decision.

## Consequences

Consequences.
WORKTRAIL_ADR
```

Do not use an unquoted heredoc delimiter. If a temporary file is necessary, create it outside formal Worktrail knowledge and remove it immediately after candidate creation.

## Failure handling

- Missing review evidence and no explicit readiness confirmation: stop before writing.
- Invalid ADR structure, status, ID, or relationship: fix or return the validation error.
- Missing or ambiguous superseded decision: stop and report the unresolved ADR ID.
- CLI JSON with `ok: false`: report the error and do not continue to review.
- Candidate creation succeeds but review plan fails: preserve the pending candidate id and report the read-only follow-up failure.

## Output

`[output: worktrail-adr | completed <confidence> | candidate_id:"..." target:"..." review_gate:"compatible_review|user_attestation" | next:worktrail-review]`
