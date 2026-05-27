package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportCodexReportsExistingTranscriptEvidence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(home, ".codex", "sessions", "2026", "05", "27", "session.jsonl")
	writeImportFlowCodexSession(t, session, project)

	t.Setenv("HOME", home)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import --all: %v stderr=%s", err, errb.String())
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import dry-run after import: %v stderr=%s", err, errb.String())
	}
	var dry importReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Matched != 1 || dry.AlreadyImported != 1 || !dry.DryRun {
		t.Fatalf("unexpected dry-run report: %+v", dry)
	}
	if len(dry.ExistingCandidates) != 1 || dry.ExistingCandidates[0] != "codex-01-session" {
		t.Fatalf("dry-run existing candidates = %+v, want codex-01-session", dry.ExistingCandidates)
	}
	if len(dry.NextSteps) == 0 || strings.Contains(dry.NextSteps[0], "--all") {
		t.Fatalf("dry-run should not push --all when all matched sessions are already imported: %+v", dry.NextSteps)
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run repeated import --all: %v stderr=%s", err, errb.String())
	}
	var repeated importReport
	if err := json.Unmarshal(out.Bytes(), &repeated); err != nil {
		t.Fatal(err)
	}
	if repeated.Synced != 0 || repeated.Extracted != 0 || repeated.Skipped != 1 || repeated.Reused != 1 {
		t.Fatalf("repeated import should reuse existing evidence without resyncing: %+v", repeated)
	}
	if len(repeated.ExistingCandidates) != 1 || repeated.ExistingCandidates[0] != "codex-01-session" {
		t.Fatalf("existing candidates = %+v, want codex-01-session", repeated.ExistingCandidates)
	}
}

func writeImportFlowCodexSession(t *testing.T, path, project string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"2026-05-27T00:00:00Z","type":"session_meta","payload":{"id":"session-1","cwd":"` + filepath.ToSlash(project) + `"}}` + "\n" +
		`{"timestamp":"2026-05-27T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Capture import flow."}]}}` + "\n" +
		`{"timestamp":"2026-05-27T00:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Keep evidence pending."}]}}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
