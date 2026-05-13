package transcript

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCodexClaudeJSONLAndMarkdown(t *testing.T) {
	codexFile := filepath.Join("..", "..", "testdata", "transcripts", "codex.jsonl")
	codex, err := os.Open(codexFile)
	if err != nil {
		t.Fatal(err)
	}
	defer codex.Close()
	codexTranscript, err := ParseCodexJSONL(codex)
	if err != nil {
		t.Fatalf("ParseCodexJSONL() error = %v", err)
	}
	if len(codexTranscript.Messages) != 2 || codexTranscript.Messages[1].Content != "Done." {
		t.Fatalf("unexpected codex transcript: %+v", codexTranscript)
	}

	claudeFile := filepath.Join("..", "..", "testdata", "transcripts", "claude.jsonl")
	claude, err := os.Open(claudeFile)
	if err != nil {
		t.Fatal(err)
	}
	defer claude.Close()
	claudeTranscript, err := ParseClaudeJSONL(claude)
	if err != nil {
		t.Fatalf("ParseClaudeJSONL() error = %v", err)
	}
	if len(claudeTranscript.Messages) != 2 || claudeTranscript.Messages[0].Role != "user" {
		t.Fatalf("unexpected claude transcript: %+v", claudeTranscript)
	}

	md, err := ParseMarkdown("codex", strings.NewReader("## User\nHello\n\n## Assistant\nHi"))
	if err != nil {
		t.Fatalf("ParseMarkdown() error = %v", err)
	}
	if len(md.Messages) != 2 || md.Messages[0].Content != "Hello" {
		t.Fatalf("unexpected markdown transcript: %+v", md)
	}
}

func TestSyncMetadataOnlyDoesNotCopyRawTranscript(t *testing.T) {
	tmp := t.TempDir()
	source := filepath.Join(tmp, "codex-session.jsonl")
	if err := os.WriteFile(source, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(tmp, "wt")
	meta, err := Sync(source, root, SyncOptions{
		Source:          "codex",
		Scope:           "project",
		RawMetadataOnly: true,
		Now:             time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if meta.Schema != SchemaTranscriptMeta || meta.Hash == "" || meta.Path != source {
		t.Fatalf("unexpected meta: %+v", meta)
	}
	if _, err := os.Stat(filepath.Join(root, "raw", "codex", filepath.Base(source))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("raw copy exists or unexpected stat error: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "raw", "codex", "*.metadata.json")); err != nil || len(matches) != 1 {
		t.Fatalf("metadata file matches = %v, err = %v", matches, err)
	}
}
