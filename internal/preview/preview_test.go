package preview

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestResolveProjectAndUserDocuments(t *testing.T) {
	env := previewTestEnv(t)
	projectSource, err := Resolve(ResolveRequest{Env: env, Scope: "project", Target: "project.md"})
	if err != nil {
		t.Fatalf("Resolve project document: %v", err)
	}
	if projectSource.Kind != SourceDocument || projectSource.Title != "Project" || projectSource.Path != "project.md" {
		t.Fatalf("project source unexpected: %+v", projectSource)
	}

	userSource, err := Resolve(ResolveRequest{Env: env, Scope: "user", Target: "workflows/project-bootstrap.md"})
	if err != nil {
		t.Fatalf("Resolve user document: %v", err)
	}
	if userSource.Kind != SourceDocument || userSource.Title != "Project Bootstrap" || userSource.Path != "workflows/project-bootstrap.md" {
		t.Fatalf("user source unexpected: %+v", userSource)
	}
}

func TestResolveCandidateUsesBodyWithoutFrontmatter(t *testing.T) {
	env := previewTestEnv(t)
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
	source, err := Resolve(ResolveRequest{Env: env, Scope: "project", CandidateID: rec.Meta.ID})
	if err != nil {
		t.Fatalf("Resolve candidate: %v", err)
	}
	if source.Kind != SourceCandidate || source.ID != "note-1" {
		t.Fatalf("candidate source unexpected: %+v", source)
	}
	if strings.Contains(source.Body, store.Marker) {
		t.Fatalf("candidate body includes Worktrail frontmatter:\n%s", source.Body)
	}
	if source.Metadata["target_path"] != "rules/note-1.md" {
		t.Fatalf("candidate metadata missing target path: %+v", source.Metadata)
	}
}

func TestResolveDocumentHidesWorktrailFrontmatter(t *testing.T) {
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
	source, err := Resolve(ResolveRequest{Env: env, Scope: "project", Target: "decisions/frontmatter.md"})
	if err != nil {
		t.Fatalf("Resolve document: %v", err)
	}
	if strings.Contains(source.Body, store.Marker) || strings.Contains(source.Body, `"stage"`) {
		t.Fatalf("document body includes Worktrail frontmatter:\n%s", source.Body)
	}
	if source.Title != "Decision Body" {
		t.Fatalf("title = %q", source.Title)
	}
}

func TestResolveRejectsEscapingAndUnsupportedPaths(t *testing.T) {
	env := previewTestEnv(t)
	if _, err := Resolve(ResolveRequest{Env: env, Scope: "project", Target: "../outside.md"}); err == nil {
		t.Fatalf("expected path escape error")
	}
	if _, err := Resolve(ResolveRequest{Env: env, Scope: "project", Target: "logs/events.jsonl"}); !errors.Is(err, ErrUnsupportedFileType) {
		t.Fatalf("expected unsupported type error, got %v", err)
	}
}

func TestRenderIncludesMetadataAndOmitsRawHTML(t *testing.T) {
	dir := t.TempDir()
	result, err := Render(Source{
		Kind:  SourceCandidate,
		Scope: "project",
		ID:    "note-1",
		Title: "Preview Title",
		Path:  "candidates/project/note-1.md",
		Body:  "# Preview Title\n\n<script>alert(1)</script>\n\n| A | B |\n| - | - |\n| 1 | 2 |",
		Metadata: map[string]string{
			"status":      "pending",
			"target_path": "rules/note-1.md",
		},
	}, dir)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if result.IndexPath != filepath.Join(dir, "index.html") {
		t.Fatalf("index path = %s", result.IndexPath)
	}
	body := string(result.HTML)
	for _, want := range []string{"Worktrail Preview", "Preview Title", "target_path", "rules/note-1.md", "<table>"} {
		if !strings.Contains(body, want) {
			t.Fatalf("rendered HTML missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "<script>") {
		t.Fatalf("rendered HTML includes raw script:\n%s", body)
	}
	if _, err := os.Stat(result.IndexPath); err != nil {
		t.Fatalf("expected index.html: %v", err)
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
