package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestLifecycleWritesMarkdownLogsAndHandoff(t *testing.T) {
	env := testEnv(t)
	restoreNow := freezeNow(time.Date(2026, 5, 14, 10, 0, 0, 0, time.UTC))
	defer restoreNow()

	started, err := Start(env, StartOptions{
		Scope:          "project",
		ID:             "st_test",
		Type:           "task",
		Title:          "Test State",
		SourceTool:     "codex",
		SourceSessions: []string{"sess-1"},
		Tags:           []string{"alpha"},
		Body:           "# Current\n\nInitial body.",
		Actor:          "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if started.Path != filepath.Join(env.ProjectWT, "state", "active", "st_test.md") {
		t.Fatalf("active path = %s", started.Path)
	}
	if started.State.Schema != model.SchemaState || started.State.Status != "active" {
		t.Fatalf("unexpected started state: %+v", started.State)
	}
	assertMarkdownMeta(t, started.Path, "st_test", "active")
	assertMarkdownMeta(t, filepath.Join(env.ProjectWT, "state", "active", "latest.md"), "st_test", "active")

	updatedBody := "# Current\n\nReplaced body."
	updated, err := Update(env, UpdateOptions{
		Scope:       "project",
		ID:          "st_test",
		Status:      "active",
		ReplaceBody: &updatedBody,
		AppendBody:  "Follow-up note.",
		Actor:       "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(updated.Body, "Replaced body.") || !strings.Contains(updated.Body, "Follow-up note.") {
		t.Fatalf("updated body = %q", updated.Body)
	}

	checkpoint, err := Checkpoint(env, CheckpointOptions{Scope: "project", ID: "st_test", Note: "before close", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.Directory != DirCheckpoints || checkpoint.Checkpoint == "" {
		t.Fatalf("checkpoint = %+v", checkpoint)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "checkpoints", checkpoint.Checkpoint+".md")); err != nil {
		t.Fatalf("checkpoint file: %v", err)
	}

	injected, err := Inject(env, InjectOptions{Scope: "project", ID: "st_test", Title: "Context", Body: "Injected context.", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(injected.Body, "## Context") || !strings.Contains(injected.Body, "Injected context.") {
		t.Fatalf("injected body = %q", injected.Body)
	}

	closed, err := Close(env, CloseOptions{Scope: "project", ID: "st_test", Summary: "Ready for the next agent.", Handoff: true, Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Capsule.State.Status != "closed" {
		t.Fatalf("closed status = %s", closed.Capsule.State.Status)
	}
	if closed.Capsule.Directory != DirArchived || closed.Capsule.Path != filepath.Join(env.ProjectWT, "state", "archived", "st_test.md") {
		t.Fatalf("closed capsule = %+v", closed.Capsule)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "candidates")); !os.IsNotExist(err) {
		t.Fatalf("close should not write candidates, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "st_test.md")); !os.IsNotExist(err) {
		t.Fatalf("close should remove active state file, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("close should remove latest alias, err=%v", err)
	}

	listed, err := List(env, ListOptions{Scope: "project", Directory: "all"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 2 {
		t.Fatalf("list all length = %d, want 2", len(listed))
	}

	archived, err := Archive(env, ArchiveOptions{Scope: "project", ID: "st_test", Actor: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if archived.Directory != DirArchived || archived.State.Status != "archived" {
		t.Fatalf("archived = %+v", archived)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "st_test.md")); !os.IsNotExist(err) {
		t.Fatalf("active should be removed, stat err=%v", err)
	}
	if _, err := Show(env, ShowOptions{Scope: "project", Directory: DirArchived, ID: "st_test"}); err != nil {
		t.Fatalf("show archived: %v", err)
	}

	logs, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range []string{"state.start", "state.update", "state.checkpoint", "state.inject", "state.close", "state.archive"} {
		if !strings.Contains(string(logs), event) {
			t.Fatalf("missing log event %q in %s", event, string(logs))
		}
	}
}

func TestStateIDRejectsPathEscape(t *testing.T) {
	env := testEnv(t)
	if _, err := Start(env, StartOptions{ID: "../bad", Title: "Bad"}); err == nil {
		t.Fatal("expected path separator error")
	}
	if _, err := Show(env, ShowOptions{ID: "../bad"}); err == nil {
		t.Fatal("expected path separator error")
	}
}

func TestStartCleansUpOnLatestAliasFailure(t *testing.T) {
	env := testEnv(t)
	if _, err := Start(env, StartOptions{Scope: "project", ID: "st_ok", Title: "Okay", Actor: "test"}); err != nil {
		t.Fatal(err)
	}
	latestPath := filepath.Join(env.ProjectWT, "state", "active", "latest.md")
	if err := os.Remove(latestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(latestPath, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Start(env, StartOptions{Scope: "project", ID: "st_fail", Title: "Fails", Actor: "test"}); err == nil {
		t.Fatal("expected latest alias sync failure")
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "st_fail.md")); !os.IsNotExist(err) {
		t.Fatalf("failed start should not leave active file, err=%v", err)
	}
}

func testEnv(t *testing.T) paths.Env {
	t.Helper()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return paths.Env{
		UserRoot:    filepath.Join(root, "user"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func assertMarkdownMeta(t *testing.T, path, id, status string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := store.ParseMarkdown(b)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Meta["schema"] != model.SchemaState {
		t.Fatalf("schema = %v", doc.Meta["schema"])
	}
	if doc.Meta["id"] != id || doc.Meta["status"] != status {
		t.Fatalf("meta = %+v", doc.Meta)
	}
}

func freezeNow(start time.Time) func() {
	old := now
	current := start
	now = func() time.Time {
		out := current
		current = current.Add(time.Second)
		return out
	}
	return func() {
		now = old
	}
}
