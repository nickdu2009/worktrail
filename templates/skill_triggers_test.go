package templates

import (
	"strings"
	"testing"
)

func TestSkillTriggerContractCoversWorktrailSkills(t *testing.T) {
	expected := map[string][]string{
		"worktrail-context":     {"worktrail context", "continue a previous task"},
		"worktrail-doc-preview": {"worktrail preview", "preview entry path", "--no-open", "Do not use the target project's dev server"},
		"worktrail-init":        {"worktrail init", "worktrail install cursor|codex|claude|all", "worktrail doctor <tool>", "worktrail` is available in `PATH", "Do not initialize the target application's language"},
		"worktrail-state":       {"worktrail state", "save progress"},
		"worktrail-handoff":     {"worktrail handoff", "new conversation", "end current chat", "Do not only output a copyable text handoff"},
		"worktrail-import":      {"worktrail import", "--since 14d", "--limit 20", "worktrail sync", "worktrail migrate kdd"},
		"worktrail-distill":     {"worktrail distill --pending --summary", "worktrail.distill.proposal.v1"},
		"worktrail-review":      {"worktrail review plan --format json", "worktrail review apply-candidates", "promoted, merged, discarded, restored, or retired", "Do not promote, merge, or discard `transcript_notes`"},
		"worktrail-maintain":    {"worktrail context \"maintenance\"", "worktrail evidence plan --format json", "worktrail note add", "review apply-candidates"},
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
	body := "before\n" + SkillTriggerRoutingPlaceholder + "\nafter"
	rendered := RenderRootTemplate(body)
	if strings.Contains(rendered, SkillTriggerRoutingPlaceholder) {
		t.Fatalf("placeholder was not replaced:\n%s", rendered)
	}
	for _, want := range []string{"## Skill Trigger Routing", "Project activation gate", "worktrail-handoff", "worktrail handoff", "new conversation", "end current chat"} {
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
		if !strings.Contains(body, SkillTriggerRoutingPlaceholder) {
			t.Fatalf("%s missing trigger routing placeholder", path)
		}
		for _, want := range []string{"project has opted in", "`.worktrail/` exists", "does not block explicit user requests"} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s missing project activation gate %q:\n%s", path, want, body)
			}
		}
	}

	nonRootPaths := []string{
		"config/codex-hooks.json",
		"config/claude-settings.json",
		"config/cursor-hooks.json",
		"config/cursor-mcp.json",
		"skills/worktrail-context/SKILL.md",
		"skills/worktrail-doc-preview/SKILL.md",
		"skills/worktrail-init/SKILL.md",
		"skills/worktrail-state/SKILL.md",
		"skills/worktrail-handoff/SKILL.md",
		"skills/worktrail-import/SKILL.md",
		"skills/worktrail-distill/SKILL.md",
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
		"worktrail-context":     {"description:", "Use this skill when", "starting", "resuming", "continuing", ".worktrail/"},
		"worktrail-doc-preview": {"description:", "Use this skill when", "preview", "overall", "candidate", ".worktrail/"},
		"worktrail-init":        {"description:", "Use this skill when", "initialize Worktrail", "install", "doctor", "worktrail --help", ".worktrail/"},
		"worktrail-state":       {"description:", "Use this skill when", "long", "risky", "checkpoint", ".worktrail/"},
		"worktrail-handoff":     {"description:", "Use this skill", "new conversation", "end current chat", "worktrail handoff", "do not only output", ".worktrail/"},
		"worktrail-import":      {"description:", "Use this skill when", "import", "sync", "migrate", ".worktrail/"},
		"worktrail-distill":     {"description:", "Use this skill when", "transcript_notes", "migration_source", "semantic Worktrail candidates", ".worktrail/"},
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
