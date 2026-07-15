package index

import (
	"net"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"
	"time"
)

func TestRebuildRejectsSymlinkedSourceWithoutReadingTarget(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside-index-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "rules", "linked.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root, RebuildOptions{}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Rebuild() error = %v, want symbolic-link refusal", err)
	}
	if _, err := os.Stat(filepath.Join(root, "index", SQLiteFile)); !os.IsNotExist(err) {
		t.Fatalf("rebuild wrote index after symlink refusal: %v", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside-index-secret" {
		t.Fatalf("outside source changed: %q", data)
	}
}

func TestRebuildRejectsNonRegularSource(t *testing.T) {
	if goruntime.GOOS == "windows" {
		t.Skip("unix sockets are unavailable")
	}
	root, err := os.MkdirTemp("/tmp", "worktrail-index-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socketPath := filepath.Join(root, "rules", "source.json")
	if err := os.MkdirAll(filepath.Dir(socketPath), 0o755); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if _, err := Rebuild(root, RebuildOptions{}); err == nil || !strings.Contains(err.Error(), "non-regular") {
		t.Fatalf("Rebuild() error = %v, want non-regular refusal", err)
	}
}

func TestRebuildRejectsSymlinkedIndexDirectory(t *testing.T) {
	root := t.TempDir()
	mustWriteDoc(t, filepath.Join(root, "rules", "safe.md"), map[string]any{
		"id": "safe", "title": "Safe",
	}, "safe body")
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "index")); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(root, RebuildOptions{}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Rebuild() error = %v, want index-dir symlink refusal", err)
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("rebuild wrote outside root through index symlink: %v", entries)
	}
}

func TestStatusRejectsSymlinkedSQLiteArtifact(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "index"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.sqlite")
	if err := os.WriteFile(outside, []byte("outside-index"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "index", SQLiteFile)); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(root); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("Status() error = %v, want sqlite symlink refusal", err)
	}
	data, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "outside-index" {
		t.Fatalf("outside sqlite changed: %q", data)
	}
}

func TestRuntimeIndexExpiryUsesCreatedAtNotUpdatedAt(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	mustWriteDoc(t, filepath.Join(root, "runtime", "sessions", "legacy.md"), map[string]any{
		"schema":           "worktrail.runtime.v2",
		"id":               "legacy",
		"object_kind":      "runtime_record",
		"scope":            "project",
		"runtime_type":     "session_state",
		"title":            "Legacy",
		"durability":       "ephemeral",
		"lifecycle_status": "active",
		"project_id":       "project-1",
		"task_id":          "task-1",
		"created_at":       now.Add(-15 * 24 * time.Hour),
		"updated_at":       now,
	}, "runtime body")
	manifest, err := Rebuild(root, RebuildOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Entries != 0 {
		t.Fatalf("expired runtime was indexed: %+v", manifest)
	}
}
