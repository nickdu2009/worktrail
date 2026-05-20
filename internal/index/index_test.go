package index

import (
	"encoding/json"
	"os"
	"path/filepath"
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

	manifest, err := Rebuild(root, RebuildOptions{Now: time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if manifest.Entries != 4 {
		t.Fatalf("manifest entries = %d, want 4", manifest.Entries)
	}
	if _, err := os.Stat(filepath.Join(root, "index", DBFile)); err != nil {
		t.Fatalf("index db missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "index", ManifestFile)); err != nil {
		t.Fatalf("manifest missing: %v", err)
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
