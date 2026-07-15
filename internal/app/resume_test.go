package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/recovery"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func resumeEnv(t *testing.T) paths.Env {
	t.Helper()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	env := paths.Env{
		Home:        filepath.Join(base, "home"),
		UserRoot:    filepath.Join(base, "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	if err := os.MkdirAll(env.ProjectWT, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), []byte(`{"project_id":"project-resume-test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestResumeSelectsOnlyRequestedTaskAndUsesStructuredRefs(t *testing.T) {
	env := resumeEnv(t)
	alpha, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		TaskID:     "task-alpha",
		Title:      "Alpha task",
		Body:       "alpha state only",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		TaskID:     "task-beta",
		Title:      "Beta task",
		Body:       "beta state must not leak",
		SourceTool: "worktrail",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope:         "project",
		TaskID:        "task-alpha",
		Title:         "Alpha handoff",
		Summary:       "Continue alpha.",
		SourceStateID: alpha.State.ID,
		Tags:          []string{"handoff", "manual"},
		Body:          "# Handoff: Alpha\n\n## Next Step\nShip alpha.\n",
		Actor:         "cli:handoff",
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runResume(context.Background(), env, IO{Out: &out}, []string{"--task-id", "task-alpha", "--format", "json"}); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	var result resumeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Schema != "worktrail.resume.v2" || result.TaskID != "task-alpha" || result.RecoverySource != recovery.SourceLocalHandoff {
		t.Fatalf("resume result = %+v", result)
	}
	if result.Source.Kind != "handoff" || filepath.IsAbs(filepath.FromSlash(result.Source.RelPath)) || filepath.IsAbs(filepath.FromSlash(result.State.RelPath)) {
		t.Fatalf("resume refs are not relocatable: %+v", result)
	}
	if strings.Contains(out.String(), env.ProjectRoot) || strings.Contains(out.String(), "Beta task") {
		t.Fatalf("resume output leaked absolute path or another task: %s", out.String())
	}
	active, err := wtstate.List(env, wtstate.ListOptions{Scope: "project", Directory: wtstate.DirActive})
	if err != nil {
		t.Fatal(err)
	}
	foundNewAlpha := false
	for _, state := range active {
		if state.State.ID == result.State.ID && wtstate.TaskID(state) == "task-alpha" {
			foundNewAlpha = true
		}
	}
	if !foundNewAlpha {
		t.Fatalf("new state did not inherit task-alpha: %+v", active)
	}
}

func TestResumeWithoutSelectorReturnsTypedAmbiguity(t *testing.T) {
	env := resumeEnv(t)
	for _, item := range []struct {
		id    string
		title string
	}{{"task-alpha", "Alpha"}, {"task-beta", "Beta"}} {
		if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: item.id, Title: item.title}); err != nil {
			t.Fatal(err)
		}
	}
	err := runResume(context.Background(), env, IO{Out: &bytes.Buffer{}}, nil)
	var ambiguity *recovery.AmbiguousTaskError
	if !errors.As(err, &ambiguity) || len(ambiguity.Candidates) != 2 {
		t.Fatalf("error = %v, ambiguity = %+v", err, ambiguity)
	}
}

func TestResumeExplicitCheckpointAndRootRelocation(t *testing.T) {
	env := resumeEnv(t)
	oldRoot := env.ProjectRoot
	active, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-checkpoint",
		Title:  "Checkpoint task",
		Body:   "Continue from " + filepath.Join(oldRoot, "internal", "recovery"),
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := wtstate.Checkpoint(env, wtstate.CheckpointOptions{Scope: "project", ID: active.State.ID, Note: "before move"})
	if err != nil {
		t.Fatal(err)
	}
	newRoot := filepath.Join(filepath.Dir(oldRoot), "moved-project")
	if err := os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	env.ProjectRoot = newRoot
	env.ProjectWT = filepath.Join(newRoot, ".worktrail")

	var out bytes.Buffer
	if err := runResume(context.Background(), env, IO{Out: &out}, []string{"--ref", "checkpoint:" + checkpoint.State.ID, "--format", "json"}); err != nil {
		t.Fatal(err)
	}
	var result resumeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.RecoverySource != recovery.SourceExplicitCheckpoint || result.TaskID != "task-checkpoint" || result.Source.ID != checkpoint.State.ID {
		t.Fatalf("checkpoint resume = %+v", result)
	}
	states, err := wtstate.List(env, wtstate.ListOptions{Scope: "project", Directory: wtstate.DirActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || strings.Contains(states[0].Body, oldRoot) || !strings.Contains(states[0].Body, "<absolute-path-omitted>") {
		t.Fatalf("relocated resumed state contains absolute root:\n%+v", states)
	}
}

func TestResumeUnboundRuntimeOnlyByExplicitRef(t *testing.T) {
	env := resumeEnv(t)
	configPath := filepath.Join(env.ProjectWT, "config.json")
	if err := os.Remove(configPath); err != nil {
		t.Fatal(err)
	}
	recorder := runtime.NewRecorder(env.ProjectWT)
	record, err := recorder.WriteSession(runtime.WriteOptions{
		Scope:      "project",
		Title:      "Unbound runtime",
		Body:       "Recover explicitly.",
		SourceTool: "codex",
		Event:      "stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, []byte(`{"project_id":"project-resume-test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runResume(context.Background(), env, IO{Out: &bytes.Buffer{}}, nil); err == nil || !strings.Contains(err.Error(), "could not find") {
		t.Fatalf("automatic unbound resume error = %v", err)
	}
	var out bytes.Buffer
	refID := runtime.StringField(record.Meta, "id")
	if err := runResume(context.Background(), env, IO{Out: &out}, []string{"--ref", "runtime_session:" + refID, "--format", "json"}); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	var result resumeResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.RecoverySource != recovery.SourceRuntimeSession || result.Source.ID != refID || result.TaskID == "" {
		t.Fatalf("unbound runtime resume = %+v", result)
	}
}

func TestResumeFallsBackToBoundRuntimeWithDegradedLabel(t *testing.T) {
	env := resumeEnv(t)
	recorder := runtime.NewRecorder(env.ProjectWT)
	if _, err := recorder.WriteSession(runtime.WriteOptions{
		Scope:      "project",
		ProjectID:  "project-resume-test",
		TaskID:     "task-runtime",
		Title:      "Runtime-only task",
		Body:       "Recover from runtime.",
		SourceTool: "codex",
		Event:      "stop",
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := runResume(context.Background(), env, IO{Out: &out}, []string{"--task-id", "task-runtime"}); err != nil {
		t.Fatalf("runResume: %v", err)
	}
	if !strings.Contains(out.String(), "degraded\truntime_session\t") {
		t.Fatalf("expected degraded runtime fallback label, got %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "runtime", "recovery", "current-state.md")); err != nil {
		t.Fatalf("expected runtime recovery dashboard: %v", err)
	}
}

func TestParseRecoveryRefSupportsStructuredForms(t *testing.T) {
	if got := parseRecoveryRef("project", "user:handoff:id-1"); got.Scope != "user" || got.Kind != "handoff" || got.ID != "id-1" {
		t.Fatalf("three-part ref = %+v", got)
	}
	if got := parseRecoveryRef("project", "checkpoint:id-2"); got.Scope != "project" || got.Kind != "checkpoint" || got.ID != "id-2" {
		t.Fatalf("two-part ref = %+v", got)
	}
	if got := parseRecoveryRef("project", "id-3"); got.Scope != "project" || got.Kind != "" || got.ID != "id-3" {
		t.Fatalf("id ref = %+v", got)
	}
}

func TestResumeDashboardFailureDoesNotRollBackSuccessfulResume(t *testing.T) {
	env := resumeEnv(t)
	source, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "task-dashboard", Title: "Dashboard"})
	if err != nil {
		t.Fatal(err)
	}
	recoveryPath := filepath.Join(env.ProjectWT, "runtime", "recovery")
	if err := os.MkdirAll(filepath.Dir(recoveryPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recoveryPath, []byte("blocks dashboard directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	var out, errOut bytes.Buffer
	if err := runResume(context.Background(), env, IO{Out: &out, Err: &errOut}, []string{"--task-id", "task-dashboard"}); err != nil {
		t.Fatalf("resume should succeed despite dashboard failure: %v", err)
	}
	if !strings.Contains(errOut.String(), "resume succeeded") {
		t.Fatalf("missing dashboard warning: %s", errOut.String())
	}
	if _, err := wtstate.Show(env, wtstate.ShowOptions{Scope: "project", ID: source.State.ID, Directory: wtstate.DirArchived}); err != nil {
		t.Fatalf("source state was not closed: %v", err)
	}
	active, err := wtstate.List(env, wtstate.ListOptions{Scope: "project", Directory: wtstate.DirActive})
	if err != nil || len(active) != 1 || active[0].State.ID == source.State.ID {
		t.Fatalf("active states = %+v, err=%v", active, err)
	}
}

func TestResumeBareRefReportsCrossKindAmbiguity(t *testing.T) {
	env := resumeEnv(t)
	runtimeRecord, err := runtime.NewRecorder(env.ProjectWT).WriteSession(runtime.WriteOptions{
		Scope: "project", ProjectID: "project-resume-test", TaskID: "task-runtime-ref", Title: "Runtime ref",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope: "project", ID: runtimeRecord.ID, TaskID: "task-state-ref", Title: "State ref",
	}); err != nil {
		t.Fatal(err)
	}
	err = runResume(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--ref", runtimeRecord.ID})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") || !strings.Contains(err.Error(), "kind:id") {
		t.Fatalf("bare ref error = %v", err)
	}
}

func TestResumeRejectsLegacyRootHandoffUntilMigration(t *testing.T) {
	env := resumeEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "task-v1", Title: "V1 gate"}); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(env.ProjectWT, "handoffs", "legacy.md")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacyPath, []byte("# Legacy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runResume(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--task-id", "task-v1"})
	if !errors.Is(err, handoff.ErrMigrationRequired) {
		t.Fatalf("resume error = %v, want migration required", err)
	}
}
