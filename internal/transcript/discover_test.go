package transcript

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverCodexSessionsMatchesProjectRoot(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "project")
	other := filepath.Join(t.TempDir(), "other")
	if err := writeCodexSession(home, project, "match.jsonl"); err != nil {
		t.Fatal(err)
	}
	if err := writeCodexSession(home, other, "skip.jsonl"); err != nil {
		t.Fatal(err)
	}
	sessions, err := DiscoverCodexSessions(home, project)
	if err != nil {
		t.Fatalf("DiscoverCodexSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1: %+v", len(sessions), sessions)
	}
	if sessions[0].ProjectRoot != project || filepath.Base(sessions[0].Path) != "match.jsonl" {
		t.Fatalf("unexpected session: %+v", sessions[0])
	}
}

func writeCodexSession(home, project, name string) error {
	path := filepath.Join(home, ".codex", "sessions", "2026", "05", "14", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := `{"timestamp":"2026-05-14T00:00:00Z","type":"session_meta","payload":{"id":"` + name + `","cwd":"` + filepath.ToSlash(project) + `"}}` + "\n"
	return os.WriteFile(path, []byte(body), 0o644)
}
