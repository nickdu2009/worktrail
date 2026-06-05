package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
	wtmpl "github.com/nickdu2009/worktrail/templates"
)

func testEnv(t *testing.T) paths.Env {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	installFakeWorktrailCommand(t)
	return paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func installFakeWorktrailCommand(t *testing.T) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "worktrail")
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

var allWorktrailSkills = []string{
	"worktrail-context",
	"worktrail-doc-preview",
	"worktrail-search",
	"worktrail-init",
	"worktrail-state",
	"worktrail-resume",
	"worktrail-handoff",
	"worktrail-import",
	"worktrail-distill",
	"worktrail-review",
	"worktrail-maintain",
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
	assertNoInstalledSkills(t, filepath.Join(env.ProjectRoot, ".agents", "skills"))
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
	report, err := InstallCodex(env, Options{})
	if err != nil {
		t.Fatalf("InstallCodex: %v", err)
	}
	assertDoctorCheckNamed(t, report, "worktrail command available")
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
		assertRenderedTriggerRouting(t, path)
	}
	assertInstalledSkills(t, filepath.Join(env.Home, ".codex", "skills"))
	if _, err := os.Stat(filepath.Join(env.UserRoot, "logs", "events.jsonl")); err != nil {
		t.Fatalf("expected user install event log: %v", err)
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
		"rerun the exact suggested `--scope` command",
		"Candidate summaries should explain reusable value",
		"`target_path` values must be stable and type-appropriate",
		"Do not automatically commit git changes",
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
		"Run the read-only discovery chain without asking for confirmation first",
		"worktrail context \"maintenance\"",
		"worktrail distill --pending --summary",
		"worktrail review plan --format json",
		"review apply-candidates batch command",
		"worktrail review apply-candidates --promote|--merge|--discard <id...> [--scope ...]",
		"worktrail evidence plan --format json",
		"maintenance.next_steps",
		"Treat those generated commands as authoritative",
		"rerun the exact suggested `--scope` command",
		"If the context suggests `--scope user`, keep that scope.",
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
		"plan `commands` array only for `promote`, `merge`, and `discard`",
		"worktrail review apply-candidates --promote|--merge|--discard <id...> [--scope ...]",
		"Preserve any `--scope` flags from the plan commands",
		"Do not generate a state-changing command for `needs_human_review`",
		"action, the exact candidate id list, and the scope",
		"Do not automatically commit git changes",
		"worktrail review --evidence",
		"worktrail review --all",
		"worktrail retire <id> --reason <text>",
		"Never promote, merge, or discard `transcript_notes` from review actions",
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
		env.ProjectWT,
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("default codex install should not create project file %s, err=%v", path, err)
		}
	}
}

func TestUninstallCodexUserOnlyDoesNotCreateProjectWorktrail(t *testing.T) {
	env := testEnv(t)
	if _, err := UninstallCodex(env, Options{User: true}); err != nil {
		t.Fatalf("UninstallCodex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.UserRoot, "logs", "events.jsonl")); err != nil {
		t.Fatalf("expected user uninstall event log: %v", err)
	}
	assertNoPath(t, env.ProjectWT)
}

func TestDoctorReportsMissingWorktrailCommand(t *testing.T) {
	env := testEnv(t)
	emptyBin := filepath.Join(t.TempDir(), "empty-bin")
	if err := os.MkdirAll(emptyBin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", emptyBin)
	report, err := Doctor(env, ToolCodex, Options{User: true})
	if err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	for _, check := range report.Checks {
		if check.Name == "worktrail command available" {
			if check.OK || !strings.Contains(check.Note, "not found in PATH") {
				t.Fatalf("expected missing worktrail command check, got %+v", check)
			}
			return
		}
	}
	t.Fatalf("missing worktrail command check: %+v", report.Checks)
}

func TestInstallClaudeUserInstallsAllSkillsAndProjectRuntimeOnly(t *testing.T) {
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
		assertRenderedTriggerRouting(t, path)
	}
	assertInstalledSkills(t, filepath.Join(env.Home, ".claude", "skills"))
	for _, path := range []string{
		filepath.Join(env.Home, ".claude", "skills", "worktrail-review", "SKILL.md"),
		filepath.Join(env.Home, ".claude", "skills", "worktrail-maintain", "SKILL.md"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
		if !strings.Contains(string(data), "worktrail review apply-candidates") {
			t.Fatalf("claude skill missing apply-candidates guidance in %s:\n%s", path, data)
		}
	}
	for _, path := range []string{
		filepath.Join(env.ProjectRoot, ".gitignore"),
		filepath.Join(env.ProjectRoot, ".claude", "settings.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	assertNoPath(t, filepath.Join(env.ProjectRoot, "CLAUDE.md"))
	assertNoInstalledSkills(t, filepath.Join(env.ProjectRoot, ".claude", "skills"))
}

func TestInstallCursorProjectManagesNativeConfigWithoutIgnoringCursor(t *testing.T) {
	env := testEnv(t)
	hooksPath := filepath.Join(env.ProjectRoot, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(hooksPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(hooksPath, []byte(`{"version":1,"hooks":{"existing":[{"command":"existing"}]}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	report, err := Install(env, ToolCursor, Options{Project: true})
	if err != nil {
		t.Fatalf("Install cursor: %v", err)
	}
	if len(report.Actions) == 0 {
		t.Fatal("expected install actions")
	}
	for _, path := range []string{
		filepath.Join(env.ProjectRoot, ".cursor", "hooks.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	assertNoPath(t, filepath.Join(env.ProjectRoot, ".cursor", "rules", "worktrail.mdc"))
	assertNoInstalledSkills(t, filepath.Join(env.ProjectRoot, ".cursor", "skills"))
	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	hooks := cfg["hooks"].(map[string]any)
	if _, ok := hooks["existing"]; !ok {
		t.Fatalf("existing hook configuration was not preserved: %s", raw)
	}
	if _, ok := hooks["stop"]; !ok {
		t.Fatalf("worktrail cursor hooks missing: %s", raw)
	}
	gitignore, err := os.ReadFile(filepath.Join(env.ProjectRoot, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(gitignore), ".cursor/") {
		t.Fatalf("project .gitignore must not ignore all .cursor: %s", gitignore)
	}

	doctor, err := Doctor(env, ToolCursor, Options{Project: true})
	if err != nil {
		t.Fatalf("Doctor cursor: %v", err)
	}
	for _, check := range doctor.Checks {
		if !check.OK {
			t.Fatalf("doctor check failed: %+v", check)
		}
		if strings.Contains(check.Name, "project rule") || strings.Contains(check.Name, "project skill") {
			t.Fatalf("project doctor should not check rule or skills: %+v", check)
		}
	}
}

func TestInstallCursorUserDoctorChecksRuleAndAllSkills(t *testing.T) {
	env := testEnv(t)
	if _, err := Install(env, ToolCursor, Options{User: true}); err != nil {
		t.Fatalf("Install cursor: %v", err)
	}
	for _, path := range []string{
		filepath.Join(env.Home, ".cursor", "rules", "worktrail.mdc"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected cursor user file %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(env.Home, ".cursor", "hooks.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("cursor user install should not create runtime config %s, err=%v", path, err)
		}
	}
	assertRenderedTriggerRouting(t, filepath.Join(env.Home, ".cursor", "rules", "worktrail.mdc"))
	assertInstalledSkills(t, filepath.Join(env.Home, ".cursor", "skills"))

	doctor, err := Doctor(env, ToolCursor, Options{User: true})
	if err != nil {
		t.Fatalf("Doctor cursor: %v", err)
	}
	for _, check := range doctor.Checks {
		if !check.OK {
			t.Fatalf("doctor check failed: %+v", check)
		}
	}
	assertDoctorCheckNamed(t, doctor, "user rule worktrail")
	for _, skill := range allWorktrailSkills {
		assertDoctorCheckNamed(t, doctor, "user skill "+skill)
	}
	for _, check := range doctor.Checks {
		if strings.Contains(check.Name, "project rule") || strings.Contains(check.Name, "project skill") {
			t.Fatalf("user doctor should not check project rule or skills: %+v", check)
		}
	}
}

func TestInstallCursorUserAlsoInstallsNativeSkillsWhenCompatibleRootsExist(t *testing.T) {
	env := testEnv(t)
	for _, skill := range allWorktrailSkills {
		writeLegacyManagedSkill(t, filepath.Join(env.Home, ".codex", "skills", skill, "SKILL.md"), "codex "+skill)
	}

	report, err := Install(env, ToolCursor, Options{User: true})
	if err != nil {
		t.Fatalf("Install cursor with compatible skills: %v", err)
	}
	assertInstalledSkills(t, filepath.Join(env.Home, ".cursor", "skills"))
	for _, action := range report.Actions {
		if action.Action == "skill-visible-via-compatible-root" {
			t.Fatalf("cursor user install should still write native skills, got %+v", action)
		}
	}

	doctor, err := Doctor(env, ToolCursor, Options{User: true})
	if err != nil {
		t.Fatalf("Doctor cursor with compatible skills: %v", err)
	}
	for _, check := range doctor.Checks {
		if strings.HasPrefix(check.Name, "user skill ") {
			if !strings.HasPrefix(check.Path, filepath.Join(env.Home, ".cursor", "skills")+string(os.PathSeparator)) {
				t.Fatalf("expected doctor to prefer native cursor skill path, got %+v", check)
			}
			if !strings.Contains(check.Note, "duplicate Cursor-visible Worktrail skills") {
				t.Fatalf("expected duplicate warning for compatible skill roots, got %+v", check)
			}
		}
	}
}

func TestProjectInstallCleansLegacyManagedRootRuleAndSkills(t *testing.T) {
	for _, tc := range []struct {
		name       string
		tool       Tool
		rootFiles  []string
		ruleFiles  []string
		skillRoots []string
	}{
		{
			name:      "codex",
			tool:      ToolCodex,
			rootFiles: []string{"AGENTS.md"},
			skillRoots: []string{
				filepath.Join(".agents", "skills"),
			},
		},
		{
			name:      "claude",
			tool:      ToolClaude,
			rootFiles: []string{"CLAUDE.md"},
			skillRoots: []string{
				filepath.Join(".claude", "skills"),
			},
		},
		{
			name:      "cursor",
			tool:      ToolCursor,
			ruleFiles: []string{filepath.Join(".cursor", "rules", "worktrail.mdc")},
			skillRoots: []string{
				filepath.Join(".cursor", "skills"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := testEnv(t)
			var seeded []string
			for _, rel := range tc.rootFiles {
				path := filepath.Join(env.ProjectRoot, rel)
				writeLegacyManagedMarkdown(t, path, tc.name+" root")
				seeded = append(seeded, path)
			}
			for _, rel := range tc.ruleFiles {
				path := filepath.Join(env.ProjectRoot, rel)
				writeLegacyManagedMarkdown(t, path, tc.name+" rule")
				seeded = append(seeded, path)
			}
			for _, relRoot := range tc.skillRoots {
				for _, skill := range legacyProjectSkills {
					path := filepath.Join(env.ProjectRoot, relRoot, skill, "SKILL.md")
					writeLegacyManagedSkill(t, path, tc.name+" "+skill)
					seeded = append(seeded, path)
				}
			}

			if _, err := Install(env, tc.tool, Options{Project: true}); err != nil {
				t.Fatalf("Install %s project: %v", tc.tool, err)
			}
			for _, path := range seeded {
				assertLegacyUnmanagedContentOnly(t, path)
			}
		})
	}
}

func TestProjectInstallDeletesManagedOnlyLegacyAgentFiles(t *testing.T) {
	env := testEnv(t)
	paths := []string{
		filepath.Join(env.ProjectRoot, "AGENTS.md"),
		filepath.Join(env.ProjectRoot, "CLAUDE.md"),
		filepath.Join(env.ProjectRoot, ".cursor", "rules", "worktrail.mdc"),
		filepath.Join(env.ProjectRoot, ".agents", "skills", "worktrail-state", "SKILL.md"),
		filepath.Join(env.ProjectRoot, ".claude", "skills", "worktrail-state", "SKILL.md"),
		filepath.Join(env.ProjectRoot, ".cursor", "skills", "worktrail-state", "SKILL.md"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		body := util.ManagedBegin + "\nlegacy managed only\n" + util.ManagedEnd + "\n"
		if strings.HasSuffix(path, ".mdc") {
			body = "---\ndescription: legacy cursor rule\n---\n\n" + body
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Install(env, ToolCursor, Options{Project: true}); err != nil {
		t.Fatalf("Install cursor project: %v", err)
	}
	for _, path := range paths {
		assertNoPath(t, path)
	}
}

func assertInstalledSkills(t *testing.T, root string) {
	t.Helper()
	for _, skill := range allWorktrailSkills {
		path := filepath.Join(root, skill, "SKILL.md")
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
}

func assertNoInstalledSkills(t *testing.T, root string) {
	t.Helper()
	for _, skill := range allWorktrailSkills {
		assertNoPath(t, filepath.Join(root, skill, "SKILL.md"))
	}
}

func assertNoPath(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be absent, err=%v", path, err)
	}
}

func assertDoctorCheckNamed(t *testing.T, report Report, name string) {
	t.Helper()
	for _, check := range report.Checks {
		if check.Name == name {
			return
		}
	}
	t.Fatalf("doctor report missing check %q: %+v", name, report.Checks)
}

func writeLegacyManagedMarkdown(t *testing.T, path, label string) {
	t.Helper()
	body := "# " + label + "\n\n" +
		"unmanaged before\n\n" +
		util.ManagedBegin + "\n" +
		"legacy worktrail managed content\n" +
		util.ManagedEnd + "\n\n" +
		"unmanaged after\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeLegacyManagedSkill(t *testing.T, path, label string) {
	t.Helper()
	body := "---\ndescription: legacy project skill\n---\n\n" +
		"# " + label + "\n\n" +
		"unmanaged before\n\n" +
		util.ManagedBegin + "\n" +
		"legacy worktrail managed skill content\n" +
		util.ManagedEnd + "\n\n" +
		"unmanaged after\n"
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertLegacyUnmanagedContentOnly(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected unmanaged legacy content to remain in %s: %v", path, err)
	}
	text := string(data)
	if strings.Contains(text, util.ManagedBegin) || strings.Contains(text, util.ManagedEnd) || strings.Contains(text, "legacy worktrail managed") {
		t.Fatalf("managed legacy content remains in %s:\n%s", path, text)
	}
	if !strings.Contains(text, "unmanaged before") || !strings.Contains(text, "unmanaged after") {
		t.Fatalf("unmanaged content was not preserved in %s:\n%s", path, text)
	}
}

func assertRenderedTriggerRouting(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)
	for _, want := range []string{
		"## Skill Trigger Routing",
		"Project activation gate",
		"`.worktrail/` exists",
		"### worktrail-context",
		"### worktrail-doc-preview",
		"### worktrail-search",
		"### worktrail-init",
		"### worktrail-state",
		"### worktrail-resume",
		"### worktrail-handoff",
		"### worktrail-import",
		"### worktrail-distill",
		"### worktrail-review",
		"### worktrail-maintain",
		"worktrail handoff",
		"new conversation",
		"end current chat",
		"Do not only output a copyable text handoff",
		"worktrail evidence plan --format json",
		"Root governance only summarizes the routing surface",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("rendered trigger routing in %s missing %q:\n%s", path, want, text)
		}
	}
	if strings.Contains(text, wtmpl.SkillTriggerRoutingPlaceholder) {
		t.Fatalf("trigger routing placeholder leaked into %s:\n%s", path, text)
	}
}
