package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

func testEnv(t *testing.T) paths.Env {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func TestInstallCodexProjectManagesHooksAndGitignoreOnly(t *testing.T) {
	env := testEnv(t)
	agents := filepath.Join(env.ProjectRoot, "AGENTS.md")
	if err := os.WriteFile(agents, []byte("# Local rules\n\nKeep this.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	hooks := filepath.Join(env.ProjectRoot, ".codex", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooks), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooks, []byte(`{"existing": {"enabled": true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := InstallCodex(env, Options{Project: true})
	if err != nil {
		t.Fatalf("InstallCodex: %v", err)
	}
	if len(report.Actions) == 0 {
		t.Fatal("expected install actions")
	}
	data, err := os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if text != "# Local rules\n\nKeep this.\n" {
		t.Fatalf("project AGENTS.md should not be modified, got:\n%s", text)
	}
	skill := filepath.Join(env.ProjectRoot, ".agents", "skills", "worktrail-state", "SKILL.md")
	if _, err := os.Stat(skill); !os.IsNotExist(err) {
		t.Fatalf("project skills should not be installed by default project integration, err=%v", err)
	}
	cfg := map[string]any{}
	raw, err := os.ReadFile(hooks)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["existing"]; !ok {
		t.Fatalf("existing JSON key was not preserved: %s", raw)
	}
	if _, ok := cfg["worktrail"]; !ok {
		t.Fatalf("worktrail JSON key missing: %s", raw)
	}
	gitignore := filepath.Join(env.ProjectRoot, ".gitignore")
	raw, err = os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), util.HashManagedBegin) || !strings.Contains(string(raw), ".codex/") {
		t.Fatalf("project .gitignore missing worktrail managed entries: %s", raw)
	}

	doctor, err := Doctor(env, ToolCodex, Options{Project: true})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, check := range doctor.Checks {
		if !check.OK {
			t.Fatalf("doctor check failed: %+v", check)
		}
	}

	if _, err := UninstallCodex(env, Options{Project: true}); err != nil {
		t.Fatalf("UninstallCodex: %v", err)
	}
	data, err = os.ReadFile(agents)
	if err != nil {
		t.Fatal(err)
	}
	text = string(data)
	if !strings.Contains(text, "Keep this.") {
		t.Fatalf("uninstall removed user content: %s", text)
	}
	raw, err = os.ReadFile(hooks)
	if err != nil {
		t.Fatal(err)
	}
	cfg = map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if _, ok := cfg["worktrail"]; ok {
		t.Fatalf("worktrail JSON key still present: %s", raw)
	}
	if _, ok := cfg["existing"]; !ok {
		t.Fatalf("existing JSON key was removed: %s", raw)
	}
	raw, err = os.ReadFile(gitignore)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), util.HashManagedBegin) {
		t.Fatalf("project .gitignore should remain after codex uninstall: %s", raw)
	}
}

func TestInstallCodexDefaultIsUserOnly(t *testing.T) {
	env := testEnv(t)
	if _, err := InstallCodex(env, Options{}); err != nil {
		t.Fatalf("InstallCodex: %v", err)
	}
	for _, path := range []string{
		filepath.Join(env.Home, ".codex", "AGENTS.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.Contains(string(data), util.ManagedBegin) {
			t.Fatalf("expected managed block in %s", path)
		}
	}
	for _, path := range []string{
		filepath.Join(env.Home, ".codex", "skills", "worktrail-context", "SKILL.md"),
		filepath.Join(env.Home, ".codex", "skills", "worktrail-handoff", "SKILL.md"),
		filepath.Join(env.Home, ".codex", "skills", "worktrail-import", "SKILL.md"),
		filepath.Join(env.Home, ".codex", "skills", "worktrail-distill", "SKILL.md"),
		filepath.Join(env.Home, ".codex", "skills", "worktrail-review", "SKILL.md"),
		filepath.Join(env.Home, ".codex", "skills", "worktrail-maintain", "SKILL.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.HasPrefix(string(data), "---\n") {
			t.Fatalf("skill frontmatter must be first in %s:\n%s", path, data)
		}
		if !strings.Contains(string(data), util.ManagedBegin) {
			t.Fatalf("expected managed block in %s", path)
		}
	}
	distillSkill, err := os.ReadFile(filepath.Join(env.Home, ".codex", "skills", "worktrail-distill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"worktrail distill --pending --summary",
		"worktrail distill --pending --all --write-pack",
		"worktrail distill validate <proposal.json>",
		"worktrail distill apply <proposal.json>",
		"worktrail review plan --format json",
		"Do not paste transcript evidence bodies",
		"Do not commit the pack or proposal",
	} {
		if !strings.Contains(string(distillSkill), want) {
			t.Fatalf("distill skill missing %q:\n%s", want, distillSkill)
		}
	}
	reviewSkill, err := os.ReadFile(filepath.Join(env.Home, ".codex", "skills", "worktrail-review", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	maintainSkill, err := os.ReadFile(filepath.Join(env.Home, ".codex", "skills", "worktrail-maintain", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"worktrail context \"maintenance\"",
		"worktrail distill --pending --summary",
		"worktrail review plan --format json",
		"worktrail evidence plan --format json",
		"Do not automatically commit git changes",
		"explicit user confirmation",
	} {
		if !strings.Contains(string(maintainSkill), want) {
			t.Fatalf("maintain skill missing %q:\n%s", want, maintainSkill)
		}
	}
	for _, want := range []string{
		"worktrail candidates list --semantic --status pending --format json",
		"Do not include `transcript_notes` evidence or non-semantic operational candidates",
		"show counts for each group",
		"commands` array only for `promote`, `merge`, and `discard`",
		"Do not generate a state-changing command for `needs_human_review`",
		"accepted batch",
		"worktrail review --evidence",
		"worktrail review --all",
		"worktrail retire <id> --reason <text>",
	} {
		if !strings.Contains(string(reviewSkill), want) {
			t.Fatalf("review skill missing %q:\n%s", want, reviewSkill)
		}
	}
	for _, path := range []string{
		filepath.Join(env.ProjectRoot, "AGENTS.md"),
		filepath.Join(env.ProjectRoot, ".agents", "skills", "worktrail-state", "SKILL.md"),
		filepath.Join(env.ProjectRoot, ".gitignore"),
		filepath.Join(env.ProjectRoot, ".codex", "hooks.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("default codex install should not create project file %s, err=%v", path, err)
		}
	}
}

func TestInstallClaudeUserAndProject(t *testing.T) {
	env := testEnv(t)
	if _, err := InstallClaude(env, Options{User: true, Project: true}); err != nil {
		t.Fatalf("InstallClaude: %v", err)
	}
	for _, path := range []string{
		filepath.Join(env.Home, ".claude", "CLAUDE.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.Contains(string(data), util.ManagedBegin) {
			t.Fatalf("expected managed block in %s", path)
		}
	}
	for _, path := range []string{
		filepath.Join(env.Home, ".claude", "skills", "worktrail-import", "SKILL.md"),
		filepath.Join(env.Home, ".claude", "skills", "worktrail-distill", "SKILL.md"),
		filepath.Join(env.Home, ".claude", "skills", "worktrail-review", "SKILL.md"),
		filepath.Join(env.Home, ".claude", "skills", "worktrail-maintain", "SKILL.md"),
		filepath.Join(env.ProjectRoot, ".claude", "skills", "worktrail-state", "SKILL.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.HasPrefix(string(data), "---\n") {
			t.Fatalf("skill frontmatter must be first in %s:\n%s", path, data)
		}
		if !strings.Contains(string(data), util.ManagedBegin) {
			t.Fatalf("expected managed block in %s", path)
		}
	}
	for _, path := range []string{
		filepath.Join(env.ProjectRoot, "CLAUDE.md"),
		filepath.Join(env.ProjectRoot, ".gitignore"),
		filepath.Join(env.ProjectRoot, ".claude", "settings.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if strings.HasSuffix(path, ".md") && !strings.Contains(string(data), util.ManagedBegin) {
			t.Fatalf("expected managed block in %s", path)
		}
	}
}
