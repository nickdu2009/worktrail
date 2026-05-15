package candidate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
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

func TestMigrationSourceCannotPromoteOrMerge(t *testing.T) {
	m := testManager(t)
	_, err := m.Create(CreateRequest{
		ID:            "kdd-active-log",
		Scope:         "project",
		CandidateType: model.CandidateTypeMigrationSource,
		TargetPath:    "imports/kdd/project/active-knowledge-log.md",
		Title:         "KDD Active Log",
		Body:          "# Active Log\n\nMixed migration evidence.",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Promote("project", "kdd-active-log"); !errors.Is(err, ErrMigrationSourceApply) {
		t.Fatalf("Promote migration_source error = %v", err)
	}
	if _, err := m.Merge("project", "kdd-active-log"); !errors.Is(err, ErrMigrationSourceApply) {
		t.Fatalf("Merge migration_source error = %v", err)
	}
}

func TestRestoreRecreatesMissingPromotedReplaceTarget(t *testing.T) {
	m := testManager(t)
	_, err := m.Create(CreateRequest{
		ID:         "restore-rule",
		Scope:      "project",
		TargetPath: "rules/restore-rule.md",
		Title:      "Restore Rule",
		Body:       "# Restore Rule\n\nRestored body.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Promote("project", "restore-rule"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(m.Env.ProjectWT, "rules", "restore-rule.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	result, err := m.Restore("project", "restore-rule")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "restored" {
		t.Fatalf("restore status = %q", result.Status)
	}
	body, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "# Restore Rule\n\nRestored body.\n" {
		t.Fatalf("restored target body = %q", body)
	}
	updated, err := m.Show("project", "restore-rule")
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
	if !strings.Contains(string(logBody), "candidate.restore") {
		t.Fatalf("event log missing restore event:\n%s", logBody)
	}
	if _, err := m.Restore("project", "restore-rule"); err == nil || !strings.Contains(err.Error(), "target") {
		t.Fatalf("restore existing target error = %v", err)
	}
}

func TestRestoreRejectsNonPromotedOrMergedCandidates(t *testing.T) {
	m := testManager(t)
	if _, err := m.Create(CreateRequest{
		ID:         "pending-rule",
		Scope:      "project",
		TargetPath: "rules/pending.md",
		Title:      "Pending Rule",
		Body:       "Pending body.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore("project", "pending-rule"); !errors.Is(err, ErrRestoreUnsupported) {
		t.Fatalf("pending restore error = %v", err)
	}

	target := filepath.Join(m.Env.ProjectWT, "project.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("# Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(CreateRequest{
		ID:         "merged-note",
		Scope:      "project",
		TargetPath: "project.md",
		Title:      "Merged Note",
		Operation:  OperationMerge,
		Body:       "Merged body.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Merge("project", "merged-note"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Restore("project", "merged-note"); !errors.Is(err, ErrRestoreUnsupported) {
		t.Fatalf("merged restore error = %v", err)
	}
}

func TestRetireAcknowledgesMissingPromotedOrMergedTargets(t *testing.T) {
	m := testManager(t)
	_, err := m.Create(CreateRequest{
		ID:         "retire-rule",
		Scope:      "project",
		TargetPath: "rules/retire-rule.md",
		Title:      "Retire Rule",
		Body:       "# Retire Rule\n\nRetired body.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Promote("project", "retire-rule"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(m.Env.ProjectWT, "rules", "retire-rule.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}

	rec, err := m.Retire("project", "retire-rule", "smoke test cleanup")
	if err != nil {
		t.Fatal(err)
	}
	if rec.Meta.Status != StatusRetired {
		t.Fatalf("retired status = %q", rec.Meta.Status)
	}
	if rec.Meta.RetireReason != "smoke test cleanup" {
		t.Fatalf("retire reason = %q", rec.Meta.RetireReason)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retire recreated target or unexpected stat error: %v", err)
	}
	if _, err := m.Promote("project", "retire-rule"); err == nil || !strings.Contains(err.Error(), "already retired") {
		t.Fatalf("promote retired candidate error = %v", err)
	}

	mergeTarget := filepath.Join(m.Env.ProjectWT, "project.md")
	if err := os.MkdirAll(filepath.Dir(mergeTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mergeTarget, []byte("# Project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Create(CreateRequest{
		ID:         "retire-merge",
		Scope:      "project",
		TargetPath: "project.md",
		Title:      "Retire Merge",
		Operation:  OperationMerge,
		Body:       "Merged body.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Merge("project", "retire-merge"); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(mergeTarget); err != nil {
		t.Fatal(err)
	}
	merged, err := m.Retire("project", "retire-merge", "project document removed")
	if err != nil {
		t.Fatal(err)
	}
	if merged.Meta.Status != StatusRetired {
		t.Fatalf("merged retire status = %q", merged.Meta.Status)
	}

	logBody, err := os.ReadFile(filepath.Join(m.Env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logBody), "candidate.retire") || !strings.Contains(string(logBody), "smoke test cleanup") {
		t.Fatalf("event log missing retire event:\n%s", logBody)
	}
}

func TestRetireRejectsPendingExistingTargetAndMissingReason(t *testing.T) {
	m := testManager(t)
	if _, err := m.Create(CreateRequest{
		ID:         "pending-retire",
		Scope:      "project",
		TargetPath: "rules/pending-retire.md",
		Title:      "Pending Retire",
		Body:       "Pending body.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Retire("project", "pending-retire", "not accepted"); !errors.Is(err, ErrRetireUnsupported) {
		t.Fatalf("pending retire error = %v", err)
	}

	if _, err := m.Create(CreateRequest{
		ID:         "existing-retire",
		Scope:      "project",
		TargetPath: "rules/existing-retire.md",
		Title:      "Existing Retire",
		Body:       "Existing body.\n",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Promote("project", "existing-retire"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Retire("project", "existing-retire", "target still active"); err == nil || !strings.Contains(err.Error(), "still exists") {
		t.Fatalf("existing target retire error = %v", err)
	}

	target := filepath.Join(m.Env.ProjectWT, "rules", "existing-retire.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Retire("project", "existing-retire", " "); !errors.Is(err, ErrRetireReasonRequired) {
		t.Fatalf("missing reason retire error = %v", err)
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

func TestTranscriptNotesCannotBePromotedOrMerged(t *testing.T) {
	m := testManager(t)
	_, err := m.Create(CreateRequest{
		ID:            "transcript-note",
		Scope:         "project",
		CandidateType: model.CandidateTypeTranscriptNotes,
		TargetPath:    "imports/transcripts/transcript-note.md",
		Title:         "Transcript Notes",
		Body:          "Evidence only.\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Promote("project", "transcript-note"); !errors.Is(err, ErrTranscriptNotesApply) {
		t.Fatalf("Promote error = %v, want ErrTranscriptNotesApply", err)
	}
	if _, err := m.Merge("project", "transcript-note"); !errors.Is(err, ErrTranscriptNotesApply) {
		t.Fatalf("Merge error = %v, want ErrTranscriptNotesApply", err)
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
