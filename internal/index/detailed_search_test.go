package index

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDetailedSearchRanksAndFiltersLexically(t *testing.T) {
	root := detailedSearchFixture(t)

	hits, err := DetailedSearch(root, Query{
		Scope:          "project",
		Type:           "rule",
		Topic:          "api",
		Content:        "target",
		IncludeContent: true,
	})
	if err != nil {
		t.Fatalf("DetailedSearch() error = %v", err)
	}
	if got, want := detailedHitIDs(hits), []string{"entry-a", "entry-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("DetailedSearch() IDs = %v, want %v", got, want)
	}
	if hits[0].LexicalLane != DetailedLexicalLane || hits[0].LexicalRank != 1 || hits[1].LexicalRank != 2 {
		t.Fatalf("unexpected lexical metadata: %+v", hits)
	}
	if hits[0].RawScore >= hits[1].RawScore {
		t.Fatalf("expected title hit to have a lower FTS5 score: %f >= %f", hits[0].RawScore, hits[1].RawScore)
	}
}

func TestDetailedSearchSupportsTagFiltersAndTagTerms(t *testing.T) {
	root := detailedSearchFixture(t)

	filtered, err := DetailedSearch(root, Query{
		Content: "target",
		Tag:     "ALPHA",
		Tags:    []string{"common"},
	})
	if err != nil {
		t.Fatalf("DetailedSearch() with tag filters error = %v", err)
	}
	if got, want := detailedHitIDs(filtered), []string{"entry-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tag-filtered IDs = %v, want %v", got, want)
	}

	tagTermHits, err := DetailedSearch(root, Query{Content: "discoverabletag"})
	if err != nil {
		t.Fatalf("DetailedSearch() with tag term error = %v", err)
	}
	if got, want := detailedHitIDs(tagTermHits), []string{"entry-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tag term IDs = %v, want %v", got, want)
	}
}

func TestDetailedSearchBatchHydratesTagsForMultipleHits(t *testing.T) {
	root := detailedSearchFixture(t)

	hits, err := DetailedSearch(root, Query{Content: "batchneedle"})
	if err != nil {
		t.Fatalf("DetailedSearch() error = %v", err)
	}
	if got, want := detailedHitIDs(hits), []string{"entry-a", "entry-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("batch tag query IDs = %v, want %v", got, want)
	}
	wantTags := [][]string{{"alpha", "common", "lexicaltag"}, {"beta", "common", "discoverabletag"}}
	for i, hit := range hits {
		if !reflect.DeepEqual(hit.Entry.Tags, wantTags[i]) || !reflect.DeepEqual(hit.Tags, wantTags[i]) {
			t.Fatalf("hit %q tags = entry:%v detail:%v, want %v", hit.Entry.ID, hit.Entry.Tags, hit.Tags, wantTags[i])
		}
	}
}

func TestDetailedSearchDoesNotChangeDefaultSearchResults(t *testing.T) {
	root := detailedSearchFixture(t)
	query := Query{
		Scope:   "project",
		Type:    "rule",
		Topic:   "api",
		Tag:     "common",
		Content: "target",
	}

	before, err := Search(root, query)
	if err != nil {
		t.Fatalf("Search() before DetailedSearch() error = %v", err)
	}
	if got, want := resultIDs(before), []string{"entry-a", "entry-b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Search() golden IDs = %v, want %v", got, want)
	}
	if _, err := DetailedSearch(root, query); err != nil {
		t.Fatalf("DetailedSearch() error = %v", err)
	}
	after, err := Search(root, query)
	if err != nil {
		t.Fatalf("Search() after DetailedSearch() error = %v", err)
	}
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("Search() changed after DetailedSearch(): before=%+v after=%+v", before, after)
	}
}

func TestDetailedSearchReportsDetailedQueryError(t *testing.T) {
	_, err := DetailedSearch(t.TempDir(), Query{})
	var detailedErr *DetailedQueryError
	if !errors.As(err, &detailedErr) {
		t.Fatalf("DetailedSearch() error = %v, want DetailedQueryError", err)
	}
}

func detailedSearchFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	mustWriteJSON(t, filepath.Join(root, "config.json"), map[string]any{"scope": "project"})
	mustWriteDoc(t, filepath.Join(root, "rules", "entry-a.md"), map[string]any{
		"id": "entry-a", "scope": "project", "type": "rule", "title": "Target rule", "topic": "api",
		"tags": []string{"alpha", "common", "lexicaltag"}, "updated_at": "2026-05-02T00:00:00Z",
	}, "batchneedle")
	mustWriteDoc(t, filepath.Join(root, "rules", "entry-b.md"), map[string]any{
		"id": "entry-b", "scope": "project", "type": "rule", "title": "Secondary rule", "topic": "api",
		"tags": []string{"beta", "common", "discoverabletag"}, "updated_at": "2026-05-01T00:00:00Z",
	}, "target batchneedle")
	mustWriteDoc(t, filepath.Join(root, "rules", "wrong-scope.md"), map[string]any{
		"id": "wrong-scope", "scope": "user", "type": "rule", "title": "Target rule", "topic": "api",
	}, "target")
	mustWriteDoc(t, filepath.Join(root, "decisions", "wrong-type.md"), map[string]any{
		"id": "wrong-type", "scope": "project", "type": "decision", "title": "Target decision", "topic": "api",
	}, "target")
	mustWriteDoc(t, filepath.Join(root, "rules", "wrong-topic.md"), map[string]any{
		"id": "wrong-topic", "scope": "project", "type": "rule", "title": "Target rule", "topic": "other",
	}, "target")
	rebuildAt := time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC)
	touchWorktrailDocs(t, root, rebuildAt)
	if _, err := Rebuild(root, RebuildOptions{Now: rebuildAt}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	return root
}

func detailedHitIDs(hits []DetailedHit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.Entry.ID
	}
	return ids
}

func resultIDs(results []Result) []string {
	ids := make([]string, len(results))
	for i, result := range results {
		ids[i] = result.Entry.ID
	}
	return ids
}
