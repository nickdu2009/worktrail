package templates

import (
	"fmt"
	"strings"
)

const SkillTriggerRoutingPlaceholder = "{{WORKTRAIL_SKILL_TRIGGER_ROUTING}}"

type SkillTrigger struct {
	Skill           string
	UseWhen         []string
	RequiredActions []string
	Never           []string
}

var skillTriggers = []SkillTrigger{
	{
		Skill: "worktrail-context",
		UseWhen: []string{
			"starting substantial project work, resuming an old task, continuing previous work, loading project memory, or when the user asks to start work, continue a previous task, load context, or load project context",
		},
		RequiredActions: []string{
			"Run `/worktrail-context` or `worktrail context \"<task>\"` before substantial work.",
			"Read the Context Pack and follow active state, constraints, maintenance hints, and next steps.",
		},
		Never: []string{
			"Do not skip Worktrail context for substantial project work unless no project knowledge exists.",
		},
	},
	{
		Skill: "worktrail-doc-preview",
		UseWhen: []string{
			"the user asks to preview Worktrail docs, render Worktrail knowledge, open a Worktrail candidate, handoff, workflow, profile, rule, lesson, or says 预览 Worktrail 文档",
		},
		RequiredActions: []string{
			"Run `worktrail preview <path> --scope <scope> --open` or `worktrail preview --candidate <id> --scope <scope> --open`.",
			"Use browser verification when available and report the preview URL, source, and validation result.",
		},
		Never: []string{
			"Do not use the target project's dev server, docs site, package manager, or framework for Worktrail document preview.",
			"Do not preview non-Worktrail content or modify target project business files.",
		},
	},
	{
		Skill: "worktrail-init",
		UseWhen: []string{
			"the user asks to initialize Worktrail, set up Worktrail, install Worktrail for Cursor, Codex, Claude, or all agents, configure Worktrail hooks, MCP, rules, or skills",
		},
		RequiredActions: []string{
			"Run `worktrail init` for Worktrail user and project initialization.",
			"Run `worktrail install cursor|codex|claude|all --user|--project` as requested and verify with `worktrail doctor <tool> --user|--project`.",
			"Report the Worktrail-managed files affected, including `.worktrail`, `.gitignore`, agent hooks, MCP config, rules, and skills as applicable.",
		},
		Never: []string{
			"Do not initialize the target application's language, framework, package manager, or git repository.",
			"Do not require the target project to have any specific technology stack or overwrite non-Worktrail configuration.",
		},
	},
	{
		Skill: "worktrail-state",
		UseWhen: []string{
			"the task is long, risky, multi-step, likely to compact, needs a checkpoint, or the user asks to record current state, update state, create a checkpoint, inject state, or save progress",
		},
		RequiredActions: []string{
			"Use `worktrail state start`, `worktrail state update --session latest`, `worktrail state checkpoint --reason <reason>`, or `worktrail state inject \"<task>\"` as appropriate.",
			"Keep state factual: goal, constraints, evidence, decisions, work done, validation, open questions, and next step.",
		},
		Never: []string{
			"Do not store secrets, raw credentials, or private runtime payloads in state.",
		},
	},
	{
		Skill: "worktrail-handoff",
		UseWhen: []string{
			"ending a session, compacting context, switching tools or agents, opening a new chat or new conversation, handing off work, requests to end current chat, or when the user says handoff, compact, switch chat, or switch agent",
		},
		RequiredActions: []string{
			"Run `/worktrail-handoff` or `worktrail handoff \"<summary>\"` and create a Worktrail handoff candidate.",
			"Summarize active state, current diff intent, validation, risks, open questions, and the next step.",
		},
		Never: []string{
			"Do not only output a copyable text handoff when the user asked for a Worktrail handoff.",
			"Do not promote, merge, discard, restore, or retire without explicit user confirmation.",
		},
	},
	{
		Skill: "worktrail-import",
		UseWhen: []string{
			"the user wants to import, sync, extract, migrate, or reuse knowledge from Codex, Claude, Cursor, transcript files, all current-project conversations, observed Cursor conversations, or legacy KDD docs",
		},
		RequiredActions: []string{
			"Run the relevant dry-run first: `worktrail import codex`, `worktrail import cursor`, `worktrail sync <source> <file>`, or `worktrail migrate kdd`.",
			"Create pending candidates only after the user asked to proceed, then hand off review to `/worktrail-review`.",
		},
		Never: []string{
			"Do not promote imported transcript notes or migration sources directly.",
			"Do not scan undocumented private Cursor directories.",
		},
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
	},
	{
		Skill: "worktrail-maintain",
		UseWhen: []string{
			"the user asks to maintain, clean up, advance, summarize, or do low-intervention upkeep for Worktrail knowledge, pending evidence, review candidates, or evidence lifecycle actions",
		},
		RequiredActions: []string{
			"Run the read-only discovery chain first: `worktrail context \"maintenance\"`, then the exact scope-aware commands from `maintenance.next_steps`.",
			"Use `worktrail distill --pending --summary`, `worktrail review plan --format json`, and `worktrail evidence plan --format json` as indicated by maintenance hints.",
			"Ask which lane to run and require explicit confirmation before distill apply, review apply-plan, review apply-candidates, promote, merge, discard, archive, restore, or retire.",
		},
		Never: []string{
			"Do not automatically commit git changes.",
			"Do not run state-changing maintenance commands without explicit user confirmation.",
		},
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
		}
	}
	return out
}

func RenderRootTemplate(body string) string {
	return strings.ReplaceAll(body, SkillTriggerRoutingPlaceholder, RenderSkillTriggerRouting())
}

func RenderSkillTriggerRouting() string {
	var b strings.Builder
	b.WriteString("## Skill Trigger Routing\n\n")
	b.WriteString("When a user request matches one of these intents, use the named Worktrail skill or run the equivalent command. Do not answer with prose only when a required Worktrail command is listed.\n\n")
	for _, trigger := range skillTriggers {
		fmt.Fprintf(&b, "### %s\n\n", trigger.Skill)
		writeTriggerList(&b, "Use when", trigger.UseWhen)
		writeTriggerList(&b, "Required actions", trigger.RequiredActions)
		writeTriggerList(&b, "Never", trigger.Never)
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
