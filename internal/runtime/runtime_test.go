package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func testRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project", ".worktrail")
	if err := EnsureDirs(root); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWriteSessionUsesRuntimeV2Metadata(t *testing.T) {
	root := testRoot(t)
	rec := NewRecorder(root)
	record, err := rec.WriteSession(WriteOptions{
		Scope:      "project",
		Title:      "Hook task",
		Body:       "# Runtime Session: Hook task\n",
		SourceTool: "cursor",
		Event:      "stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(record.Path, filepath.Join("runtime", "sessions")) {
		t.Fatalf("unexpected session path: %s", record.Path)
	}
	data, err := os.ReadFile(record.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		model.SchemaRuntimeV2,
		model.RuntimeTypeSessionState,
		model.ResumePriorityHookRuntimeState,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in runtime session metadata:\n%s", want, text)
		}
	}
}

func TestWriteRecoveryDashboardOnlyUnderRuntime(t *testing.T) {
	root := testRoot(t)
	path, err := NewRecorder(root).WriteRecoveryDashboard("# Recovery\n")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "runtime", "recovery", "current-state.md")
	if filepath.Clean(path) != filepath.Clean(want) {
		t.Fatalf("dashboard path = %s, want %s", path, want)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(root), "current-state.md")); !os.IsNotExist(err) {
		t.Fatalf("durable root current-state.md must not exist")
	}
}

func TestListLatestSession(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	rec := NewRecorder(env.ProjectWT)
	if _, err := rec.WriteSession(WriteOptions{Title: "First", Body: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.WriteSession(WriteOptions{Title: "Second", Body: "two", FileSuffix: "second"}); err != nil {
		t.Fatal(err)
	}
	latest, err := Latest(env, "project", DirSessions)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(latest.Body) != "two" {
		t.Fatalf("Latest session body = %q", latest.Body)
	}
}

func TestWriteSessionWithSameSuffixKeepsDistinctFiles(t *testing.T) {
	root := testRoot(t)
	rec := NewRecorder(root)
	first, err := rec.WriteSession(WriteOptions{
		Title:      "Shared suffix",
		Body:       "one",
		SessionID:  "session-a",
		SourceTool: "cursor",
		FileSuffix: "20260606-000000-stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := rec.WriteSession(WriteOptions{
		Title:      "Shared suffix",
		Body:       "two",
		SessionID:  "session-b",
		SourceTool: "cursor",
		FileSuffix: "20260606-000000-stop",
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Path == second.Path {
		t.Fatalf("runtime paths must be unique, got %s", first.Path)
	}
	records, err := filepath.Glob(filepath.Join(root, "runtime", "sessions", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("runtime records = %d, want 2", len(records))
	}
}
