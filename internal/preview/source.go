package preview

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

type SourceKind string

const (
	SourceCollection SourceKind = "collection"
	SourceDocument   SourceKind = "document"
	SourceCandidate  SourceKind = "candidate"
)

type Source struct {
	Kind              SourceKind        `json:"kind"`
	Scope             string            `json:"scope"`
	ID                string            `json:"id,omitempty"`
	Title             string            `json:"title"`
	Path              string            `json:"path"`
	Body              string            `json:"-"`
	Summary           string            `json:"summary,omitempty"`
	Tags              []string          `json:"tags,omitempty"`
	Metadata          map[string]string `json:"metadata,omitempty"`
	Children          []Source          `json:"children,omitempty"`
	PendingCandidates []Source          `json:"pending_candidates,omitempty"`
}

type ResolveRequest struct {
	Env   paths.Env
	Scope string
}

type RenderResult struct {
	Source    Source `json:"source"`
	HTML      []byte `json:"-"`
	OutputDir string `json:"output_dir"`
	IndexPath string `json:"index_path"`
}

func Resolve(req ResolveRequest) (Source, error) {
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "project"
	}
	root, err := req.Env.ScopeRoot(scope)
	if err != nil {
		return Source{}, err
	}

	docs, err := resolveDocuments(scope, root)
	if err != nil {
		return Source{}, err
	}
	pending, err := resolvePendingCandidates(req.Env, scope)
	if err != nil {
		return Source{}, err
	}

	return Source{
		Kind:              SourceCollection,
		Scope:             scope,
		Title:             titleForScope(scope),
		Path:              ".",
		Children:          docs,
		PendingCandidates: pending,
		Metadata: map[string]string{
			"source_path":             ".",
			"file_count":              fmt.Sprintf("%d", len(docs)),
			"pending_candidate_count": fmt.Sprintf("%d", len(pending)),
		},
	}, nil
}

func resolveDocuments(scope, root string) ([]Source, error) {
	var docs []Source
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && shouldSkipPreviewDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(entry.Name()) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if shouldSkipPreviewPath(filepath.ToSlash(rel)) {
			return nil
		}
		src, err := readDocumentSource(scope, root, path)
		if err != nil {
			return err
		}
		docs = append(docs, src)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(docs, func(i, j int) bool {
		return docs[i].Path < docs[j].Path
	})
	return docs, nil
}

func resolvePendingCandidates(env paths.Env, scope string) ([]Source, error) {
	records, err := (candidate.Manager{Env: env, Actor: "cli:preview"}).List(scope)
	if err != nil {
		return nil, err
	}
	pending := make([]Source, 0, len(records))
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPending {
			continue
		}
		title := strings.TrimSpace(rec.Meta.Title)
		if title == "" {
			title = rec.Meta.ID
		}
		pending = append(pending, Source{
			Kind:    SourceCandidate,
			Scope:   rec.Meta.Scope,
			ID:      rec.Meta.ID,
			Title:   title,
			Path:    filepath.ToSlash(filepath.Join("candidates", rec.Meta.Scope, rec.Meta.ID+".md")),
			Body:    rec.Body,
			Summary: rec.Meta.Summary,
			Tags:    append([]string(nil), rec.Meta.Tags...),
			Metadata: map[string]string{
				"candidate_id":      rec.Meta.ID,
				"candidate_surface": model.CandidateSurface(rec.Meta.CandidateType),
				"status":            rec.Meta.Status,
				"candidate_type":    rec.Meta.CandidateType,
				"operation":         rec.Meta.Operation,
				"target_path":       rec.Meta.TargetPath,
				"redaction_status":  rec.Meta.RedactionStatus,
			},
		})
	}
	return pending, nil
}

func readDocumentSource(scope, root, path string) (Source, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Source{}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return Source{}, err
	}
	rel = filepath.ToSlash(rel)
	body := string(data)
	if doc, err := store.ParseMarkdown(data); err == nil {
		body = doc.Body
	}
	title := titleFromMarkdown(body)
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	}
	return Source{
		Kind:  SourceDocument,
		Scope: scope,
		Title: title,
		Path:  rel,
		Body:  body,
		Metadata: map[string]string{
			"source_path": rel,
		},
	}, nil
}

func shouldSkipPreviewDir(name string) bool {
	switch name {
	case "index", "logs", "raw", "exports", "candidates", "state", "imports", ".cache":
		return true
	default:
		return false
	}
}

func shouldSkipPreviewPath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || strings.HasPrefix(rel, ".cache/") {
		return true
	}
	return false
}

func titleForScope(scope string) string {
	if scope == "user" {
		return "User Knowledge"
	}
	return "Project Knowledge"
}

func titleFromMarkdown(body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "# "))
		}
	}
	return ""
}
