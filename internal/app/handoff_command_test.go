package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

func TestRunHandoffClosesLatestExplicitStateByDefault(t *testing.T) {
	env := handoffCommandEnv(t)
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
	if err := runHandoff(context.Background(), env, IO{Out: &out}, []string{"--next-step", "Ship it", "validated", "and", "ready", "for", "handoff"}); err != nil {
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
	env := handoffCommandEnv(t)
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

	if err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--handoff-only", "--next-step", "continue", "leave", "state", "active"}); err != nil {
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
	env := handoffCommandEnv(t)
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
	env := handoffCommandEnv(t)
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

	if err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--next-step", "continue", "newer", "handoff"}); err != nil {
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

func TestRunHandoffCreateJSONMatchesFlagShape(t *testing.T) {
	env := handoffCommandEnv(t)
	input := `{"summary":"json handoff","complete":false,"task_id":"task-json","next_steps":[{"action":"continue"}]}`
	var out bytes.Buffer
	if err := runHandoff(context.Background(), env, IO{In: strings.NewReader(input), Out: &out}, []string{"create", "--stdin", "--format", "json"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"summary":"json handoff"`) || !strings.Contains(out.String(), `"action":"continue"`) {
		t.Fatalf("JSON output = %s", out.String())
	}
}

func TestRunHandoffCreateStdinRejectsPositionalAndPayloadFlags(t *testing.T) {
	env := handoffCommandEnv(t)
	input := `{"summary":"json handoff","complete":true,"task_id":"task-json"}`
	cases := [][]string{
		{"create", "--stdin", "positional summary"},
		{"create", "--stdin", "--complete"},
		{"create", "--stdin", "--next-step", "continue"},
		{"create", "--stdin", "--body", "body"},
		{"create", "--stdin", "--title", "title"},
		{"create", "--stdin", "--project-id", "project"},
		{"create", "--stdin", "--task-id", "task"},
		{"create", "--stdin", "--question", "question"},
		{"create", "--stdin", "--risk", "risk"},
		{"create", "--stdin", "--tags", "tag"},
		{"create", "--stdin", "--source-tool", "tool"},
		{"create", "--stdin", "--validation-status", "passed"},
		{"create", "--stdin", "--validation-command", "go test"},
		{"create", "--stdin", "--validation-note", "passed"},
		{"create", "--stdin", "--validation-exit-code", "0"},
	}
	for _, args := range cases {
		err := runHandoff(context.Background(), env, IO{
			In: strings.NewReader(input), Out: &bytes.Buffer{},
		}, args)
		if err == nil || !strings.Contains(err.Error(), "--stdin cannot be combined") {
			t.Fatalf("runHandoff(%v) error = %v", args, err)
		}
	}
}

func TestParseHandoffArgsRejectsRepeatedScalarFlags(t *testing.T) {
	for _, args := range [][]string{
		{"--scope", "project", "--scope", "user"},
		{"--complete", "--complete"},
		{"--stdin", "--stdin"},
		{"--title", "one", "--title", "two"},
		{"--validation-note", "one", "--validation-note", "two"},
		{"--supersedes", "one", "--supersedes", "two"},
	} {
		if _, err := parseHandoffArgs(args); err == nil || !strings.Contains(err.Error(), "may not be repeated") {
			t.Fatalf("parseHandoffArgs(%v) error = %v", args, err)
		}
	}
}

func TestParseHandoffArgsAllowsExplicitRepeatableFlags(t *testing.T) {
	parsed, err := parseHandoffArgs([]string{
		"--next-step", "one", "--next-step", "two",
		"--question", "one", "--question", "two",
		"--risk", "one", "--risk", "two",
		"--tags", "one", "--tags", "two",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"next-step", "question", "risk", "tags"} {
		if len(parsed.values(flag)) != 2 {
			t.Fatalf("--%s values = %v", flag, parsed.values(flag))
		}
	}
}

func TestRunStateCloseToHandoffUsesAtomicV2Path(t *testing.T) {
	env := handoffCommandEnv(t)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Atomic close",
		Body:       "# State Capsule: Atomic close\n",
		SourceTool: "worktrail",
		Actor:      "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runState(context.Background(), env, IO{Out: &out}, []string{"close", "--to", "handoff", "--next-step", "continue", "ready"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Show(env, wtstate.ShowOptions{Scope: "project", ID: cap.State.ID, Directory: wtstate.DirActive}); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("active state still exists: %v", err)
	}
	if _, err := wtstate.Show(env, wtstate.ShowOptions{Scope: "project", ID: cap.State.ID, Directory: wtstate.DirArchived}); err != nil {
		t.Fatalf("archived state missing: %v", err)
	}
	record, err := handoff.Latest(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.SourceState == nil || record.Meta.SourceState.RelPath != filepath.ToSlash(filepath.Join("state", wtstate.DirArchived, cap.State.ID+".md")) {
		t.Fatalf("source state ref = %+v", record.Meta.SourceState)
	}
}

func TestRunStateCloseToHandoffAcceptsCreateRequestJSON(t *testing.T) {
	env := handoffCommandEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "JSON close",
		Body:       "# State Capsule: JSON close\n",
		SourceTool: "worktrail",
		Actor:      "test",
	}); err != nil {
		t.Fatal(err)
	}
	input := `{"summary":"close from json","next_steps":[{"action":"continue from json"}]}`
	if err := runState(context.Background(), env, IO{In: strings.NewReader(input), Out: &bytes.Buffer{}}, []string{"close", "--to", "handoff", "--stdin"}); err != nil {
		t.Fatal(err)
	}
	record, err := handoff.Latest(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.Summary != "close from json" || len(record.Meta.NextSteps) != 1 || record.Meta.NextSteps[0].Action != "continue from json" {
		t.Fatalf("record = %+v", record.Meta)
	}
}

func TestStateCloseUsesStrictFlagAllowlistAndSafeHelpExamples(t *testing.T) {
	env := handoffCommandEnv(t)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope: "project", TaskID: "task-flags", Title: "Strict flags",
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"close", "--complete", "not a handoff"},
		{"close", "--to", "handoff", "--format", "json", "--complete", "ignored format"},
		{"close", "--to", "handoff", "--title", "ignored", "--complete", "ignored title"},
		{"close", "--unknown", "value", "summary"},
		{"close", "--to", "elsewhere", "summary"},
	} {
		err := runState(context.Background(), env, IO{Out: &bytes.Buffer{}}, args)
		if err == nil {
			t.Fatalf("state args unexpectedly accepted: %v", args)
		}
	}
	if _, err := wtstate.Show(env, wtstate.ShowOptions{
		Scope: "project", ID: cap.State.ID, Directory: wtstate.DirActive,
	}); err != nil {
		t.Fatalf("invalid flags changed active state: %v", err)
	}

	var help bytes.Buffer
	if err := runState(context.Background(), env, IO{Out: &help}, []string{"--help"}); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(help.String(), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "worktrail state close --to handoff") &&
			!strings.Contains(line, "--next-step") && !strings.Contains(line, "--complete") {
			t.Fatalf("unsafe handoff help example: %q", line)
		}
	}
	if !strings.Contains(help.String(), "--next-step") || !strings.Contains(help.String(), "--complete") {
		t.Fatalf("state help lacks safe handoff alternatives:\n%s", help.String())
	}
}

func TestRunHandoffTaskIDSelectsMatchingActiveState(t *testing.T) {
	env := handoffCommandEnv(t)
	alpha, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "task-alpha", Title: "Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "task-beta", Title: "Beta"}); err != nil {
		t.Fatal(err)
	}
	if err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{
		"create", "--task-id", "task-alpha", "--handoff-only", "--complete", "alpha handoff",
	}); err != nil {
		t.Fatal(err)
	}
	record, err := handoff.Latest(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.TaskID != "task-alpha" || record.Meta.SourceState == nil || record.Meta.SourceState.ID != alpha.State.ID {
		t.Fatalf("record selected wrong state: %+v", record.Meta)
	}
}

func TestRunHandoffWithoutTaskSelectorRejectsMultipleActiveTasks(t *testing.T) {
	env := handoffCommandEnv(t)
	for _, taskID := range []string{"task-alpha", "task-beta"} {
		if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: taskID, Title: taskID}); err != nil {
			t.Fatal(err)
		}
	}
	err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"create", "--complete", "ambiguous"})
	if err == nil || !strings.Contains(err.Error(), "multiple active tasks") {
		t.Fatalf("runHandoff error = %v", err)
	}
}

func TestHandoffSubcommandsRejectUnknownAndUnrelatedFlags(t *testing.T) {
	env := handoffCommandEnv(t)
	cases := [][]string{
		{"list", "--complete"},
		{"doctor", "--task-id", "task"},
		{"create", "--visibility", "team", "--complete", "summary"},
		{"list", "--unknown", "value"},
	}
	for _, args := range cases {
		if err := runHandoff(context.Background(), env, IO{Out: &bytes.Buffer{}}, args); err == nil {
			t.Fatalf("runHandoff(%v) accepted invalid flags", args)
		}
	}
	var help bytes.Buffer
	printHandoffHelp(&help)
	for _, want := range []string{"--task-id", "--visibility", "--supersedes", "--apply --confirm", "--validation-command"} {
		if !strings.Contains(help.String(), want) {
			t.Fatalf("help missing %q:\n%s", want, help.String())
		}
	}
}

func handoffCommandEnv(t *testing.T) paths.Env {
	t.Helper()
	env := resumeEnv(t)
	if err := os.MkdirAll(env.ProjectWT, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), []byte(`{"project_id":"project-app-test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}
