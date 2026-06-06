package app

import (
	"bytes"
	"context"
	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resumeEnv(t *testing.T) paths.Env {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	return paths.Env{
		Home:        filepath.Join(t.TempDir(), "home"),
		UserRoot:    filepath.Join(t.TempDir(), "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func TestResumePrefersManualHandoffOverHookRuntime(t *testing.T) {
	env := resumeEnv(t)
	if err := os.MkdirAll(filepath.Join(env.ProjectWT, "runtime", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Explicit session task",
		Body:       "# State Capsule: Explicit session task\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	}); err != nil {
		t.Fatal(err)
	}
	recorder := runtime.NewRecorder(env.ProjectWT)
	if _, err := recorder.WriteSession(runtime.WriteOptions{
		Scope:      "project",
		Title:      "Hook polluted task",
		Body:       "# Runtime Session: Hook polluted task\n",
		SourceTool: "cursor",
		Event:      "stop",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope:   "project",
		Title:   "Real handoff task",
		Summary: "Continue the real work.",
		Tags:    []string{"handoff", "manual"},
		Body:    "# Handoff: Real handoff task\n\n## Next Step\nShip the real feature.\n",
		Actor:   "cli:handoff",
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runResume(context.Background(), env, IO{Out: &out}, []string{"--format", "json"}); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	if !strings.Contains(out.String(), `"recovery_source_kind":"manual_handoff"`) {
		t.Fatalf("expected manual handoff recovery source, got %s", out.String())
	}
	if !strings.Contains(out.String(), `"source_state"`) {
		t.Fatalf("resume JSON should retain explicit session as supporting source, got %s", out.String())
	}
	if !strings.Contains(out.String(), "Real handoff task") {
		t.Fatalf("resume body should prefer handoff, got %s", out.String())
	}
}

func TestResumeFallsBackToRuntimeWithDegradedLabel(t *testing.T) {
	env := resumeEnv(t)
	recorder := runtime.NewRecorder(env.ProjectWT)
	if _, err := recorder.WriteSession(runtime.WriteOptions{
		Scope:      "project",
		Title:      "Runtime-only task",
		Body:       "# Runtime Session: Runtime-only task\n\n## Next Step\nRecover from runtime.\n",
		SourceTool: "codex",
		Event:      "stop",
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runResume(context.Background(), env, IO{Out: &out}, nil); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	if !strings.Contains(out.String(), "degraded\thook_runtime_state\t") {
		t.Fatalf("expected degraded runtime fallback label, got %s", out.String())
	}
	cap, err := wtstate.LatestExplicit(env, "project")
	if err != nil {
		t.Fatalf("LatestExplicit after resume: %v", err)
	}
	if !strings.Contains(cap.Body, "Runtime fallback artifact") {
		t.Fatalf("resume body missing runtime fallback link:\n%s", cap.Body)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "runtime", "recovery", "current-state.md")); err != nil {
		t.Fatalf("expected runtime recovery dashboard: %v", err)
	}
}
