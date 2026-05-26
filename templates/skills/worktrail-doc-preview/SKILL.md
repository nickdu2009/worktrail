---
name: worktrail-doc-preview
description: Preview Worktrail-managed documents and candidates in a browser. Use when the user asks to preview Worktrail docs, render Worktrail knowledge, open a Worktrail candidate, handoff, workflow, profile, rule, lesson, or says 预览 Worktrail 文档.
---

# Worktrail Doc Preview

Use this skill when previewing Worktrail-maintained Markdown or candidate content.

## Rules

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Preview only Worktrail-managed content: user/project Worktrail documents or Worktrail candidates.
- Do not use the target project's docs site, dev server, package manager, or framework.
- Do not modify target project business files.
- Do not preview arbitrary external Markdown unless the user explicitly asks for a future non-Worktrail flow.

## Workflow

1. Identify the source: document path, candidate id, handoff, workflow, profile, rule, or lesson.
2. Choose scope:
   - Use `--scope project` by default.
   - Use `--scope user` for user-level workflows, profile, prompts, or lessons.
3. Run one of:
   - `worktrail preview <path> --scope <scope> --open`
   - `worktrail preview --candidate <id> --scope <scope> --open`
4. In Cursor, verify the browser result:
   - list tabs before interacting
   - navigate to the local preview URL if needed
   - inspect a fresh snapshot
   - confirm the title and key body text are visible
5. Report the source, URL, and validation result.

## Automation

For machine-readable output without a long-running server, use:

`worktrail preview <path> --scope <scope> --render-only --format json`

## Output

`[output: worktrail-doc-preview | completed <confidence> | source:"..." url:"..." validation:"..." | next:<action>]`
