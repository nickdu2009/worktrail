package candidate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
)

func testManager(t *testing.T) Manager {
	t.Helper()
	root := t.TempDir()
	now := time.Date(2026, 5, 14, 12, 30, 0, 0, time.UTC)
	return Manager{
		Env: paths.Env{
			Home:        root,
			UserRoot:    filepath.Join(root, "user"),
			ProjectRoot: filepath.Join(root, "project"),
			ProjectWT:   filepath.Join(root, "project", ".worktrail"),
		},
		Actor: "test",
		Now: func() time.Time {
			return now
		},
	}
}

func TestCreateListShowAndDiff(t *testing.T) {
	m := testManager(t)
	target := filepath.Join(m.Env.ProjectWT, "rules", "testing.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old line\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	rec, err := m.Create(CreateRequest{
		ID:         "testing-rules",
		Scope:      "project",
		TargetPath: "rules/testing.md",
		Title:      "Testing Rules",
		Body:       "new line\nOPENAI_API_KEY=sk-proj-abcdefghijklmnopqrstuvwxyz123456\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if rec.Meta.RedactionStatus != string(redact.StatusRedacted) {
		t.Fatalf("redaction status = %q", rec.Meta.RedactionStatus)
	}
	if strings.Contains(rec.Body, "sk-proj-abcdefghijklmnopqrstuvwxyz123456") {
		t.Fatalf("candidate body was not redacted:\n%s", rec.Body)
	}

	list, err := m.List("project")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Meta.ID != "testing-rules" {
		t.Fatalf("list = %#v", list)
	}
	shown, err := m.Show("project", "testing-rules")
	if err != nil {
		t.Fatal(err)
	}
	if shown.Meta.Title != "Testing Rules" {
		t.Fatalf("shown title = %q", shown.Meta.Title)
	}
	diff, err := m.Diff("project", "testing-rules")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(diff, "-old line") || !strings.Contains(diff, "+new line") {
		t.Fatalf("diff did not include expected lines:\n%s", diff)
	}
}

func TestPromoteBacksUpWritesAtomicallyUpdatesStatusAndLogs(t *testing.T) {
	m := testManager(t)
	target := filepath.Join(m.Env.ProjectWT, "decisions", "adr.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(CreateRequest{
		ID:         "adr",
		Scope:      "project",
		TargetPath: "decisions/adr.md",
		Title:      "ADR",
		Body:       "replacement\n",
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := m.Promote("project", "adr")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusPromoted {
		t.Fatalf("status = %q", result.Status)
	}
	targetBody, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(targetBody) != "replacement\n" {
		t.Fatalf("target body = %q", targetBody)
	}
	backupBody, err := os.ReadFile(result.BackupPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(backupBody) != "existing\n" {
		t.Fatalf("backup body = %q", backupBody)
	}
	updated, err := m.Show("project", "adr")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Meta.Status != StatusPromoted {
		t.Fatalf("candidate status = %q", updated.Meta.Status)
	}
	logBody, err := os.ReadFile(filepath.Join(m.Env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBody), "candidate.promote") {
		t.Fatalf("event log missing promote event:\n%s", logBody)
	}
}

func TestMergeBacksUpAndAppendsCandidateBody(t *testing.T) {
	m := testManager(t)
	target := filepath.Join(m.Env.ProjectWT, "project.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# Project\n\nExisting.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := m.Create(CreateRequest{
		ID:         "project-note",
		Scope:      "project",
		TargetPath: "project.md",
		Operation:  OperationMerge,
		Title:      "Project Note",
		Body:       "New note.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := m.Merge("project", "project-note")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusMerged || result.BackupPath == "" {
		t.Fatalf("merge result = %#v", result)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Existing.\n\nNew note.") {
		t.Fatalf("merged body = %q", body)
	}
}

func TestApplyRejectsEscapingTargetAndBlockedContent(t *testing.T) {
	m := testManager(t)
	if _, err := m.Create(CreateRequest{
		ID:         "escape",
		Scope:      "project",
		TargetPath: "../outside.md",
		Title:      "Escape",
		Body:       "content",
	}); err == nil {
		t.Fatal("Create accepted escaping target path")
	}

	if _, err := m.Create(CreateRequest{
		ID:         "blocked",
		Scope:      "project",
		TargetPath: "project.md",
		Title:      "Blocked",
		Body:       "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n",
	}); !errors.Is(err, ErrBlocked) {
		t.Fatalf("Create error = %v, want ErrBlocked", err)
	}

	rec, err := m.Create(CreateRequest{
		ID:         "later-blocked",
		Scope:      "project",
		TargetPath: "project.md",
		Title:      "Later Blocked",
		Body:       "safe\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	rec.Body = "-----BEGIN OPENSSH PRIVATE KEY-----\nabc\n-----END OPENSSH PRIVATE KEY-----\n"
	if err := writeRecord(rec); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Promote("project", "later-blocked"); !errors.Is(err, ErrBlocked) {
		t.Fatalf("Promote error = %v, want ErrBlocked", err)
	}
	if _, err := os.Stat(filepath.Join(m.Env.ProjectWT, "project.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked promote wrote target or unexpected stat err: %v", err)
	}
}

func TestDiscardUpdatesStatusAndPreventsApply(t *testing.T) {
	m := testManager(t)
	if _, err := m.Create(CreateRequest{
		ID:         "discard-me",
		Scope:      "project",
		TargetPath: "project.md",
		Title:      "Discard Me",
		Body:       "content\n",
	}); err != nil {
		t.Fatal(err)
	}
	rec, err := m.Discard("project", "discard-me")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Meta.Status != StatusDiscarded {
		t.Fatalf("status = %q", rec.Meta.Status)
	}
	if _, err := m.Promote("project", "discard-me"); err == nil {
		t.Fatal("Promote succeeded after discard")
	}
}
