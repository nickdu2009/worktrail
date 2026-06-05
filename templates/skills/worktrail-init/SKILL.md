---
name: worktrail-init
description: Initialize Worktrail and install Cursor, Codex, or Claude integrations. Use when the user wants to initialize Worktrail or configure Worktrail-managed hooks, skills, rules, or tool settings.
---

# Worktrail Init

Use this skill when initializing Worktrail or installing Worktrail-managed agent integrations.

## Guardrails

- Initialize Worktrail only; do not initialize the target application's language, framework, package manager, or git repository.
- Do not require the target project to have Node, Python, Go, package manifests, or any app structure.
- Do not overwrite non-Worktrail configuration.
- Explain write impact before running commands that install or merge Worktrail integration files.
- Worktrail-managed skills, hooks, and tool settings call the `worktrail` command. Before installing integrations, ensure the Worktrail CLI is installed and available in `PATH`.
- `--user` installs user-level Worktrail instructions and skills where supported. `--project` installs local integration files where supported, such as Cursor hooks and tool settings.
- User-level instructions and skills must not auto-run Worktrail in a project unless `.worktrail/` exists at that workspace or repository root; project initialization or project install creates the project opt-in marker.
- Cursor project installs do not create project-level skills or rules; they merge `.cursor/hooks.json`.

## Workflow

1. Confirm the target directory. Default to the current workspace unless the user specified another directory.
2. Verify the CLI:
   - Run `worktrail --help`.
   - If it is missing, install the CLI first, for example `go install ./cmd/worktrail` from this repository or by using the packaged binary, then ensure `worktrail` is in `PATH`.
3. Inspect current Worktrail state in `.worktrail/config.json`, `.gitignore`, `.cursor/hooks.json`, `.codex/hooks.json`, and `.claude/settings.json`.
4. Run `worktrail init` to initialize user and project Worktrail roots. It may create or update project `.worktrail`, `.gitignore`, and `.codex/hooks.json`.
5. Install requested integrations with `worktrail install cursor|codex|claude|all --user|--project`.
6. Verify with `worktrail doctor <tool> --user|--project`; the doctor report must include a passing `worktrail command available` check.
7. Report only Worktrail-managed changes and any remaining setup step.

## Output

`[output: worktrail-init | completed <confidence> | target:"..." initialized:"..." installed:"..." validation:"..." | next:<action>]`
