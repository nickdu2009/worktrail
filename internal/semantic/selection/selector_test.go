package selection

import (
	"reflect"
	"testing"

	"github.com/nickdu2009/worktrail/internal/contextpack"
)

func TestSelectOrdersRanksThenFillsWithoutDuplicates(t *testing.T) {
	selector := New([]Ranking{
		{Path: "rules/b.md", Rank: 2},
		{Path: "rules/c.md", Rank: 1},
	})

	got := selectPaths(t, selector, contextpack.SelectionRequest{
		Limit: 4,
		Candidates: []contextpack.Item{
			item("rules/a.md"),
			item("rules/b.md"),
			item("rules/c.md"),
			item("rules/b.md"),
		},
	})

	want := []string{"rules/c.md", "rules/b.md", "rules/a.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected paths = %v, want %v", got, want)
	}
}

func TestSelectKeepsInputOrderForEqualRanks(t *testing.T) {
	selector := New([]Ranking{
		{Path: "rules/a.md", Rank: 3},
		{Path: "rules/b.md", Rank: 3},
	})

	got := selectPaths(t, selector, contextpack.SelectionRequest{
		Limit:      3,
		Candidates: []contextpack.Item{item("rules/b.md"), item("rules/a.md"), item("rules/c.md")},
	})

	want := []string{"rules/b.md", "rules/a.md", "rules/c.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected paths = %v, want %v", got, want)
	}
}

func TestSelectFillsAbsentRanksInCandidateOrder(t *testing.T) {
	selector := New([]Ranking{{Path: "rules/b.md", Rank: 1}})

	got := selectPaths(t, selector, contextpack.SelectionRequest{
		Limit:      3,
		Candidates: []contextpack.Item{item("rules/a.md"), item("rules/b.md"), item("rules/c.md")},
	})

	want := []string{"rules/b.md", "rules/a.md", "rules/c.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected paths = %v, want %v", got, want)
	}
}

func TestSelectHandlesZeroLimitAndEmptyCandidates(t *testing.T) {
	selector := New([]Ranking{{Path: "rules/a.md", Rank: 1}})

	for _, request := range []contextpack.SelectionRequest{
		{Limit: 0, Candidates: []contextpack.Item{item("rules/a.md")}},
		{Limit: 3},
	} {
		selected, err := selector.Select(request)
		if err != nil {
			t.Fatalf("Select() error = %v", err)
		}
		if len(selected) != 0 {
			t.Fatalf("Select() = %+v, want no candidates", selected)
		}
		if selected == nil {
			t.Fatal("Select() returned nil, want an empty slice")
		}
	}
}

func TestNewKeepsFirstDuplicateRankAndIgnoresUnknownRankKeys(t *testing.T) {
	selector := New([]Ranking{
		{Path: "rules/b.md", Rank: 4},
		{Path: "rules/b.md", Rank: 1},
		{Path: "rules/unknown.md", Rank: 0},
		{Path: "rules/c.md", Rank: 2},
	})

	got := selectPaths(t, selector, contextpack.SelectionRequest{
		Limit:      3,
		Candidates: []contextpack.Item{item("rules/a.md"), item("rules/b.md"), item("rules/c.md")},
	})

	want := []string{"rules/c.md", "rules/b.md", "rules/a.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected paths = %v, want %v", got, want)
	}
}

func TestSelectRanksSamePathInDifferentScopes(t *testing.T) {
	selector := New([]Ranking{
		{Scope: "project", Path: "rules/shared.md", Rank: 2},
		{Scope: "user", Path: "rules/shared.md", Rank: 1},
	})

	got := selectIdentities(t, selector, contextpack.SelectionRequest{
		Limit: 2,
		Candidates: []contextpack.Item{
			scopedItem("project", "rules/shared.md"),
			scopedItem("user", "rules/shared.md"),
		},
	})
	want := []string{"user:rules/shared.md", "project:rules/shared.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected identities = %v, want %v", got, want)
	}
}

func TestSelectPrefersEntryIDOverPathAndIgnoresUnscopedPath(t *testing.T) {
	selector := New([]Ranking{
		{Scope: "project", EntryID: "shared", Path: "rules/shared.md", Rank: 2},
		{Scope: "project", Path: "rules/other.md", Rank: 1},
		{Path: "rules/shared.md", Rank: 0}, // unscoped path must not cross into project
	})
	got := selectIdentities(t, selector, contextpack.SelectionRequest{
		Limit: 2,
		Candidates: []contextpack.Item{
			scopedEntryItem("project", "shared", "rules/shared.md"),
			scopedItem("project", "rules/other.md"),
		},
	})
	want := []string{"project:rules/other.md", "project:rules/shared.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected identities = %v, want %v", got, want)
	}
}

func TestSelectFallsBackToScopedPathWithoutEntryID(t *testing.T) {
	selector := New([]Ranking{
		{Scope: "project", Path: "rules/shared.md", Rank: 1},
		{Scope: "project", Path: "rules/other.md", Rank: 2},
	})
	got := selectIdentities(t, selector, contextpack.SelectionRequest{
		Limit: 2,
		Candidates: []contextpack.Item{
			scopedItem("project", "rules/other.md"),
			scopedItem("project", "rules/shared.md"),
		},
	})
	want := []string{"project:rules/shared.md", "project:rules/other.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected identities = %v, want %v", got, want)
	}
}

func TestSelectDeduplicatesCandidatesByScopeAndPath(t *testing.T) {
	firstProject := scopedItem("project", "rules/shared.md")
	firstProject.Title = "first project candidate"
	duplicateProject := scopedItem("project", "rules/shared.md")
	duplicateProject.Title = "duplicate project candidate"

	selector := New(nil)
	selected, err := selector.Select(contextpack.SelectionRequest{
		Limit: 3,
		Candidates: []contextpack.Item{
			firstProject,
			scopedItem("user", "rules/shared.md"),
			duplicateProject,
		},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}

	got := selectedIdentities(selected)
	want := []string{"project:rules/shared.md", "user:rules/shared.md"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("selected identities = %v, want %v", got, want)
	}
	if selected[0].Title != firstProject.Title {
		t.Fatalf("first duplicate candidate title = %q, want %q", selected[0].Title, firstProject.Title)
	}
}

func TestSelectorDependsOnlyOnCandidatesAndCopiedRankings(t *testing.T) {
	rankings := []Ranking{{Path: "rules/b.md", Rank: 1}}
	selector := New(rankings)
	rankings[0] = Ranking{Path: "rules/a.md", Rank: 1}

	request := contextpack.SelectionRequest{
		Task:       "first task",
		Section:    "Rules",
		Topic:      "delivery",
		Limit:      2,
		Candidates: []contextpack.Item{item("rules/a.md"), item("rules/b.md")},
	}
	first := selectPaths(t, selector, request)
	request.Task = "different task"
	request.Section = "Architecture"
	request.Topic = "billing"
	second := selectPaths(t, selector, request)

	want := []string{"rules/b.md", "rules/a.md"}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatalf("selector output changed with non-candidate inputs: first=%v second=%v want=%v", first, second, want)
	}
}

func TestSelectReturnsCopiesOfMutableItemFields(t *testing.T) {
	selector := New([]Ranking{{Path: "rules/a.md", Rank: 1}})
	candidate := item("rules/a.md")
	candidate.Tags = []string{"original"}
	candidate.SupersededBy = []string{"old.md"}

	selected, err := selector.Select(contextpack.SelectionRequest{
		Limit:      1,
		Candidates: []contextpack.Item{candidate},
	})
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	selected[0].Tags[0] = "mutated"
	selected[0].SupersededBy[0] = "mutated.md"

	if candidate.Tags[0] != "original" || candidate.SupersededBy[0] != "old.md" {
		t.Fatalf("Select() output mutated request candidate: %+v", candidate)
	}
}

func selectPaths(t *testing.T, selector *Selector, request contextpack.SelectionRequest) []string {
	t.Helper()
	selected, err := selector.Select(request)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	paths := make([]string, len(selected))
	for i, candidate := range selected {
		paths[i] = candidate.Path
	}
	return paths
}

func selectIdentities(t *testing.T, selector *Selector, request contextpack.SelectionRequest) []string {
	t.Helper()
	selected, err := selector.Select(request)
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	return selectedIdentities(selected)
}

func selectedIdentities(selected []contextpack.Item) []string {
	identities := make([]string, len(selected))
	for i, candidate := range selected {
		identities[i] = candidate.Scope + ":" + candidate.Path
	}
	return identities
}

func item(path string) contextpack.Item {
	return contextpack.Item{Path: path, Title: path}
}

func scopedItem(scope, path string) contextpack.Item {
	item := item(path)
	item.Scope = scope
	return item
}

func scopedEntryItem(scope, entryID, path string) contextpack.Item {
	item := scopedItem(scope, path)
	item.EntryID = entryID
	return item
}
