package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/handoff"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

func TestRunHandoffClosesLatestExplicitStateByDefault(t *testing.T) {
	env := resumeEnv(t)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Close me",
		Body:       "# State Capsule: Close me\n\n## Next Step\n\nShip it.\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	})
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runHandoff(context.Background(), env, IO{Out: &out}, []string{"validated", "and", "ready", "for", "handoff"}); err != nil {
		t.Fatalf("runHandoff: %v", err)
	}

	if _, err := wtstate.LatestExplicit(env, "project"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no active explicit state after handoff, got err=%v", err)
	}
	archived, err := wtstate.Show(env, wtstate.ShowOptions{Scope: "project", ID: cap.State.ID, Directory: wtstate.DirArchived})
	if err != nil {
		t.Fatalf("expected archived state after handoff: %v", err)
	}
	if archived.State.Status != "closed" {
		t.Fatalf("expected archived state status closed, got %s", archived.State.Status)
	}

	rec, err := handoff.Latest(env, "project")
	if err != nil {
		t.Fatalf("Latest handoff: %v", err)
	}
	if rec.Meta.SourceStateID != cap.State.ID {
		t.Fatalf("expected handoff to reference source state %s, got %s", cap.State.ID, rec.Meta.SourceStateID)
	}
	if !strings.Contains(rec.Body, wtstate.DirArchived) {
		t.Fatalf("expected handoff body to point at archived state path:\n%s", rec.Body)
	}
}

func TestRunHandoffOnlyKeepsLatestExplicitState(t *testing.T) {
	env := resumeEnv(t)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Keep me open",
		Body:       "# State Capsule: Keep me open\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--handoff-only", "leave", "state", "active"}); err != nil {
		t.Fatalf("runHandoff --handoff-only: %v", err)
	}

	active, err := wtstate.LatestExplicit(env, "project")
	if err != nil {
		t.Fatalf("LatestExplicit after handoff-only: %v", err)
	}
	if active.State.ID != cap.State.ID {
		t.Fatalf("expected active explicit state to remain %s, got %s", cap.State.ID, active.State.ID)
	}
}

func TestRunHandoffRequiresSummary(t *testing.T) {
	env := resumeEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Need summary",
		Body:       "# State Capsule: Need summary\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	}); err != nil {
		t.Fatal(err)
	}

	err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, nil)
	if err == nil || !strings.Contains(err.Error(), "handoff summary is required") {
		t.Fatalf("expected summary requirement error, got %v", err)
	}
}

func TestRunHandoffSupersedesPreviousCurrentHandoffForSameTask(t *testing.T) {
	env := resumeEnv(t)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Supersede me",
		Body:       "# State Capsule: Supersede me\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope:         "project",
		Title:         "Supersede me",
		Summary:       "older handoff",
		TaskID:        wtstate.TaskID(cap),
		SourceStateID: cap.State.ID,
		Tags:          []string{"handoff", "manual"},
		Body:          "# Handoff: Supersede me\n\nold\n",
		Actor:         "cli:handoff",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Supersede me",
		TaskID:     wtstate.TaskID(cap),
		Body:       "# State Capsule: Supersede me\n\nnew\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	}); err != nil {
		t.Fatal(err)
	}

	if err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"newer", "handoff"}); err != nil {
		t.Fatalf("runHandoff second handoff: %v", err)
	}

	items, err := handoff.List(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 2 {
		t.Fatalf("expected two handoffs, got %+v", items)
	}
	if items[0].Meta.Status != "current" {
		t.Fatalf("expected latest handoff to remain current, got %+v", items[0].Meta)
	}
	if items[1].Meta.Status != "superseded" {
		t.Fatalf("expected prior handoff to be superseded, got %+v", items[1].Meta)
	}
}
