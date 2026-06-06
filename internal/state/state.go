package state

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

const (
	DirActive      = "active"
	DirCheckpoints = "checkpoints"
	DirArchived    = "archived"

	defaultActor      = "state"
	defaultSourceTool = "worktrail"
	defaultType       = "session"
)

type StartOptions struct {
	Scope                string
	ID                   string
	TaskID               string
	Type                 string
	Title                string
	SourceTool           string
	SourceSessions       []string
	Tags                 []string
	Body                 string
	ResumedFromStateID   string
	ResumedFromHandoffID string
	Actor                string
}

type UpdateOptions struct {
	Scope          string
	ID             string
	Type           string
	Title          string
	Status         string
	SourceTool     string
	SourceSessions []string
	Tags           []string
	ReplaceBody    *string
	AppendBody     string
	Actor          string
}

type CheckpointOptions struct {
	Scope string
	ID    string
	Note  string
	Actor string
}

type InjectOptions struct {
	Scope string
	ID    string
	Title string
	Body  string
	Actor string
}

type CloseOptions struct {
	Scope   string
	ID      string
	Summary string
	Handoff bool
	Actor   string
}

type ArchiveOptions struct {
	Scope string
	ID    string
	Actor string
}

type ListOptions struct {
	Scope     string
	Directory string
}

type ShowOptions struct {
	Scope     string
	ID        string
	Directory string
}

type Capsule struct {
	State       model.State
	Body        string
	Path        string
	Directory   string
	Checkpoint  string
	Metadata    map[string]any
	UpdatedTime time.Time
}

type CloseResult struct {
	Capsule Capsule
}

type metadata struct {
	Schema               string     `json:"schema"`
	ID                   string     `json:"id"`
	Scope                string     `json:"scope"`
	TaskID               string     `json:"task_id,omitempty"`
	Type                 string     `json:"type"`
	Title                string     `json:"title"`
	Status               string     `json:"status"`
	SourceTool           string     `json:"source_tool"`
	SourceSessions       []string   `json:"source_sessions,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	Tags                 []string   `json:"tags,omitempty"`
	CheckpointID         string     `json:"checkpoint_id,omitempty"`
	CheckpointOf         string     `json:"checkpoint_of,omitempty"`
	CheckpointNote       string     `json:"checkpoint_note,omitempty"`
	CheckpointAt         *time.Time `json:"checkpoint_at,omitempty"`
	ClosedAt             *time.Time `json:"closed_at,omitempty"`
	ArchivedAt           *time.Time `json:"archived_at,omitempty"`
	ResumedFromStateID   string     `json:"resumed_from_state_id,omitempty"`
	ResumedFromHandoffID string     `json:"resumed_from_handoff_id,omitempty"`
}

var now = time.Now

func Start(env paths.Env, opts StartOptions) (Capsule, error) {
	root, scope, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	if strings.TrimSpace(opts.Title) == "" {
		return Capsule{}, errors.New("state title is required")
	}
	ts := now().UTC()
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = newID("st", opts.Title, ts)
	}
	meta := metadata{
		Schema:               model.SchemaState,
		ID:                   id,
		Scope:                scope,
		TaskID:               withDefault(opts.TaskID, "task-"+util.Slug(opts.Title)),
		Type:                 withDefault(opts.Type, defaultType),
		Title:                strings.TrimSpace(opts.Title),
		Status:               "active",
		SourceTool:           withDefault(opts.SourceTool, defaultSourceTool),
		SourceSessions:       cleanList(opts.SourceSessions),
		CreatedAt:            ts,
		UpdatedAt:            ts,
		Tags:                 cleanList(opts.Tags),
		ResumedFromStateID:   strings.TrimSpace(opts.ResumedFromStateID),
		ResumedFromHandoffID: strings.TrimSpace(opts.ResumedFromHandoffID),
	}
	path, err := statePath(root, DirActive, id)
	if err != nil {
		return Capsule{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return Capsule{}, fmt.Errorf("state %q already exists", id)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Capsule{}, err
	}
	cap, err := writeCapsule(path, DirActive, meta, opts.Body)
	if err != nil {
		return Capsule{}, err
	}
	if err := syncLatestAlias(root, cap); err != nil {
		_ = os.Remove(path)
		return Capsule{}, err
	}
	if err := appendEvent(root, "state.start", id, opts.Actor, map[string]any{"scope": scope, "path": relToRoot(root, path)}); err != nil {
		return Capsule{}, err
	}
	return cap, nil
}

func Update(env paths.Env, opts UpdateOptions) (Capsule, error) {
	return updateActive(env, opts, "state.update", nil)
}

func updateActive(env paths.Env, opts UpdateOptions, event string, extraEventData map[string]any) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	cap, meta, err := readByID(root, DirActive, opts.ID)
	if err != nil {
		return Capsule{}, err
	}
	if opts.Type != "" {
		meta.Type = strings.TrimSpace(opts.Type)
	}
	if opts.Title != "" {
		meta.Title = strings.TrimSpace(opts.Title)
	}
	if opts.Status != "" {
		meta.Status = strings.TrimSpace(opts.Status)
	}
	if opts.SourceTool != "" {
		meta.SourceTool = strings.TrimSpace(opts.SourceTool)
	}
	if opts.SourceSessions != nil {
		meta.SourceSessions = cleanList(opts.SourceSessions)
	}
	if opts.Tags != nil {
		meta.Tags = cleanList(opts.Tags)
	}
	body := cap.Body
	if opts.ReplaceBody != nil {
		body = *opts.ReplaceBody
	}
	if strings.TrimSpace(opts.AppendBody) != "" {
		body = appendMarkdown(body, opts.AppendBody)
	}
	meta.UpdatedAt = now().UTC()
	updated, err := writeCapsule(cap.Path, DirActive, meta, body)
	if err != nil {
		return Capsule{}, err
	}
	if err := syncLatestAlias(root, updated); err != nil {
		return Capsule{}, err
	}
	eventData := map[string]any{"status": meta.Status, "path": relToRoot(root, cap.Path)}
	for key, value := range extraEventData {
		eventData[key] = value
	}
	if err := appendEvent(root, event, meta.ID, opts.Actor, eventData); err != nil {
		return Capsule{}, err
	}
	return updated, nil
}

func Checkpoint(env paths.Env, opts CheckpointOptions) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	cap, meta, err := readByID(root, DirActive, opts.ID)
	if err != nil {
		return Capsule{}, err
	}
	ts := now().UTC()
	checkpointID := newID("cp", meta.Title, ts)
	meta.CheckpointID = checkpointID
	meta.CheckpointOf = meta.ID
	meta.CheckpointNote = strings.TrimSpace(opts.Note)
	meta.CheckpointAt = &ts
	meta.UpdatedAt = ts
	path, err := statePath(root, DirCheckpoints, checkpointID)
	if err != nil {
		return Capsule{}, err
	}
	checkpoint, err := writeCapsule(path, DirCheckpoints, meta, cap.Body)
	if err != nil {
		return Capsule{}, err
	}
	if err := appendEvent(root, "state.checkpoint", meta.ID, opts.Actor, map[string]any{"checkpoint_id": checkpointID, "path": relToRoot(root, path)}); err != nil {
		return Capsule{}, err
	}
	return checkpoint, nil
}

func Inject(env paths.Env, opts InjectOptions) (Capsule, error) {
	if strings.TrimSpace(opts.Body) == "" {
		return Capsule{}, errors.New("inject body is required")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Injection"
	}
	section := "## " + title + "\n\n" + strings.TrimSpace(opts.Body)
	return updateActive(env, UpdateOptions{
		Scope:      opts.Scope,
		ID:         opts.ID,
		AppendBody: section,
		Actor:      opts.Actor,
	}, "state.inject", map[string]any{"title": title})
}

func Close(env paths.Env, opts CloseOptions) (CloseResult, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return CloseResult{}, err
	}
	cap, meta, err := readByID(root, DirActive, opts.ID)
	if err != nil {
		return CloseResult{}, err
	}
	ts := now().UTC()
	body := cap.Body
	if strings.TrimSpace(opts.Summary) != "" {
		body = appendMarkdown(body, "## Close Summary\n\n"+strings.TrimSpace(opts.Summary))
	}
	meta.Status = "closed"
	meta.ClosedAt = &ts
	meta.UpdatedAt = ts
	closedPath, err := statePath(root, DirArchived, meta.ID)
	if err != nil {
		return CloseResult{}, err
	}
	closed, err := writeCapsule(closedPath, DirArchived, meta, body)
	if err != nil {
		return CloseResult{}, err
	}
	if err := os.Remove(cap.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return CloseResult{}, err
	}
	if err := syncLatestAliasFromActive(root); err != nil {
		return CloseResult{}, err
	}
	if err := appendEvent(root, "state.close", meta.ID, opts.Actor, map[string]any{"handoff": opts.Handoff, "path": relToRoot(root, closedPath)}); err != nil {
		return CloseResult{}, err
	}
	return CloseResult{Capsule: closed}, nil
}

func Archive(env paths.Env, opts ArchiveOptions) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	cap, meta, err := readByID(root, DirArchived, opts.ID)
	if err != nil {
		return Capsule{}, err
	}
	ts := now().UTC()
	meta.Status = "archived"
	meta.ArchivedAt = &ts
	meta.UpdatedAt = ts
	archived, err := writeCapsule(cap.Path, DirArchived, meta, cap.Body)
	if err != nil {
		return Capsule{}, err
	}
	if err := appendEvent(root, "state.archive", meta.ID, opts.Actor, map[string]any{"path": relToRoot(root, cap.Path)}); err != nil {
		return Capsule{}, err
	}
	return archived, nil
}

func List(env paths.Env, opts ListOptions) ([]Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return nil, err
	}
	dirs := []string{withDefault(opts.Directory, DirActive)}
	if opts.Directory == "all" {
		dirs = []string{DirActive, DirCheckpoints, DirArchived}
	}
	var out []Capsule
	for _, dir := range dirs {
		if err := validateDirectory(dir); err != nil {
			return nil, err
		}
		dirPath, err := stateDir(root, dir)
		if err != nil {
			return nil, err
		}
		entries, err := os.ReadDir(dirPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || (dir == DirActive && entry.Name() == latestAliasName) {
				continue
			}
			path, err := paths.SafeJoin(dirPath, entry.Name())
			if err != nil {
				return nil, err
			}
			cap, err := readCapsule(path, dir)
			if err != nil {
				return nil, err
			}
			out = append(out, cap)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedTime.Equal(out[j].UpdatedTime) {
			return out[i].UpdatedTime.After(out[j].UpdatedTime)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func LatestActive(env paths.Env, scope string) (Capsule, error) {
	items, err := List(env, ListOptions{Scope: scope, Directory: DirActive})
	if err != nil {
		return Capsule{}, err
	}
	if len(items) == 0 {
		return Capsule{}, os.ErrNotExist
	}
	return items[0], nil
}

func LatestExplicit(env paths.Env, scope string) (Capsule, error) {
	items, err := List(env, ListOptions{Scope: scope, Directory: DirActive})
	if err != nil {
		return Capsule{}, err
	}
	for _, item := range items {
		if isExplicitSession(item) {
			return item, nil
		}
	}
	return Capsule{}, os.ErrNotExist
}

func isExplicitSession(cap Capsule) bool {
	tool := strings.TrimSpace(cap.State.SourceTool)
	if tool == "" || tool == defaultSourceTool {
		return true
	}
	return false
}

func TaskID(cap Capsule) string {
	meta, err := decodeMetadata(cap.Metadata)
	if err != nil {
		return ""
	}
	return meta.TaskID
}

func Show(env paths.Env, opts ShowOptions) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	dir := withDefault(opts.Directory, DirActive)
	if err := validateDirectory(dir); err != nil {
		return Capsule{}, err
	}
	cap, _, err := readByID(root, dir, opts.ID)
	return cap, err
}

func scopeRoot(env paths.Env, scope string) (string, string, error) {
	resolved := scope
	if resolved == "" {
		resolved = "project"
	}
	root, err := env.ScopeRoot(resolved)
	if err != nil {
		return "", "", err
	}
	return root, resolved, nil
}

func stateDir(root, dir string) (string, error) {
	if err := validateDirectory(dir); err != nil {
		return "", err
	}
	return paths.SafeJoin(root, "state", dir)
}

func statePath(root, dir, id string) (string, error) {
	if strings.TrimSpace(id) == "" {
		return "", errors.New("state id is required")
	}
	if strings.ContainsAny(id, `/\`) {
		return "", errors.New("state id must not contain path separators")
	}
	return paths.SafeJoin(root, "state", dir, id+".md")
}

func validateDirectory(dir string) error {
	switch dir {
	case DirActive, DirCheckpoints, DirArchived:
		return nil
	default:
		return fmt.Errorf("unknown state directory %q", dir)
	}
}

func readByID(root, dir, id string) (Capsule, metadata, error) {
	if dir == DirActive && id == "latest" {
		path, err := latestAliasPath(root)
		if err != nil {
			return Capsule{}, metadata{}, err
		}
		if cap, err := readCapsule(path, dir); err == nil {
			meta, err := decodeMetadata(cap.Metadata)
			return cap, meta, err
		}
		caps, err := List(paths.Env{ProjectWT: root}, ListOptions{Directory: DirActive})
		if err != nil {
			return Capsule{}, metadata{}, err
		}
		if len(caps) == 0 {
			return Capsule{}, metadata{}, os.ErrNotExist
		}
		meta, err := decodeMetadata(caps[0].Metadata)
		return caps[0], meta, err
	}
	path, err := statePath(root, dir, id)
	if err != nil {
		return Capsule{}, metadata{}, err
	}
	cap, err := readCapsule(path, dir)
	if err == nil {
		meta, err := decodeMetadata(cap.Metadata)
		return cap, meta, err
	}
	if dir != DirCheckpoints || !errors.Is(err, os.ErrNotExist) {
		return Capsule{}, metadata{}, err
	}
	caps, err := List(paths.Env{ProjectWT: root}, ListOptions{Directory: DirCheckpoints})
	if err != nil {
		return Capsule{}, metadata{}, err
	}
	for _, candidate := range caps {
		if candidate.Checkpoint == id || filepath.Base(strings.TrimSuffix(candidate.Path, ".md")) == id {
			meta, err := decodeMetadata(candidate.Metadata)
			return candidate, meta, err
		}
	}
	return Capsule{}, metadata{}, os.ErrNotExist
}

func readCapsule(path, dir string) (Capsule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Capsule{}, err
	}
	doc, err := store.ParseMarkdown(b)
	if err != nil {
		return Capsule{}, err
	}
	meta, err := decodeMetadata(doc.Meta)
	if err != nil {
		return Capsule{}, err
	}
	state := model.State{
		Schema:         meta.Schema,
		ID:             meta.ID,
		Scope:          meta.Scope,
		Type:           meta.Type,
		Title:          meta.Title,
		Status:         meta.Status,
		SourceTool:     meta.SourceTool,
		SourceSessions: append([]string(nil), meta.SourceSessions...),
		CreatedAt:      meta.CreatedAt,
		UpdatedAt:      meta.UpdatedAt,
		Tags:           append([]string(nil), meta.Tags...),
	}
	return Capsule{
		State:       state,
		Body:        doc.Body,
		Path:        path,
		Directory:   dir,
		Checkpoint:  meta.CheckpointID,
		Metadata:    doc.Meta,
		UpdatedTime: meta.UpdatedAt,
	}, nil
}

func writeCapsule(path, dir string, meta metadata, body string) (Capsule, error) {
	if meta.Schema == "" {
		meta.Schema = model.SchemaState
	}
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return Capsule{}, err
	}
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return Capsule{}, err
	}
	return readCapsule(path, dir)
}

func decodeMetadata(raw map[string]any) (metadata, error) {
	var meta metadata
	b, err := json.Marshal(raw)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, err
	}
	if meta.Schema != model.SchemaState {
		return meta, fmt.Errorf("unexpected state schema %q", meta.Schema)
	}
	if meta.ID == "" {
		return meta, errors.New("state metadata missing id")
	}
	return meta, nil
}

func appendEvent(root, event, id, actor string, data map[string]any) error {
	return wlog.Append(root, event, id, withDefault(actor, defaultActor), data)
}

func newID(prefix, title string, ts time.Time) string {
	return fmt.Sprintf("%s_%s_%s", prefix, util.Slug(title), ts.Format("20060102T150405.000000000Z"))
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

func appendMarkdown(existing, addition string) string {
	existing = strings.TrimSpace(existing)
	addition = strings.TrimSpace(addition)
	if existing == "" {
		return addition
	}
	if addition == "" {
		return existing
	}
	return existing + "\n\n" + addition
}

func renderHandoffBody(cap Capsule, summary string) string {
	var b strings.Builder
	b.WriteString("# Handoff: ")
	b.WriteString(cap.State.Title)
	b.WriteString("\n\n")
	if strings.TrimSpace(summary) != "" {
		b.WriteString("## Summary\n\n")
		b.WriteString(strings.TrimSpace(summary))
		b.WriteString("\n\n")
	}
	b.WriteString("## State Capsule\n\n")
	b.WriteString("- ID: ")
	b.WriteString(cap.State.ID)
	b.WriteString("\n- Scope: ")
	b.WriteString(cap.State.Scope)
	b.WriteString("\n- Status: ")
	b.WriteString(cap.State.Status)
	b.WriteString("\n\n## Body\n\n")
	b.WriteString(strings.TrimSpace(cap.Body))
	b.WriteByte('\n')
	return b.String()
}

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return rel
}

const latestAliasName = "latest.md"

func latestAliasPath(root string) (string, error) {
	return paths.SafeJoin(root, "state", DirActive, latestAliasName)
}

func syncLatestAlias(root string, cap Capsule) error {
	aliasPath, err := latestAliasPath(root)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(cap.Path)
	if err != nil {
		return err
	}
	return util.AtomicWrite(aliasPath, data, 0o644)
}

func syncLatestAliasFromActive(root string) error {
	caps, err := List(paths.Env{ProjectWT: root}, ListOptions{Directory: DirActive})
	if err != nil {
		return err
	}
	if len(caps) == 0 {
		aliasPath, aliasErr := latestAliasPath(root)
		if aliasErr != nil {
			return aliasErr
		}
		if err := os.Remove(aliasPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return syncLatestAlias(root, caps[0])
}
