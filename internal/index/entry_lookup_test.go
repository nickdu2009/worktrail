package index

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEntriesByIDPreservesRequestOrderDeduplicatesAndHydratesTags(t *testing.T) {
	root := detailedSearchFixture(t)
	requested := []string{"entry-b", "missing", "entry-a", "entry-b"}

	entries, err := EntriesByID(root, requested)
	if err != nil {
		t.Fatalf("EntriesByID() error = %v", err)
	}
	if got, want := lookupEntryIDs(entries), []string{"entry-b", "entry-a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("EntriesByID() IDs = %v, want %v", got, want)
	}
	wantTags := [][]string{{"beta", "common", "discoverabletag"}, {"alpha", "common", "lexicaltag"}}
	for i, entry := range entries {
		if !reflect.DeepEqual(entry.Tags, wantTags[i]) {
			t.Fatalf("entry %q tags = %v, want %v", entry.ID, entry.Tags, wantTags[i])
		}
		if entry.Content == "" {
			t.Fatalf("entry %q content was not hydrated", entry.ID)
		}
	}
	if got := missingEntryIDs(requested, entries); !reflect.DeepEqual(got, []string{"missing"}) {
		t.Fatalf("missing IDs = %v, want [missing]", got)
	}
}

func TestEntriesByIDRefreshesBeforeLookup(t *testing.T) {
	root := detailedSearchFixture(t)
	mustWriteDoc(t, filepath.Join(root, "rules", "entry-c.md"), map[string]any{
		"id": "entry-c", "scope": "project", "type": "rule", "title": "Refreshed entry",
		"tags": []string{"fresh"},
	}, "refreshed body")

	entries, err := EntriesByID(root, []string{"entry-c"})
	if err != nil {
		t.Fatalf("EntriesByID() error = %v", err)
	}
	if len(entries) != 1 || entries[0].ID != "entry-c" || !reflect.DeepEqual(entries[0].Tags, []string{"fresh"}) {
		t.Fatalf("EntriesByID() entries = %+v", entries)
	}
}

func TestEntriesByIDRejectsBlankIDsAndAllowsEmptyRequests(t *testing.T) {
	entries, err := EntriesByID(t.TempDir(), nil)
	if err != nil {
		t.Fatalf("EntriesByID() empty request error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("EntriesByID() empty request = %v, want no entries", entries)
	}

	if _, err := EntriesByID(t.TempDir(), []string{"valid", " "}); err == nil {
		t.Fatal("EntriesByID() blank ID error = nil, want error")
	}
}

func TestEntriesByIDDoesNotModifyFreshSQLiteOrSearchResults(t *testing.T) {
	root := detailedSearchFixture(t)
	indexPath := filepath.Join(root, "index", SQLiteFile)
	query := Query{Content: "target", IncludeContent: true}
	beforeSearch, err := Search(root, query)
	if err != nil {
		t.Fatalf("Search() before EntriesByID() error = %v", err)
	}
	beforeSearchJSON, err := json.Marshal(beforeSearch)
	if err != nil {
		t.Fatal(err)
	}
	beforeDB, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := EntriesByID(root, []string{"entry-b", "entry-a"}); err != nil {
		t.Fatalf("EntriesByID() error = %v", err)
	}

	afterDB, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(indexPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterDB, beforeDB) {
		t.Fatal("EntriesByID() changed SQLite bytes for a fresh index")
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("EntriesByID() changed SQLite mtime: before=%v after=%v", beforeInfo.ModTime(), afterInfo.ModTime())
	}

	afterSearch, err := Search(root, query)
	if err != nil {
		t.Fatalf("Search() after EntriesByID() error = %v", err)
	}
	afterSearchJSON, err := json.Marshal(afterSearch)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterSearchJSON, beforeSearchJSON) {
		t.Fatalf("Search() changed after EntriesByID(): before=%s after=%s", beforeSearchJSON, afterSearchJSON)
	}
}

func lookupEntryIDs(entries []Entry) []string {
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	return ids
}

func missingEntryIDs(requested []string, entries []Entry) []string {
	found := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		found[entry.ID] = struct{}{}
	}
	seen := make(map[string]struct{}, len(requested))
	var missing []string
	for _, id := range requested {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		if _, ok := found[id]; !ok {
			missing = append(missing, id)
		}
	}
	return missing
}
