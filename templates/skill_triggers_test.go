package templates

import (
	"strings"
	"testing"
)

func TestSkillTriggerContractCoversWorktrailSkills(t *testing.T) {
	expected := map[string][]string{
		"worktrail-context":     {"worktrail context --semantic=auto", "load context", "worktrail resume"},
		"worktrail-doc-preview": {"worktrail preview", "--no-open", "keyword lookup", "worktrail search"},
		"worktrail-search":      {"worktrail search --semantic=auto", "keyword", "`rg`, `grep`, `find`, `worktrail context`, or `worktrail preview`"},
		"worktrail-init":        {"worktrail init", "worktrail install cursor|codex|claude|zcode|all", "worktrail doctor <tool>", "`worktrail` CLI is available in `PATH`"},
		"worktrail-state":       {"worktrail state", "checkpoint", "worktrail resume"},
		"worktrail-resume":      {"worktrail resume", "latest state", "durable handoff", "worktrail state inject", "worktrail state list", "worktrail state show"},
		"worktrail-handoff":     {"worktrail state close --to handoff", "continue later in another chat", "switch agents", "do not only output a copyable text handoff"},
		"worktrail-import":      {"worktrail import codex --since 14d", "worktrail import cursor --limit 20", "worktrail sync", "worktrail migrate kdd"},
		"worktrail-distill":     {"worktrail distill --pending --summary", "validate/apply workflow", "Do not promote, merge, discard, archive, restore, or retire"},
		"worktrail-draft":       {"worktrail draft create", "single-quoted heredoc", "frontmatter-bearing", "do not create `docs/`, `.plans/`"},
		"worktrail-adr":         {"worktrail adr create", "pending decision candidate", "do not require agent-skills", "`.worktrail/decisions/`"},
		"worktrail-review":      {"worktrail review plan --format json", "worktrail review apply-candidates", "promoted, merged, discarded, restored, or retired", "evidence or operational drafts"},
		"worktrail-maintain":    {"worktrail context --semantic=auto \"maintenance\"", "worktrail evidence plan --format json", "worktrail note add", "worktrail doctor recovery", "state-changing maintenance action"},
	}
	seen := map[string]bool{}
	rendered := RenderSkillTriggerRouting()
	for _, want := range []string{"Project activation gate", "`.worktrail/` exists", "do not run Worktrail automatically"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered trigger routing missing project gate %q:\n%s", want, rendered)
		}
	}
	for _, trigger := range SkillTriggers() {
		seen[trigger.Skill] = true
		if len(trigger.UseWhen) == 0 || len(trigger.RequiredActions) == 0 || len(trigger.Never) == 0 {
			t.Fatalf("%s must define use_when, required_actions, and never entries: %+v", trigger.Skill, trigger)
		}
		if strings.TrimSpace(trigger.RootIntent) == "" || strings.TrimSpace(trigger.RootCommand) == "" || strings.TrimSpace(trigger.RootGuardrail) == "" {
			t.Fatalf("%s must define root routing fields: %+v", trigger.Skill, trigger)
		}
		for _, want := range expected[trigger.Skill] {
			if !strings.Contains(rendered, want) {
				t.Fatalf("rendered trigger routing missing %q for %s:\n%s", want, trigger.Skill, rendered)
			}
		}
	}
	for skill := range expected {
		if !seen[skill] {
			t.Fatalf("trigger contract missing %s", skill)
		}
	}
	if strings.Contains(rendered, "worktrail-evidence") {
		t.Fatalf("evidence lifecycle should belong to worktrail-maintain, not a separate skill:\n%s", rendered)
	}
}

func TestRenderRootTemplateReplacesRoutingPlaceholder(t *testing.T) {
	body := "before\n" + RootSharedPlaceholder + "\nafter"
	rendered := RenderRootTemplate(body)
	for _, placeholder := range []string{RootSharedPlaceholder, SkillTriggerRoutingPlaceholder} {
		if strings.Contains(rendered, placeholder) {
			t.Fatalf("placeholder %q was not replaced:\n%s", placeholder, rendered)
		}
	}
	for _, want := range []string{"# Worktrail", "## Skill Trigger Routing", "Project activation gate", "worktrail-handoff", "worktrail state close --to handoff", "continue later in another chat", "switch agents", "installed `worktrail-context` skill", "skill `worktrail-search`", "artifact only as Worktrail knowledge", "do not create `docs/`, `.plans/`"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered root template missing %q:\n%s", want, rendered)
		}
	}
}

func TestPlaceholderIsOnlyUsedByRootRuleTemplates(t *testing.T) {
	rootPaths := []string{
		"root/AGENTS.md",
		"root/CLAUDE.md",
		"root/cursor-worktrail.mdc",
	}
	for _, path := range rootPaths {
		body, err := Read(path)
		if err != nil {
			t.Fatalf("Read(%s): %v", path, err)
		}
		if !strings.Contains(body, RootSharedPlaceholder) {
			t.Fatalf("%s missing root shared placeholder", path)
		}
		if strings.Contains(body, SkillTriggerRoutingPlaceholder) {
			t.Fatalf("%s should not embed trigger routing directly anymore:\n%s", path, body)
		}
	}

	nonRootPaths := []string{
		"config/codex-hooks.json",
		"config/claude-settings.json",
		"config/cursor-hooks.json",
		"skills/worktrail-context/SKILL.md",
		"skills/worktrail-doc-preview/SKILL.md",
		"skills/worktrail-search/SKILL.md",
		"skills/worktrail-init/SKILL.md",
		"skills/worktrail-state/SKILL.md",
		"skills/worktrail-resume/SKILL.md",
		"skills/worktrail-handoff/SKILL.md",
		"skills/worktrail-import/SKILL.md",
		"skills/worktrail-distill/SKILL.md",
		"skills/worktrail-draft/SKILL.md",
		"skills/worktrail-adr/SKILL.md",
		"skills/worktrail-review/SKILL.md",
		"skills/worktrail-maintain/SKILL.md",
	}
	for _, path := range nonRootPaths {
		body, err := Read(path)
		if err != nil {
			t.Fatalf("Read(%s): %v", path, err)
		}
		if strings.Contains(body, SkillTriggerRoutingPlaceholder) {
			t.Fatalf("%s must not depend on trigger routing placeholder", path)
		}
	}
}

func TestSkillTemplatesExposeTriggerIntent(t *testing.T) {
	expected := map[string][]string{
		"worktrail-context":     {"description:", "Use this skill when", "starting", "continuing", "load context", "--semantic=auto", ".worktrail/"},
		"worktrail-doc-preview": {"description:", "Use this skill when", "preview", "overall", "candidate", ".worktrail/"},
		"worktrail-search":      {"description:", "Use this skill when", "search", "keyword", "--semantic=auto", "do not substitute", ".worktrail/"},
		"worktrail-init":        {"description:", "Use this skill when", "initialize Worktrail", "install", "doctor", "worktrail --help", ".worktrail/", ".zcode/skills"},
		"worktrail-state":       {"description:", "Use this skill when", "long", "risky", "checkpoint", "worktrail-resume", ".worktrail/"},
		"worktrail-resume":      {"description:", "Use this skill when", "resume", "latest state", "state inject", ".worktrail/"},
		"worktrail-handoff":     {"description:", "Use this skill", "continue later in another chat", "switch agents", "worktrail state close --to handoff", "do not only output", ".worktrail/"},
		"worktrail-import":      {"description:", "Use this skill when", "import", "sync", "migrate", ".worktrail/"},
		"worktrail-distill":     {"description:", "Use this skill when", "transcript_notes", "migration_source", "semantic Worktrail candidates", ".worktrail/"},
		"worktrail-draft":       {"description:", "Use this skill", "non-ADR semantic artifacts", "standalone-file policy", "single-quoted heredoc", "frontmatter", ".worktrail/"},
		"worktrail-adr":         {"description:", "Use this skill", "persist", "pending", "review", "omitted/empty lifecycle as current", ".worktrail/"},
		"worktrail-review":      {"description:", "Use this skill when", "review candidates", "promoted", "retired", ".worktrail/"},
		"worktrail-maintain":    {"description:", "Use this skill when", "maintain", "clean up", "evidence lifecycle", ".worktrail/"},
	}
	for skill, wants := range expected {
		body, err := Read("skills/" + skill + "/SKILL.md")
		if err != nil {
			t.Fatalf("Read skill %s: %v", skill, err)
		}
		if !strings.HasPrefix(body, "---\n") {
			t.Fatalf("%s frontmatter must be first:\n%s", skill, body)
		}
		for _, want := range wants {
			if !strings.Contains(strings.ToLower(body), strings.ToLower(want)) {
				t.Fatalf("%s skill template missing %q:\n%s", skill, want, body)
			}
		}
	}
}

func TestHandoffV2TriggerRoutingSemantics(t *testing.T) {
	var handoffTrigger, resumeTrigger SkillTrigger
	for _, trigger := range SkillTriggers() {
		switch trigger.Skill {
		case "worktrail-handoff":
			handoffTrigger = trigger
		case "worktrail-resume":
			resumeTrigger = trigger
		}
	}
	if handoffTrigger.Skill == "" || resumeTrigger.Skill == "" {
		t.Fatalf("missing handoff or resume trigger: handoff=%+v resume=%+v", handoffTrigger, resumeTrigger)
	}

	handoffContract := strings.Join(append(append(
		[]string{handoffTrigger.RootIntent, handoffTrigger.RootCommand, handoffTrigger.RootGuardrail},
		handoffTrigger.RequiredActions...),
		handoffTrigger.Never...), "\n")
	for _, want := range []string{
		"explicitly",
		"worktrail state close --to handoff",
		"--next-step",
		"If no active explicit state exists",
		"ordinary task progress",
		"Do not only output a copyable text handoff",
	} {
		if !strings.Contains(handoffContract, want) {
			t.Fatalf("handoff trigger contract missing %q:\n%s", want, handoffContract)
		}
	}

	resumeContract := strings.Join(append(append(
		[]string{resumeTrigger.RootIntent, resumeTrigger.RootCommand, resumeTrigger.RootGuardrail},
		resumeTrigger.RequiredActions...),
		resumeTrigger.Never...), "\n")
	for _, want := range []string{
		"new session",
		"worktrail resume",
		"worktrail context",
		"worktrail state inject",
	} {
		if !strings.Contains(resumeContract, want) {
			t.Fatalf("resume trigger contract missing %q:\n%s", want, resumeContract)
		}
	}

	rendered := RenderRootShared()
	for _, want := range []string{
		"Prefer `worktrail state close --to handoff",
		"--next-step",
		"Continue prior Worktrail work in a new session",
		"Create a durable handoff because the user explicitly wants to hand off",
		"`worktrail state checkpoint` alone",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("rendered root routing missing %q:\n%s", want, rendered)
		}
	}
}

func TestHandoffTemplatesRequireNextStepOrComplete(t *testing.T) {
	rendered := RenderRootShared()
	stateSkill, err := Read("skills/worktrail-state/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	handoffSkill, err := Read("skills/worktrail-handoff/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	combined := strings.Join([]string{rendered, stateSkill, handoffSkill}, "\n")
	for _, obsolete := range []string{
		"worktrail state close --to handoff \"<summary>\"",
		"worktrail handoff \"<summary>\"",
		"bare `worktrail handoff",
	} {
		if strings.Contains(combined, obsolete) {
			t.Fatalf("handoff templates retained obsolete incomplete entry %q:\n%s", obsolete, combined)
		}
	}
	for _, want := range []string{
		"worktrail state close --to handoff --next-step",
		"worktrail handoff create --next-step",
		"--complete",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("handoff templates missing %q:\n%s", want, combined)
		}
	}
}

func TestRecoveryQuarantineTemplatesUseApplyConfirm(t *testing.T) {
	handoffSkill, err := Read("skills/worktrail-handoff/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	maintainSkill, err := Read("skills/worktrail-maintain/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	combined := handoffSkill + "\n" + maintainSkill
	for _, want := range []string{
		"worktrail doctor recovery",
		"worktrail doctor recovery --apply --confirm",
		"malformed state",
		"malformed local handoff",
	} {
		if !strings.Contains(combined, want) {
			t.Fatalf("recovery templates missing %q:\n%s", want, combined)
		}
	}
	if strings.Contains(combined, "worktrail doctor recovery --repair") {
		t.Fatalf("recovery templates contain obsolete repair command:\n%s", combined)
	}
}

func TestSkillTemplateDescriptionsUseCanonicalTwoSentenceStyle(t *testing.T) {
	skills := []string{
		"worktrail-context",
		"worktrail-doc-preview",
		"worktrail-search",
		"worktrail-init",
		"worktrail-state",
		"worktrail-resume",
		"worktrail-handoff",
		"worktrail-import",
		"worktrail-distill",
		"worktrail-draft",
		"worktrail-adr",
		"worktrail-review",
		"worktrail-maintain",
	}
	for _, skill := range skills {
		body, err := Read("skills/" + skill + "/SKILL.md")
		if err != nil {
			t.Fatalf("Read skill %s: %v", skill, err)
		}
		desc := frontmatterDescription(body)
		if desc == "" {
			t.Fatalf("%s missing frontmatter description:\n%s", skill, body)
		}
		parts := strings.Split(desc, ". Use when ")
		if len(parts) != 2 {
			t.Fatalf("%s description must follow '<capability>. Use when <trigger>.': %q", skill, desc)
		}
		if strings.TrimSpace(parts[0]) == "" {
			t.Fatalf("%s description capability sentence is empty: %q", skill, desc)
		}
		if !strings.HasSuffix(desc, ".") {
			t.Fatalf("%s description must end with a period: %q", skill, desc)
		}
		trigger := strings.TrimSpace(strings.TrimSuffix(parts[1], "."))
		if trigger == "" {
			t.Fatalf("%s description trigger sentence is empty: %q", skill, desc)
		}
	}
}

func frontmatterDescription(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return ""
}
