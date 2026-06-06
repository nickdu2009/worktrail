package index

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSearchIncludeContentContract(t *testing.T) {
	entries := []Entry{
		{ID: "rule", Scope: "project", Type: "rule", Title: "Rule", Content: "needle body", UpdatedAt: time.Now().UTC()},
	}
	withContent := SearchEntries(entries, Query{Scope: "project", Content: "needle", IncludeContent: true})
	if len(withContent) != 1 || withContent[0].Entry.Content == "" {
		t.Fatalf("IncludeContent=true should keep body: %+v", withContent)
	}
	withoutContent := SearchEntries(entries, Query{Scope: "project", Content: "needle", IncludeContent: false})
	if len(withoutContent) != 1 || withoutContent[0].Entry.Content != "" {
		t.Fatalf("IncludeContent=false should clear body: %+v", withoutContent)
	}
}

func TestSQLiteRebuildLoadRoundTrip(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "rules", "alpha.md"), map[string]any{
		"id": "alpha", "scope": "project", "type": "rule", "title": "Alpha", "tags": []string{"api"},
	}, "alpha needle content")
	mustWriteDoc(t, filepath.Join(root, "handoffs", "next.md"), map[string]any{
		"id": "handoff", "scope": "project", "type": "handoff", "title": "Next",
	}, "handoff summary body")

	now := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, now)
	manifest, err := Rebuild(root, RebuildOptions{Now: now})
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if manifest.Entries != 2 {
		t.Fatalf("manifest entries = %d, want 2", manifest.Entries)
	}

	scanned, err := scan(root, "project")
	if err != nil {
		t.Fatalf("scan() error = %v", err)
	}
	loaded, err := loadSQLite(root)
	if err != nil {
		t.Fatalf("loadSQLite() error = %v", err)
	}
	if len(scanned) != len(loaded.Entries) {
		t.Fatalf("scan/load entry counts differ: scan=%d load=%d", len(scanned), len(loaded.Entries))
	}
}
