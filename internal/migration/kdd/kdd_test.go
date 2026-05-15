package kdd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestRunMigratesLocalItemsToUserAndSanitizesDistillationPack(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	home := filepath.Join(t.TempDir(), "home")
	root := filepath.Join(project, DefaultLegacyRoot)
	pack := filepath.Join(t.TempDir(), "pack.md")

	writeKDDTestFile(t, filepath.Join(root, "project", "README.md"), "# Project\n\nShared context.")
	writeKDDTestFile(t, filepath.Join(root, "project", "notes", "unclassified.md"), "# Unclassified\n\nToken sk-proj-123456789012345678901234 should be redacted.")
	writeKDDTestFile(t, filepath.Join(root, "project", "notes", "blocked.md"), "# Blocked\n\n-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----\n")
	writeKDDTestFile(t, filepath.Join(root, "local", "active-knowledge-log.md"), "# Local Active Log\n\nPersonal scratchpad.")
	writeKDDTestFile(t, filepath.Join(root, "local", "notes", "private.md"), "# Private\n\nC:\\Users\\alice\\vault has local setup details and TOKEN=sk-proj-abcdefghijklmnopqrstuv.")

	report, err := Run(paths.Env{
		UserRoot:    home,
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}, Options{
		WriteCandidates: true,
		WritePack:       pack,
		NowActor:        "test",
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.DryRun || report.Matched != 4 || report.Created != 4 || report.Blocked != 1 || report.ProjectItems != 2 || report.LocalItems != 2 {
		t.Fatalf("unexpected report: %+v", report)
	}

	localCandidate := filepath.Join(home, "candidates", "user", "kdd-local-notes-private.md")
	body, err := os.ReadFile(localCandidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`"scope": "user"`,
		`"candidate_type": "lesson"`,
		`"target_path": "lessons/kdd-local-notes-private.md"`,
		`"local_scope_only"`,
		`"local_path_detected"`,
		`[REDACTED:api-key]`,
	} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("local candidate missing %q:\n%s", want, body)
		}
	}
	if bytes.Contains(body, []byte("sk-proj-abcdefghijklmnopqrstuv")) {
		t.Fatalf("local candidate leaked raw token:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "candidates", "project", "kdd-local-notes-private.md")); !os.IsNotExist(err) {
		t.Fatalf("local item should not be written to project scope, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "candidates", "project", "kdd-project-notes-blocked.md")); !os.IsNotExist(err) {
		t.Fatalf("blocked migration source should not be written, err=%v", err)
	}

	migrationSource := filepath.Join(project, ".worktrail", "candidates", "project", "kdd-project-notes-unclassified.md")
	body, err = os.ReadFile(migrationSource)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte(`"candidate_type": "migration_source"`)) || !bytes.Contains(body, []byte(`"redaction_status": "redacted"`)) {
		t.Fatalf("migration source metadata unexpected:\n%s", body)
	}
	if bytes.Contains(body, []byte("sk-proj-123456789012345678901234")) {
		t.Fatalf("migration source candidate leaked raw token:\n%s", body)
	}

	packBody, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packBody, []byte("kdd-project-notes-unclassified")) || !bytes.Contains(packBody, []byte("kdd-local-active-knowledge-log")) {
		t.Fatalf("pack missing migration sources:\n%s", packBody)
	}
	if bytes.Contains(packBody, []byte("kdd-project-notes-blocked")) || bytes.Contains(packBody, []byte("OPENSSH PRIVATE KEY")) {
		t.Fatalf("pack included blocked migration source:\n%s", packBody)
	}
	if bytes.Contains(packBody, []byte("sk-proj-123456789012345678901234")) || !bytes.Contains(packBody, []byte("[REDACTED:api-key]")) {
		t.Fatalf("pack did not sanitize redactable token:\n%s", packBody)
	}
}

func TestMapPathAndLocalPathDetectionCoverMigrationEdges(t *testing.T) {
	tests := []struct {
		rel           string
		scope         string
		candidateType string
		target        string
		warning       string
	}{
		{"project/active-knowledge-log.md", "project", model.CandidateTypeMigrationSource, ProjectActiveLogTarget, ""},
		{"local/active-knowledge-log.md", "user", model.CandidateTypeMigrationSource, LocalActiveLogTarget, ""},
		{"local/notes/private.md", "user", "lesson", "lessons/kdd-local-notes-private.md", "local_scope_only"},
		{"project/runbooks/release.md", "project", "workflow", "workflows/release.md", ""},
		{"project/unknown/source.md", "project", model.CandidateTypeMigrationSource, "imports/kdd/project/source.md", "needs_classification"},
	}
	for _, tt := range tests {
		item, ok := MapPath(tt.rel)
		if !ok {
			t.Fatalf("MapPath(%q) returned !ok", tt.rel)
		}
		if item.Scope != tt.scope || item.CandidateType != tt.candidateType || item.TargetPath != tt.target {
			t.Fatalf("MapPath(%q) = scope=%q type=%q target=%q", tt.rel, item.Scope, item.CandidateType, item.TargetPath)
		}
		if tt.warning != "" && !hasKDDTestWarning(item.Warnings, tt.warning) {
			t.Fatalf("MapPath(%q) missing warning %q: %+v", tt.rel, tt.warning, item.Warnings)
		}
		if len(item.CandidateID) > 64 {
			t.Fatalf("candidate id too long for %q: %q", tt.rel, item.CandidateID)
		}
	}

	for _, text := range []string{
		"/Users/alice/work/private",
		"/home/alice/work/private",
		`C:\Users\alice\work\private`,
	} {
		if !HasLocalAbsolutePath(text) {
			t.Fatalf("expected local path detection for %q", text)
		}
	}
	for _, text := range []string{
		"Use ./relative/path inside the repo.",
		"See /usr/local/bin/tool for a system binary.",
		`D:\Projects\repo`,
	} {
		if HasLocalAbsolutePath(text) {
			t.Fatalf("unexpected local path detection for %q", text)
		}
	}
}

func TestCandidateIDTruncatesLongPathsWithStableHashSuffix(t *testing.T) {
	relA := "project/architecture/" + strings.Repeat("very-long-segment-", 8) + "alpha.md"
	relB := "project/architecture/" + strings.Repeat("very-long-segment-", 8) + "beta.md"

	idA := CandidateID(relA)
	idB := CandidateID(relB)
	if len(idA) > 64 || len(idB) > 64 {
		t.Fatalf("candidate ids exceed 64 chars: %q %q", idA, idB)
	}
	if idA == idB {
		t.Fatalf("distinct long paths produced duplicate ids: %q", idA)
	}
	if !strings.HasPrefix(idA, "kdd-project-architecture-very-long") || !strings.HasPrefix(idB, "kdd-project-architecture-very-long") {
		t.Fatalf("candidate ids lost useful prefix: %q %q", idA, idB)
	}
}

func writeKDDTestFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasKDDTestWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if warning == want {
			return true
		}
	}
	return false
}
