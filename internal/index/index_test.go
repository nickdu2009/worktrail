package index

import (
	"crypto/sha256"
	"encoding/hex"
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

func TestRebuildQuarantinesMalformedMarkdownWithoutMovingSource(t *testing.T) {
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "rules", "good.md"), map[string]any{
		"id": "good", "title": "Good",
	}, "good body")
	badPath := filepath.Join(root, "rules", "bad.md")
	if err := os.WriteFile(badPath, []byte("---worktrail\n{\"id\":\n---\n# Broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := Rebuild(root, RebuildOptions{})
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if manifest.Entries != 1 || manifest.Ignored != 1 {
		t.Fatalf("manifest = %+v, want one indexed and one ignored", manifest)
	}
	if _, err := os.Stat(badPath); err != nil {
		t.Fatalf("bad source was moved or removed: %v", err)
	}
	sidecarData, err := os.ReadFile(filepath.Join(root, "index", "ignored.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sidecarData), `"path": "rules/bad.md"`) || !strings.Contains(string(sidecarData), "parse markdown") {
		t.Fatalf("ignored sidecar missing diagnostic:\n%s", sidecarData)
	}
}

func TestRebuildStillFailsForDuplicateExplicitIDs(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"first.md", "second.md"} {
		mustWriteDoc(t, filepath.Join(root, "rules", name), map[string]any{
			"id": "duplicate-id", "title": name,
		}, "body")
	}
	if _, err := Rebuild(root, RebuildOptions{}); err == nil || !strings.Contains(err.Error(), "duplicate explicit Worktrail id") {
		t.Fatalf("Rebuild() error = %v, want explicit ID conflict", err)
	}
}

func TestTeamHandoffDAGDerivesCurrentAndSupportsFilters(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	base := map[string]any{
		"schema":           "worktrail.handoff.v2",
		"object_kind":      "runtime_record",
		"scope":            "project",
		"project_id":       "project-1",
		"task_id":          "task-1",
		"runtime_type":     "handoff",
		"summary":          "handoff summary",
		"visibility":       "team",
		"storage_class":    "team",
		"durability":       "durable",
		"lifecycle_status": "current",
		"resume_priority":  "manual_handoff",
		"format_version":   2,
		"created_at":       now,
		"updated_at":       now,
	}
	oldMeta := cloneMap(base)
	oldMeta["id"] = "handoff-old"
	oldMeta["title"] = "Old handoff"
	oldMeta["content_hash"] = testContentHash(t, oldMeta, "old body secret")
	mustWriteDoc(t, filepath.Join(root, "handoffs", "team", "old.md"), oldMeta, "old body secret")
	newMeta := cloneMap(base)
	newMeta["id"] = "handoff-new"
	newMeta["title"] = "New handoff"
	newMeta["updated_at"] = now.Add(time.Minute)
	newMeta["supersedes"] = []map[string]any{{"id": "handoff-old"}}
	newMeta["content_hash"] = testContentHash(t, newMeta, "new body secret")
	mustWriteDoc(t, filepath.Join(root, "handoffs", "team", "new.md"), newMeta, "new body secret")

	if _, err := Rebuild(root, RebuildOptions{Now: now.Add(2 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	current, err := Search(root, Query{
		Type: "handoff", TaskID: "task-1", Visibility: "team", Status: "current", Lifecycle: "current",
		Content: "handoff summary",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(current) != 1 || current[0].Entry.ID != "handoff-new" {
		t.Fatalf("current team handoff = %+v", current)
	}
	superseded, err := Search(root, Query{
		Type: "handoff", TaskID: "task-1", Visibility: "team", Lifecycle: "superseded",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(superseded) != 1 || superseded[0].Entry.ID != "handoff-old" || len(superseded[0].Entry.SupersededBy) != 1 {
		t.Fatalf("superseded team handoff = %+v", superseded)
	}
}

func TestRuntimeBodiesAreNotIndexedAndOnlyLatestFiveRemain(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		id := "runtime-" + string(rune('a'+i))
		mustWriteDoc(t, filepath.Join(root, "runtime", "sessions", id+".md"), map[string]any{
			"schema":           "worktrail.runtime.v2",
			"id":               id,
			"object_kind":      "runtime_record",
			"scope":            "project",
			"runtime_type":     "session_state",
			"title":            "Runtime " + id,
			"durability":       "ephemeral",
			"lifecycle_status": "active",
			"project_id":       "project-1",
			"task_id":          "task-1",
			"created_at":       now.Add(time.Duration(i) * time.Minute),
			"updated_at":       now.Add(time.Duration(i) * time.Minute),
			"expires_at":       now.Add(24 * time.Hour),
		}, "runtime-body-secret-"+id)
	}
	manifest, err := Rebuild(root, RebuildOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Entries != 5 {
		t.Fatalf("runtime entries = %d, want latest 5", manifest.Entries)
	}
	results, err := Search(root, Query{Content: "runtime-body-secret", IncludeContent: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("runtime body unexpectedly searchable: %+v", results)
	}
}

func TestUnboundRuntimeEntriesDoNotShareLatestFiveBucket(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	for i := 0; i < 6; i++ {
		id := "unbound-" + string(rune('a'+i))
		mustWriteDoc(t, filepath.Join(root, "runtime", "sessions", id+".md"), map[string]any{
			"schema":           "worktrail.runtime.v2",
			"id":               id,
			"object_kind":      "runtime_record",
			"scope":            "project",
			"runtime_type":     "session_state",
			"title":            id,
			"durability":       "ephemeral",
			"lifecycle_status": "active",
			"binding_status":   "unbound",
			"created_at":       now.Add(time.Duration(i) * time.Minute),
			"updated_at":       now.Add(time.Duration(i) * time.Minute),
			"expires_at":       now.Add(time.Hour),
		}, "runtime body")
	}
	manifest, err := Rebuild(root, RebuildOptions{Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Entries != 6 {
		t.Fatalf("unbound runtime entries = %d, want 6", manifest.Entries)
	}
}

func TestRebuildIgnoresV2NormalizationAndContentHashErrors(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	mustWriteDoc(t, filepath.Join(root, "rules", "good.md"), map[string]any{
		"id": "good", "title": "Good",
	}, "good body")
	mustWriteDoc(t, filepath.Join(root, "runtime", "sessions", "bad-meta.md"), map[string]any{
		"schema":           "worktrail.runtime.v2",
		"id":               "bad-meta",
		"object_kind":      "runtime_record",
		"scope":            "project",
		"runtime_type":     "session_state",
		"title":            "Bad metadata",
		"created_at":       map[string]any{"invalid": true},
		"updated_at":       now,
		"expires_at":       now.Add(time.Hour),
		"lifecycle_status": "active",
	}, "bad metadata body")
	badHashMeta := map[string]any{
		"schema":           "worktrail.handoff.v2",
		"id":               "bad-hash",
		"object_kind":      "runtime_record",
		"scope":            "project",
		"runtime_type":     "handoff",
		"title":            "Bad hash",
		"project_id":       "project-1",
		"task_id":          "task-1",
		"visibility":       "team",
		"storage_class":    "team",
		"durability":       "durable",
		"lifecycle_status": "current",
		"resume_priority":  "manual_handoff",
		"content_hash":     "definitely-wrong",
		"format_version":   2,
		"created_at":       now,
		"updated_at":       now,
	}
	mustWriteDoc(t, filepath.Join(root, "handoffs", "team", "bad-hash.md"), badHashMeta, "tampered body")

	manifest, err := Rebuild(root, RebuildOptions{Now: now})
	if err != nil {
		t.Fatalf("Rebuild() should fail-soft: %v", err)
	}
	if manifest.Entries != 1 || manifest.Ignored != 2 {
		t.Fatalf("manifest = %+v, want one indexed and two ignored", manifest)
	}
	sidecar, err := os.ReadFile(filepath.Join(root, "index", "ignored.json"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(sidecar)
	for _, want := range []string{"runtime/sessions/bad-meta.md", "normalize v2 metadata", "handoffs/team/bad-hash.md", "content hash mismatch"} {
		if !strings.Contains(text, want) {
			t.Fatalf("ignored sidecar missing %q:\n%s", want, text)
		}
	}
}

func TestActiveBoostIsAppliedOnceAndSupersededIsDownranked(t *testing.T) {
	now := time.Now().UTC()
	inactive := Entry{ID: "inactive", UpdatedAt: now}
	active := Entry{ID: "active", Active: true, UpdatedAt: now}
	if got := scoreEntry(active, "") - scoreEntry(inactive, ""); got != 5 {
		t.Fatalf("active boost = %v, want 5", got)
	}
	current := Entry{ID: "current", UpdatedAt: now}
	superseded := Entry{ID: "superseded", Lifecycle: "superseded", UpdatedAt: now}
	if scoreEntry(superseded, "") >= scoreEntry(current, "") {
		t.Fatalf("superseded entry was not downranked")
	}
}

func cloneMap(input map[string]any) map[string]any {
	out := make(map[string]any, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

func testContentHash(t *testing.T, meta map[string]any, body string) string {
	t.Helper()
	canonical := cloneMap(meta)
	delete(canonical, "content_hash")
	data, err := json.Marshal(canonical)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(append(append(data, '\n'), []byte(strings.TrimSpace(body))...))
	return hex.EncodeToString(sum[:])
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
