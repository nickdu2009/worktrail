package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

func stateCommandEnv(t *testing.T) paths.Env {
	t.Helper()
	return resumeEnv(t)
}

func TestRunStateUpdateLatestUsesExplicitState(t *testing.T) {
	env := stateCommandEnv(t)
	explicit, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Explicit task",
		Body:       "# State Capsule: Explicit task\n\n## Work Done\n\nkeep me\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	})
	if err != nil {
		t.Fatal(err)
	}
	hookState, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Hook task",
		Body:       "# State Capsule: Hook task\n\n## Work Done\n\nhook body\n",
		SourceTool: "cursor",
		Actor:      "hook:cursor-stop",
	})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runState(context.Background(), env, IO{Out: &out}, []string{"update", "verified explicit state"}); err != nil {
		t.Fatalf("runState update: %v", err)
	}

	updatedExplicit, err := wtstate.Show(env, wtstate.ShowOptions{Scope: "project", ID: explicit.State.ID, Directory: wtstate.DirActive})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updatedExplicit.Body, "verified explicit state") {
		t.Fatalf("explicit state was not updated:\n%s", updatedExplicit.Body)
	}

	unchangedHook, err := wtstate.Show(env, wtstate.ShowOptions{Scope: "project", ID: hookState.State.ID, Directory: wtstate.DirActive})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(unchangedHook.Body, "verified explicit state") {
		t.Fatalf("hook state should not be updated by latest:\n%s", unchangedHook.Body)
	}
}
