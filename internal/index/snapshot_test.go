package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/store"
)

func TestFreshSnapshotScansSelectsAndSorts(t *testing.T) {
	root := t.TempDir()
	writeSnapshotDoc(t, filepath.Join(root, "rules", "beta.md"), map[string]any{
		"id":     "beta",
		"scope":  "project",
		"type":   "rule",
		"status": "current",
		"tags":   []string{"beta"},
	}, "beta body")
	writeSnapshotDoc(t, filepath.Join(root, "rules", "alpha.md"), map[string]any{
		"id":              "alpha",
		"scope":           "project",
		"type":            "rule",
		"status":          "current",
		"lifecycle":       "promoted",
		"topic":           "snapshot",
		"source_of_truth": true,
		"supersedes":      []string{"rules/old.md"},
		"superseded_by":   []string{"rules/new.md"},
		"tags":            []string{"zeta", "alpha"},
		"updated_at":      "2026-01-01T00:00:00Z",
	}, "alpha body")

	var received []Entry
	snapshot, err := FreshSnapshot(root, "project", "policy-v1", func(entries []Entry) []Entry {
		received = append([]Entry(nil), entries...)
		return []Entry{entries[1], entries[0]}
	})
	if err != nil {
		t.Fatalf("FreshSnapshot() error = %v", err)
	}
	if len(received) != 2 {
		t.Fatalf("selector received %d entries, want 2", len(received))
	}
	if snapshot.Schema != snapshotSchema || snapshot.Scope != "project" || snapshot.PolicyVersion != "policy-v1" {
		t.Fatalf("unexpected snapshot metadata: %+v", snapshot)
	}
	if len(snapshot.Entries) != 2 {
		t.Fatalf("snapshot entries = %d, want 2", len(snapshot.Entries))
	}
	if snapshot.Entries[0].Path != "rules/alpha.md" || snapshot.Entries[1].Path != "rules/beta.md" {
		t.Fatalf("snapshot entries are not sorted by path: %+v", snapshot.Entries)
	}
	if got, want := snapshot.Entries[0].Tags, []string{"alpha", "zeta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tags = %v, want %v", got, want)
	}
	if snapshot.Entries[0].ContentHash != contentHash("alpha body") {
		t.Fatalf("content hash = %q, want hash of alpha body", snapshot.Entries[0].ContentHash)
	}
}

func TestFreshSourceSnapshotMatchesFreshSnapshotAndClonesSelectedEntries(t *testing.T) {
	root := t.TempDir()
	body := "  alpha body  \n  "
	writeRawSnapshotDoc(t, filepath.Join(root, "rules", "alpha.md"), map[string]any{
		"id":         "alpha",
		"scope":      "project",
		"type":       "rule",
		"status":     "current",
		"tags":       []string{"alpha"},
		"updated_at": "2026-01-01T00:00:00Z",
	}, body)
	writeSnapshotDoc(t, filepath.Join(root, "rules", "beta.md"), map[string]any{
		"id":         "beta",
		"scope":      "project",
		"type":       "rule",
		"status":     "current",
		"tags":       []string{"beta"},
		"updated_at": "2026-01-02T00:00:00Z",
	}, "beta body")

	var retainedSelectorEntries []Entry
	selector := func(entries []Entry) []Entry {
		retainedSelectorEntries = entries
		return []Entry{entries[1], entries[0]}
	}
	sourceSnapshot, err := FreshSourceSnapshot(root, "project", "policy-v1", selector)
	if err != nil {
		t.Fatalf("FreshSourceSnapshot() error = %v", err)
	}
	sourceSelectorEntries := retainedSelectorEntries
	snapshot, err := FreshSnapshot(root, "project", "policy-v1", selector)
	if err != nil {
		t.Fatalf("FreshSnapshot() error = %v", err)
	}
	if sourceSnapshot.Snapshot.SnapshotHash != snapshot.SnapshotHash {
		t.Fatalf("snapshot hash = %q, want %q", sourceSnapshot.Snapshot.SnapshotHash, snapshot.SnapshotHash)
	}
	if got, want := sourceSnapshot.Snapshot.Entries[0].ContentHash, contentHash("alpha body"); got != want {
		t.Fatalf("snapshot content hash = %q, want hash of trimmed entry content", got)
	}
	if got, want := entryIDs(sourceSnapshot.Entries), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source entries IDs = %v, want %v", got, want)
	}
	if got, want := recordIDs(sourceSnapshot.Records), []string{"alpha", "beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source record IDs = %v, want %v", got, want)
	}
	if got, want := sourceSnapshot.Records[0].Body, body; got != want {
		t.Fatalf("source record body = %q, want %q", got, want)
	}

	sourceSelectorEntries[0].Content = "mutated"
	sourceSelectorEntries[0].Tags[0] = "mutated"
	if got, want := sourceSnapshot.Entries[1].Tags, []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source entry tags changed through selector slice: got %v, want %v", got, want)
	}
	if got, want := sourceSnapshot.Records[1].Entry.Tags, []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source record tags changed through selector slice: got %v, want %v", got, want)
	}
	sourceSnapshot.Entries[1].Tags[0] = "mutated-by-caller"
	if got, want := sourceSnapshot.Records[1].Entry.Tags, []string{"beta"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("source record tags changed through entries slice: got %v, want %v", got, want)
	}
}

func TestFreshSourceSnapshotReturnsNoPartialResultWhenBodyReadFails(t *testing.T) {
	root := t.TempDir()
	writeSnapshotDoc(t, filepath.Join(root, "rules", "current.md"), map[string]any{
		"id":     "current",
		"scope":  "project",
		"type":   "rule",
		"status": "current",
	}, "current body")

	sourceSnapshot, err := FreshSourceSnapshot(root, "project", "policy-v1", func(entries []Entry) []Entry {
		entries[0].Path = "rules/missing.md"
		return entries
	})
	if err == nil {
		t.Fatal("FreshSourceSnapshot() succeeded with a missing selected source")
	}
	if sourceSnapshot.Snapshot.SnapshotHash != "" || len(sourceSnapshot.Entries) != 0 || len(sourceSnapshot.Records) != 0 {
		t.Fatalf("FreshSourceSnapshot() returned a partial result: %+v", sourceSnapshot)
	}
}

func TestFreshSourceSnapshotAcceptsMarkdownWithoutFrontmatter(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "index.md")
	if err := os.WriteFile(path, []byte("# Overview\n\nseed body\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	sourceSnapshot, err := FreshSourceSnapshot(root, "project", "policy-v1", func(entries []Entry) []Entry {
		return []Entry{{
			ID:    "index",
			Scope: "project",
			Path:  "index.md",
			Type:  "index",
		}}
	})
	if err != nil {
		t.Fatalf("FreshSourceSnapshot() error = %v", err)
	}
	if got, want := sourceSnapshot.Records[0].Body, "# Overview\n\nseed body\n"; got != want {
		t.Fatalf("source body = %q, want %q", got, want)
	}
}

func TestSemanticSnapshotHashTracksRawBodyAndIgnoresUpdatedAtAndOrder(t *testing.T) {
	root := t.TempDir()
	alphaPath := filepath.Join(root, "rules", "alpha.md")
	betaPath := filepath.Join(root, "rules", "beta.md")
	writeRawSnapshotDoc(t, alphaPath, map[string]any{
		"id":         "alpha",
		"scope":      "project",
		"type":       "rule",
		"status":     "current",
		"updated_at": "2026-01-01T00:00:00Z",
	}, "  body  ")
	writeRawSnapshotDoc(t, betaPath, map[string]any{
		"id":     "beta",
		"scope":  "project",
		"type":   "rule",
		"status": "current",
	}, "beta body")

	first, err := FreshSourceSnapshot(root, "project", "policy-v1", func(entries []Entry) []Entry {
		return entries
	})
	if err != nil {
		t.Fatalf("FreshSourceSnapshot() error = %v", err)
	}

	writeRawSnapshotDoc(t, alphaPath, map[string]any{
		"id":         "alpha",
		"scope":      "project",
		"type":       "rule",
		"status":     "current",
		"updated_at": "2026-01-02T00:00:00Z",
	}, " body ")
	second, err := FreshSourceSnapshot(root, "project", "policy-v1", func(entries []Entry) []Entry {
		return []Entry{entries[1], entries[0]}
	})
	if err != nil {
		t.Fatalf("FreshSourceSnapshot() error = %v", err)
	}
	if first.Snapshot.SnapshotHash != second.Snapshot.SnapshotHash {
		t.Fatalf("lexical snapshot hash changed for raw whitespace only: first=%q second=%q", first.Snapshot.SnapshotHash, second.Snapshot.SnapshotHash)
	}
	if first.SemanticSnapshotHash == second.SemanticSnapshotHash {
		t.Fatal("semantic snapshot hash did not change for raw body whitespace")
	}

	writeRawSnapshotDoc(t, alphaPath, map[string]any{
		"id":         "alpha",
		"scope":      "project",
		"type":       "rule",
		"status":     "current",
		"updated_at": "2026-01-03T00:00:00Z",
	}, " body ")
	third, err := FreshSourceSnapshot(root, "project", "policy-v1", func(entries []Entry) []Entry {
		return entries
	})
	if err != nil {
		t.Fatalf("FreshSourceSnapshot() error = %v", err)
	}
	if second.SemanticSnapshotHash != third.SemanticSnapshotHash {
		t.Fatalf("semantic snapshot hash changed for updated_at or source order: second=%q third=%q", second.SemanticSnapshotHash, third.SemanticSnapshotHash)
	}
}

func TestFreshSnapshotRejectsNilSelectorAndInvalidScope(t *testing.T) {
	root := t.TempDir()
	selector := func(entries []Entry) []Entry { return entries }

	if _, err := FreshSnapshot(root, "project", "policy-v1", nil); err == nil {
		t.Fatal("FreshSnapshot() with nil selector succeeded")
	}
	if _, err := FreshSnapshot(root, "workspace", "policy-v1", selector); err == nil {
		t.Fatal("FreshSnapshot() with invalid scope succeeded")
	}
}

func TestSnapshotHashExcludesUpdatedAtAndIncludesFingerprintFields(t *testing.T) {
	entry := snapshotTestEntry()
	base, err := newSnapshot("project", "policy-v1", []Entry{entry})
	if err != nil {
		t.Fatalf("newSnapshot() error = %v", err)
	}

	updated := entry
	updated.UpdatedAt = updated.UpdatedAt.Add(24 * time.Hour)
	updatedSnapshot, err := newSnapshot("project", "policy-v1", []Entry{updated})
	if err != nil {
		t.Fatalf("newSnapshot() error = %v", err)
	}
	if updatedSnapshot.SnapshotHash != base.SnapshotHash {
		t.Fatalf("updated_at changed snapshot hash: got %q, want %q", updatedSnapshot.SnapshotHash, base.SnapshotHash)
	}

	bodyChanged := entry
	bodyChanged.Content = "changed body"
	assertSnapshotHashChanged(t, base, bodyChanged)

	metadataChanged := entry
	metadataChanged.Topic = "changed-topic"
	assertSnapshotHashChanged(t, base, metadataChanged)
}

func TestSnapshotHashIsIndependentOfInputOrder(t *testing.T) {
	alpha := snapshotTestEntry()
	alpha.ID = "alpha"
	alpha.Path = "rules/alpha.md"
	beta := snapshotTestEntry()
	beta.ID = "beta"
	beta.Path = "rules/beta.md"

	ordered, err := newSnapshot("project", "policy-v1", []Entry{alpha, beta})
	if err != nil {
		t.Fatalf("newSnapshot() error = %v", err)
	}
	reversedEntries := []Entry{beta, alpha}
	reversed, err := newSnapshot("project", "policy-v1", reversedEntries)
	if err != nil {
		t.Fatalf("newSnapshot() error = %v", err)
	}
	if ordered.SnapshotHash != reversed.SnapshotHash {
		t.Fatalf("hash depends on input order: ordered=%q reversed=%q", ordered.SnapshotHash, reversed.SnapshotHash)
	}
	if reversedEntries[0].ID != "beta" || reversedEntries[1].ID != "alpha" {
		t.Fatalf("newSnapshot() modified input order: %+v", reversedEntries)
	}
}

func assertSnapshotHashChanged(t *testing.T, base Snapshot, entry Entry) {
	t.Helper()
	changed, err := newSnapshot("project", "policy-v1", []Entry{entry})
	if err != nil {
		t.Fatalf("newSnapshot() error = %v", err)
	}
	if changed.SnapshotHash == base.SnapshotHash {
		t.Fatal("fingerprint change did not change snapshot hash")
	}
}

func snapshotTestEntry() Entry {
	return Entry{
		ID:            "rule",
		Scope:         "project",
		Type:          "rule",
		Path:          "rules/rule.md",
		Status:        "current",
		Lifecycle:     "promoted",
		Topic:         "snapshot",
		SourceOfTruth: true,
		Supersedes:    []string{"rules/old.md"},
		SupersededBy:  []string{"rules/new.md"},
		Tags:          []string{"beta", "alpha"},
		Content:       "original body",
		UpdatedAt:     time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Active:        true,
	}
}

func entryIDs(entries []Entry) []string {
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	return ids
}

func recordIDs(records []SourceRecord) []string {
	ids := make([]string, len(records))
	for i, record := range records {
		ids[i] = record.Entry.ID
	}
	return ids
}

func writeSnapshotDoc(t *testing.T, path string, meta any, body string) {
	t.Helper()
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRawSnapshotDoc(t *testing.T, path string, meta any, body string) {
	t.Helper()
	rawMeta, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := []byte(store.Marker + "\n" + string(rawMeta) + "\n---\n\n" + body)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
