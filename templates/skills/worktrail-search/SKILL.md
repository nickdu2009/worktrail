---
name: worktrail-search
description: Search Worktrail knowledge by keyword or paraphrase. Use when the user wants to find a Worktrail-managed doc, rule, lesson, workflow, state, or handoff without browsing preview.
---

# Worktrail Search

Use this skill when the user wants a keyword-based or paraphrase lookup in Worktrail instead of browsing the full preview site.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Search only Worktrail-managed content.
- Use `worktrail search --semantic=auto "<keyword>"` as the first command when the user wants to pinpoint a doc, rule, workflow, or lesson. Prefer `auto` over `required` so missing or mismatched semantic generations visibly degrade instead of blocking lookup.
- For exact identifier lookups (ADR IDs, paths, entry ids), omit `--semantic` and keep lexical search so identity matches stay exact.
- If stderr or JSON v2 reports semantic degradation/fallback, continue with the results and note the reason; do not silently treat the run as full hybrid recall.
- Do not substitute `worktrail context` or `worktrail preview` for the initial keyword lookup.
- Do not substitute generic shell tools such as `rg`, `grep`, `find`, or `cat` for Worktrail keyword lookup.
- Do not open the preview site unless the user also asks to browse rendered Worktrail pages.
- Do not use browser verification for search-only flows.
- When a match is a handoff, identify it as a local/team task-scoped runtime record. Do not describe it as formal knowledge or a pending candidate.

## Workflow

1. Identify the keyword, phrase, or paraphrase the user wants to locate.
2. Run `worktrail search --semantic=auto "<keyword>"` before any fallback exploration. Use `--format json-v2` when diagnostics (policy, lanes, degraded) are needed.
3. Report the relevant document paths or the lack of matches. For handoffs, include visibility and task id when available so local and immutable team records are not confused.
4. If the result is too broad, refine the keyword and rerun only while the user intent is still clearly search-oriented.
5. Only after `worktrail search` proves insufficient, recommend `worktrail preview` for browsing a rendered page.

## Output

`[output: worktrail-search | completed <confidence> | keyword:"..." validation:"..." | next:<action>]`
