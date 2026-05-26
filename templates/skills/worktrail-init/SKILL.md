---
name: worktrail-init
description: Initialize Worktrail and install Worktrail agent integrations. Use when the user asks to initialize Worktrail, set up Worktrail, install Worktrail for Cursor, Codex, Claude, or all agents, configure Worktrail hooks, MCP, rules, skills, or says 初始化 Worktrail.
---

# Worktrail Init

Use this skill when initializing Worktrail or installing Worktrail-managed agent integrations.

## Rules

- Initialize Worktrail only; do not initialize the target application's language, framework, package manager, or git repository.
- Do not require the target project to have Node, Python, Go, package manifests, or any app structure.
- Do not overwrite non-Worktrail configuration.
- Explain write impact before running commands that install or merge Worktrail integration files.
- Worktrail-managed skills, hooks, and MCP configs call the `worktrail` command. Before installing integrations, ensure the Worktrail CLI is installed and available in `PATH`.

## Workflow

1. Confirm the target directory. Default to the current workspace unless the user specified another directory.
2. Verify the Worktrail CLI is installed:
   - Run `worktrail --help`.
   - If it is missing, install the CLI first, for example `go install ./cmd/worktrail` from this repository or by using the packaged binary, then ensure `worktrail` is in `PATH`.
3. Inspect current Worktrail state:
   - `.worktrail/config.json`
   - `.gitignore`
   - `.cursor/mcp.json`
   - `.cursor/hooks.json`
   - `.codex/hooks.json`
   - `.claude/settings.json`
4. Run `worktrail init`.
   - This initializes user and project Worktrail roots.
   - It may create or update project `.worktrail`, `.gitignore`, and `.codex/hooks.json`.
5. Install requested integrations:
   - `worktrail install cursor --user|--project`
   - `worktrail install codex --user|--project`
   - `worktrail install claude --user|--project`
   - `worktrail install all --user|--project`
6. Verify with `worktrail doctor <tool> --user|--project`; the doctor report must include a passing `worktrail command available` check.
7. Report only Worktrail-managed changes and any remaining setup step.

## Scope Notes

- `--user` installs user-level Worktrail instructions and skills where supported.
- `--project` installs local project integration files where supported, such as Cursor MCP/hooks config.
- User-level instructions and skills must not auto-run Worktrail in a project unless `.worktrail/` exists at that workspace or repository root; project initialization or project install creates the project opt-in marker.
- Cursor project installs do not create project-level skills or rules; they merge `.cursor/mcp.json` and `.cursor/hooks.json`.

## Output

`[output: worktrail-init | completed <confidence> | target:"..." initialized:"..." installed:"..." validation:"..." | next:<action>]`
