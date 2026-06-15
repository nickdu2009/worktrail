package templates

import (
	"fmt"
	"strings"
)

const SkillTriggerRoutingPlaceholder = "{{WORKTRAIL_SKILL_TRIGGER_ROUTING}}"
const RootSharedPlaceholder = "{{WORKTRAIL_ROOT_SHARED}}"

type SkillTrigger struct {
	Skill           string
	UseWhen         []string
	RequiredActions []string
	Never           []string
	RootIntent      string
	RootCommand     string
	RootGuardrail   string
}

var skillTriggers = []SkillTrigger{
	{
		Skill: "worktrail-context",
		UseWhen: []string{
			"starting substantial project work, loading project memory, or when the user asks to start work, load context, or load project context",
		},
		RequiredActions: []string{
			"Run `/worktrail-context` or `worktrail context \"<task>\"` before substantial work.",
			"Read the Context Pack and follow active state, constraints, maintenance hints, and next steps.",
		},
		Never: []string{
			"Do not skip Worktrail context for substantial project work unless no project knowledge exists.",
			"Do not use `worktrail context` as the recovery command for a new session that should continue prior state or handoff; use `worktrail resume` instead.",
		},
		RootIntent:    "starting substantial project work, loading project memory, or when the user asks to start work, load context, or load project context",
		RootCommand:   "Run `worktrail context \"<task>\"`.",
		RootGuardrail: "Do not use `worktrail context` to resume prior state or handoff; use `worktrail resume` instead.",
	},
	{
		Skill: "worktrail-doc-preview",
		UseWhen: []string{
			"the user asks to preview Worktrail docs, render Worktrail knowledge, open a Worktrail candidate, handoff, workflow, profile, rule, lesson, or says 预览 Worktrail 文档",
		},
		RequiredActions: []string{
			"Run `worktrail preview --scope <scope>` or `worktrail preview --scope <scope> --no-open`.",
			"Use browser verification when available for preview flows, and report the preview entry path, scope, and validation result.",
		},
		Never: []string{
			"Do not use `worktrail preview` for keyword lookup requests; use `worktrail search` instead.",
			"Do not use the target project's dev server, docs site, package manager, or framework for Worktrail document preview.",
			"Do not preview non-Worktrail content or modify target project business files.",
		},
		RootIntent:    "the user asks to preview Worktrail docs, render Worktrail knowledge, or browse docs/candidates/handoffs/rules/workflows through the rendered site",
		RootCommand:   "Run `worktrail preview --scope <scope>` or `worktrail preview --scope <scope> --no-open`.",
		RootGuardrail: "Do not use preview for keyword lookup; use `worktrail search` first.",
	},
	{
		Skill: "worktrail-search",
		UseWhen: []string{
			"the user asks to search Worktrail knowledge, find a Worktrail document by keyword, locate a rule or lesson in Worktrail, or pinpoint Worktrail knowledge without opening the full preview site",
		},
		RequiredActions: []string{
			"Run `worktrail search \"<keyword>\"` for keyword lookup when the user wants to pinpoint Worktrail knowledge quickly.",
			"Report the keyword used, the scope if relevant, and the search result or next step.",
		},
		Never: []string{
			"Do not substitute `worktrail context` or `worktrail preview` for the initial keyword lookup.",
			"Do not substitute generic shell tools such as `rg`, `grep`, or `find` for Worktrail keyword lookup.",
			"Do not open the preview site unless the user also asked to browse rendered Worktrail pages.",
			"Do not use browser verification for search-only flows.",
		},
		RootIntent:    "the user wants to look up Worktrail knowledge by keyword, term, or phrase without browsing the full preview site",
		RootCommand:   "Run `worktrail search \"<keyword>\"`.",
		RootGuardrail: "Do not substitute `rg`, `grep`, `find`, `worktrail context`, or `worktrail preview` for the initial lookup.",
	},
	{
		Skill: "worktrail-init",
		UseWhen: []string{
			"the user asks to initialize Worktrail, set up Worktrail, install Worktrail for Cursor, Codex, Claude, or all agents, configure Worktrail hooks, rules, skills, or tool settings",
		},
		RequiredActions: []string{
			"Ensure the Worktrail CLI is installed and `worktrail` is available in `PATH` before installing Worktrail-managed skills, hooks, or tool settings.",
			"Run `worktrail init` for Worktrail user and project initialization.",
			"Run `worktrail install cursor|codex|claude|all --user|--project` as requested and verify with `worktrail doctor <tool> --user|--project`.",
			"Report the Worktrail-managed files affected, including `.worktrail`, `.gitignore`, agent hooks, tool settings, rules, and skills as applicable.",
		},
		Never: []string{
			"Do not initialize the target application's language, framework, package manager, or git repository.",
			"Do not require the target project to have any specific technology stack or overwrite non-Worktrail configuration.",
		},
		RootIntent:    "the user asks to initialize Worktrail, install Worktrail integrations, or configure Worktrail hooks, rules, skills, or tool settings",
		RootCommand:   "Run `worktrail init`, then `worktrail install cursor|codex|claude|all --user|--project`, and verify with `worktrail doctor <tool> --user|--project`.",
		RootGuardrail: "Ensure the `worktrail` CLI is available in `PATH`, and do not initialize unrelated app/framework tooling.",
	},
	{
		Skill: "worktrail-state",
		UseWhen: []string{
			"the task is long, risky, multi-step, likely to compact, needs a checkpoint, or the user asks to record current state, update state, create a checkpoint, inject state, or save progress",
		},
		RequiredActions: []string{
			"Use `worktrail state start`, `worktrail state update \"<note>\"`, `worktrail state checkpoint --reason <reason>`, or `worktrail state inject \"<task>\"` as appropriate.",
			"Keep state factual: goal, constraints, evidence, decisions, work done, validation, open questions, and next step.",
		},
		Never: []string{
			"Do not use `worktrail state start` or `worktrail state inject` when the user is resuming prior Worktrail work in a new session; use `worktrail resume` instead.",
			"Do not store secrets, raw credentials, or private runtime payloads in state.",
		},
		RootIntent:    "the task is long, risky, multi-step, likely to compact, or the user asks to record current state, update progress, checkpoint, or inject state",
		RootCommand:   "Use `worktrail state start|update|checkpoint|inject` as appropriate.",
		RootGuardrail: "Use state only for the current session; do not use it as a substitute for `worktrail resume` in a new session.",
	},
	{
		Skill: "worktrail-resume",
		UseWhen: []string{
			"starting a new session that should continue prior work, resuming from the latest state or handoff, or when the user asks to continue the previous Worktrail session",
		},
		RequiredActions: []string{
			"Run `worktrail resume \"<task>\"` when a new session should continue from the latest state and/or durable handoff.",
			"Read the resumed state output and continue work from the linked records instead of reconstructing context manually.",
		},
		Never: []string{
			"Do not use `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, or `worktrail state show` as substitutes when the intent is to resume prior Worktrail state.",
			"Do not skip reading the resumed records before continuing risky work.",
		},
		RootIntent:    "starting a new session that should continue prior work from the latest state or durable handoff",
		RootCommand:   "Run `worktrail resume [<task>]`.",
		RootGuardrail: "Do not substitute `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, or `worktrail state show` for resume.",
	},
	{
		Skill: "worktrail-handoff",
		UseWhen: []string{
			"the user explicitly asks to create a handoff, pause here for the next chat, switch agents, hand off work, end the current chat, or continue later from another conversation",
		},
		RequiredActions: []string{
			"Summarize active state, current diff intent, validation, risks, open questions, and the next step.",
			"If an active explicit state exists, run `worktrail state close --to handoff \"<summary>\"` so the handoff is bound to the latest state and that state is archived atomically.",
			"If no active explicit state exists, run `/worktrail-handoff` or `worktrail handoff \"<summary>\"` and write a durable Worktrail handoff record.",
		},
		Never: []string{
			"Do not only output a copyable text handoff when the user asked for a Worktrail handoff.",
			"Do not create a durable handoff just because a normal reply finished, a subtask completed, or compacting happened without an explicit handoff boundary.",
			"Do not promote, merge, discard, restore, or retire without explicit user confirmation.",
		},
		RootIntent:    "the user explicitly wants a handoff, wants to continue later in another chat, or asks to switch agents and preserve recovery context",
		RootCommand:   "Prefer `worktrail state close --to handoff \"<summary>\"` when an active explicit state exists; otherwise run `worktrail handoff \"<summary>\"`.",
		RootGuardrail: "Do not create a durable handoff for ordinary task progress; only do it at an explicit handoff boundary, and do not only output a copyable text handoff.",
	},
	{
		Skill: "worktrail-import",
		UseWhen: []string{
			"the user wants to import, sync, extract, migrate, or reuse knowledge from Codex, Claude, Cursor, transcript files, all current-project conversations, observed Cursor conversations, or legacy KDD docs",
		},
		RequiredActions: []string{
			"Run the relevant bounded dry-run first: `worktrail import codex --since 14d`, `worktrail import cursor --limit 20`, `worktrail sync <source> <file>`, or `worktrail migrate kdd`; prefer the exact command from `worktrail context \"maintenance\"` when present.",
			"Create pending candidates only after the user asked to proceed, then hand off review to `/worktrail-review`.",
		},
		Never: []string{
			"Do not promote imported transcript notes or migration sources directly.",
			"Do not scan undocumented private Cursor directories.",
		},
		RootIntent:    "the user wants to import, sync, extract, migrate, or reuse knowledge from transcripts, observed sessions, or legacy KDD docs",
		RootCommand:   "Start with the relevant bounded dry-run such as `worktrail import codex --since 14d`, `worktrail import cursor --limit 20`, `worktrail sync <source> <file>`, or `worktrail migrate kdd`.",
		RootGuardrail: "Create pending candidates only after the user asks to proceed, and never promote imported evidence directly.",
	},
	{
		Skill: "worktrail-distill",
		UseWhen: []string{
			"pending transcript_notes, migration_source, KDD split-source evidence, or imported evidence should become semantic Worktrail candidates",
		},
		RequiredActions: []string{
			"Run `worktrail distill --pending --summary` first and preserve any suggested `--scope`.",
			"Create a temporary evidence pack, draft a `worktrail.distill.proposal.v1` proposal, run `worktrail distill validate <proposal.json>`, wait for explicit confirmation, then run `worktrail distill apply <proposal.json>`.",
			"Run `worktrail review plan --format json` after apply and hand off review to `/worktrail-review`.",
		},
		Never: []string{
			"Do not paste transcript evidence bodies, local paths, usernames, session ids, or temporary file paths into durable docs.",
			"Do not promote, merge, discard, archive, restore, or retire from this skill.",
		},
		RootIntent:    "pending transcript_notes, migration_source, KDD split-source evidence, or imported evidence should become semantic Worktrail candidates",
		RootCommand:   "Run `worktrail distill --pending --summary`, then the validate/apply workflow when the user confirms.",
		RootGuardrail: "Do not promote, merge, discard, archive, restore, or retire from the distill lane.",
	},
	{
		Skill: "worktrail-review",
		UseWhen: []string{
			"the user asks to review candidates, decide whether knowledge should be promoted, merged, discarded, restored, or retired, or inspect pending Worktrail knowledge",
		},
		RequiredActions: []string{
			"Run `worktrail review plan --format json` for pending semantic candidates.",
			"Group items by `recommended_action`, show counts and source traceability, and use plan `commands` or `worktrail review apply-candidates` only after explicit confirmation.",
			"Wait for explicit confirmation identifying the candidate id or accepted batch before any state-changing command.",
		},
		Never: []string{
			"Do not generate state-changing commands for `needs_human_review`.",
			"Do not promote, merge, or discard `transcript_notes`, `migration_source`, or KDD split-source evidence directly from review actions.",
		},
		RootIntent:    "the user asks to review candidates, inspect pending Worktrail knowledge, or decide whether knowledge should be promoted, merged, discarded, restored, or retired",
		RootCommand:   "Run `worktrail review plan --format json`; after confirmation, use the plan `commands` array or `worktrail review apply-candidates --promote|--merge|--discard <id...> [--scope ...]`.",
		RootGuardrail: "Wait for explicit confirmation before any state-changing command, and keep evidence or operational drafts out of the default review lane.",
	},
	{
		Skill: "worktrail-maintain",
		UseWhen: []string{
			"the user asks to maintain, clean up, advance, summarize, or do low-intervention upkeep for Worktrail knowledge, pending evidence, review candidates, or evidence lifecycle actions",
		},
		RequiredActions: []string{
			"Run the read-only discovery chain first: `worktrail context \"maintenance\"`, then the exact scope-aware commands from `maintenance.next_steps`.",
			"Use `worktrail distill --pending --summary`, `worktrail review plan --format json`, and `worktrail evidence plan --format json` as indicated by maintenance hints.",
			"Use `worktrail note add ...` to capture a confirmed finding as a pending semantic candidate instead of editing formal `.worktrail` knowledge directly.",
			"Ask which lane to run and require explicit confirmation before distill apply, review apply-plan, review apply-candidates, promote, merge, discard, archive, restore, or retire.",
		},
		Never: []string{
			"Do not automatically commit git changes.",
			"Do not run state-changing maintenance commands without explicit user confirmation.",
		},
		RootIntent:    "the user asks to maintain, clean up, advance, or summarize Worktrail knowledge, pending evidence, review candidates, or evidence lifecycle work",
		RootCommand:   "Start with `worktrail context \"maintenance\"`, then follow the exact scoped next steps such as `worktrail distill --pending --summary`, `worktrail review plan --format json`, or `worktrail evidence plan --format json`.",
		RootGuardrail: "Use `worktrail note add ...` for confirmed findings, and require explicit confirmation before any state-changing maintenance action.",
	},
}

func SkillTriggers() []SkillTrigger {
	out := make([]SkillTrigger, len(skillTriggers))
	for i, trigger := range skillTriggers {
		out[i] = SkillTrigger{
			Skill:           trigger.Skill,
			UseWhen:         append([]string(nil), trigger.UseWhen...),
			RequiredActions: append([]string(nil), trigger.RequiredActions...),
			Never:           append([]string(nil), trigger.Never...),
			RootIntent:      trigger.RootIntent,
			RootCommand:     trigger.RootCommand,
			RootGuardrail:   trigger.RootGuardrail,
		}
	}
	return out
}

func RenderRootTemplate(body string) string {
	body = strings.ReplaceAll(body, RootSharedPlaceholder, RenderRootShared())
	return strings.ReplaceAll(body, SkillTriggerRoutingPlaceholder, RenderSkillTriggerRouting())
}

func RenderRootShared() string {
	return strings.Join([]string{
		"# Worktrail",
		"",
		"Use Worktrail at the start of substantial coding tasks, and at explicit handoff boundaries, only when the current project has opted in:",
		"",
		"- Treat the project as Worktrail-enabled only when `.worktrail/` exists at the current workspace or repository root.",
		"- If `.worktrail/` is absent, do not automatically run Worktrail context, preview, search, state, resume, handoff, import, review, maintain, distill, or note workflows.",
		"- This gate does not block explicit user requests to initialize, install, inspect, or repair Worktrail itself.",
		"",
		"- Run `/worktrail-context` or `worktrail context \"<task>\"` before starting substantial work.",
		"- Use `/worktrail-doc-preview` or `worktrail preview` when you need to browse Worktrail knowledge.",
		"- Use `/worktrail-search` or `worktrail search \"<keyword>\"` when you need to pinpoint Worktrail knowledge.",
		"- Keep `/worktrail-state` current for long or risky sessions.",
		"- Use `/worktrail-handoff` only when the user explicitly asks to hand off, continue later in another chat, or switch agents with durable recovery context.",
		"- Prefer `worktrail state close --to handoff \"<summary>\"` over bare `worktrail handoff` whenever an active explicit state exists.",
		"- Use `/worktrail-resume` or `worktrail resume \"<task>\"` at the start of a new session when you need to continue prior work from the latest state and handoff.",
		"- Use `/worktrail-import` only for explicit transcript files that should become pending candidates.",
		"- Use `worktrail note add ...` when the user asks to write, capture, or land a finding into Worktrail knowledge; this creates a pending candidate instead of editing formal `.worktrail` knowledge directly.",
		"- Use `/worktrail-review` or `worktrail review` to inspect candidates before any promote, merge, discard, restore, or retire action.",
		"",
		"Worktrail is the only project knowledge route. Use Worktrail context, note, review, and handoff flows for durable knowledge; do not read from or write to legacy KDD directories. Do not directly edit formal `.worktrail` knowledge files. Use `worktrail handoff` for durable handoff records under `.worktrail/handoffs/`.",
		"",
		"## Worktrail command picker",
		"",
		"Use this picker before reaching for shell commands or adjacent Worktrail subcommands. Match the user intent to the first row that fits, run the listed command, and avoid the substitutes.",
		"",
		"| User intent | Use this command | Do NOT use these substitutes |",
		"| --- | --- | --- |",
		"| Look up a Worktrail rule, lesson, decision, workflow, or note by keyword/term/phrase (\"find\", \"search\", \"look up\", \"where is X documented\") | `worktrail search \"<keyword>\"` (skill `/worktrail-search`) | `rg`, `grep`, `find`, `cat`, `worktrail context`, `worktrail preview` |",
		"| Continue prior Worktrail work in a new session (\"resume\", \"pick up where I left off\", \"continue previous session\", \"load my previous Worktrail context for this new chat\") | `worktrail resume [<task>]` (skill `/worktrail-resume`) | `worktrail context`, `worktrail state inject`, `worktrail state start`, `worktrail state list`, `worktrail state show` |",
		"| Browse the rendered Worktrail knowledge site for a doc, candidate, handoff, workflow, profile, rule, or lesson | `worktrail preview --scope <scope>` (skill `/worktrail-doc-preview`) | `worktrail search`, target project's dev server, ad hoc Markdown viewers |",
		"| Start substantial project work or load project memory for a new task | `worktrail context \"<task>\"` (skill `/worktrail-context`) | `worktrail resume`, `worktrail search`, `worktrail preview` |",
		"| Record current state, checkpoint, or update progress for the active session | `worktrail state start|update|checkpoint|inject` (skill `/worktrail-state`) | `worktrail resume`, `worktrail handoff` (until ending the session) |",
		"| Create a durable handoff because the user explicitly wants to hand off, switch agents, or continue later in a new chat | `worktrail state close --to handoff \"<summary>\"` (or `worktrail handoff \"<summary>\"` when no active explicit state exists) (skill `/worktrail-handoff`) | `worktrail state checkpoint` alone, copy-pasted text summaries, automatic handoff at normal task boundaries |",
		"",
		"If two rows seem to fit, pick the more specific intent (search/resume win over context/preview/state when keyword lookup or session recovery is the primary goal).",
		"",
		SkillTriggerRoutingPlaceholder,
		"",
		"Worktrail hooks may create state, checkpoints, runtime records, event logs, and local transcript metadata. Hooks must never promote, merge, discard, restore, retire, delete, or replace knowledge.",
	}, "\n")
}

func RenderSkillTriggerRouting() string {
	var b strings.Builder
	b.WriteString("## Skill Trigger Routing\n\n")
	b.WriteString("When a user request matches one of these intents, use the named Worktrail skill or run the equivalent command. Root governance only summarizes the routing surface; read the skill itself for the full workflow and edge cases.\n\n")
	b.WriteString("Project activation gate: automatic Worktrail workflows are active only when `.worktrail/` exists at the current workspace or repository root. If `.worktrail/` is absent, do not run Worktrail automatically; only proceed when the user explicitly asks to initialize, install, inspect, repair, or otherwise manage Worktrail itself.\n\n")
	for _, trigger := range skillTriggers {
		fmt.Fprintf(&b, "### %s\n\n", trigger.Skill)
		fmt.Fprintf(&b, "- Route when: %s\n", trigger.RootIntent)
		fmt.Fprintf(&b, "- First command: %s\n", trigger.RootCommand)
		fmt.Fprintf(&b, "- Hard constraint: %s\n\n", trigger.RootGuardrail)
	}
	return strings.TrimRight(b.String(), "\n")
}

func writeTriggerList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "%s:\n", title)
	for _, item := range items {
		fmt.Fprintf(b, "- %s\n", item)
	}
	b.WriteByte('\n')
}
