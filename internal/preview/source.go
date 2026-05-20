package preview

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

type SourceKind string

const (
	SourceDocument  SourceKind = "document"
	SourceCandidate SourceKind = "candidate"
)

var (
	ErrTargetRequired      = errors.New("preview target is required")
	ErrUnsupportedFileType = errors.New("unsupported Worktrail document type")
)

type Source struct {
	Kind     SourceKind        `json:"kind"`
	Scope    string            `json:"scope"`
	ID       string            `json:"id,omitempty"`
	Title    string            `json:"title"`
	Path     string            `json:"path"`
	Body     string            `json:"-"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type ResolveRequest struct {
	Env         paths.Env
	Scope       string
	Target      string
	CandidateID string
}

type RenderResult struct {
	Source    Source `json:"source"`
	HTML      []byte `json:"-"`
	OutputDir string `json:"output_dir"`
	IndexPath string `json:"index_path"`
	Temporary bool   `json:"temporary"`
}

type ServeResult struct {
	URL  string
	Stop func() error
}

func Resolve(req ResolveRequest) (Source, error) {
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		scope = "project"
	}
	if strings.TrimSpace(req.CandidateID) != "" {
		return resolveCandidate(req.Env, scope, req.CandidateID)
	}

	target := strings.TrimSpace(req.Target)
	if target == "" {
		return Source{}, ErrTargetRequired
	}
	if shouldTryCandidate(target) {
		if src, err := resolveCandidate(req.Env, scope, target); err == nil {
			return src, nil
		} else if !errors.Is(err, candidate.ErrNotFound) {
			return Source{}, err
		}
	}
	return resolveDocument(req.Env, scope, target)
}

func resolveCandidate(env paths.Env, scope, id string) (Source, error) {
	rec, err := (candidate.Manager{Env: env, Actor: "cli:preview"}).Show(scope, id)
	if err != nil {
		return Source{}, err
	}
	title := strings.TrimSpace(rec.Meta.Title)
	if title == "" {
		title = rec.Meta.ID
	}
	return Source{
		Kind:  SourceCandidate,
		Scope: rec.Meta.Scope,
		ID:    rec.Meta.ID,
		Title: title,
		Path:  filepath.ToSlash(filepath.Join("candidates", rec.Meta.Scope, rec.Meta.ID+".md")),
		Body:  rec.Body,
		Metadata: map[string]string{
			"candidate_id":     rec.Meta.ID,
			"status":           rec.Meta.Status,
			"candidate_type":   rec.Meta.CandidateType,
			"operation":        rec.Meta.Operation,
			"target_path":      rec.Meta.TargetPath,
			"redaction_status": rec.Meta.RedactionStatus,
		},
	}, nil
}

func resolveDocument(env paths.Env, scope, target string) (Source, error) {
	if filepath.IsAbs(target) {
		return Source{}, fmt.Errorf("%w: absolute paths are outside Worktrail roots", ErrUnsupportedFileType)
	}
	if filepath.Ext(target) != ".md" {
		return Source{}, fmt.Errorf("%w: %s", ErrUnsupportedFileType, filepath.Ext(target))
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return Source{}, err
	}
	path, err := paths.SafeJoin(root, target)
	if err != nil {
		return Source{}, err
	}
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

func shouldTryCandidate(target string) bool {
	return !strings.ContainsAny(target, `/\`) && filepath.Ext(target) == ""
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
