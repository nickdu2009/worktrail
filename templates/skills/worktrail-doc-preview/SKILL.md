---
name: worktrail-doc-preview
description: Preview Worktrail user or project knowledge in the static browser site. Use when the user wants to browse rendered Worktrail docs, candidates, handoffs, workflows, profiles, rules, or lessons.
---

# Worktrail Doc Preview

Use this skill when previewing the overall Worktrail knowledge view for a project or user scope through the static multi-page preview site.

## Guardrails

- If `.worktrail/` is absent at the current workspace or repository root and the user did not explicitly ask to initialize, install, inspect, repair, or otherwise manage Worktrail, stop; Worktrail is not enabled for this project.
- Preview only Worktrail-managed content: the overall user/project Worktrail knowledge view, which already includes pending candidates.
- If the user wants a keyword lookup, stop and use the installed `worktrail-search` skill or `worktrail search` instead of `worktrail preview`.
- Do not use the target project's docs site, dev server, package manager, or framework.
- Do not modify target project business files.
- Do not preview arbitrary external Markdown unless the user explicitly asks for a future non-Worktrail flow.
- Present local/team handoffs under runtime recovery, separate from formal knowledge and pending semantic review.

## Workflow

1. Choose scope:
   - Use `project` by default for repository knowledge.
   - Use `user` for user-level workflows, profile, prompts, or lessons.
2. Run `worktrail preview --scope <scope>` or `worktrail preview --scope <scope> --no-open` when you only need the preview entry path.
3. When browser verification is available in the current agent surface, verify that the generated entry page opens and that the relevant section, document page, runtime handoff, or pending candidate is reachable from the site.
4. If browsing a handoff, report local/team visibility and task id. Team handoffs are immutable DAG nodes; local handoffs are the default private recovery records.
5. Report the scope, preview entry path, and validation result.

## Output

`[output: worktrail-doc-preview | completed <confidence> | scope:"..." file:"..." validation:"..." | next:<action>]`
