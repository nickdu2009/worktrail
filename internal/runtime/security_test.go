package runtime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestEnsureDirsRejectsSymlinkAtEveryWritableLevel(t *testing.T) {
	for _, rel := range []string{
		"runtime",
		filepath.Join("runtime", DirSessions),
		"raw",
		filepath.Join("raw", "cursor"),
		"logs",
	} {
		t.Run(filepath.ToSlash(rel), func(t *testing.T) {
			root := filepath.Join(t.TempDir(), ".worktrail")
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			outside := t.TempDir()
			link := filepath.Join(root, rel)
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, link); err != nil {
				t.Fatal(err)
			}
			if err := EnsureDirs(root); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("EnsureDirs() error = %v, want symbolic-link refusal", err)
			}
			entries, err := os.ReadDir(outside)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("EnsureDirs wrote through %s symlink: %v", rel, entries)
			}
		})
	}
}

func TestListDiagnosesSymlinkAndPruneRefusesIt(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.ProjectWT, "runtime", DirSessions, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	result, err := ListWithDiagnostics(env, "project", DirSessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Kind != "symlink" {
		t.Fatalf("ListWithDiagnostics() = %+v", result)
	}
	if _, err := PrunePlan(env, "project", time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "non-regular runtime file") {
		t.Fatalf("PrunePlan() error = %v, want symlink refusal", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside secret" {
		t.Fatalf("outside target changed: %q", data)
	}
}

func TestWriteRecoveryDashboardRejectsSymlinkTarget(t *testing.T) {
	root := testRoot(t)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "runtime", DirRecovery, "current-state.md")
	if err := os.Symlink(outside, target); err != nil {
		t.Fatal(err)
	}
	if _, err := NewRecorder(root).WriteRecoveryDashboard("replacement"); err == nil ||
		!strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("WriteRecoveryDashboard() error = %v, want symlink refusal", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside" {
		t.Fatalf("outside dashboard target changed: %q", data)
	}
}

func TestMissingExpiresAtUsesCreatedAtNotUpdatedAt(t *testing.T) {
	env := paths.Env{ProjectWT: testRoot(t)}
	now := time.Now().UTC()
	createdAt := now.Add(-15 * 24 * time.Hour)
	writeRuntimeFixture(t, env.ProjectWT, DirSessions, "legacy-expired", map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "legacy-expired",
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            "Legacy expired",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"project_id":       "project-1",
		"task_id":          "task-1",
		"created_at":       createdAt,
		"updated_at":       now,
	})
	records, err := List(env, "project", DirSessions)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("List() retained expired legacy runtime: %+v", records)
	}
	plan, err := PrunePlan(env, "project", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].Reason != "expired" || !plan.Items[0].ExpiresAt.Equal(createdAt.Add(RetentionWindow)) {
		t.Fatalf("PrunePlan() = %+v", plan)
	}
}
