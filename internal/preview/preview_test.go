package preview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestResolveProjectAndUserCollections(t *testing.T) {
	env := previewTestEnv(t)
	projectSource, err := Resolve(ResolveRequest{Env: env, Scope: "project"})
	if err != nil {
		t.Fatalf("Resolve project collection: %v", err)
	}
	if projectSource.Kind != SourceCollection || projectSource.Title != "Project Knowledge" || projectSource.Path != "." {
		t.Fatalf("project source unexpected: %+v", projectSource)
	}
	if !hasSourcePath(projectSource.Children, "project.md") {
		t.Fatalf("project collection missing project.md: %+v", projectSource.Children)
	}

	userSource, err := Resolve(ResolveRequest{Env: env, Scope: "user"})
	if err != nil {
		t.Fatalf("Resolve user collection: %v", err)
	}
	if userSource.Kind != SourceCollection || userSource.Title != "User Knowledge" || userSource.Path != "." {
		t.Fatalf("user source unexpected: %+v", userSource)
	}
	if !hasSourcePath(userSource.Children, "workflows/project-bootstrap.md") {
		t.Fatalf("user collection missing bootstrap workflow: %+v", userSource.Children)
	}
}

func TestResolveSkipsRuntimeArtifactsAndCollectsPendingCandidates(t *testing.T) {
	env := previewTestEnv(t)
	writePreviewFile(t, filepath.Join(env.ProjectWT, "rules", "visible.md"), "# Visible\n\nRule body.")
	writePreviewFile(t, filepath.Join(env.ProjectWT, "raw", "hidden.md"), "# Hidden Raw\n\nShould stay hidden.")
	writePreviewFile(t, filepath.Join(env.ProjectWT, "imports", "hidden.md"), "# Hidden Import\n\nShould stay hidden.")
	rec, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:         "project",
		ID:            "note-1",
		CandidateType: "rule",
		TargetPath:    "rules/note-1.md",
		Title:         "Candidate Preview",
		Body:          "# Candidate Body\n\nRendered body.",
	})
	if err != nil {
		t.Fatalf("Create candidate: %v", err)
	}
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:         "project",
		ID:            "discard-me",
		CandidateType: "rule",
		TargetPath:    "rules/discard-me.md",
		Title:         "Discard Me",
		Body:          "# Discarded\n\nHidden candidate.",
	}); err != nil {
		t.Fatalf("Create discard candidate: %v", err)
	}
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Discard("project", "discard-me"); err != nil {
		t.Fatalf("Discard candidate: %v", err)
	}
	source, err := Resolve(ResolveRequest{Env: env, Scope: "project"})
	if err != nil {
		t.Fatalf("Resolve project collection: %v", err)
	}
	if !hasSourcePath(source.Children, "rules/visible.md") {
		t.Fatalf("visible formal document missing: %+v", source.Children)
	}
	for _, hidden := range []string{"raw/hidden.md", "imports/hidden.md"} {
		if hasSourcePath(source.Children, hidden) {
			t.Fatalf("runtime/import document should be hidden: %s", hidden)
		}
	}
	if len(source.PendingCandidates) != 1 || source.PendingCandidates[0].ID != rec.Meta.ID {
		t.Fatalf("pending candidates unexpected: %+v", source.PendingCandidates)
	}
	if strings.Contains(source.PendingCandidates[0].Body, store.Marker) {
		t.Fatalf("pending candidate leaked frontmatter:\n%s", source.PendingCandidates[0].Body)
	}
}

func TestResolveExcludesHandoffRuntimeAndRetiredCandidates(t *testing.T) {
	env := previewTestEnv(t)
	writePreviewFile(t, filepath.Join(env.ProjectWT, "handoffs", "local", "local.md"), "# Local Handoff\n\nRuntime.")
	writePreviewFile(t, filepath.Join(env.ProjectWT, "handoffs", "team", "team.md"), "# Team Handoff\n\nRuntime.")
	legacyCandidate := model.Candidate{
		Schema: model.SchemaCandidate, ID: "legacy-handoff", Scope: "project",
		CandidateType: model.CandidateTypeHandoff, TargetPath: "handoffs/legacy.md",
		Title: "Legacy Handoff", Operation: candidate.OperationReplace, Status: candidate.StatusPending,
	}
	data, err := store.RenderMarkdown(legacyCandidate, "# Legacy Handoff\n\nOperational candidate.")
	if err != nil {
		t.Fatal(err)
	}
	writePreviewFile(t, filepath.Join(env.ProjectWT, "candidates", "project", "legacy-handoff.md"), string(data))

	source, err := Resolve(ResolveRequest{Env: env, Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if hasSourcePath(source.Children, "handoffs/local/local.md") || hasSourcePath(source.Children, "handoffs/team/team.md") {
		t.Fatalf("handoff runtime leaked into formal preview: %+v", source.Children)
	}
	for _, pending := range source.PendingCandidates {
		if pending.ID == "legacy-handoff" {
			t.Fatalf("retired handoff candidate leaked into preview: %+v", pending)
		}
	}
}

func TestResolveCollectionHidesWorktrailFrontmatter(t *testing.T) {
	env := previewTestEnv(t)
	path := filepath.Join(env.ProjectWT, "decisions", "frontmatter.md")
	if err := os.WriteFile(path, []byte(`---worktrail
{
  "stage": "decision"
}
---

# Decision Body

Visible content.
`), 0o644); err != nil {
		t.Fatal(err)
	}
	source, err := Resolve(ResolveRequest{Env: env, Scope: "project"})
	if err != nil {
		t.Fatalf("Resolve collection: %v", err)
	}
	doc := sourceByPath(source.Children, "decisions/frontmatter.md")
	if doc == nil {
		t.Fatalf("frontmatter document missing from collection: %+v", source.Children)
	}
	if strings.Contains(doc.Body, store.Marker) || strings.Contains(doc.Body, `"stage"`) {
		t.Fatalf("document body includes Worktrail frontmatter:\n%s", doc.Body)
	}
	if doc.Title != "Decision Body" {
		t.Fatalf("title = %q", doc.Title)
	}
}

func TestRenderBuildsStableKnowledgePage(t *testing.T) {
	env := previewTestEnv(t)
	writePreviewFile(t, filepath.Join(env.ProjectWT, "decisions", "choice.md"), "# Choice\n\nDecision body.")
	writePreviewFile(t, filepath.Join(env.ProjectWT, "workflows", "release.md"), "# Release\n\n```sh\necho ship\n```\n\n<script>alert(1)</script>\n\nWorkflow body.")
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:         "project",
		ID:            "semantic-rule",
		CandidateType: "rule",
		TargetPath:    "rules/semantic-rule.md",
		Title:         "Semantic Rule",
		Body:          "# Semantic Rule\n\n```text\ncandidate noise\n```\n\n<script>alert(1)</script>\n\nSemantic candidate body.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:         "project",
		ID:            "evidence-note",
		CandidateType: model.CandidateTypeTranscriptNotes,
		TargetPath:    "imports/transcripts/evidence-note.md",
		Title:         "Evidence Note",
		Body:          "# Evidence Note\n\nEvidence candidate body.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:         "project",
		ID:            "split-source",
		CandidateType: "lesson",
		TargetPath:    "lessons/kdd-active-knowledge-log.md",
		Title:         "Split Source",
		Summary:       "Do not promote directly.",
		Tags:          []string{"kdd", "split-source"},
		Body:          "# Split Source\n\nDo not promote directly.\n\nEvidence bucket body.",
	}); err != nil {
		t.Fatal(err)
	}
	source, err := Resolve(ResolveRequest{Env: env, Scope: "project"})
	if err != nil {
		t.Fatalf("Resolve collection: %v", err)
	}
	cacheDir := t.TempDir()
	first, err := Render(source, cacheDir)
	if err != nil {
		t.Fatalf("Render first: %v", err)
	}
	writePreviewFile(t, filepath.Join(cacheDir, "stale.html"), "<html>stale</html>")
	second, err := Render(source, cacheDir)
	if err != nil {
		t.Fatalf("Render second: %v", err)
	}
	if first.IndexPath != second.IndexPath {
		t.Fatalf("stable cache path mismatch: %s vs %s", first.IndexPath, second.IndexPath)
	}
	if first.IndexPath != filepath.Join(cacheDir, "index.html") {
		t.Fatalf("index path = %s", first.IndexPath)
	}
	if _, err := os.Stat(first.IndexPath); err != nil {
		t.Fatalf("expected preview file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "stale.html")); !os.IsNotExist(err) {
		t.Fatalf("stale file still exists after full-site rewrite: %v", err)
	}

	indexBody := string(first.HTML)
	for _, want := range []string{"Worktrail Preview", "Decisions", "Workflows", "Pending Candidates", "Semantic Candidates", "Evidence Candidates", "sections/decisions.html", "candidates/index.html", "candidates/semantic-rule.html"} {
		if !strings.Contains(indexBody, want) {
			t.Fatalf("index preview missing %q:\n%s", want, indexBody)
		}
	}
	for _, absent := range []string{"Decision body.", "Workflow body.", "Semantic candidate body.", "<script>"} {
		if strings.Contains(indexBody, absent) {
			t.Fatalf("index preview should not include %q:\n%s", absent, indexBody)
		}
	}

	sectionBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "sections", "decisions.html"))
	if !strings.Contains(sectionBody, "Choice") || !strings.Contains(sectionBody, "../docs/decisions-choice.html") {
		t.Fatalf("section page missing document link:\n%s", sectionBody)
	}
	if strings.Contains(sectionBody, `<article class="panel prose">`) {
		t.Fatalf("section page should not render full document articles:\n%s", sectionBody)
	}

	documentBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "docs", "decisions-choice.html"))
	if !strings.Contains(documentBody, "Decision body.") {
		t.Fatalf("document page missing full body:\n%s", documentBody)
	}

	workflowBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "docs", "workflows-release.html"))
	if !strings.Contains(workflowBody, "Workflow body.") {
		t.Fatalf("workflow page missing body:\n%s", workflowBody)
	}
	if strings.Contains(workflowBody, "<script>") {
		t.Fatalf("workflow page leaked raw script:\n%s", workflowBody)
	}

	workflowSectionBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "sections", "workflows.html"))
	if !strings.Contains(workflowSectionBody, "Workflow body.") {
		t.Fatalf("workflow section missing readable summary:\n%s", workflowSectionBody)
	}
	for _, absent := range []string{"<script>", "alert(1)", "echo ship"} {
		if strings.Contains(workflowSectionBody, absent) {
			t.Fatalf("workflow section summary leaked %q:\n%s", absent, workflowSectionBody)
		}
	}

	candidatesBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "candidates", "index.html"))
	for _, want := range []string{"Semantic Candidates", "Evidence Candidates", "Semantic Rule", "Evidence Note", "Split Source"} {
		if !strings.Contains(candidatesBody, want) {
			t.Fatalf("candidates page missing %q:\n%s", want, candidatesBody)
		}
	}
	if !strings.Contains(candidatesBody, "Semantic candidate body.") {
		t.Fatalf("candidates page missing readable semantic summary:\n%s", candidatesBody)
	}
	for _, absent := range []string{"<script>", "alert(1)", "candidate noise"} {
		if strings.Contains(candidatesBody, absent) {
			t.Fatalf("candidates summary leaked %q:\n%s", absent, candidatesBody)
		}
	}

	semanticBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "candidates", "semantic-rule.html"))
	if !strings.Contains(semanticBody, "Semantic candidate body.") {
		t.Fatalf("semantic candidate page missing body:\n%s", semanticBody)
	}
	if strings.Contains(semanticBody, "<script>") {
		t.Fatalf("candidate page leaked raw script:\n%s", semanticBody)
	}

	evidenceBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "candidates", "evidence-note.html"))
	if !strings.Contains(evidenceBody, "Evidence candidate body.") {
		t.Fatalf("evidence candidate page missing body:\n%s", evidenceBody)
	}

	splitSourceBody := mustReadPreviewFile(t, filepath.Join(cacheDir, "candidates", "split-source.html"))
	if !strings.Contains(splitSourceBody, "Do not promote directly.") {
		t.Fatalf("split-source candidate page missing body:\n%s", splitSourceBody)
	}
}

func TestClearCacheRemovesPreviewDirectory(t *testing.T) {
	env := previewTestEnv(t)
	dir, err := CacheDir(env, "project")
	if err != nil {
		t.Fatalf("CacheDir: %v", err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writePreviewFile(t, filepath.Join(dir, "stale.html"), "<html>old</html>")
	cleared, err := ClearCache(env, "project")
	if err != nil {
		t.Fatalf("ClearCache: %v", err)
	}
	if cleared != dir {
		t.Fatalf("cleared dir = %s, want %s", cleared, dir)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("cache dir still exists: %v", err)
	}
}

func TestPublishSiteRestoresPreviousSiteOnActivationFailure(t *testing.T) {
	root := t.TempDir()
	outDir := filepath.Join(root, "preview")
	stageDir := filepath.Join(root, "stage")
	writePreviewFile(t, filepath.Join(outDir, "index.html"), "<html>old</html>")
	writePreviewFile(t, filepath.Join(stageDir, "index.html"), "<html>new</html>")

	renameCalls := 0
	err := publishSiteWithOps(stageDir, outDir, os.Stat, func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("activate failed")
		}
		return os.Rename(oldPath, newPath)
	}, os.RemoveAll)
	if err == nil || !strings.Contains(err.Error(), "activate failed") {
		t.Fatalf("expected activation failure, got %v", err)
	}

	if got := mustReadPreviewFile(t, filepath.Join(outDir, "index.html")); !strings.Contains(got, "old") {
		t.Fatalf("rollback did not restore previous site:\n%s", got)
	}
	if got := mustReadPreviewFile(t, filepath.Join(stageDir, "index.html")); !strings.Contains(got, "new") {
		t.Fatalf("staged site should remain available after failed activation:\n%s", got)
	}
	if _, err := os.Stat(outDir + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("backup directory should not remain after rollback: %v", err)
	}
}

func previewTestEnv(t *testing.T) paths.Env {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	env := paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	if err := store.InitUser(env); err != nil {
		t.Fatalf("InitUser: %v", err)
	}
	if err := store.InitProject(env); err != nil {
		t.Fatalf("InitProject: %v", err)
	}
	return env
}

func hasSourcePath(items []Source, want string) bool {
	return sourceByPath(items, want) != nil
}

func sourceByPath(items []Source, want string) *Source {
	for i := range items {
		if items[i].Path == want {
			return &items[i]
		}
	}
	return nil
}

func writePreviewFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadPreviewFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read preview file %s: %v", path, err)
	}
	return string(body)
}
