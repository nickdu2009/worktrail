package handoff

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

const Schema = model.SchemaHandoff

type Metadata struct {
	Schema            string    `json:"schema"`
	ID                string    `json:"id"`
	Scope             string    `json:"scope"`
	Type              string    `json:"type"`
	Title             string    `json:"title"`
	Summary           string    `json:"summary,omitempty"`
	Status            string    `json:"status,omitempty"`
	TaskID            string    `json:"task_id,omitempty"`
	SourceStateID     string    `json:"source_state_id,omitempty"`
	PreviousHandoffID string    `json:"previous_handoff_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Tags              []string  `json:"tags,omitempty"`
}

type Record struct {
	Meta        Metadata       `json:"meta"`
	Body        string         `json:"body"`
	Path        string         `json:"path"`
	MetadataMap map[string]any `json:"metadata"`
}

type CreateOptions struct {
	Scope             string
	Title             string
	Summary           string
	TaskID            string
	SourceStateID     string
	PreviousHandoffID string
	Tags              []string
	Body              string
	Actor             string
}

func Create(env paths.Env, opts CreateOptions) (Record, error) {
	root, scope, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Record{}, err
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Handoff"
	}
	ts := time.Now().UTC()
	id := fmt.Sprintf("handoff_%s_%s", util.Slug(title), ts.Format("20060102T150405.000000000Z"))
	fileName := ts.Format("20060102-150405") + "-" + util.Slug(title) + ".md"
	path, err := paths.SafeJoin(root, "handoffs", fileName)
	if err != nil {
		return Record{}, err
	}
	meta := Metadata{
		Schema:            Schema,
		ID:                id,
		Scope:             scope,
		Type:              "handoff",
		Title:             title,
		Summary:           strings.TrimSpace(opts.Summary),
		Status:            "current",
		TaskID:            withDefault(opts.TaskID, "task-"+util.Slug(title)),
		SourceStateID:     strings.TrimSpace(opts.SourceStateID),
		PreviousHandoffID: strings.TrimSpace(opts.PreviousHandoffID),
		CreatedAt:         ts,
		UpdatedAt:         ts,
		Tags:              cleanList(opts.Tags),
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Record{}, err
	}
	data, err := store.RenderMarkdown(meta, opts.Body)
	if err != nil {
		return Record{}, err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(root, "handoff.create", id, withDefault(opts.Actor, "handoff"), map[string]any{"path": relToRoot(root, path), "task_id": meta.TaskID}); err != nil {
		return Record{}, err
	}
	return Read(path)
}

func Latest(env paths.Env, scope string) (Record, error) {
	items, err := List(env, scope)
	if err != nil {
		return Record{}, err
	}
	if len(items) == 0 {
		return Record{}, os.ErrNotExist
	}
	return items[0], nil
}

func List(env paths.Env, scope string) ([]Record, error) {
	root, _, err := scopeRoot(env, scope)
	if err != nil {
		return nil, err
	}
	dir, err := paths.SafeJoin(root, "handoffs")
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Record
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path, err := paths.SafeJoin(dir, entry.Name())
		if err != nil {
			return nil, err
		}
		record, err := Read(path)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Meta.UpdatedAt.Equal(out[j].Meta.UpdatedAt) {
			return out[i].Meta.UpdatedAt.After(out[j].Meta.UpdatedAt)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func Read(path string) (Record, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	doc, err := store.ParseMarkdown(data)
	if err != nil {
		return legacyRecord(path, string(data))
	}
	var meta Metadata
	raw, err := json.Marshal(doc.Meta)
	if err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Record{}, err
	}
	if meta.Schema != Schema {
		return legacyRecord(path, doc.Body)
	}
	return Record{
		Meta:        meta,
		Body:        doc.Body,
		Path:        path,
		MetadataMap: doc.Meta,
	}, nil
}

func legacyRecord(path, body string) (Record, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Record{}, err
	}
	meta := Metadata{
		Schema:    Schema,
		ID:        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Type:      "handoff",
		Title:     inferTitle(path, body),
		Status:    "current",
		CreatedAt: info.ModTime().UTC(),
		UpdatedAt: info.ModTime().UTC(),
	}
	return Record{
		Meta: meta,
		Body: body,
		Path: path,
	}, nil
}

func scopeRoot(env paths.Env, scope string) (string, string, error) {
	resolved := strings.TrimSpace(scope)
	if resolved == "" {
		resolved = "project"
	}
	root, err := env.ScopeRoot(resolved)
	if err != nil {
		return "", "", err
	}
	return root, resolved, nil
}

func withDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
}

func inferTitle(path, body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	return strings.ReplaceAll(name, "-", " ")
}
