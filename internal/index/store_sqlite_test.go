package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFTS5Smoke(t *testing.T) {
	root := t.TempDir()
	db, err := openSQLite(root)
	if err != nil {
		t.Fatalf("openSQLite() error = %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO entry_fts(entry_id, title_terms, body_terms) VALUES ('smoke', '索引', '中文分词')`); err != nil {
		t.Fatalf("fts insert failed: %v", err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM entry_fts WHERE entry_fts MATCH '"索引"'`).Scan(&count); err != nil {
		t.Fatalf("fts match failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("fts count = %d, want 1", count)
	}
}

func TestRefreshUpdatesChangedFile(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	path := filepath.Join(root, "rules", "current.md")
	mustWriteDoc(t, path, map[string]any{"id": "current", "title": "Current"}, "original body")
	now := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	if _, err := Rebuild(root, RebuildOptions{Now: now}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	mustWriteDoc(t, path, map[string]any{"id": "current", "title": "Current"}, "updated body with needle")
	later := now.Add(2 * time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if err := refreshSQLite(root, defaultTokenizer); err != nil {
		t.Fatalf("refreshSQLite() error = %v", err)
	}
	results, err := Search(root, Query{Content: "needle", IncludeContent: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || !strings.Contains(results[0].Entry.Content, "updated") {
		t.Fatalf("refresh search results = %+v", results)
	}
}

func TestRefreshUpdatesGeneratedAtForFilterFresh(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	path := filepath.Join(root, "rules", "current.md")
	mustWriteDoc(t, path, map[string]any{"id": "current", "title": "Current"}, "original body")
	now := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, now)
	if _, err := Rebuild(root, RebuildOptions{Now: now}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	mustWriteDoc(t, path, map[string]any{"id": "current", "title": "Current"}, "updated body")
	later := now.Add(2 * time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if err := refreshSQLite(root, defaultTokenizer); err != nil {
		t.Fatalf("refreshSQLite() error = %v", err)
	}
	db, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	entries, report, err := FilterFresh(root, db)
	if err != nil {
		t.Fatalf("FilterFresh() error = %v", err)
	}
	if report.Stale || len(report.Changed) != 0 {
		t.Fatalf("FilterFresh() should include refreshed entry: report=%+v", report)
	}
	if len(entries) != 1 || !strings.Contains(entries[0].Content, "updated") {
		t.Fatalf("FilterFresh() entries = %+v", entries)
	}
}

func TestRecoverCorruptSQLite(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	path := filepath.Join(root, "rules", "recover.md")
	mustWriteDoc(t, path, map[string]any{
		"id": "recover", "scope": "project", "type": "rule", "title": "Recover Rule",
	}, "recover needle content")
	now := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, now)
	if _, err := Rebuild(root, RebuildOptions{Now: now}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index", SQLiteFile), []byte("not-a-sqlite-db"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := Search(root, Query{Content: "needle", IncludeContent: true})
	if err != nil {
		t.Fatalf("Search() after corrupt db error = %v", err)
	}
	if len(results) != 1 || results[0].Entry.ID != "recover" {
		t.Fatalf("Search() after recover = %+v", results)
	}
	broken, err := filepath.Glob(filepath.Join(root, "index", "index.sqlite.broken-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(broken) != 1 {
		t.Fatalf("expected renamed broken db, got %v", broken)
	}
}

func TestChineseFTSSearch(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "rules", "zh.md"), map[string]any{
		"id": "zh-rule", "scope": "project", "type": "rule", "title": "中文索引规则",
	}, "Worktrail 需要支持中文分词和索引搜索")
	now := time.Date(2026, 6, 6, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, now)
	if _, err := Rebuild(root, RebuildOptions{Now: now}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	results, err := Search(root, Query{Scope: "project", Content: "中文分词"})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 1 || results[0].Entry.ID != "zh-rule" {
		t.Fatalf("Chinese search results = %+v", results)
	}
}
