---
name: worktrail-search
description: Search Worktrail knowledge by keyword. Use when the user wants to find a Worktrail-managed doc, rule, lesson, workflow, state, or handoff without browsing preview.
---

# Worktrail Search

Use this skill when the user wants a keyword-based lookup in Worktrail instead of browsing the full preview site.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Search only Worktrail-managed content.
- Use `worktrail search` as the first command when the user wants to pinpoint a doc, rule, workflow, or lesson by keyword.
- Do not substitute `worktrail context` or `worktrail preview` for the initial keyword lookup.
- Do not substitute generic shell tools such as `rg`, `grep`, `find`, or `cat` for Worktrail keyword lookup.
- Do not open the preview site unless the user also asks to browse rendered Worktrail pages.
- Do not use browser verification for search-only flows.

## Workflow

1. Identify the keyword or phrase the user wants to locate.
2. Run `worktrail search "<keyword>"` before any fallback exploration.
3. Report the relevant document paths or the lack of matches.
4. If the result is too broad, refine the keyword and rerun only while the user intent is still clearly search-oriented.
5. Only after `worktrail search` proves insufficient, recommend `worktrail preview` for browsing a rendered page.

## Output

`[output: worktrail-search | completed <confidence> | keyword:"..." validation:"..." | next:<action>]`
