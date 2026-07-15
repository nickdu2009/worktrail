package recovery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/store"
)

func testEnv(t *testing.T) paths.Env {
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
	config, _ := json.Marshal(map[string]any{"project_id": "project-test"})
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), config, 0o644); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestTaskScopedResolverKeepsTasksSeparateAndPrefersLocalHandoff(t *testing.T) {
	env := testEnv(t)
	alpha, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-alpha",
		Title:  "Alpha state",
		Body:   "alpha only",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope:         "project",
		TaskID:        "task-alpha",
		Title:         "Alpha handoff",
		Summary:       "continue alpha",
		Body:          "alpha handoff only",
		SourceStateID: alpha.State.ID,
		Tags:          []string{"handoff", "manual"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-beta",
		Title:  "Beta state",
		Body:   "beta only",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := NewTaskScopedResolver(env)
	sel, err := resolver.Resolve("project", TaskSelector{TaskID: "task-alpha"})
	if err != nil {
		t.Fatal(err)
	}
	if sel.SourceKind != SourceLocalHandoff {
		t.Fatalf("source = %s, want local handoff", sel.SourceKind)
	}
	if sel.TaskID != "task-alpha" || sel.Handoff == nil || strings.Contains(sel.Handoff.Body, "beta") {
		t.Fatalf("cross-task selection leaked: %+v", sel)
	}
	for _, ref := range sel.SupportingRefs {
		if ref.ID == "task-beta" {
			t.Fatalf("beta ref leaked into alpha selection: %+v", sel.SupportingRefs)
		}
	}
}

func TestTaskOmissionReturnsTypedAmbiguity(t *testing.T) {
	env := testEnv(t)
	for _, item := range []struct {
		id    string
		title string
	}{{"task-alpha", "Alpha"}, {"task-beta", "Beta"}} {
		if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: item.id, Title: item.title}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Select(env, "project")
	var ambiguity *AmbiguousTaskError
	if !errors.As(err, &ambiguity) {
		t.Fatalf("error = %v, want AmbiguousTaskError", err)
	}
	if len(ambiguity.Candidates) != 2 || ambiguity.Candidates[0].ID != "task-alpha" || ambiguity.Candidates[1].ID != "task-beta" {
		t.Fatalf("candidates = %+v", ambiguity.Candidates)
	}
	if !strings.Contains(err.Error(), "task-alpha (Alpha)") || !strings.Contains(err.Error(), "task-beta (Beta)") {
		t.Fatalf("ambiguity text lacks ids/titles: %v", err)
	}
}

func TestTaskTitleAmbiguityDoesNotMixTasks(t *testing.T) {
	env := testEnv(t)
	for _, id := range []string{"task-a", "task-b"} {
		if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: id, Title: "Same title"}); err != nil {
			t.Fatal(err)
		}
	}
	_, err := NewTaskScopedResolver(env).Resolve("project", TaskSelector{Title: "Same title"})
	var ambiguity *AmbiguousTaskError
	if !errors.As(err, &ambiguity) || len(ambiguity.Candidates) != 2 {
		t.Fatalf("error = %v, candidates = %+v", err, ambiguity)
	}
}

func TestExplicitCheckpointCanBeRecoveredByRef(t *testing.T) {
	env := testEnv(t)
	active, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-checkpoint",
		Title:  "Checkpoint task",
		Body:   "before checkpoint",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := wtstate.Checkpoint(env, wtstate.CheckpointOptions{Scope: "project", ID: active.State.ID, Note: "recover me"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := wtstate.Start(env, wtstate.StartOptions{
		Scope: "project", TaskID: "task-other", Title: "Other task", Body: "must stay separate",
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewTaskScopedResolver(env)
	normal, err := resolver.Resolve("project", TaskSelector{TaskID: "task-checkpoint"})
	if err != nil {
		t.Fatal(err)
	}
	if normal.SourceKind != SourceExplicitState {
		t.Fatalf("normal source = %s, want explicit state", normal.SourceKind)
	}
	selected, err := resolver.Resolve("project", TaskSelector{Ref: &model.Ref{Scope: "project", Kind: "checkpoint", ID: checkpoint.State.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceExplicitCheckpoint || selected.State == nil || selected.State.State.ID != checkpoint.State.ID {
		t.Fatalf("explicit checkpoint selection = %+v", selected)
	}
	if selected.ActiveState == nil || wtstate.TaskID(*selected.ActiveState) != "task-checkpoint" {
		t.Fatalf("checkpoint selected another task's active state: %+v", selected.ActiveState)
	}
	for _, ref := range selected.SupportingRefs {
		if ref.ID == other.State.ID {
			t.Fatalf("checkpoint refs leaked other task: %+v", selected.SupportingRefs)
		}
	}
}

func TestBareRefCollectsAllMatchesAndReturnsTypedAmbiguity(t *testing.T) {
	env := testEnv(t)
	logical := time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC)
	base := map[string]any{
		"schema":      model.SchemaState,
		"id":          "shared-ref",
		"scope":       "project",
		"task_id":     "task-shared",
		"type":        "session",
		"title":       "Shared ref",
		"status":      "active",
		"source_tool": "worktrail",
		"created_at":  logical,
		"updated_at":  logical,
	}
	writeStateRecord(t, env, wtstate.DirActive, base, "active")
	checkpoint := make(map[string]any, len(base)+2)
	for key, value := range base {
		checkpoint[key] = value
	}
	checkpoint["checkpoint_id"] = "shared-ref"
	checkpoint["checkpoint_of"] = "shared-ref"
	writeStateRecord(t, env, wtstate.DirCheckpoints, checkpoint, "checkpoint")

	resolver := NewTaskScopedResolver(env)
	_, err := resolver.Resolve("project", TaskSelector{Ref: &model.Ref{ID: "shared-ref"}})
	var ambiguity *AmbiguousRefError
	if !errors.As(err, &ambiguity) || len(ambiguity.Matches) != 2 {
		t.Fatalf("bare ref error = %v, ambiguity=%+v", err, ambiguity)
	}
	if ambiguity.Matches[0].Kind != "checkpoint" || ambiguity.Matches[1].Kind != "state" {
		t.Fatalf("ambiguous matches = %+v", ambiguity.Matches)
	}
	selected, err := resolver.Resolve("project", TaskSelector{Ref: &model.Ref{
		Scope: "project", Kind: "state", ID: "shared-ref",
	}})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceExplicitState || selected.State == nil || strings.TrimSpace(selected.State.Body) != "active" {
		t.Fatalf("qualified ref selection = %+v", selected)
	}
}

func TestSameLogicalTimeUsesCreatedAtThenIDWithoutMtime(t *testing.T) {
	env := testEnv(t)
	logical := time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC)
	for _, id := range []string{"state-b", "state-a"} {
		writeStateRecord(t, env, wtstate.DirActive, map[string]any{
			"schema":      model.SchemaState,
			"id":          id,
			"scope":       "project",
			"task_id":     "task-tie",
			"type":        "session",
			"title":       "Tie task",
			"status":      "active",
			"source_tool": "worktrail",
			"created_at":  logical,
			"updated_at":  logical,
		}, id)
	}
	path := filepath.Join(env.ProjectWT, "state", "active", "state-b.md")
	future := logical.Add(24 * time.Hour)
	if err := os.Chtimes(path, future, future); err != nil {
		t.Fatal(err)
	}
	sel, err := Select(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if sel.SourceRef.ID != "state-a" {
		t.Fatalf("tie selected %q, want lexical state-a", sel.SourceRef.ID)
	}
}

func TestRootRelocationKeepsStableProjectAndRelativeRefs(t *testing.T) {
	env := testEnv(t)
	if _, err := handoff.Create(env, handoff.CreateOptions{
		Scope:   "project",
		TaskID:  "task-relocate",
		Title:   "Relocate",
		Summary: "move project",
		Body:    "continue after moving the root",
	}); err != nil {
		t.Fatal(err)
	}
	oldRoot := env.ProjectRoot
	newRoot := filepath.Join(filepath.Dir(oldRoot), "relocated-project")
	if err := os.Rename(oldRoot, newRoot); err != nil {
		t.Fatal(err)
	}
	env.ProjectRoot = newRoot
	env.ProjectWT = filepath.Join(newRoot, ".worktrail")
	sel, err := Select(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if sel.ProjectID != "project-test" || filepath.IsAbs(filepath.FromSlash(sel.SourceRef.RelPath)) || strings.Contains(sel.SourceRef.RelPath, oldRoot) {
		t.Fatalf("relocated selection = %+v", sel)
	}
}

func TestLocalTeamAndUserLocalFallbacks(t *testing.T) {
	env := testEnv(t)
	initGitRepo(t, env.ProjectRoot)
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, "dirty.txt"), []byte("uncommitted code"), 0o644); err != nil {
		t.Fatal(err)
	}
	local, err := handoff.Create(env, handoff.CreateOptions{
		Scope:   "project",
		TaskID:  "task-team",
		Title:   "Team task",
		Summary: "publish task",
		Body:    "team body",
	})
	if err != nil {
		t.Fatal(err)
	}
	team, err := handoff.Publish(context.Background(), env, handoff.PublishRequest{
		Scope: "project", ID: local.Meta.ID, AllowDirty: true, Confirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	resolver := NewTaskScopedResolver(env)
	selected, err := resolver.Resolve("project", TaskSelector{TaskID: "task-team"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceLocalHandoff {
		t.Fatalf("source with local present = %s", selected.SourceKind)
	}
	if err := os.Remove(local.Path); err != nil {
		t.Fatal(err)
	}
	selected, err = resolver.Resolve("project", TaskSelector{TaskID: "task-team"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceTeamHandoff || selected.SourceRef.ID != team.Meta.ID || !strings.Contains(selected.CodeAvailabilityHint, "code is unavailable") {
		t.Fatalf("team fallback = %+v", selected)
	}

	userLocal, err := handoff.CreateLocal(context.Background(), env, handoff.CreateRequest{
		Scope:     "user",
		Title:     "User-local task",
		Summary:   "user fallback",
		Complete:  true,
		ProjectID: "project-test",
		TaskID:    "task-user-local",
		Body:      "user local body",
	})
	if err != nil {
		t.Fatal(err)
	}
	selected, err = resolver.Resolve("project", TaskSelector{TaskID: "task-user-local"})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceLocalHandoff || selected.SourceRef.Scope != "user" || selected.SourceRef.ID != userLocal.Meta.ID {
		t.Fatalf("user-local fallback = %+v", selected)
	}
}

func TestUnboundRuntimeRequiresExplicitRef(t *testing.T) {
	env := testEnv(t)
	now := time.Now().UTC()
	writeRuntimeRecord(t, env, runtime.DirSessions, map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "unbound-session",
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            "Unbound runtime",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"resume_priority":  model.ResumePriorityHookRuntimeState,
		"created_at":       now,
		"updated_at":       now,
		"expires_at":       now.Add(time.Hour),
	}, "unbound only")
	if _, err := Select(env, "project"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("automatic unbound selection error = %v, want not exist", err)
	}
	selected, err := NewTaskScopedResolver(env).Resolve("project", TaskSelector{Ref: &model.Ref{ID: "unbound-session"}})
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceRuntimeSession || selected.TaskID != "" || selected.Runtime == nil {
		t.Fatalf("explicit unbound selection = %+v", selected)
	}
}

func TestRuntimeCheckpointPrecedesRuntimeSession(t *testing.T) {
	env := testEnv(t)
	recorder := runtime.NewRecorder(env.ProjectWT)
	if _, err := recorder.WriteSession(runtime.WriteOptions{
		Scope: "project", ProjectID: "project-test", TaskID: "task-runtime", Title: "Runtime session", Body: "session",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := recorder.WriteCheckpoint(runtime.WriteOptions{
		Scope: "project", ProjectID: "project-test", TaskID: "task-runtime", Title: "Runtime checkpoint", Body: "checkpoint",
	}); err != nil {
		t.Fatal(err)
	}
	selected, err := Select(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if selected.SourceKind != SourceRuntimeCheckpoint || selected.Quality != QualityDegraded {
		t.Fatalf("runtime fallback = %+v", selected)
	}
}

func TestWriteRecoveryDashboardUsesOneRowPerTask(t *testing.T) {
	env := testEnv(t)
	for _, id := range []string{"task-one", "task-two"} {
		if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: id, Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	path, err := WriteRecoveryDashboard(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.ToSlash(path) != filepath.ToSlash(filepath.Join(env.ProjectWT, "runtime", "recovery", "current-state.md")) {
		t.Fatalf("unexpected dashboard path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if strings.Count(text, "| `task-") != 2 || !strings.Contains(text, "| Source | Priority |") {
		t.Fatalf("dashboard does not contain one source/priority row per task:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "current-state.md")); !os.IsNotExist(err) {
		t.Fatalf("durable root current-state.md must not be created")
	}
}

func writeStateRecord(t *testing.T, env paths.Env, dir string, meta map[string]any, body string) {
	t.Helper()
	id, _ := meta["id"].(string)
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.ProjectWT, "state", dir, id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeRuntimeRecord(t *testing.T, env paths.Env, dir string, meta map[string]any, body string) {
	t.Helper()
	id, _ := meta["id"].(string)
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.ProjectWT, "runtime", dir, id+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func initGitRepo(t *testing.T, root string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(strings.Join([]string{
		".worktrail/config.json",
		".worktrail/handoffs/local/",
		".worktrail/logs/",
		".worktrail/ops/",
		".worktrail/index/",
		"",
	}, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "Test"},
		{"add", ".gitignore"},
		{"commit", "-m", "init"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, output)
		}
	}
}
