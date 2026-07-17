package policy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestFreshSourcesMatchesSnapshotAndExcludesCandidates(t *testing.T) {
	root := t.TempDir()
	formalPath := filepath.Join("rules", "current.md")
	candidatePath := filepath.Join("candidates", "project", "candidate.md")
	formalBody := "  formal content  \n  "
	writeRawSourceDoc(t, root, formalPath, "formal", "project", formalBody)
	writeSnapshotDoc(t, root, candidatePath, "candidate", "project")

	sources, err := FreshSources(root, "project")
	if err != nil {
		t.Fatalf("FreshSources() error = %v", err)
	}
	snapshot, err := FreshSnapshot(root, "project")
	if err != nil {
		t.Fatalf("FreshSnapshot() error = %v", err)
	}
	if !reflect.DeepEqual(sources.Snapshot, snapshot) {
		t.Fatalf("source snapshot = %+v, want %+v", sources.Snapshot, snapshot)
	}
	if sources.SemanticSnapshotHash == "" {
		t.Fatal("semantic snapshot hash is empty")
	}
	if got, want := sourcePaths(sources.Entries), []string{filepath.ToSlash(formalPath)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source paths = %v, want %v", got, want)
	}
	if got, want := sourceRecordPaths(sources.Records), []string{filepath.ToSlash(formalPath)}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source record paths = %v, want %v", got, want)
	}
	if got, want := sources.Records[0].Body, formalBody; got != want {
		t.Fatalf("source body = %q, want %q", got, want)
	}
	start := strings.Index(formalBody, "content")
	if got, want := sources.Records[0].Body[start:start+len("content")], "content"; got != want {
		t.Fatalf("body slice = %q, want %q", got, want)
	}

	writeRawSourceDoc(t, root, candidatePath, "candidate", "project", "  changed candidate body  ")
	afterCandidateChange, err := FreshSources(root, "project")
	if err != nil {
		t.Fatalf("FreshSources() after candidate change error = %v", err)
	}
	if afterCandidateChange.SemanticSnapshotHash != sources.SemanticSnapshotHash {
		t.Fatalf("semantic snapshot hash changed for excluded candidate: before=%q after=%q", sources.SemanticSnapshotHash, afterCandidateChange.SemanticSnapshotHash)
	}
}

func sourcePaths(entries []index.Entry) []string {
	paths := make([]string, len(entries))
	for i, entry := range entries {
		paths[i] = entry.Path
	}
	return paths
}

func sourceRecordPaths(records []index.SourceRecord) []string {
	paths := make([]string, len(records))
	for i, record := range records {
		paths[i] = record.Entry.Path
	}
	return paths
}

func writeRawSourceDoc(t *testing.T, root, relativePath, id, scope, body string) {
	t.Helper()
	rawMeta, err := json.Marshal(map[string]string{
		"id":    id,
		"scope": scope,
		"type":  "rule",
		"title": id,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	data := []byte(store.Marker + "\n" + string(rawMeta) + "\n---\n\n" + body)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}
