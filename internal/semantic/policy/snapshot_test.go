package policy

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nickdu2009/worktrail/internal/index"
)

func TestFreshSnapshotSelectsCurrentFormalKnowledge(t *testing.T) {
	tests := []struct {
		name       string
		scope      string
		formalPath string
	}{
		{name: "project", scope: "project", formalPath: "rules/current.md"},
		{name: "user", scope: "user", formalPath: "profile/preferences.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			writeSnapshotDoc(t, root, tt.formalPath, "formal", tt.scope)
			candidatePath := filepath.Join("candidates", tt.scope, "candidate.md")
			writeSnapshotDoc(t, root, candidatePath, "candidate", tt.scope)

			snapshot, err := FreshSnapshot(root, tt.scope)
			if err != nil {
				t.Fatalf("FreshSnapshot() error = %v", err)
			}
			if snapshot.PolicyVersion != Version {
				t.Fatalf("snapshot policy version = %q, want %q", snapshot.PolicyVersion, Version)
			}
			if !snapshotHasPath(snapshot.Entries, tt.formalPath) {
				t.Fatalf("snapshot entries = %+v, want formal knowledge", snapshot.Entries)
			}
			if snapshotHasPath(snapshot.Entries, candidatePath) {
				t.Fatalf("snapshot entries = %+v, must exclude candidate knowledge", snapshot.Entries)
			}
		})
	}
}

func writeSnapshotDoc(t *testing.T, root, relativePath, id, scope string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	content := "---\n" +
		"id: " + id + "\n" +
		"scope: " + scope + "\n" +
		"type: rule\n" +
		"title: " + id + "\n" +
		"---\n\n" +
		id + " content\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func snapshotHasPath(entries []index.EntryFingerprint, path string) bool {
	for _, entry := range entries {
		if entry.Path == filepath.ToSlash(path) {
			return true
		}
	}
	return false
}
