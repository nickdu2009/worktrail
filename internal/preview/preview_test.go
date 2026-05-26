package preview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
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
	writePreviewFile(t, filepath.Join(env.ProjectWT, "workflows", "release.md"), "# Release\n\nWorkflow body.")
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:         "project",
		ID:            "pending-rule",
		CandidateType: "rule",
		TargetPath:    "rules/pending-rule.md",
		Title:         "Pending Rule",
		Body:          "# Pending Rule\n\n<script>alert(1)</script>\n\nCandidate body.",
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
	second, err := Render(source, cacheDir)
	if err != nil {
		t.Fatalf("Render second: %v", err)
	}
	if first.IndexPath != second.IndexPath {
		t.Fatalf("stable cache path mismatch: %s vs %s", first.IndexPath, second.IndexPath)
	}
	body := string(first.HTML)
	for _, want := range []string{"Worktrail Preview", "Decisions", "Workflows", "Pending Candidates", "Decision body.", "Workflow body.", "Pending Rule"} {
		if !strings.Contains(body, want) {
			t.Fatalf("knowledge preview missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Fatalf("rendered HTML includes raw script:\n%s", body)
	}
	if _, err := os.Stat(first.IndexPath); err != nil {
		t.Fatalf("expected preview file: %v", err)
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
