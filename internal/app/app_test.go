package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestInitCreatesUserAndProjectRoots(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	for _, path := range []string{
		filepath.Join(home, "config.json"),
		filepath.Join(home, "logs", "events.jsonl"),
		filepath.Join(project, ".worktrail", "config.json"),
		filepath.Join(project, ".worktrail", "logs", "events.jsonl"),
		filepath.Join(project, ".gitignore"),
		filepath.Join(project, ".codex", "hooks.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(project, ".gitignore")); err != nil || !bytes.Contains(data, []byte(".codex/")) {
		t.Fatalf("expected project .gitignore to contain worktrail entries: %v %s", err, data)
	}
	for _, path := range []string{
		filepath.Join(project, "AGENTS.md"),
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(project, ".agents", "skills", "worktrail-state", "SKILL.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("did not expect init to create %s, err=%v", path, err)
		}
	}
}

func TestExtractionCandidateIDIncludesSourceAndOrdinal(t *testing.T) {
	got := extractionCandidateID("codex", "/tmp/session.jsonl", 1, "user")
	if got != "codex-02-user" {
		t.Fatalf("id = %s", got)
	}
	got = extractionCandidateID("", "/tmp/session.jsonl", 0, "")
	if got != "manual-01-session" {
		t.Fatalf("fallback id = %s", got)
	}
}

func TestImportCodexDiscoversAndExtractsProjectSessions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(home, ".codex", "sessions", "2026", "05", "14", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"2026-05-14T00:00:00Z","type":"session_meta","payload":{"id":"session-1","cwd":"` + filepath.ToSlash(project) + `"}}` + "\n" +
		`{"timestamp":"2026-05-14T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Capture this workflow."}]}}` + "\n" +
		`{"timestamp":"2026-05-14T00:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Keep review candidates pending."}]}}` + "\n"
	if err := os.WriteFile(session, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import dry-run: %v stderr=%s", err, errb.String())
	}
	var dry importReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Matched != 1 || !dry.DryRun {
		t.Fatalf("unexpected dry-run report: %+v", dry)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import --all: %v stderr=%s", err, errb.String())
	}
	var report importReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Matched != 1 || report.Synced != 1 || report.Extracted != 1 || report.DryRun {
		t.Fatalf("unexpected import report: %+v", report)
	}
	candidates, err := filepath.Glob(filepath.Join(project, ".worktrail", "candidates", "project", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate files = %d, want 1", len(candidates))
	}
}
