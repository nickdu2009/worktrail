package contextpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
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
	writePackDoc(t, filepath.Join(env.ProjectWT, "architecture", "discovery.md"), map[string]any{
		"id":    "discovery",
		"scope": "project",
		"type":  "architecture",
		"title": "Discovery Architecture",
		"stage": "requirements",
	}, "Clarify user problem before design.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "requirements", "old.md"), map[string]any{
		"id":         "old-requirement",
		"scope":      "project",
		"title":      "Old Requirement",
		"stage":      "historical",
		"topic":      "delivery",
		"updated_at": "2026-05-03T00:00:00Z",
	}, "Older PRD.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "requirements", "new.md"), map[string]any{
		"id":              "new-requirement",
		"scope":           "project",
		"title":           "New Requirement",
		"stage":           "requirements",
		"topic":           "delivery",
		"source_of_truth": true,
		"supersedes":      []string{"requirements/old.md"},
		"updated_at":      "2026-05-01T00:00:00Z",
	}, "Current PRD.")
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
	if pack.Maintenance.PendingEvidenceCandidates != 1 || pack.Maintenance.PendingSemanticCandidates != 1 || pack.Maintenance.EvidenceLifecycleCandidates != 0 {
		t.Fatalf("maintenance counts unexpected: %+v", pack.Maintenance)
	}
	if !containsStep(pack.Maintenance.NextSteps, "worktrail distill --pending --summary") || !containsStep(pack.Maintenance.NextSteps, "worktrail review plan --format json") {
		t.Fatalf("maintenance next steps unexpected: %+v", pack.Maintenance.NextSteps)
	}
	for _, title := range []string{"User Knowledge", "Requirements", "Architecture", "Workflows", "Active State", "Decisions", "Handoffs", "Rules", "Pending Candidates"} {
		if !hasSection(pack, title) {
			t.Fatalf("missing section %q in %+v", title, pack.Sections)
		}
	}
	requirements := section(pack, "Requirements")
	if len(requirements.Items) != 1 || requirements.Items[0].Title != "New Requirement" {
		t.Fatalf("requirements priority or superseded marker unexpected: %+v", requirements.Items)
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
	if !strings.Contains(rendered, "[stage:requirements] [topic:delivery] [source_of_truth]") {
		t.Fatalf("rendered pack missing governance metadata:\n%s", rendered)
	}
	if strings.Contains(rendered, "Old Requirement") {
		t.Fatalf("default rendered pack should hide historical requirements:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Hidden evidence candidates: 1") || !strings.Contains(rendered, "worktrail context --evidence <task>") {
		t.Fatalf("rendered pack missing hidden evidence guidance:\n%s", rendered)
	}
	if !strings.Contains(rendered, "## Maintenance") || !strings.Contains(rendered, "Pending evidence candidates: 1") || !strings.Contains(rendered, "Pending review candidates: 1") {
		t.Fatalf("rendered pack missing maintenance hints:\n%s", rendered)
	}
	if strings.Contains(rendered, "Raw transcript evidence content.") || strings.Contains(rendered, "Pending non-semantic candidate content.") {
		t.Fatalf("default rendered pack leaked hidden candidates:\n%s", rendered)
	}

	implementationPack, err := Build(env, Options{Task: "ship packages", Stage: "implementation"})
	if err != nil {
		t.Fatalf("Build(Stage implementation) error = %v", err)
	}
	if sectionIndex(implementationPack, "Architecture") > sectionIndex(implementationPack, "Requirements") {
		t.Fatalf("implementation stage should prioritize architecture before requirements: %+v", implementationPack.Sections)
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
	if strings.Contains(rendered, "Raw transcript evidence content.") || strings.Contains(rendered, "Hidden evidence candidates") {
		t.Fatalf("IncludeEvidence rendered pack unexpected:\n%s", rendered)
	}

	withHistorical, err := Build(env, Options{Task: "ship packages", IncludeLifecycle: []string{knowledge.LifecycleCurrent, knowledge.LifecycleHistorical}})
	if err != nil {
		t.Fatalf("Build(IncludeLifecycle historical) error = %v", err)
	}
	requirements = section(withHistorical, "Requirements")
	if len(requirements.Items) != 2 || requirements.Items[1].Title != "Old Requirement" || len(requirements.Items[1].SupersededBy) != 1 {
		t.Fatalf("historical lifecycle view unexpected: %+v", requirements.Items)
	}
	rendered = RenderMarkdown(withHistorical)
	if !strings.Contains(rendered, "[lifecycle:historical]") {
		t.Fatalf("rendered historical pack missing lifecycle marker:\n%s", rendered)
	}
}

func TestBuildOmitsMaintenanceTextWhenCountsAreZero(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "testing.md"), map[string]any{
		"id":    "testing",
		"scope": "project",
		"type":  "rule",
		"title": "Testing Rule",
	}, "Run targeted tests.")

	pack, err := Build(env, Options{Task: "quiet"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if hasMaintenance(pack.Maintenance) {
		t.Fatalf("maintenance should be empty: %+v", pack.Maintenance)
	}
	rendered := RenderMarkdown(pack)
	if strings.Contains(rendered, "## Maintenance") {
		t.Fatalf("rendered quiet pack included maintenance section:\n%s", rendered)
	}
}

func TestBuildSkipsStaleIndexedEntriesAndReportsIndexHealth(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	currentPath := filepath.Join(env.ProjectWT, "rules", "current.md")
	deletedPath := filepath.Join(env.ProjectWT, "rules", "deleted.md")
	newPath := filepath.Join(env.ProjectWT, "rules", "new.md")
	writePackDoc(t, currentPath, map[string]any{
		"id":    "current",
		"scope": "project",
		"type":  "rule",
		"title": "Current Rule",
	}, "current body")
	writePackDoc(t, deletedPath, map[string]any{
		"id":    "deleted",
		"scope": "project",
		"type":  "rule",
		"title": "Deleted Rule",
	}, "deleted body")
	writePackDoc(t, filepath.Join(env.ProjectWT, "workflows", "stable.md"), map[string]any{
		"id":    "stable",
		"scope": "project",
		"type":  "workflow",
		"title": "Stable Workflow",
	}, "stable body")
	if _, err := Build(env, Options{Task: "prime index"}); err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	db, err := index.Load(env.ProjectWT)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	writePackDoc(t, currentPath, map[string]any{
		"id":    "current",
		"scope": "project",
		"type":  "rule",
		"title": "Current Rule",
	}, "updated body")
	writePackDoc(t, newPath, map[string]any{
		"id":    "new",
		"scope": "project",
		"type":  "rule",
		"title": "New Rule",
	}, "new body")
	later := db.GeneratedAt.Add(time.Hour)
	if err := os.Chtimes(currentPath, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, later, later); err != nil {
		t.Fatal(err)
	}

	pack, err := Build(env, Options{Task: "stale index"})
	if err != nil {
		t.Fatalf("Build() with stale index error = %v", err)
	}
	if len(pack.IndexHealth) != 1 {
		t.Fatalf("IndexHealth len = %d, want 1: %+v", len(pack.IndexHealth), pack.IndexHealth)
	}
	health := pack.IndexHealth[0]
	if !health.Stale || health.StaleEntriesSkipped != 2 || health.MissingFromFS != 1 || health.Changed != 1 || health.MissingFromIndex != 1 {
		t.Fatalf("unexpected index health: %+v", health)
	}
	rendered := RenderMarkdown(pack)
	if !strings.Contains(rendered, "## Index Health") || !strings.Contains(rendered, "worktrail index rebuild --scope project") {
		t.Fatalf("rendered pack missing index health guidance:\n%s", rendered)
	}
	if strings.Contains(rendered, "Current Rule") || strings.Contains(rendered, "Deleted Rule") || strings.Contains(rendered, "New Rule") {
		t.Fatalf("rendered pack should hide stale or unindexed rule entries:\n%s", rendered)
	}
}

func TestBuildMaintenanceNextStepsIncludeUserScope(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	writePackDoc(t, filepath.Join(env.UserRoot, "candidates", "user", "transcript.md"), map[string]any{
		"id":             "user-transcript",
		"scope":          "user",
		"candidate_type": "transcript_notes",
		"title":          "User Transcript Evidence",
		"status":         "pending",
	}, "User transcript evidence content.")

	pack, err := Build(env, Options{Task: "maintain"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if pack.Maintenance.PendingEvidenceCandidates != 1 {
		t.Fatalf("pending evidence = %d, want 1", pack.Maintenance.PendingEvidenceCandidates)
	}
	if !containsStep(pack.Maintenance.NextSteps, "worktrail distill --pending --summary --scope user") {
		t.Fatalf("maintenance next steps missing user scope: %+v", pack.Maintenance.NextSteps)
	}
	rendered := RenderMarkdown(pack)
	if !strings.Contains(rendered, "`worktrail distill --pending --summary --scope user`") {
		t.Fatalf("rendered maintenance hint missing user scope:\n%s", rendered)
	}
}

func TestBuildMaintenanceSurfacesImportableCodexSessions(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	project := filepath.Join(tmp, "project")
	env := paths.Env{
		Home:        home,
		ProjectRoot: project,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	session := filepath.Join(home, ".codex", "sessions", "2026", "05", "26", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","type":"session_meta","payload":{"id":"session-1","cwd":"` + filepath.ToSlash(project) + `"}}` + "\n"
	if err := os.WriteFile(session, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	pack, err := Build(env, Options{Task: "import discovery"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if pack.Maintenance.ImportableCodexSessions != 1 {
		t.Fatalf("importable codex = %d, want 1: %+v", pack.Maintenance.ImportableCodexSessions, pack.Maintenance)
	}
	if !containsStep(pack.Maintenance.NextSteps, "worktrail import codex --since 14d --all") {
		t.Fatalf("maintenance next steps missing bounded import: %+v", pack.Maintenance.NextSteps)
	}
	rendered := RenderMarkdown(pack)
	if !strings.Contains(rendered, "Importable current-project Codex sessions: 1") || !strings.Contains(rendered, "`worktrail import codex --since 14d --all`") {
		t.Fatalf("rendered maintenance hint missing import discovery:\n%s", rendered)
	}
}

func TestBuildMaintenanceSkipsCodexSessionsAlreadyRepresentedByCandidate(t *testing.T) {
	tmp := t.TempDir()
	home := filepath.Join(tmp, "home")
	project := filepath.Join(tmp, "project")
	env := paths.Env{
		Home:        home,
		ProjectRoot: project,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	session := filepath.Join(home, ".codex", "sessions", "2026", "05", "26", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `","type":"session_meta","payload":{"id":"session-1","cwd":"` + filepath.ToSlash(project) + `"}}` + "\n"
	if err := os.WriteFile(session, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	writePackDoc(t, filepath.Join(env.ProjectWT, "candidates", "project", "codex-01-session.md"), map[string]any{
		"schema":           "worktrail.candidate.v1",
		"id":               "codex-01-session",
		"scope":            "project",
		"candidate_type":   "transcript_notes",
		"target_path":      "imports/transcripts/codex-01-session.md",
		"title":            "Transcript Notes",
		"summary":          "Already imported transcript evidence.",
		"operation":        "replace",
		"status":           "pending",
		"source_sessions":  []string{"codex:session.jsonl"},
		"redaction_status": "clean",
		"created_at":       time.Now().UTC(),
		"updated_at":       time.Now().UTC(),
	}, "Evidence body.")

	pack, err := Build(env, Options{Task: "import discovery"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if pack.Maintenance.ImportableCodexSessions != 0 {
		t.Fatalf("importable codex = %d, want 0: %+v", pack.Maintenance.ImportableCodexSessions, pack.Maintenance)
	}
	if containsStep(pack.Maintenance.NextSteps, "worktrail import codex --since 14d --all") {
		t.Fatalf("maintenance next steps should not re-import represented transcript: %+v", pack.Maintenance.NextSteps)
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

func sectionIndex(pack Pack, title string) int {
	for i, section := range pack.Sections {
		if section.Title == title {
			return i
		}
	}
	return len(pack.Sections)
}

func containsStep(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
