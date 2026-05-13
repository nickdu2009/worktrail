package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

func TestEnsureProjectGitignoreMigratesLegacyBlock(t *testing.T) {
	project := t.TempDir()
	path := filepath.Join(project, ".gitignore")
	if err := os.WriteFile(path, []byte("*.test\n"+legacyProjectGitignoreBody+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureProjectGitignore(paths.Env{ProjectRoot: project}); err != nil {
		t.Fatalf("EnsureProjectGitignore: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, "*.test") {
		t.Fatalf("existing gitignore content was not preserved: %s", text)
	}
	if strings.Count(text, ".codex/") != 1 {
		t.Fatalf("expected one .codex entry after migration: %s", text)
	}
	if !strings.Contains(text, util.HashManagedBegin) || strings.Contains(text, util.ManagedBegin) {
		t.Fatalf("expected hash managed block only: %s", text)
	}
}
