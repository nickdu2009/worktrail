package recovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

func testEnv(t *testing.T) paths.Env {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	return paths.Env{
		Home:        filepath.Join(t.TempDir(), "home"),
		UserRoot:    filepath.Join(t.TempDir(), "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func TestSelectPrefersHandoffOverExplicitState(t *testing.T) {
	env := testEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope: "project",
		Title: "Explicit state",
		Body:  "explicit",
	}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope: "project",
		Title: "Manual handoff",
		Body:  "handoff",
		Tags:  []string{"handoff", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	sel, err := Select(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if sel.SourceKind != model.ResumePriorityManualHandoff {
		t.Fatalf("got %s, want manual_handoff", sel.SourceKind)
	}
	if sel.Quality != QualityPrimary {
		t.Fatalf("got quality %s", sel.Quality)
	}
	if sel.Handoff == nil || sel.State == nil {
		t.Fatalf("selection should retain both handoff and explicit state: %+v", sel)
	}
}

func TestSelectFallsBackToHookRuntimeState(t *testing.T) {
	env := testEnv(t)
	recorder := runtime.NewRecorder(env.ProjectWT)
	if _, err := recorder.WriteSession(runtime.WriteOptions{
		Scope: "project",
		Title: "Hook runtime",
		Body:  "runtime",
	}); err != nil {
		t.Fatal(err)
	}
	sel, err := Select(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if sel.SourceKind != model.ResumePriorityHookRuntimeState {
		t.Fatalf("got %s", sel.SourceKind)
	}
	if sel.Quality != QualityDegraded {
		t.Fatalf("got quality %s", sel.Quality)
	}
}

func TestWriteRecoveryDashboardUsesRuntimePath(t *testing.T) {
	env := testEnv(t)
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope: "project",
		Title: "Dashboard handoff",
		Body:  "handoff",
		Tags:  []string{"handoff", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	path, err := WriteRecoveryDashboard(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(path) != filepath.ToSlash(filepath.Join(env.ProjectWT, "runtime", "recovery", "current-state.md")) {
		t.Fatalf("unexpected dashboard path: %s", path)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "current-state.md")); !os.IsNotExist(err) {
		t.Fatalf("durable root current-state.md must not be created")
	}
}
