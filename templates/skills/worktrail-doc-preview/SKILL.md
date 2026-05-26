---
name: worktrail-doc-preview
description: Preview the overall Worktrail knowledge library in a browser. Use when the user asks to preview Worktrail docs, render Worktrail knowledge, open a Worktrail candidate, handoff, workflow, profile, rule, lesson, or says 预览 Worktrail 文档.
---

# Worktrail Doc Preview

Use this skill when previewing the overall Worktrail knowledge view for a project or user scope.

## Rules

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Preview only Worktrail-managed content: the overall user/project Worktrail knowledge view, which already includes pending candidates.
- Do not use the target project's docs site, dev server, package manager, or framework.
- Do not modify target project business files.
- Do not preview arbitrary external Markdown unless the user explicitly asks for a future non-Worktrail flow.

## Workflow

1. Identify the scope the user wants to inspect:
   - Use `project` by default for repository knowledge.
   - Use `user` for user-level workflows, profile, prompts, or lessons.
2. Choose scope:
   - Use `--scope project` by default.
   - Use `--scope user` for user-level workflows, profile, prompts, or lessons.
3. Run one of:
   - `worktrail preview --scope <scope>`
   - `worktrail preview --scope <scope> --no-open` when you only need the preview file path
4. In Cursor, verify the browser result:
   - list tabs before interacting
   - open the generated local preview file if needed
   - inspect a fresh snapshot when browser automation supports the page
   - confirm the title and the relevant section or pending candidate are visible
5. Report the scope, preview file path, and validation result.

## Output

`[output: worktrail-doc-preview | completed <confidence> | scope:"..." file:"..." validation:"..." | next:<action>]`
