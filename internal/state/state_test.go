package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
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

func TestLatestExplicitIgnoresHookSourceToolStates(t *testing.T) {
	env := testEnv(t)
	if _, err := Start(env, StartOptions{
		Scope:      "project",
		Title:      "Hook state",
		Body:       "hook",
		SourceTool: "cursor",
		Actor:      "hook:cursor-stop",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(env, StartOptions{
		Scope: "project",
		Title: "Explicit state",
		Body:  "explicit",
		Actor: "cli:state-start",
	}); err != nil {
		t.Fatal(err)
	}
	cap, err := LatestExplicit(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if cap.State.Title != "Explicit state" {
		t.Fatalf("LatestExplicit = %q, want Explicit state", cap.State.Title)
	}
}

func TestLatestExplicitReturnsNotExistWhenOnlyHookStates(t *testing.T) {
	env := testEnv(t)
	if _, err := Start(env, StartOptions{
		Scope:      "project",
		Title:      "Hook only",
		Body:       "hook",
		SourceTool: "codex",
		Actor:      "hook:codex-stop",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestExplicit(env, "project"); err != os.ErrNotExist {
		t.Fatalf("LatestExplicit err = %v, want ErrNotExist", err)
	}
}
