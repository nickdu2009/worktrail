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

func TestParseCodexDesktopPayloadJSONL(t *testing.T) {
	raw := strings.Join([]string{
		`{"timestamp":"2026-04-12T00:48:18Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"What remains for codexlite?"}]}}`,
		`{"timestamp":"2026-04-12T00:48:19Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Add integration coverage."}]}}`,
		`{"timestamp":"2026-04-12T00:48:20Z","type":"event_msg","payload":{"type":"agent_message","message":"I will inspect the repo."}}`,
	}, "\n")
	tr, err := ParseCodexJSONL(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseCodexJSONL() error = %v", err)
	}
	if len(tr.Messages) != 3 {
		t.Fatalf("messages = %d, want 3: %+v", len(tr.Messages), tr.Messages)
	}
	if tr.Messages[0].Role != "user" || tr.Messages[0].Content != "What remains for codexlite?" {
		t.Fatalf("unexpected first message: %+v", tr.Messages[0])
	}
	if tr.Messages[1].Role != "assistant" || tr.Messages[1].Content != "Add integration coverage." {
		t.Fatalf("unexpected second message: %+v", tr.Messages[1])
	}
}

func TestParseCodexDesktopDedupesMirroredEventMessages(t *testing.T) {
	raw := strings.Join([]string{
		`{"timestamp":"2026-04-12T00:48:18Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Continue work."}]}}`,
		`{"timestamp":"2026-04-12T00:48:19Z","type":"event_msg","payload":{"type":"agent_message","message":"I will inspect the repo."}}`,
		`{"timestamp":"2026-04-12T00:48:20Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I will inspect the repo."}]}}`,
	}, "\n")
	tr, err := ParseCodexJSONL(strings.NewReader(raw))
	if err != nil {
		t.Fatalf("ParseCodexJSONL() error = %v", err)
	}
	if len(tr.Messages) != 2 {
		t.Fatalf("messages = %d, want 2: %+v", len(tr.Messages), tr.Messages)
	}
	if tr.Messages[1].Role != "assistant" || tr.Messages[1].Content != "I will inspect the repo." || tr.Messages[1].RawType != "message" {
		t.Fatalf("unexpected deduped assistant message: %+v", tr.Messages[1])
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
