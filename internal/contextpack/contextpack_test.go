package contextpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestBuildIncludesRequiredSectionsAndMarksCandidatesUnapproved(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	writePackDoc(t, filepath.Join(env.UserRoot, "profile", "preferences.md"), map[string]any{
		"id":    "prefs",
		"scope": "user",
		"type":  "profile",
		"title": "Preferences",
	}, "Prefer concise handoffs.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "testing.md"), map[string]any{
		"id":    "testing",
		"scope": "project",
		"type":  "rule",
		"title": "Testing Rule",
	}, "Run targeted tests.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "workflows", "release.md"), map[string]any{
		"id":    "release",
		"scope": "project",
		"type":  "workflow",
		"title": "Release Workflow",
	}, "Build, test, then ship.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "state", "active", "current.md"), map[string]any{
		"id":     "current",
		"scope":  "project",
		"type":   "state",
		"title":  "Current Work",
		"status": "active",
	}, "Implement local packages.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "decisions", "index.md"), map[string]any{
		"id":    "decision",
		"scope": "project",
		"type":  "decision",
		"title": "Index Decision",
	}, "Use a JSON text index.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "handoffs", "next.md"), map[string]any{
		"id":    "handoff",
		"scope": "project",
		"type":  "handoff",
		"title": "Next Handoff",
	}, "Wire CLI later.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "candidates", "project", "rule.md"), map[string]any{
		"id":             "candidate",
		"scope":          "project",
		"candidate_type": "rule",
		"title":          "Candidate Rule",
		"status":         "pending",
	}, "Candidate content.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "candidates", "project", "transcript.md"), map[string]any{
		"id":             "transcript",
		"scope":          "project",
		"candidate_type": "transcript_notes",
		"title":          "Transcript Evidence",
		"status":         "pending",
	}, "Raw transcript evidence content.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "candidates", "project", "handoff-candidate.md"), map[string]any{
		"id":             "handoff-candidate",
		"scope":          "project",
		"candidate_type": "handoff",
		"title":          "Handoff Candidate",
		"status":         "pending",
	}, "Pending non-semantic candidate content.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "candidates", "project", "promoted.md"), map[string]any{
		"id":             "promoted",
		"scope":          "project",
		"candidate_type": "rule",
		"title":          "Promoted Rule",
		"status":         "promoted",
	}, "Already promoted candidate content.")

	pack, err := Build(env, Options{Task: "ship packages"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if pack.HiddenEvidenceCandidates != 1 {
		t.Fatalf("HiddenEvidenceCandidates = %d, want 1", pack.HiddenEvidenceCandidates)
	}
	for _, title := range []string{"User Knowledge", "Workflows", "Active State", "Decisions", "Handoffs", "Rules", "Pending Candidates"} {
		if !hasSection(pack, title) {
			t.Fatalf("missing section %q in %+v", title, pack.Sections)
		}
	}
	workflows := section(pack, "Workflows")
	if len(workflows.Items) != 1 || workflows.Items[0].Title != "Release Workflow" {
		t.Fatalf("workflow section unexpected: %+v", workflows.Items)
	}
	pending := section(pack, "Pending Candidates")
	if len(pending.Items) != 1 || !pending.Items[0].Unapproved {
		t.Fatalf("pending candidate not marked unapproved: %+v", pending.Items)
	}
	if pending.Items[0].Title != "Candidate Rule" {
		t.Fatalf("pending section included non-pending candidate: %+v", pending.Items)
	}
	rendered := RenderMarkdown(pack)
	if rendered == "" || !strings.Contains(rendered, "unapproved") {
		t.Fatalf("rendered pack missing unapproved marker:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Hidden transcript evidence candidates: 1") || !strings.Contains(rendered, "worktrail context --evidence <task>") {
		t.Fatalf("rendered pack missing hidden evidence guidance:\n%s", rendered)
	}
	if strings.Contains(rendered, "Raw transcript evidence content.") || strings.Contains(rendered, "Pending non-semantic candidate content.") {
		t.Fatalf("default rendered pack leaked hidden candidates:\n%s", rendered)
	}

	withEvidence, err := Build(env, Options{Task: "ship packages", IncludeEvidence: true})
	if err != nil {
		t.Fatalf("Build(IncludeEvidence) error = %v", err)
	}
	if withEvidence.HiddenEvidenceCandidates != 1 {
		t.Fatalf("IncludeEvidence HiddenEvidenceCandidates = %d, want 1", withEvidence.HiddenEvidenceCandidates)
	}
	pending = section(withEvidence, "Pending Candidates")
	if len(pending.Items) != 2 || !hasItem(pending, "Transcript Evidence") || !hasItem(pending, "Candidate Rule") {
		t.Fatalf("IncludeEvidence pending section unexpected: %+v", pending.Items)
	}
	rendered = RenderMarkdown(withEvidence)
	if !strings.Contains(rendered, "Raw transcript evidence content.") || strings.Contains(rendered, "Hidden transcript evidence candidates") {
		t.Fatalf("IncludeEvidence rendered pack unexpected:\n%s", rendered)
	}
}

func writePackDoc(t *testing.T, path string, meta any, body string) {
	t.Helper()
	b, err := store.RenderMarkdown(meta, body)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasSection(pack Pack, title string) bool {
	return len(section(pack, title).Items) > 0
}

func section(pack Pack, title string) Section {
	for _, section := range pack.Sections {
		if section.Title == title {
			return section
		}
	}
	return Section{}
}

func hasItem(section Section, title string) bool {
	for _, item := range section.Items {
		if item.Title == title {
			return true
		}
	}
	return false
}
