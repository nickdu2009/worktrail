package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/integrations"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestCodexCLIVersionMatchesFixtureMetadata(t *testing.T) {
	metaPath := filepath.Join("..", "..", "testdata", "fixtures", "hooks", "metadata.json")
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		CodexCLIVersion string `json:"codex_cli_version"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(meta.CodexCLIVersion) == "" {
		t.Fatal("fixture metadata missing codex_cli_version")
	}
	path, err := exec.LookPath("codex")
	if err != nil {
		t.Skipf("codex CLI unavailable; pinned version=%s", meta.CodexCLIVersion)
	}
	out, err := exec.Command(path, "--version").CombinedOutput()
	if err != nil {
		t.Fatalf("codex --version: %v (%s)", err, out)
	}
	got := strings.TrimSpace(string(out))
	if !strings.Contains(got, meta.CodexCLIVersion) {
		t.Fatalf("codex version %q does not contain pinned %q", got, meta.CodexCLIVersion)
	}
}

func TestProjectInstallHookSmokeE2E(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "worktrail"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	env := paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	if err := store.InitProject(env); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(project, ".codex", "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("init must not write codex hooks, err=%v", err)
	}

	if _, err := integrations.Install(env, integrations.ToolCursor, integrations.Options{Project: true}); err != nil {
		t.Fatalf("install cursor: %v", err)
	}
	if _, err := integrations.Install(env, integrations.ToolCodex, integrations.Options{Project: true}); err != nil {
		t.Fatalf("install codex: %v", err)
	}

	cursorHooks, err := os.ReadFile(filepath.Join(project, ".cursor", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(cursorHooks), "worktrail hook cursor beforeShellExecution") {
		t.Fatalf("cursor hooks missing direct command: %s", cursorHooks)
	}
	codexHooks, err := os.ReadFile(filepath.Join(project, ".codex", "hooks.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(codexHooks), `"timeout"`) || !strings.Contains(string(codexHooks), "worktrail hook codex PreToolUse") {
		t.Fatalf("codex hooks missing matcher/timeout command: %s", codexHooks)
	}

	doctor, err := integrations.Doctor(env, integrations.ToolCodex, integrations.Options{Project: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range doctor.Checks {
		if strings.Contains(check.Name, "hooks") && !check.OK && !strings.Contains(check.Name, "trust") {
			t.Fatalf("doctor failed: %+v", check)
		}
	}

	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", "cursor-before-shell-deny.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	err = Run(context.Background(), env, "cursor", "beforeShellExecution", bytes.NewReader(fixture), &out)
	if err == nil {
		t.Fatal("expected deny exit error")
	}
	if !strings.Contains(out.String(), `"permission":"deny"`) {
		t.Fatalf("smoke deny wire=%s", out.String())
	}

	if _, err := integrations.Uninstall(env, integrations.ToolCursor, integrations.Options{Project: true}); err != nil {
		t.Fatalf("uninstall cursor: %v", err)
	}
	if _, err := integrations.Uninstall(env, integrations.ToolCodex, integrations.Options{Project: true}); err != nil {
		t.Fatalf("uninstall codex: %v", err)
	}
}

func TestLegacyCodexUserScalarBlocksInstall(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(filepath.Join(project, ".codex"), 0o755); err != nil {
		t.Fatal(err)
	}
	hooksPath := filepath.Join(project, ".codex", "hooks.json")
	if err := os.WriteFile(hooksPath, []byte(`{"hooks":{"Stop":"echo custom"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	_ = os.MkdirAll(bin, 0o755)
	_ = os.WriteFile(filepath.Join(bin, "worktrail"), []byte("#!/bin/sh\nexit 0\n"), 0o755)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	env := paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	if err := store.InitProject(env); err != nil {
		t.Fatal(err)
	}
	_, err := integrations.Install(env, integrations.ToolCodex, integrations.Options{Project: true})
	if err == nil || !strings.Contains(err.Error(), "legacy_codex_user_hook_requires_manual_migration") {
		t.Fatalf("err=%v", err)
	}
	raw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "worktrail hook") {
		t.Fatalf("legacy conflict must be zero-write: %s", raw)
	}
}
