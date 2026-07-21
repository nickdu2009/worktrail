package index

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEntryResultMarshalOmitsZeroTimestamps(t *testing.T) {
	zeroEntry := Entry{
		Schema:    "worktrail.index.entry.v1",
		ID:        "e1",
		Scope:     "project",
		Type:      "rule",
		Path:      "rules/e1.md",
		Title:     "E1",
		Content:   "",
		UpdatedAt: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	}
	for _, name := range []string{"entry", "result"} {
		t.Run(name+"_zero", func(t *testing.T) {
			var raw []byte
			var err error
			if name == "entry" {
				raw, err = json.Marshal(zeroEntry)
			} else {
				raw, err = json.Marshal(Result{Entry: zeroEntry, Score: 1.0})
			}
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			s := string(raw)
			if strings.Contains(s, `"created_at"`) {
				t.Fatalf("zero CreatedAt must omit created_at: %s", s)
			}
			if strings.Contains(s, `"expires_at"`) {
				t.Fatalf("zero ExpiresAt must omit expires_at: %s", s)
			}
		})
	}

	nonZero := zeroEntry
	nonZero.CreatedAt = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	nonZero.ExpiresAt = time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	for _, name := range []string{"entry", "result"} {
		t.Run(name+"_nonzero", func(t *testing.T) {
			var raw []byte
			var err error
			if name == "entry" {
				raw, err = json.Marshal(nonZero)
			} else {
				raw, err = json.Marshal(Result{Entry: nonZero, Score: 1.0})
			}
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			s := string(raw)
			if !strings.Contains(s, `"created_at":"2026-07-01T12:00:00Z"`) {
				t.Fatalf("non-zero CreatedAt must be present: %s", s)
			}
			if !strings.Contains(s, `"expires_at":"2026-08-01T12:00:00Z"`) {
				t.Fatalf("non-zero ExpiresAt must be present: %s", s)
			}
		})
	}
}

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
