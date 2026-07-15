package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/store"
)

func TestRebuildStatusSearch(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "decisions", "api.md"), map[string]any{
		"id":              "decision-api",
		"scope":           "project",
		"type":            "decision",
		"title":           "API Decision",
		"stage":           "decision",
		"topic":           "api",
		"source_of_truth": true,
		"supersedes":      []string{"architecture/old-api.md"},
		"updated_at":      "2026-05-01T00:00:00Z",
		"tags":            []string{"api", "search"},
	}, "needle in an older decision")
	mustWriteDoc(t, filepath.Join(root, "requirements", "prd.md"), map[string]any{
		"id":         "requirement-prd",
		"scope":      "project",
		"title":      "PRD",
		"stage":      "requirements",
		"updated_at": "2026-05-01T12:00:00Z",
	}, "Primary user scope and MVP boundary.")
	mustWriteDoc(t, filepath.Join(root, "state", "active", "today.md"), map[string]any{
		"id":         "active-state",
		"scope":      "project",
		"type":       "state",
		"title":      "Active State",
		"status":     "active",
		"tags":       []string{"api"},
		"updated_at": "2026-05-02T00:00:00Z",
	}, "needle in active work")
	mustWriteDoc(t, filepath.Join(root, "candidates", "project", "draft.md"), map[string]any{
		"id":             "candidate-draft",
		"scope":          "project",
		"candidate_type": "rule",
		"title":          "Candidate Draft",
		"status":         "pending",
		"tags":           []string{"api"},
	}, "candidate body")

	rebuildAt := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, rebuildAt)
	manifest, err := Rebuild(root, RebuildOptions{Now: rebuildAt})
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if manifest.Entries != 4 {
		t.Fatalf("manifest entries = %d, want 4", manifest.Entries)
	}
	if _, err := os.Stat(filepath.Join(root, "index", SQLiteFile)); err != nil {
		t.Fatalf("sqlite index missing: %v", err)
	}

	status, err := Status(root)
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if !status.Exists || status.Entries != 4 || status.Scope != "project" {
		t.Fatalf("unexpected status: %+v", status)
	}

	results, err := Search(root, Query{Content: "needle", Tags: []string{"api"}, IncludeContent: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].Entry.ID != "active-state" {
		t.Fatalf("first result = %q, want active-state", results[0].Entry.ID)
	}

	candidates, err := Search(root, Query{Type: "candidate"})
	if err != nil {
		t.Fatalf("Search(candidate) error = %v", err)
	}
	if len(candidates) != 1 || candidates[0].Entry.CandidateType != "rule" {
		t.Fatalf("candidate result = %+v", candidates)
	}

	requirements, err := Search(root, Query{Type: "requirement"})
	if err != nil {
		t.Fatalf("Search(requirement) error = %v", err)
	}
	if len(requirements) != 1 || requirements[0].Entry.Stage != "requirements" {
		t.Fatalf("requirement metadata not indexed: %+v", requirements)
	}
	decisions, err := Search(root, Query{Type: "decision"})
	if err != nil {
		t.Fatalf("Search(decision) error = %v", err)
	}
	if len(decisions) != 1 || !decisions[0].Entry.SourceOfTruth || decisions[0].Entry.Topic != "api" || len(decisions[0].Entry.Supersedes) != 1 {
		t.Fatalf("decision governance metadata not indexed: %+v", decisions)
	}
}

func TestRebuildCanonicalizesLegacyADRCandidateType(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "candidates", "project", "legacy-adr.md"), map[string]any{
		"schema":           "worktrail.candidate.v1",
		"id":               "legacy-adr",
		"scope":            "project",
		"candidate_type":   "adr",
		"target_path":      "decisions/ADR-0001-choice.md",
		"title":            "Choice",
		"operation":        "replace",
		"status":           "pending",
		"redaction_status": "clean",
	}, "legacy ADR body")
	rebuildAt := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, rebuildAt)
	if _, err := Rebuild(root, RebuildOptions{Now: rebuildAt}); err != nil {
		t.Fatal(err)
	}
	results, err := Search(root, Query{Type: "candidate"})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Entry.CandidateType != "decision" {
		t.Fatalf("unexpected candidate index: %+v", results)
	}
}

func TestDiffHealthAndFilterFresh(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "rules", "current.md"), map[string]any{
		"id":    "current-rule",
		"title": "Current Rule",
	}, "current body")
	mustWriteDoc(t, filepath.Join(root, "rules", "deleted.md"), map[string]any{
		"id":    "deleted-rule",
		"title": "Deleted Rule",
	}, "deleted body")

	rebuildAt := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if _, err := Rebuild(root, RebuildOptions{Now: rebuildAt}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}

	if err := os.Remove(filepath.Join(root, "rules", "deleted.md")); err != nil {
		t.Fatal(err)
	}
	mustWriteDoc(t, filepath.Join(root, "rules", "new.md"), map[string]any{
		"id":    "new-rule",
		"title": "New Rule",
	}, "new body")
	later := rebuildAt.Add(2 * time.Hour)
	currentPath := filepath.Join(root, "rules", "current.md")
	if err := os.Chtimes(currentPath, later, later); err != nil {
		t.Fatal(err)
	}
	newPath := filepath.Join(root, "rules", "new.md")
	if err := os.Chtimes(newPath, later, later); err != nil {
		t.Fatal(err)
	}

	report, err := Diff(root)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	if !report.Stale || report.Summary.Deleted != 1 || report.Summary.Unindexed != 1 || report.Summary.New != 1 || report.Summary.Changed != 1 {
		t.Fatalf("unexpected diff report: %+v", report)
	}
	if report.Deleted[0].Path != "rules/deleted.md" || report.Unindexed[0].Path != "rules/new.md" || report.Changed[0].Path != "rules/current.md" {
		t.Fatalf("unexpected diff paths: %+v", report)
	}

	health, err := Health(root)
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if !health.Stale || health.IndexedEntries != 2 || health.FreshEntries != 0 || len(health.MissingFromFS) != 1 || len(health.MissingFromIndex) != 1 || len(health.Changed) != 1 {
		t.Fatalf("unexpected health report: %+v", health)
	}

	db, err := Load(root)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	fresh, freshReport, err := FilterFresh(root, db)
	if err != nil {
		t.Fatalf("FilterFresh() error = %v", err)
	}
	if len(fresh) != 0 || !freshReport.Stale || freshReport.IndexedEntries != 2 || len(freshReport.Deleted) != 1 || len(freshReport.Changed) != 1 {
		t.Fatalf("unexpected fresh filter result: fresh=%+v report=%+v", fresh, freshReport)
	}
}

func TestRankSearchResultsLimitsGlobally(t *testing.T) {
	now := time.Now().UTC()
	results := []Result{
		{Entry: Entry{ID: "low", UpdatedAt: now}, Score: 1},
		{Entry: Entry{ID: "high", UpdatedAt: now}, Score: 9},
		{Entry: Entry{ID: "mid", UpdatedAt: now}, Score: 5},
	}
	ranked := RankSearchResults(results, 2)
	if len(ranked) != 2 || ranked[0].Entry.ID != "high" || ranked[1].Entry.ID != "mid" {
		t.Fatalf("RankSearchResults() = %+v", ranked)
	}
}

func TestSearchEntriesUsesProvidedEntries(t *testing.T) {
	entries := []Entry{
		{ID: "rule", Scope: "project", Type: "rule", Title: "Rule", Content: "needle", UpdatedAt: time.Now().UTC()},
		{ID: "other", Scope: "project", Type: "rule", Title: "Other", Content: "different", UpdatedAt: time.Now().UTC().Add(-time.Hour)},
	}
	results := SearchEntries(entries, Query{Scope: "project", Content: "needle", Limit: 5})
	if len(results) != 1 || results[0].Entry.ID != "rule" {
		t.Fatalf("SearchEntries() = %+v, want rule", results)
	}
}

func mustWriteDoc(t *testing.T, path string, meta any, body string) {
	t.Helper()
	b, err := store.RenderMarkdown(meta, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func touchWorktrailDocs(t *testing.T, root string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(path, ".md") || strings.HasSuffix(path, ".json") {
			return os.Chtimes(path, at, at)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func mustWriteJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
