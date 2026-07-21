package contextpack

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestBuildIncludesRequiredSectionsAndMarksCandidatesUnapproved(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	if err := os.MkdirAll(env.ProjectWT, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), []byte(`{"project_id":"project-context-test"}`), 0o644); err != nil {
		t.Fatal(err)
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
		"schema":      model.SchemaState,
		"id":          "current",
		"scope":       "project",
		"task_id":     "task-current",
		"type":        "session",
		"title":       "Current Work",
		"status":      "active",
		"source_tool": "worktrail",
		"created_at":  time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
		"updated_at":  time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
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
	for _, title := range []string{"User Knowledge", "Requirements", "Architecture", "Workflows", "Decisions", "Rules", "Pending Candidates"} {
		if !hasSection(pack, title) {
			t.Fatalf("missing section %q in %+v", title, pack.Sections)
		}
	}
	if len(pack.Tasks) != 1 || pack.Tasks[0].TaskID != "task-current" || pack.Tasks[0].SourceKind != "explicit_state" {
		t.Fatalf("task recovery summary unexpected: %+v", pack.Tasks)
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
	if !strings.Contains(rendered, "## Task Recovery Summary") || !strings.Contains(rendered, "`task-current` — Current Work [source:explicit_state]") {
		t.Fatalf("rendered pack missing task summary:\n%s", rendered)
	}
	if strings.Contains(rendered, "Implement local packages.") || strings.Contains(rendered, "Wire CLI later.") {
		t.Fatalf("rendered pack leaked raw task recovery content:\n%s", rendered)
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

func TestBuildRefreshesIndexBeforeContext(t *testing.T) {
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
	if len(pack.IndexHealth) != 0 {
		t.Fatalf("refresh should keep index fresh, IndexHealth = %+v", pack.IndexHealth)
	}
	rendered := RenderMarkdown(pack)
	if !strings.Contains(rendered, "Current Rule") || !strings.Contains(rendered, "New Rule") {
		t.Fatalf("rendered pack should include refreshed entries:\n%s", rendered)
	}
	if strings.Contains(rendered, "Deleted Rule") {
		t.Fatalf("rendered pack should hide deleted indexed entries:\n%s", rendered)
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

func TestBuildTopicFiltersStateAndHandoffs(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	if err := os.MkdirAll(env.ProjectWT, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), []byte(`{"project_id":"project-context-test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "delivery.md"), map[string]any{
		"id":    "delivery-rule",
		"scope": "project",
		"type":  "rule",
		"title": "Delivery Rule",
		"topic": "delivery",
	}, "Delivery guidance.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "billing.md"), map[string]any{
		"id":    "billing-rule",
		"scope": "project",
		"type":  "rule",
		"title": "Billing Rule",
		"topic": "billing",
	}, "Billing guidance.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "state", "active", "delivery.md"), map[string]any{
		"schema":      model.SchemaState,
		"id":          "delivery-state",
		"scope":       "project",
		"task_id":     "task-delivery",
		"type":        "session",
		"title":       "Delivery State",
		"status":      "active",
		"source_tool": "worktrail",
		"topic":       "delivery",
		"created_at":  time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
		"updated_at":  time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
	}, "Work on delivery thread.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "state", "active", "billing.md"), map[string]any{
		"schema":      model.SchemaState,
		"id":          "billing-state",
		"scope":       "project",
		"task_id":     "task-billing",
		"type":        "session",
		"title":       "Billing State",
		"status":      "active",
		"source_tool": "worktrail",
		"topic":       "billing",
		"created_at":  time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC),
		"updated_at":  time.Date(2026, 7, 15, 2, 0, 0, 0, time.UTC),
	}, "Work on billing thread.")
	pack, err := Build(env, Options{Task: "delivery task", Topic: "delivery"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	rendered := RenderMarkdown(pack)
	for _, want := range []string{"Delivery Rule", "Delivery State", "Billing State"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("topic-scoped context missing %q:\n%s", want, rendered)
		}
	}
	for _, absent := range []string{"Billing Rule", "Delivery Handoff", "Billing Handoff", "Work on delivery thread.", "Work on billing thread."} {
		if strings.Contains(rendered, absent) {
			t.Fatalf("topic-scoped context leaked %q:\n%s", absent, rendered)
		}
	}
	if len(pack.Tasks) != 2 {
		t.Fatalf("expected one summary per task, got %+v", pack.Tasks)
	}
}

func TestBuildReplacesRawStateAndHandoffsWithOneTaskSummary(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	if err := os.MkdirAll(env.ProjectWT, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), []byte(`{"project_id":"project-context-test"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	writePackDoc(t, filepath.Join(env.ProjectWT, "state", "active", "current.md"), map[string]any{
		"schema":      model.SchemaState,
		"id":          "current",
		"scope":       "project",
		"task_id":     "task-current",
		"type":        "session",
		"title":       "Current Work",
		"status":      "active",
		"source_tool": "worktrail",
		"created_at":  time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
		"updated_at":  time.Date(2026, 7, 15, 1, 0, 0, 0, time.UTC),
	}, "Continue current work.")
	createPackHandoff(t, env, "handoff-c", "Superseded Handoff", "C")
	createPackHandoff(t, env, "handoff-b", "Current Handoff B", "B")
	createPackHandoff(t, env, "handoff-a", "Current Handoff A", "A")

	pack, err := Build(env, Options{Task: "limit handoffs"})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if hasSection(pack, "Handoffs") || hasSection(pack, "Active State") {
		t.Fatalf("raw task recovery sections should be omitted: %+v", pack.Sections)
	}
	if len(pack.Tasks) != 1 || pack.Tasks[0].TaskID != "task-current" || pack.Tasks[0].Title != "Current Handoff A" || pack.Tasks[0].SourceKind != "local_handoff" {
		t.Fatalf("task summaries = %+v", pack.Tasks)
	}
	rendered := RenderMarkdown(pack)
	for _, raw := range []string{"Continue current work.", "\nA\n", "\nB\n", "\nC\n"} {
		if strings.Contains(rendered, raw) {
			t.Fatalf("rendered context leaked raw task record %q:\n%s", raw, rendered)
		}
	}
}

func TestBuildSelectorOnlyUsesKnowledgeSections(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	writeSelectorKnowledgeFixture(t, env)
	selector := &fakeSelector{}

	pack, err := Build(env, Options{Task: "select knowledge", IncludeEvidence: true, Selector: selector})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	wantSections := map[string]bool{
		"User Knowledge": true, "Project Knowledge": true, "Requirements": true, "Architecture": true,
		"Decisions": true, "Validation": true, "Rules": true, "Workflows": true,
		"Integrations": true, "Glossary": true,
	}
	if len(selector.calls) != len(wantSections) {
		t.Fatalf("selector calls = %d, want %d: %+v", len(selector.calls), len(wantSections), selector.calls)
	}
	for _, call := range selector.calls {
		if !wantSections[call.Section] {
			t.Fatalf("selector called deterministic section %q", call.Section)
		}
		if len(call.Candidates) != 1 {
			t.Fatalf("selector candidates for %q = %+v, want one pre-filtered item", call.Section, call.Candidates)
		}
		delete(wantSections, call.Section)
	}
	if len(wantSections) != 0 {
		t.Fatalf("selector missed knowledge sections: %+v", wantSections)
	}
	for _, taskRecovery := range []string{"Active State", "Handoffs", "Recovery"} {
		if hasSection(pack, taskRecovery) {
			t.Fatalf("task recovery section %q should not be rendered directly: %+v", taskRecovery, pack.Sections)
		}
	}
	if !hasItem(section(pack, "Pending Candidates"), "Transcript Evidence") {
		t.Fatalf("evidence toggle should remain deterministic: %+v", section(pack, "Pending Candidates"))
	}
}

func TestBuildSelectorRequirementsWithoutTopicRanksAllCandidates(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{ProjectWT: filepath.Join(tmp, "project", ".worktrail")}
	for _, requirement := range []struct {
		name, topic, title string
	}{
		{"delivery", "delivery", "Delivery Requirement"},
		{"billing", "billing", "Billing Requirement"},
		{"security", "security", "Security Requirement"},
	} {
		writePackDoc(t, filepath.Join(env.ProjectWT, "requirements", requirement.name+".md"), map[string]any{
			"id": requirement.name, "scope": "project", "type": "requirement", "title": requirement.title, "topic": requirement.topic,
		}, requirement.title)
	}
	selector := &fakeSelector{selectFn: func(request SelectionRequest) ([]Item, error) {
		if request.Section != "Requirements" {
			return request.Candidates, nil
		}
		return []Item{request.Candidates[2], request.Candidates[1], request.Candidates[0]}, nil
	}}

	pack, err := Build(env, Options{Task: "rank requirements", Limit: 3, Selector: selector})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if len(selector.calls) != 1 || selector.calls[0].Section != "Requirements" || len(selector.calls[0].Candidates) != 3 {
		t.Fatalf("requirements selector request = %+v, want all three candidates", selector.calls)
	}
	requirements := section(pack, "Requirements")
	want := itemTitles(selector.calls[0].Candidates)
	reverseStrings(want)
	if got := itemTitles(requirements.Items); !sameStrings(got, want) {
		t.Fatalf("requirements selector order = %v, want %v", got, want)
	}
}

func TestBuildSelectorTopicPinsRequirementsAndFiltersOtherKnowledge(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{ProjectWT: filepath.Join(tmp, "project", ".worktrail")}
	writePackDoc(t, filepath.Join(env.ProjectWT, "requirements", "delivery.md"), map[string]any{
		"id": "delivery", "scope": "project", "type": "requirement", "title": "Delivery Requirement", "topic": "delivery",
		"updated_at": "2026-07-01T00:00:00Z",
	}, "Pinned delivery requirement.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "requirements", "billing.md"), map[string]any{
		"id": "billing", "scope": "project", "type": "requirement", "title": "Billing Requirement", "topic": "billing",
		"updated_at": "2026-07-02T00:00:00Z",
	}, "Cross-topic fill requirement.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "delivery.md"), map[string]any{
		"id": "delivery-rule", "scope": "project", "type": "rule", "title": "Delivery Rule", "topic": "delivery",
	}, "Delivery guidance.")
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "billing.md"), map[string]any{
		"id": "billing-rule", "scope": "project", "type": "rule", "title": "Billing Rule", "topic": "billing",
	}, "Billing guidance.")
	selector := &fakeSelector{selectFn: func(request SelectionRequest) ([]Item, error) {
		switch request.Section {
		case "Requirements":
			if request.Limit != 1 || len(request.Candidates) != 1 || request.Candidates[0].Topic != "billing" {
				t.Fatalf("requirements fill request = %+v, want one billing candidate and one remaining slot", request)
			}
		case "Rules":
			if len(request.Candidates) != 1 || request.Candidates[0].Topic != "delivery" {
				t.Fatalf("rules request should hard-filter topic: %+v", request)
			}
		}
		return request.Candidates, nil
	}}

	pack, err := Build(env, Options{Task: "delivery work", Topic: "delivery", Limit: 2, Selector: selector})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if got, want := itemTitles(section(pack, "Requirements").Items), []string{"Delivery Requirement", "Billing Requirement"}; !sameStrings(got, want) {
		t.Fatalf("requirements = %v, want pinned item before semantic fill %v", got, want)
	}
	if got, want := itemTitles(section(pack, "Rules").Items), []string{"Delivery Rule"}; !sameStrings(got, want) {
		t.Fatalf("rules = %v, want topic-filtered %v", got, want)
	}
}

func TestBuildSelectorRejectsFailureAndInvalidOutput(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{ProjectWT: filepath.Join(tmp, "project", ".worktrail")}
	for _, name := range []string{"one", "two"} {
		writePackDoc(t, filepath.Join(env.ProjectWT, "rules", name+".md"), map[string]any{
			"id": name, "scope": "project", "type": "rule", "title": name,
		}, "rule")
	}
	tests := []struct {
		name     string
		limit    int
		selectFn func(SelectionRequest) ([]Item, error)
		want     string
	}{
		{
			name:  "selector error",
			limit: 1,
			selectFn: func(SelectionRequest) ([]Item, error) {
				return nil, errors.New("semantic runtime unavailable")
			},
			want: "semantic runtime unavailable",
		},
		{
			name:  "unknown candidate",
			limit: 1,
			selectFn: func(SelectionRequest) ([]Item, error) {
				return []Item{{Path: "rules/not-a-candidate.md"}}, nil
			},
			want: "non-candidate",
		},
		{
			name:  "duplicate candidate",
			limit: 2,
			selectFn: func(request SelectionRequest) ([]Item, error) {
				return []Item{request.Candidates[0], request.Candidates[0]}, nil
			},
			want: "duplicate",
		},
		{
			name:  "exceeds limit",
			limit: 1,
			selectFn: func(request SelectionRequest) ([]Item, error) {
				return request.Candidates, nil
			},
			want: "limit is 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Build(env, Options{Task: "validate selector", Limit: tt.limit, Selector: &fakeSelector{selectFn: tt.selectFn}})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Build() error = %v, want %q", err, tt.want)
			}
		})
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

type fakeSelector struct {
	calls    []SelectionRequest
	selectFn func(SelectionRequest) ([]Item, error)
}

func (s *fakeSelector) Select(request SelectionRequest) ([]Item, error) {
	s.calls = append(s.calls, request)
	if s.selectFn != nil {
		return s.selectFn(request)
	}
	return request.Candidates, nil
}

func writeSelectorKnowledgeFixture(t *testing.T, env paths.Env) {
	t.Helper()
	docs := []struct {
		path string
		meta map[string]any
	}{
		{filepath.Join(env.UserRoot, "profile", "preferences.md"), map[string]any{"id": "profile", "scope": "user", "type": "profile", "title": "Preferences"}},
		{filepath.Join(env.ProjectWT, "project.md"), map[string]any{"id": "project", "scope": "project", "type": "project", "title": "Project"}},
		{filepath.Join(env.ProjectWT, "requirements", "requirement.md"), map[string]any{"id": "requirement", "scope": "project", "type": "requirement", "title": "Requirement"}},
		{filepath.Join(env.ProjectWT, "architecture", "architecture.md"), map[string]any{"id": "architecture", "scope": "project", "type": "architecture", "title": "Architecture"}},
		{filepath.Join(env.ProjectWT, "decisions", "decision.md"), map[string]any{"id": "decision", "scope": "project", "type": "decision", "title": "Decision"}},
		{filepath.Join(env.ProjectWT, "validation", "validation.md"), map[string]any{"id": "validation", "scope": "project", "type": "validation", "title": "Validation"}},
		{filepath.Join(env.ProjectWT, "rules", "rule.md"), map[string]any{"id": "rule", "scope": "project", "type": "rule", "title": "Rule"}},
		{filepath.Join(env.ProjectWT, "workflows", "workflow.md"), map[string]any{"id": "workflow", "scope": "project", "type": "workflow", "title": "Workflow"}},
		{filepath.Join(env.ProjectWT, "integrations", "integration.md"), map[string]any{"id": "integration", "scope": "project", "type": "integration", "title": "Integration"}},
		{filepath.Join(env.ProjectWT, "glossary", "glossary.md"), map[string]any{"id": "glossary", "scope": "project", "type": "glossary", "title": "Glossary"}},
		{filepath.Join(env.ProjectWT, "candidates", "project", "evidence.md"), map[string]any{"id": "evidence", "scope": "project", "candidate_type": "transcript_notes", "title": "Transcript Evidence", "status": "pending"}},
	}
	for _, doc := range docs {
		writePackDoc(t, doc.path, doc.meta, doc.meta["title"].(string))
	}
}

func itemTitles(items []Item) []string {
	titles := make([]string, len(items))
	for i, item := range items {
		titles[i] = item.Title
	}
	return titles
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func reverseStrings(values []string) {
	for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
		values[i], values[j] = values[j], values[i]
	}
}


func TestBuildScopeQualifiedSupersessionAndEntryID(t *testing.T) {
	tmp := t.TempDir()
	env := paths.Env{
		UserRoot:  filepath.Join(tmp, "user"),
		ProjectWT: filepath.Join(tmp, "project", ".worktrail"),
	}
	if err := os.MkdirAll(env.ProjectWT, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(env.ProjectWT, "config.json"), []byte(`{"project_id":"dual-scope"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Same relative path and same entry ID across scopes, with different supersession.
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "shared.md"), map[string]any{
		"id": "shared-id", "scope": "project", "type": "rule", "title": "Project Shared",
		"supersedes": []string{"rules/old.md"},
	}, "project shared")
	writePackDoc(t, filepath.Join(env.ProjectWT, "rules", "old.md"), map[string]any{
		"id": "project-old", "scope": "project", "type": "rule", "title": "Project Old",
		"lifecycle": "historical",
	}, "project old")
	writePackDoc(t, filepath.Join(env.UserRoot, "rules", "shared.md"), map[string]any{
		"id": "shared-id", "scope": "user", "type": "rule", "title": "User Shared",
	}, "user shared")
	writePackDoc(t, filepath.Join(env.UserRoot, "rules", "old.md"), map[string]any{
		"id": "user-old", "scope": "user", "type": "rule", "title": "User Old",
	}, "user old")

	pack, err := Build(env, Options{Task: "dual scope", Limit: 10, IncludeLifecycle: []string{"current", "historical"}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	rules := section(pack, "Rules")
	var projectOld, userOld, projectShared, userShared Item
	for _, item := range rules.Items {
		switch item.Scope + ":" + item.Title {
		case "project:Project Old":
			projectOld = item
		case "user:User Old":
			userOld = item
		case "project:Project Shared":
			projectShared = item
		case "user:User Shared":
			userShared = item
		}
	}
	if projectShared.EntryID != "shared-id" || userShared.EntryID != "shared-id" {
		t.Fatalf("EntryID not populated: project=%q user=%q", projectShared.EntryID, userShared.EntryID)
	}
	if len(projectOld.SupersededBy) != 1 || projectOld.SupersededBy[0] != "rules/shared.md" {
		t.Fatalf("project old supersession = %#v", projectOld.SupersededBy)
	}
	if len(userOld.SupersededBy) != 0 {
		t.Fatalf("user old unexpectedly superseded across scope: %#v", userOld.SupersededBy)
	}

	selector := &fakeSelector{selectFn: func(request SelectionRequest) ([]Item, error) {
		// Prefer the user item with same entry id over project via ranking identity.
		out := make([]Item, 0, len(request.Candidates))
		for _, item := range request.Candidates {
			if item.Scope == "user" && item.EntryID == "shared-id" {
				out = append([]Item{item}, out...)
				continue
			}
			out = append(out, item)
		}
		if len(out) > request.Limit {
			out = out[:request.Limit]
		}
		return out, nil
	}}
	pack, err = Build(env, Options{Task: "dual scope", Limit: 2, Selector: selector, IncludeLifecycle: []string{"current", "historical"}})
	if err != nil {
		t.Fatalf("Build with selector: %v", err)
	}
	rules = section(pack, "Rules")
	if len(rules.Items) == 0 || rules.Items[0].Scope != "user" || rules.Items[0].EntryID != "shared-id" {
		t.Fatalf("selector did not prefer (scope,entry_id): %#v", rules.Items)
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

func createPackHandoff(t *testing.T, env paths.Env, id, title, body string) {
	t.Helper()
	_, err := handoff.CreateLocal(context.Background(), env, handoff.CreateRequest{
		ID:         id,
		Scope:      "project",
		Title:      title,
		Summary:    title,
		Complete:   true,
		TaskID:     "task-current",
		Body:       body,
		SourceTool: "worktrail",
		Worktree: model.WorktreeSnapshot{
			CodeAvailability: model.CodeAvailabilityUnavailable,
			CapturedAt:       time.Now().UTC(),
		},
	})
	if err != nil {
		t.Fatalf("CreateLocal(%s): %v", id, err)
	}
}
