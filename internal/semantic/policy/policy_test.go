package policy

import (
	"reflect"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/index"
)

func TestSelectDefaultCorpus(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	entries := []index.Entry{
		{ID: "formal", Scope: "project", Path: "rules/current.md"},
		{ID: "historical", Scope: "project", Path: "rules/historical.md", Lifecycle: "historical"},
		{ID: "retired", Scope: "project", Path: "rules/retired.md", Status: "retired"},
		{ID: "candidate", Scope: "project", Path: "candidates/project/rule.md"},
		{ID: "evidence", Scope: "project", Path: "evidence/transcript.md"},
		{ID: "runtime", Scope: "project", Path: "runtime/sessions/session.md"},
		{ID: "raw", Scope: "project", Path: "raw/cursor/chat.md"},
		{ID: "import", Scope: "project", Path: "imports/legacy.md"},
		{ID: "log", Scope: "project", Path: "logs/events.md"},
		{ID: "export", Scope: "project", Path: "exports/context.md"},
		{ID: "active", Scope: "project", Path: "state/active/session.md", Active: true},
		{ID: "inactive", Scope: "project", Path: "state/active/inactive.md"},
		{ID: "latest", Scope: "project", Path: "state/active/latest.md", Active: true},
		{ID: "archived", Scope: "project", Path: "state/archived/session.md", Active: true},
		{ID: "checkpoint", Scope: "project", Path: "state/checkpoints/session.md", Active: true},
		{ID: "project-old", Scope: "project", Path: "handoffs/project-old.md", UpdatedAt: now.Add(-time.Hour)},
		{ID: "project-new", Scope: "project", Path: "handoffs/project-new.md", UpdatedAt: now},
		{ID: "user-handoff", Scope: "user", Path: "handoffs/user.md", UpdatedAt: now},
		{ID: "profile", Scope: "user", Path: "profile/preferences.md"},
	}

	if got, want := ids(Select(entries)), []string{"project-new", "user-handoff", "profile", "formal", "active"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() IDs = %v, want %v", got, want)
	}
}

func TestSelectKeepsLatestCurrentHandoffPerScope(t *testing.T) {
	now := time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC)
	entries := []index.Entry{
		{ID: "tie-z", Scope: "project", Path: "handoffs/z.md", UpdatedAt: now},
		{ID: "tie-a", Scope: "project", Path: "handoffs/a.md", UpdatedAt: now},
		{ID: "historical", Scope: "project", Path: "handoffs/historical.md", UpdatedAt: now.Add(time.Hour), Lifecycle: "historical"},
		{ID: "user", Scope: "user", Path: "handoffs/user.md", UpdatedAt: now.Add(-time.Hour)},
		{ID: "candidate", Scope: "project", Path: "candidates/project/handoff.md", UpdatedAt: now.Add(2 * time.Hour)},
	}

	if got, want := ids(Select(entries)), []string{"tie-a", "user"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() IDs = %v, want %v", got, want)
	}
}

func TestSelectIsDeterministicAndDoesNotModifyInput(t *testing.T) {
	entries := []index.Entry{
		{ID: "c", Scope: "project", Path: "rules\\c.md", Tags: []string{"unchanged"}},
		{ID: "a", Scope: "project", Path: "./rules/a.md"},
		{ID: "b", Scope: "project", Path: "rules/b.md"},
		{ID: "user-state", Scope: "user", Path: "state/active/session.md", Active: true},
		{ID: "project-state", Scope: "project", Path: "state/active/session.md", Active: true},
	}
	original := append([]index.Entry(nil), entries...)

	first := Select(entries)
	second := Select(entries)
	if got, want := ids(first), []string{"a", "b", "c", "project-state", "user-state"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Select() IDs = %v, want %v", got, want)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Select() output is not deterministic: first=%+v second=%+v", first, second)
	}
	if !reflect.DeepEqual(entries, original) {
		t.Fatalf("Select() modified input: got=%+v want=%+v", entries, original)
	}
}

func ids(entries []index.Entry) []string {
	ids := make([]string, len(entries))
	for i, entry := range entries {
		ids[i] = entry.ID
	}
	return ids
}
