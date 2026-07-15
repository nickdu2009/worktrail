package runtime

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
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
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"project_id":"project-test"}`), 0o600); err != nil {
		t.Fatal(err)
	}
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
		`"project_id": "project-test"`,
		`"binding_status": "unbound"`,
		`"expires_at":`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in runtime session metadata:\n%s", want, text)
		}
	}
	info, err := os.Stat(record.Path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("runtime file mode = %o, want 600", got)
	}
	dirInfo, err := os.Stat(filepath.Dir(record.Path))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("runtime directory mode = %o, want 700", got)
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

func TestListSkipsMalformedMarkdownAndReturnsDiagnostics(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	writeRuntimeFixture(t, env.ProjectWT, DirSessions, "good", map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "good",
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            "Good",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"project_id":       "project-1",
		"task_id":          "task-1",
		"updated_at":       now,
		"expires_at":       now.Add(time.Hour),
	})
	badPath := filepath.Join(env.ProjectWT, "runtime", DirSessions, "bad.md")
	if err := os.WriteFile(badPath, []byte("---worktrail\n{\"id\":\n---\nbroken"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ListWithDiagnostics(env, "project", DirSessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].ID != "good" {
		t.Fatalf("records = %+v, want good runtime", result.Records)
	}
	if len(result.Diagnostics) != 1 || !strings.HasSuffix(result.Diagnostics[0].Path, "runtime/sessions/bad.md") {
		t.Fatalf("diagnostics = %+v, want malformed runtime diagnostic", result.Diagnostics)
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), "runtime.read_diagnostic") || !strings.Contains(string(events), "runtime/sessions/bad.md") {
		t.Fatalf("runtime diagnostic was not recorded:\n%s", events)
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

func TestWriteSessionMarksFullyUnboundRuntime(t *testing.T) {
	root := testRoot(t)
	record, err := NewRecorder(root).WriteSession(WriteOptions{Title: "Unbound", Body: "body"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(record.Path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"binding_status": "unbound"`, `"project_id"`, `"task_id"`} {
		if !strings.Contains(text, want) {
			t.Fatalf("unbound runtime missing %q:\n%s", want, text)
		}
	}
}

func TestListKeepsLatestFiveValidRecordsPerTask(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		writeRuntimeFixture(t, env.ProjectWT, DirSessions, "record-"+string(rune('a'+i)), map[string]any{
			"schema":           model.SchemaRuntimeV2,
			"id":               "record-" + string(rune('a'+i)),
			"object_kind":      model.ObjectKindRuntime,
			"scope":            "project",
			"runtime_type":     model.RuntimeTypeSessionState,
			"title":            "Record",
			"durability":       model.DurabilityEphemeral,
			"lifecycle_status": model.LifecycleActive,
			"project_id":       "project-1",
			"task_id":          "task-1",
			"created_at":       now.Add(time.Duration(i) * time.Minute),
			"updated_at":       now.Add(time.Duration(i) * time.Minute),
			"expires_at":       now.Add(24 * time.Hour),
		})
	}
	items, err := List(env, "project", DirSessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != LatestPerTaskLimit {
		t.Fatalf("List() returned %d records, want %d", len(items), LatestPerTaskLimit)
	}
	if items[0].Meta["id"] != "record-g" || items[len(items)-1].Meta["id"] != "record-c" {
		t.Fatalf("List() did not retain latest five: %+v", items)
	}
	latest, err := LatestForTask(env, "project", "project-1", "task-1", now)
	if err != nil {
		t.Fatal(err)
	}
	if latest.Meta["id"] != "record-g" {
		t.Fatalf("LatestForTask() = %v, want record-g", latest.Meta["id"])
	}
}

func TestPrunePlanRequiresExplicitApply(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	expiredPath := writeRuntimeFixture(t, env.ProjectWT, DirSessions, "expired", map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "expired",
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            "Expired",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"project_id":       "project-1",
		"task_id":          "task-1",
		"created_at":       now.Add(-15 * 24 * time.Hour),
		"updated_at":       now.Add(-15 * 24 * time.Hour),
		"expires_at":       now.Add(-24 * time.Hour),
	})
	plan, err := PrunePlan(env, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Reason != "expired" {
		t.Fatalf("PrunePlan() = %+v", plan)
	}
	if _, err := os.Stat(expiredPath); err != nil {
		t.Fatalf("PrunePlan removed file before apply: %v", err)
	}
	result, err := ApplyPrune(plan)
	if err != nil {
		t.Fatal(err)
	}
	if result.Deleted != 1 {
		t.Fatalf("ApplyPrune() deleted %d, want 1", result.Deleted)
	}
	if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
		t.Fatalf("expired runtime still exists after apply: %v", err)
	}
}

func TestPrunePlanIncludesRecordsBeyondLatestFivePerTask(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		writeRuntimeFixture(t, env.ProjectWT, DirSessions, "record-"+string(rune('a'+i)), map[string]any{
			"schema":           model.SchemaRuntimeV2,
			"id":               "record-" + string(rune('a'+i)),
			"object_kind":      model.ObjectKindRuntime,
			"scope":            "project",
			"runtime_type":     model.RuntimeTypeSessionState,
			"title":            "Record",
			"durability":       model.DurabilityEphemeral,
			"lifecycle_status": model.LifecycleActive,
			"project_id":       "project-1",
			"task_id":          "task-1",
			"created_at":       now.Add(time.Duration(i) * time.Minute),
			"updated_at":       now.Add(time.Duration(i) * time.Minute),
			"expires_at":       now.Add(24 * time.Hour),
		})
	}
	plan, err := PrunePlan(env, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 2 {
		t.Fatalf("PrunePlan() items = %d, want 2: %+v", len(plan.Items), plan.Items)
	}
	for _, item := range plan.Items {
		if item.Reason != "exceeds_latest_per_task" {
			t.Fatalf("unexpected prune reason: %+v", item)
		}
	}
}

func TestApplyPruneRejectsHashStalePlanWithoutDeleting(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	path := writeRuntimeFixture(t, env.ProjectWT, DirSessions, "expired", expiredRuntimeMeta(now, "expired"))
	plan, err := PrunePlan(env, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].RelPath == "" || plan.Items[0].ContentHash == "" || plan.Items[0].Identity.ID != "expired" {
		t.Fatalf("prune item does not carry stable identity: %+v", plan.Items)
	}
	if err := os.WriteFile(path, []byte("changed after planning"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPrune(plan); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("ApplyPrune() error = %v, want stale plan", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stale plan deleted changed target: %v", err)
	}
}

func TestApplyPruneRejectsSymlinkTarget(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	path := writeRuntimeFixture(t, env.ProjectWT, DirSessions, "expired", expiredRuntimeMeta(now, "expired"))
	plan, err := PrunePlan(env, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyPrune(plan); err == nil || !strings.Contains(err.Error(), "non-regular runtime file") {
		t.Fatalf("ApplyPrune() error = %v, want symlink refusal", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside" {
		t.Fatalf("symlink target changed: %q", data)
	}
}

func TestApplyPrunePartialFailureLeavesReplayableJournal(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	first := writeRuntimeFixture(t, env.ProjectWT, DirSessions, "expired-a", expiredRuntimeMeta(now, "expired-a"))
	second := writeRuntimeFixture(t, env.ProjectWT, DirSessions, "expired-b", expiredRuntimeMeta(now, "expired-b"))
	plan, err := PrunePlan(env, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	coordinator := ops.New(env.ProjectWT)
	coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
		if phase == "commit" && index == 1 {
			return errors.New("injected prune failure")
		}
		return nil
	}
	result, err := applyPruneWithCoordinator(plan, coordinator)
	if err == nil || result.OperationID == "" {
		t.Fatalf("partial ApplyPrune result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("first action was not applied before failure: %v", err)
	}
	if _, err := os.Stat(second); err != nil {
		t.Fatalf("second target should remain pending replay: %v", err)
	}
	replayed, err := ops.New(env.ProjectWT).ReplayPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0] != result.OperationID {
		t.Fatalf("replayed = %v, want %s", replayed, result.OperationID)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("replayed prune left %s: %v", path, err)
		}
	}
}

func expiredRuntimeMeta(now time.Time, id string) map[string]any {
	return map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               id,
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            id,
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"project_id":       "project-1",
		"task_id":          id,
		"created_at":       now.Add(-48 * time.Hour),
		"updated_at":       now.Add(-48 * time.Hour),
		"expires_at":       now.Add(-time.Hour),
	}
}

func writeRuntimeFixture(t *testing.T, root, dir, name string, meta map[string]any) string {
	t.Helper()
	data, err := store.RenderMarkdown(meta, "runtime body")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "runtime", dir, name+".md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
