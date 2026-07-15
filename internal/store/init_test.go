package store

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

func TestEnsureProjectGitignoreMigratesLegacyBlock(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".gitignore")
	if err := os.WriteFile(path, []byte("*.test\n"+legacyProjectGitignoreBody+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProjectGitignore(paths.Env{ProjectRoot: project}); err != nil {
		t.Fatalf("EnsureProjectGitignore: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "*.test") {
		t.Fatalf("existing gitignore content was not preserved: %s", text)
	}
	if strings.Count(text, ".codex/") != 1 {
		t.Fatalf("expected one .codex entry after migration: %s", text)
	}
	if strings.Count(text, "/.worktrail-handoff-v2-backups/") != 1 {
		t.Fatalf("expected one precise handoff-v2 backup ignore entry: %s", text)
	}
	if !strings.Contains(text, util.HashManagedBegin) || strings.Contains(text, util.ManagedBegin) {
		t.Fatalf("expected hash managed block only: %s", text)
	}
}

func TestInitProjectMigratesStableProjectID(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".worktrail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	oldConfig := `{
  "schema": "worktrail.config.v1",
  "scope": "project",
  "version": "0.1.0",
  "created_at": "2026-07-15T00:00:00Z",
  "custom": "preserved"
}
`
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(oldConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InitProject(paths.Env{ProjectRoot: project, ProjectWT: root}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}
	first := readConfigMap(t, filepath.Join(root, "config.json"))
	projectID, _ := first["project_id"].(string)
	if !strings.HasPrefix(projectID, "project_") {
		t.Fatalf("project_id = %q", projectID)
	}
	if first["custom"] != "preserved" || first["created_at"] != "2026-07-15T00:00:00Z" {
		t.Fatalf("additive migration lost config fields: %+v", first)
	}

	rerootedProject := t.TempDir()
	rerootedRoot := filepath.Join(rerootedProject, ".worktrail")
	if err := os.MkdirAll(rerootedRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	configData, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rerootedRoot, "config.json"), configData, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := InitProject(paths.Env{ProjectRoot: rerootedProject, ProjectWT: rerootedRoot}); err != nil {
		t.Fatalf("InitProject() after reroot error = %v", err)
	}
	second := readConfigMap(t, filepath.Join(rerootedRoot, "config.json"))
	if second["project_id"] != projectID {
		t.Fatalf("project_id changed after reroot: got %q, want %q", second["project_id"], projectID)
	}
}

func TestInitUserDoesNotGenerateProjectID(t *testing.T) {
	root := filepath.Join(t.TempDir(), "user-worktrail")
	if err := InitUser(paths.Env{UserRoot: root}); err != nil {
		t.Fatalf("InitUser() error = %v", err)
	}
	cfg := readConfigMap(t, filepath.Join(root, "config.json"))
	if _, ok := cfg["project_id"]; ok {
		t.Fatalf("user config unexpectedly contains project_id: %+v", cfg)
	}
}

func TestInitProjectCreatesHandoffAndOperationalLayout(t *testing.T) {
	project := t.TempDir()
	root := filepath.Join(project, ".worktrail")
	if err := InitProject(paths.Env{ProjectRoot: project, ProjectWT: root}); err != nil {
		t.Fatalf("InitProject() error = %v", err)
	}
	for _, rel := range []string{
		"handoffs/local",
		"handoffs/team",
		"ops",
		"runtime/migrations",
	} {
		if info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil || !info.IsDir() {
			t.Fatalf("expected directory %q: info=%v err=%v", rel, info, err)
		}
	}
	if runtime.GOOS != "windows" {
		for _, rel := range []string{"handoffs/local", "ops"} {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil {
				t.Fatal(err)
			}
			if got := info.Mode().Perm(); got != 0o700 {
				t.Fatalf("%s mode = %o, want 700", rel, got)
			}
		}
	}

	if err := exec.Command("git", "-C", project, "init", "--quiet").Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	for _, rel := range []string{
		".worktrail/handoffs/local/example.md",
		".worktrail/ops/intent.json",
		".worktrail/runtime/migrations/manifest.json",
	} {
		if err := exec.Command("git", "-C", project, "check-ignore", "--quiet", rel).Run(); err != nil {
			t.Fatalf("expected %q to be ignored: %v", rel, err)
		}
	}
	if err := exec.Command("git", "-C", project, "check-ignore", "--quiet", ".worktrail/handoffs/team/example.md").Run(); err == nil {
		t.Fatal("team handoff path must remain trackable")
	}
}

func readConfigMap(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatal(err)
	}
	return cfg
}
