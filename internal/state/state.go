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

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
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

type ResumeOptions struct {
	StartOptions
	SourceActiveID string
	CloseSummary   string
	CloseActor     string
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

type Diagnostic struct {
	Code       string `json:"code"`
	Path       string `json:"path"`
	Message    string `json:"message"`
	Repairable bool   `json:"repairable,omitempty"`
}

type ListResult struct {
	Capsules    []Capsule    `json:"capsules"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type QuarantineRequest struct {
	Scope   string `json:"scope,omitempty"`
	Apply   bool   `json:"apply,omitempty"`
	Confirm bool   `json:"confirm,omitempty"`
	Actor   string `json:"actor,omitempty"`
}

type QuarantineReport struct {
	Applied     bool         `json:"applied"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Actions     []string     `json:"actions,omitempty"`
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

// Reference is a stable state identifier plus a scope-root-relative fallback
// path. Callers that need a lightweight "latest" pointer should persist this
// shape instead of copying a capsule or storing an absolute path.
type Reference struct {
	ID      string `json:"id"`
	RelPath string `json:"rel_path"`
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
var newCoordinator = ops.New

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
	aliasPath, err := latestAliasPath(root)
	if err != nil {
		return Capsule{}, err
	}
	data, err := renderCapsuleData(meta, opts.Body)
	if err != nil {
		return Capsule{}, err
	}
	operation, err := newCoordinator(root).BeginBuild(stateOperationID("start"), func() (ops.Spec, error) {
		if _, statErr := os.Stat(path); statErr == nil {
			return ops.Spec{}, fmt.Errorf("state %q already exists", id)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return ops.Spec{}, statErr
		}
		writes := []ops.Write{
			{Path: filepath.ToSlash(relToRoot(root, path)), Data: data, Mode: 0o644},
			{Path: filepath.ToSlash(relToRoot(root, aliasPath)), Data: data, Mode: 0o644},
		}
		writes, buildErr := appendTransactionEvents(root, writes, []transactionEvent{{
			Name:  "state.start",
			ID:    id,
			Actor: opts.Actor,
			Data:  map[string]any{"scope": scope, "path": filepath.ToSlash(relToRoot(root, path))},
			Time:  ts,
		}})
		return ops.Spec{Writes: writes}, buildErr
	})
	if err != nil {
		return Capsule{}, err
	}
	if err := operation.Commit(); err != nil {
		return Capsule{}, fmt.Errorf("commit state start operation %s: %w", operation.Intent().ID, err)
	}
	return readCapsule(path, DirActive)
}

// Resume atomically creates the fresh active state, closes the selected source
// state when present, refreshes latest.md, and appends both lifecycle events.
func Resume(env paths.Env, opts ResumeOptions) (Capsule, error) {
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
	path, err := statePath(root, DirActive, id)
	if err != nil {
		return Capsule{}, err
	}
	aliasPath, err := latestAliasPath(root)
	if err != nil {
		return Capsule{}, err
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
	newData, err := renderCapsuleData(meta, opts.Body)
	if err != nil {
		return Capsule{}, err
	}
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(stateOperationID("resume"), func() (ops.Spec, error) {
		if _, statErr := os.Stat(path); statErr == nil {
			return ops.Spec{}, fmt.Errorf("state %q already exists", id)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return ops.Spec{}, statErr
		}
		writes := []ops.Write{
			{Path: filepath.ToSlash(relToRoot(root, path)), Data: newData, Mode: 0o644},
			{Path: filepath.ToSlash(relToRoot(root, aliasPath)), Data: newData, Mode: 0o644},
		}
		var deletes []string
		events := []transactionEvent{{
			Name:  "state.start",
			ID:    id,
			Actor: opts.Actor,
			Data:  map[string]any{"scope": scope, "path": filepath.ToSlash(relToRoot(root, path))},
			Time:  ts,
		}}
		if sourceID := strings.TrimSpace(opts.SourceActiveID); sourceID != "" && sourceID != id {
			source, sourceMeta, readErr := readByID(root, DirActive, sourceID)
			if readErr != nil {
				return ops.Spec{}, readErr
			}
			if sourceMeta.Scope != scope || sourceMeta.TaskID != meta.TaskID {
				return ops.Spec{}, fmt.Errorf("resume source state %q does not belong to task %q in scope %q", sourceID, meta.TaskID, scope)
			}
			closeTime := ts
			sourceMeta.Status = "closed"
			sourceMeta.ClosedAt = &closeTime
			sourceMeta.UpdatedAt = closeTime
			sourceBody := source.Body
			if summary := strings.TrimSpace(opts.CloseSummary); summary != "" {
				sourceBody = appendMarkdown(sourceBody, "## Close Summary\n\n"+summary)
			}
			archivedPath, pathErr := statePath(root, DirArchived, sourceID)
			if pathErr != nil {
				return ops.Spec{}, pathErr
			}
			archivedData, renderErr := renderCapsuleData(sourceMeta, sourceBody)
			if renderErr != nil {
				return ops.Spec{}, renderErr
			}
			writes = append(writes, ops.Write{
				Path: filepath.ToSlash(relToRoot(root, archivedPath)),
				Data: archivedData,
				Mode: 0o644,
			})
			deletes = append(deletes, filepath.ToSlash(relToRoot(root, source.Path)))
			events = append(events, transactionEvent{
				Name:  "state.close",
				ID:    sourceID,
				Actor: withDefault(opts.CloseActor, opts.Actor),
				Data:  map[string]any{"handoff": false, "path": filepath.ToSlash(relToRoot(root, archivedPath))},
				Time:  closeTime,
			})
		}
		writes, writeErr := appendTransactionEvents(root, writes, events)
		if writeErr != nil {
			return ops.Spec{}, writeErr
		}
		return ops.Spec{Writes: writes, Deletes: deletes}, nil
	})
	if err != nil {
		return Capsule{}, err
	}
	if err := operation.Commit(); err != nil {
		return Capsule{}, fmt.Errorf("commit state resume operation %s: %w", operation.Intent().ID, err)
	}
	return readCapsule(path, DirActive)
}

func Update(env paths.Env, opts UpdateOptions) (Capsule, error) {
	return updateActive(env, opts, "state.update", nil)
}

func updateActive(env paths.Env, opts UpdateOptions, event string, extraEventData map[string]any) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	aliasPath, err := latestAliasPath(root)
	if err != nil {
		return Capsule{}, err
	}
	var path string
	operation, err := newCoordinator(root).BeginBuild(stateOperationID("update"), func() (ops.Spec, error) {
		cap, meta, readErr := readByID(root, DirActive, opts.ID)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		path = cap.Path
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
		ts := now().UTC()
		meta.UpdatedAt = ts
		data, renderErr := renderCapsuleData(meta, body)
		if renderErr != nil {
			return ops.Spec{}, renderErr
		}
		writes := []ops.Write{
			{Path: filepath.ToSlash(relToRoot(root, cap.Path)), Data: data, Mode: 0o644},
			{Path: filepath.ToSlash(relToRoot(root, aliasPath)), Data: data, Mode: 0o644},
		}
		eventData := map[string]any{"status": meta.Status, "path": filepath.ToSlash(relToRoot(root, cap.Path))}
		for key, value := range extraEventData {
			eventData[key] = value
		}
		writes, readErr = appendTransactionEvents(root, writes, []transactionEvent{{
			Name: event, ID: meta.ID, Actor: opts.Actor, Data: eventData, Time: ts,
		}})
		return ops.Spec{Writes: writes}, readErr
	})
	if err != nil {
		return Capsule{}, err
	}
	if err := operation.Commit(); err != nil {
		return Capsule{}, fmt.Errorf("commit state update operation %s: %w", operation.Intent().ID, err)
	}
	return readCapsule(path, DirActive)
}

func Checkpoint(env paths.Env, opts CheckpointOptions) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	var checkpointID, path string
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(stateOperationID("checkpoint"), func() (ops.Spec, error) {
		cap, meta, readErr := readByID(root, DirActive, opts.ID)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		ts := now().UTC()
		checkpointID = newID("cp", meta.Title, ts)
		parentID := meta.ID
		meta.ID = checkpointID
		meta.CheckpointID = checkpointID
		meta.CheckpointOf = parentID
		meta.CheckpointNote = strings.TrimSpace(opts.Note)
		meta.CheckpointAt = &ts
		meta.CreatedAt = ts
		meta.UpdatedAt = ts
		path, readErr = statePath(root, DirCheckpoints, checkpointID)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		data, renderErr := renderCapsuleData(meta, cap.Body)
		if renderErr != nil {
			return ops.Spec{}, renderErr
		}
		writes := []ops.Write{{Path: filepath.ToSlash(relToRoot(root, path)), Data: data, Mode: 0o644}}
		writes, readErr = appendTransactionEvents(root, writes, []transactionEvent{{
			Name:  "state.checkpoint",
			ID:    checkpointID,
			Actor: opts.Actor,
			Data: map[string]any{
				"checkpoint_id": checkpointID,
				"checkpoint_of": parentID,
				"path":          filepath.ToSlash(relToRoot(root, path)),
			},
			Time: ts,
		}})
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		return ops.Spec{Writes: writes}, nil
	})
	if err != nil {
		return Capsule{}, err
	}
	if err := operation.Commit(); err != nil {
		return Capsule{}, fmt.Errorf("commit state checkpoint operation %s: %w", operation.Intent().ID, err)
	}
	return readCapsule(path, DirCheckpoints)
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
	aliasPath, err := latestAliasPath(root)
	if err != nil {
		return CloseResult{}, err
	}
	var closedPath string
	operation, err := newCoordinator(root).BeginBuild(stateOperationID("close"), func() (ops.Spec, error) {
		cap, meta, readErr := readByID(root, DirActive, opts.ID)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		ts := now().UTC()
		body := cap.Body
		if strings.TrimSpace(opts.Summary) != "" {
			body = appendMarkdown(body, "## Close Summary\n\n"+strings.TrimSpace(opts.Summary))
		}
		meta.Status = "closed"
		meta.ClosedAt = &ts
		meta.UpdatedAt = ts
		closedPath, readErr = statePath(root, DirArchived, meta.ID)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		data, renderErr := renderCapsuleData(meta, body)
		if renderErr != nil {
			return ops.Spec{}, renderErr
		}
		writes := []ops.Write{{
			Path: filepath.ToSlash(relToRoot(root, closedPath)), Data: data, Mode: 0o644,
		}}
		deletes := []string{filepath.ToSlash(relToRoot(root, cap.Path))}
		active, listErr := List(env, ListOptions{Scope: opts.Scope, Directory: DirActive})
		if listErr != nil {
			return ops.Spec{}, listErr
		}
		var aliasData []byte
		for _, candidate := range active {
			if candidate.State.ID == meta.ID {
				continue
			}
			aliasData, readErr = os.ReadFile(candidate.Path)
			if readErr != nil {
				return ops.Spec{}, readErr
			}
			break
		}
		if aliasData == nil {
			deletes = append(deletes, filepath.ToSlash(relToRoot(root, aliasPath)))
		} else {
			writes = append(writes, ops.Write{
				Path: filepath.ToSlash(relToRoot(root, aliasPath)), Data: aliasData, Mode: 0o644,
			})
		}
		writes, readErr = appendTransactionEvents(root, writes, []transactionEvent{{
			Name:  "state.close",
			ID:    meta.ID,
			Actor: opts.Actor,
			Data:  map[string]any{"handoff": opts.Handoff, "path": filepath.ToSlash(relToRoot(root, closedPath))},
			Time:  ts,
		}})
		return ops.Spec{Writes: writes, Deletes: deletes}, readErr
	})
	if err != nil {
		return CloseResult{}, err
	}
	if err := operation.Commit(); err != nil {
		return CloseResult{}, fmt.Errorf("commit state close operation %s: %w", operation.Intent().ID, err)
	}
	closed, err := readCapsule(closedPath, DirArchived)
	return CloseResult{Capsule: closed}, err
}

func Archive(env paths.Env, opts ArchiveOptions) (Capsule, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return Capsule{}, err
	}
	var path string
	operation, err := newCoordinator(root).BeginBuild(stateOperationID("archive"), func() (ops.Spec, error) {
		cap, meta, readErr := readByID(root, DirArchived, opts.ID)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		path = cap.Path
		ts := now().UTC()
		meta.Status = "archived"
		meta.ArchivedAt = &ts
		meta.UpdatedAt = ts
		data, renderErr := renderCapsuleData(meta, cap.Body)
		if renderErr != nil {
			return ops.Spec{}, renderErr
		}
		writes := []ops.Write{{
			Path: filepath.ToSlash(relToRoot(root, cap.Path)), Data: data, Mode: 0o644,
		}}
		writes, readErr = appendTransactionEvents(root, writes, []transactionEvent{{
			Name: "state.archive", ID: meta.ID, Actor: opts.Actor,
			Data: map[string]any{"path": filepath.ToSlash(relToRoot(root, cap.Path))}, Time: ts,
		}})
		return ops.Spec{Writes: writes}, readErr
	})
	if err != nil {
		return Capsule{}, err
	}
	if err := operation.Commit(); err != nil {
		return Capsule{}, fmt.Errorf("commit state archive operation %s: %w", operation.Intent().ID, err)
	}
	return readCapsule(path, DirArchived)
}

func List(env paths.Env, opts ListOptions) ([]Capsule, error) {
	result, err := ListWithDiagnostics(env, opts)
	return result.Capsules, err
}

func ListWithDiagnostics(env paths.Env, opts ListOptions) (ListResult, error) {
	root, _, err := scopeRoot(env, opts.Scope)
	if err != nil {
		return ListResult{}, err
	}
	dirs := []string{withDefault(opts.Directory, DirActive)}
	if opts.Directory == "all" {
		dirs = []string{DirActive, DirCheckpoints, DirArchived}
	}
	var result ListResult
	for _, dir := range dirs {
		if err := validateDirectory(dir); err != nil {
			return result, err
		}
		dirPath, err := stateDir(root, dir)
		if err != nil {
			return result, err
		}
		entries, err := os.ReadDir(dirPath)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return result, err
		}
		for _, entry := range entries {
			if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" || (dir == DirActive && entry.Name() == latestAliasName) {
				continue
			}
			path, err := paths.SafeJoin(dirPath, entry.Name())
			if err != nil {
				return result, err
			}
			cap, err := readCapsule(path, dir)
			if err != nil {
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					Code:       "invalid_state",
					Path:       filepath.ToSlash(relToRoot(root, path)),
					Message:    err.Error(),
					Repairable: !errors.Is(err, errUnsafeStatePath),
				})
				continue
			}
			result.Capsules = append(result.Capsules, cap)
		}
	}
	sort.Slice(result.Capsules, func(i, j int) bool {
		if !result.Capsules[i].UpdatedTime.Equal(result.Capsules[j].UpdatedTime) {
			return result.Capsules[i].UpdatedTime.After(result.Capsules[j].UpdatedTime)
		}
		return result.Capsules[i].Path < result.Capsules[j].Path
	})
	sort.Slice(result.Diagnostics, func(i, j int) bool {
		return result.Diagnostics[i].Path < result.Diagnostics[j].Path
	})
	return result, nil
}

// QuarantineMalformed moves malformed state files into the ignored runtime
// quarantine using replayable operations. It is dry-run unless Apply and
// Confirm are both set.
func QuarantineMalformed(env paths.Env, request QuarantineRequest) (QuarantineReport, error) {
	if request.Apply && !request.Confirm {
		return QuarantineReport{}, errors.New("state quarantine --apply requires --confirm")
	}
	root, scope, err := scopeRoot(env, request.Scope)
	if err != nil {
		return QuarantineReport{}, err
	}
	listed, err := ListWithDiagnostics(env, ListOptions{Scope: scope, Directory: "all"})
	if err != nil {
		return QuarantineReport{}, err
	}
	report := QuarantineReport{Diagnostics: listed.Diagnostics}
	for _, diagnostic := range listed.Diagnostics {
		if diagnostic.Repairable {
			report.Actions = append(report.Actions, "quarantine malformed state: "+diagnostic.Path)
		}
	}
	if !request.Apply {
		return report, nil
	}
	for _, diagnostic := range listed.Diagnostics {
		if !diagnostic.Repairable {
			continue
		}
		if err := quarantineMalformedState(root, diagnostic, withDefault(request.Actor, defaultActor)); err != nil {
			return QuarantineReport{}, err
		}
		report.Applied = true
	}
	return report, nil
}

func quarantineMalformedState(root string, diagnostic Diagnostic, actor string) error {
	relPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(diagnostic.Path)))
	parts := strings.Split(relPath, "/")
	if len(parts) != 3 || parts[0] != "state" || filepath.Ext(parts[2]) != ".md" || parts[2] == latestAliasName {
		return fmt.Errorf("refuse to quarantine invalid state path %q", diagnostic.Path)
	}
	if err := validateDirectory(parts[1]); err != nil {
		return err
	}
	opID := stateOperationID("quarantine")
	quarantineRel := filepath.ToSlash(filepath.Join(
		"runtime", "quarantine", "state", opID+"-"+filepath.Base(relPath),
	))
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(opID, func() (ops.Spec, error) {
		source := filepath.Join(root, filepath.FromSlash(relPath))
		info, statErr := os.Lstat(source)
		if statErr != nil {
			return ops.Spec{}, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ops.Spec{}, fmt.Errorf("refuse to quarantine unsafe state path %q", relPath)
		}
		if _, readErr := readCapsule(source, parts[1]); readErr == nil {
			return ops.Spec{}, fmt.Errorf("refuse to quarantine valid state %q", relPath)
		}
		data, readErr := os.ReadFile(source)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		target := filepath.Join(root, filepath.FromSlash(quarantineRel))
		if _, statErr := os.Lstat(target); statErr == nil {
			return ops.Spec{}, fmt.Errorf("state quarantine target already exists: %s", quarantineRel)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return ops.Spec{}, statErr
		}
		writes, writeErr := appendTransactionEvents(root, []ops.Write{{
			Path: quarantineRel,
			Data: data,
			Mode: 0o600,
		}}, []transactionEvent{{
			Name:  "state.quarantine",
			Actor: actor,
			Data:  map[string]any{"source": relPath, "quarantine": quarantineRel},
			Time:  now().UTC(),
		}})
		if writeErr != nil {
			return ops.Spec{}, writeErr
		}
		return ops.Spec{Writes: writes, Deletes: []string{relPath}}, nil
	})
	if err != nil {
		return err
	}
	if err := operation.Commit(); err != nil {
		return fmt.Errorf("commit state quarantine operation %s: %w", opID, err)
	}
	return nil
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

func LatestReference(env paths.Env, scope string) (Reference, error) {
	root, _, err := scopeRoot(env, scope)
	if err != nil {
		return Reference{}, err
	}
	cap, err := LatestActive(env, scope)
	if err != nil {
		return Reference{}, err
	}
	return Reference{ID: cap.State.ID, RelPath: filepath.ToSlash(relToRoot(root, cap.Path))}, nil
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
	info, err := os.Lstat(path)
	if err != nil {
		return Capsule{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Capsule{}, fmt.Errorf("%w: state path %q is not a regular non-symlink file", errUnsafeStatePath, path)
	}
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

var errUnsafeStatePath = errors.New("unsafe state storage path")

func renderCapsuleData(meta metadata, body string) ([]byte, error) {
	if meta.Schema == "" {
		meta.Schema = model.SchemaState
	}
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return nil, err
	}
	return data, nil
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

type transactionEvent struct {
	Name  string
	ID    string
	Actor string
	Data  map[string]any
	Time  time.Time
}

func appendTransactionEvents(root string, writes []ops.Write, events []transactionEvent) ([]ops.Write, error) {
	if len(events) == 0 {
		return writes, nil
	}
	const relPath = "logs/events.jsonl"
	existing, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relPath)))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}
	for _, event := range events {
		when := event.Time
		if when.IsZero() {
			when = now().UTC()
		}
		data, err := json.Marshal(model.Event{
			Time:  when.UTC(),
			Event: event.Name,
			ID:    event.ID,
			Actor: withDefault(event.Actor, defaultActor),
			Data:  event.Data,
		})
		if err != nil {
			return nil, err
		}
		existing = append(existing, data...)
		existing = append(existing, '\n')
	}
	return append(writes, ops.Write{Path: relPath, Data: existing, Mode: 0o644}), nil
}

func newID(prefix, title string, ts time.Time) string {
	return fmt.Sprintf("%s_%s_%s", prefix, util.Slug(title), ts.Format("20060102T150405.000000000Z"))
}

func stateOperationID(kind string) string {
	return fmt.Sprintf("op_state_%s_%d_%d", kind, time.Now().UTC().UnixNano(), os.Getpid())
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
