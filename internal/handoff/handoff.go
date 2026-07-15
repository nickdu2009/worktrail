package handoff

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/textsafety"
	"github.com/nickdu2009/worktrail/internal/worktreesnap"
)

const (
	Schema       = model.SchemaHandoffV2
	LocalBodyMax = 32 * 1024
	TeamBodyMax  = 16 * 1024
)

var (
	newCoordinator = ops.New
	mutationMu     sync.Mutex
)

type Metadata struct {
	model.HandoffMetaV2

	// Compatibility projections for callers that still consume the additive V2
	// service through the old read interface.
	Status            string `json:"-"`
	SourceStateID     string `json:"-"`
	PreviousHandoffID string `json:"-"`
}

type Record struct {
	Meta        Metadata       `json:"meta"`
	Body        string         `json:"body"`
	RelPath     string         `json:"rel_path"`
	Path        string         `json:"-"`
	MetadataMap map[string]any `json:"-"`
}

type CreateRequest struct {
	ID            string                     `json:"id,omitempty"`
	Scope         string                     `json:"scope,omitempty"`
	Title         string                     `json:"title,omitempty"`
	Summary       string                     `json:"summary"`
	Complete      bool                       `json:"complete,omitempty"`
	ProjectID     string                     `json:"project_id,omitempty"`
	TaskID        string                     `json:"task_id,omitempty"`
	SourceState   *model.Ref                 `json:"source_state,omitempty"`
	Previous      *model.Ref                 `json:"previous_handoff,omitempty"`
	NextSteps     []model.NextStep           `json:"next_steps,omitempty"`
	OpenQuestions []string                   `json:"open_questions,omitempty"`
	Risks         []string                   `json:"risks,omitempty"`
	Validation    []model.ValidationEvidence `json:"validation,omitempty"`
	Worktree      model.WorktreeSnapshot     `json:"worktree,omitempty"`
	Tags          []string                   `json:"tags,omitempty"`
	Body          string                     `json:"body,omitempty"`
	SourceTool    string                     `json:"source_tool,omitempty"`
	Actor         string                     `json:"actor,omitempty"`
}

// CreateOptions keeps the pre-cutover internal API compiling while writing a
// V2 local record. New callers should use CreateLocal and CreateRequest.
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

type FileWrite struct {
	Path string
	Data []byte
	Mode os.FileMode
}

type Event struct {
	Name  string
	ID    string
	Actor string
	Data  map[string]any
	Time  time.Time
}

type Mutation struct {
	Writes  []FileWrite
	Deletes []string
	Events  []Event
	Build   func() (Mutation, error)
}

type PublishRequest struct {
	Scope      string   `json:"scope,omitempty"`
	ID         string   `json:"id"`
	AllowDirty bool     `json:"allow_dirty,omitempty"`
	Confirm    bool     `json:"confirm,omitempty"`
	Supersedes []string `json:"supersedes,omitempty"`
	Actor      string   `json:"actor,omitempty"`
}

type CloseRequest struct {
	Scope string `json:"scope,omitempty"`
	ID    string `json:"id"`
	Actor string `json:"actor,omitempty"`
}

func Create(env paths.Env, opts CreateOptions) (Record, error) {
	scope := withDefault(opts.Scope, "project")
	projectID, _ := projectID(env, scope)
	taskID := strings.TrimSpace(opts.TaskID)
	if taskID == "" {
		taskID, _ = newOpaqueID("task")
	}
	request := CreateRequest{
		Scope:     scope,
		Title:     opts.Title,
		Summary:   opts.Summary,
		Complete:  true,
		ProjectID: withDefault(projectID, "project_legacy_unbound"),
		TaskID:    taskID,
		Tags:      opts.Tags,
		Body:      opts.Body,
		Actor:     opts.Actor,
	}
	if id := strings.TrimSpace(opts.SourceStateID); id != "" {
		request.SourceState = &model.Ref{Scope: scope, Kind: "state", ID: id}
	}
	if id := strings.TrimSpace(opts.PreviousHandoffID); id != "" {
		request.Previous = &model.Ref{Scope: scope, Kind: "handoff", ID: id}
	}
	return CreateLocal(context.Background(), env, request)
}

func CreateLocal(ctx context.Context, env paths.Env, request CreateRequest) (Record, error) {
	return CreateLocalWithMutation(ctx, env, request, Mutation{})
}

func CreateLocalWithMutation(ctx context.Context, env paths.Env, request CreateRequest, mutation Mutation) (Record, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	root, scope, err := scopeRoot(env, request.Scope)
	if err != nil {
		return Record{}, err
	}
	request.Scope = scope
	if err := validateCreateRequest(request); err != nil {
		return Record{}, err
	}
	if strings.TrimSpace(request.ProjectID) == "" {
		request.ProjectID, err = projectID(env, scope)
		if err != nil {
			return Record{}, err
		}
	}
	if strings.TrimSpace(request.TaskID) == "" {
		return Record{}, errors.New("task_id is required")
	}
	if err := validateIdentity("project_id", request.ProjectID); err != nil {
		return Record{}, err
	}
	if err := validateIdentity("task_id", request.TaskID); err != nil {
		return Record{}, err
	}
	if request.SourceState != nil {
		if err := validateIdentity("source state id", request.SourceState.ID); err != nil {
			return Record{}, err
		}
	}
	if request.Worktree.CapturedAt.IsZero() && scope == "project" {
		if snapshot, snapErr := worktreesnap.Capture(ctx, env.ProjectRoot); snapErr == nil {
			request.Worktree = snapshot
		} else {
			request.Worktree = model.WorktreeSnapshot{
				CodeAvailability: model.CodeAvailabilityUnavailable,
				CapturedAt:       time.Now().UTC(),
			}
		}
	}
	if request.Worktree.CapturedAt.IsZero() {
		request.Worktree.CapturedAt = time.Now().UTC()
	}
	if err := normalizeWorktree(&request.Worktree); err != nil {
		return Record{}, err
	}
	request, redacted, err := sanitizeCreateRequest(request, textsafety.ProfileLocal)
	if err != nil {
		return Record{}, err
	}
	id := strings.TrimSpace(request.ID)
	if id == "" {
		id, err = newOpaqueID("handoff")
		if err != nil {
			return Record{}, err
		}
	}
	if err := validateID(id); err != nil {
		return Record{}, err
	}
	relPath := handoffRelPath(model.VisibilityLocal, id)
	opID, err := newOpaqueID("op_handoff_create")
	if err != nil {
		return Record{}, err
	}
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(opID, func() (ops.Spec, error) {
		if err := validateSourceState(env, scope, request.ProjectID, request.TaskID, request.SourceState); err != nil {
			return ops.Spec{}, err
		}
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(relPath))); err == nil {
			return ops.Spec{}, fmt.Errorf("handoff %q already exists", id)
		} else if !errors.Is(err, os.ErrNotExist) {
			return ops.Spec{}, err
		}
		listed, err := ListWithDiagnostics(env, ListOptions{Scope: scope, Visibility: model.VisibilityLocal, TaskID: request.TaskID})
		if err != nil {
			return ops.Spec{}, err
		}
		current := currentLocalRecords(listed.Records)
		previous := request.Previous
		if previous != nil {
			validated, validateErr := validateLocalPrevious(scope, request.TaskID, *previous, listed.Records)
			if validateErr != nil {
				return ops.Spec{}, validateErr
			}
			previous = &validated
		} else if len(current) > 0 {
			ref := refForRecord(current[0])
			previous = &ref
		}
		resolvedMutation := mutation
		if mutation.Build != nil {
			built, buildErr := mutation.Build()
			if buildErr != nil {
				return ops.Spec{}, buildErr
			}
			resolvedMutation.Writes = append(resolvedMutation.Writes, built.Writes...)
			resolvedMutation.Deletes = append(resolvedMutation.Deletes, built.Deletes...)
			resolvedMutation.Events = append(resolvedMutation.Events, built.Events...)
			resolvedMutation.Build = nil
		}
		now := time.Now().UTC()
		body := strings.TrimSpace(request.Body)
		request.Previous = previous
		request.SourceState = cleanRef(request.SourceState)
		if body == "" {
			body = renderBody(request)
		}
		body = normalizeNewlines(body)
		if len([]byte(body)) > LocalBodyMax {
			return ops.Spec{}, fmt.Errorf("local handoff body exceeds %d bytes", LocalBodyMax)
		}
		meta := Metadata{HandoffMetaV2: model.HandoffMetaV2{
			BaseMetaV2: model.BaseMetaV2{
				Schema: Schema, ID: id, Scope: scope, ObjectKind: model.ObjectKindRuntime,
				Title: withDefault(request.Title, "Handoff"), Tags: cleanList(request.Tags),
				CreatedAt: now, UpdatedAt: now,
			},
			ProjectID: strings.TrimSpace(request.ProjectID), TaskID: strings.TrimSpace(request.TaskID),
			RuntimeType: model.RuntimeTypeHandoff, Summary: strings.TrimSpace(request.Summary),
			Complete: request.Complete, Visibility: model.VisibilityLocal, StorageClass: model.StorageClassLocal,
			Durability: model.DurabilityEphemeral, LifecycleStatus: model.LifecycleCurrent,
			SourceState: cleanRef(request.SourceState), PreviousHandoff: cleanRef(previous),
			NextSteps: cleanNextSteps(request.NextSteps), OpenQuestions: cleanList(request.OpenQuestions),
			Risks: cleanList(request.Risks), Validation: cleanValidation(request.Validation),
			Worktree: request.Worktree, RedactionStatus: redactionStatus(redacted),
			ResumePriority: model.ResumePriorityManualHandoff, FormatVersion: 2, SchemaCompat: []string{Schema},
			SourceTool: withDefault(request.SourceTool, "worktrail"), Actor: withDefault(request.Actor, "handoff"),
		}}
		if err := validateFinalRecord(meta, body, textsafety.ProfileLocal); err != nil {
			return ops.Spec{}, err
		}
		if err := setContentHash(&meta, body); err != nil {
			return ops.Spec{}, err
		}
		data, err := renderRecord(meta, body)
		if err != nil {
			return ops.Spec{}, err
		}
		writes := make([]ops.Write, 0, len(current)+len(resolvedMutation.Writes)+2)
		for _, previousRecord := range current {
			if previousRecord.Meta.ID == id {
				continue
			}
			updated := previousRecord
			updated.Meta.LifecycleStatus = model.LifecycleSuperseded
			updated.Meta.Status = model.LifecycleSuperseded
			updated.Meta.UpdatedAt = now
			if err := setContentHash(&updated.Meta, updated.Body); err != nil {
				return ops.Spec{}, err
			}
			updatedData, err := renderRecord(updated.Meta, updated.Body)
			if err != nil {
				return ops.Spec{}, err
			}
			writes = append(writes, ops.Write{Path: updated.RelPath, Data: updatedData, Mode: 0o600})
		}
		writes = append(writes, ops.Write{Path: relPath, Data: data, Mode: 0o600})
		for _, write := range resolvedMutation.Writes {
			writes = append(writes, ops.Write{Path: write.Path, Data: write.Data, Mode: write.Mode})
		}
		events := append([]Event{{
			Name: "handoff.create", ID: id, Actor: withDefault(request.Actor, "handoff"),
			Data: map[string]any{"path": relPath, "task_id": request.TaskID, "visibility": model.VisibilityLocal}, Time: now,
		}}, resolvedMutation.Events...)
		writes, err = appendEventWrite(root, writes, events)
		if err != nil {
			return ops.Spec{}, err
		}
		return ops.Spec{Writes: writes, Deletes: resolvedMutation.Deletes}, nil
	})
	if err != nil {
		return Record{}, err
	}
	if err := operation.Commit(); err != nil {
		return Record{}, fmt.Errorf("commit handoff create operation %s: %w", opID, err)
	}
	return Read(filepath.Join(root, filepath.FromSlash(relPath)))
}

func Publish(ctx context.Context, env paths.Env, request PublishRequest) (Record, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	scope := withDefault(request.Scope, "project")
	if scope != "project" {
		return Record{}, errors.New("team publish is only supported for project scope")
	}
	if strings.TrimSpace(request.ID) == "" {
		return Record{}, errors.New("handoff id is required")
	}
	root, _, err := scopeRoot(env, scope)
	if err != nil {
		return Record{}, err
	}
	id, err := newOpaqueID("handoff")
	if err != nil {
		return Record{}, err
	}
	relPath := handoffRelPath(model.VisibilityTeam, id)
	opID, err := newOpaqueID("op_handoff_publish")
	if err != nil {
		return Record{}, err
	}
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(opID, func() (ops.Spec, error) {
		local, err := Show(env, ShowRequest{Scope: scope, ID: request.ID, Visibility: model.VisibilityLocal})
		if err != nil {
			return ops.Spec{}, err
		}
		snapshot, err := worktreesnap.Capture(ctx, env.ProjectRoot)
		if err != nil {
			return ops.Spec{}, err
		}
		if snapshot.Dirty {
			if !request.AllowDirty {
				return ops.Spec{}, errors.New("worktree is dirty; publish requires --allow-dirty --confirm")
			}
			if !request.Confirm {
				return ops.Spec{}, errors.New("--allow-dirty requires --confirm")
			}
			snapshot.CodeAvailability = model.CodeAvailabilityUnavailable
		} else {
			snapshot.CodeAvailability = model.CodeAvailabilityAvailable
		}
		if len([]byte(local.Body)) > TeamBodyMax {
			return ops.Spec{}, fmt.Errorf("team handoff body exceeds %d bytes", TeamBodyMax)
		}
		team, err := ListWithDiagnostics(env, ListOptions{Scope: scope, Visibility: model.VisibilityTeam, TaskID: local.Meta.TaskID})
		if err != nil {
			return ops.Spec{}, err
		}
		headRecords := teamHeads(team.Records)
		supersedes, err := resolveTeamSupersedes(scope, local.Meta.TaskID, request.Supersedes, headRecords, team.Records)
		if err != nil {
			return ops.Spec{}, err
		}
		now := time.Now().UTC()
		teamMeta := local.Meta
		teamMeta.ID = id
		teamMeta.CreatedAt = now
		teamMeta.UpdatedAt = now
		teamMeta.Visibility = model.VisibilityTeam
		teamMeta.StorageClass = model.StorageClassTeam
		teamMeta.Durability = model.DurabilityDurable
		teamMeta.LifecycleStatus = model.LifecyclePublished
		teamMeta.Status = model.LifecyclePublished
		teamMeta.SourceState = refWithoutPath(teamMeta.SourceState)
		teamMeta.PreviousHandoff = nil
		teamMeta.PublishedFrom = &model.Ref{Scope: scope, Kind: "handoff", ID: local.Meta.ID}
		teamMeta.Supersedes = supersedes
		teamMeta.SupersededBy = nil
		teamMeta.Worktree = snapshot
		teamMeta.Actor = withDefault(request.Actor, "handoff-publish")
		teamMeta.PublishedAt = &now
		teamMeta.ClosedAt = nil
		if err := validateFinalRecord(teamMeta, local.Body, textsafety.ProfileTeam); err != nil {
			return ops.Spec{}, err
		}
		if err := setContentHash(&teamMeta, local.Body); err != nil {
			return ops.Spec{}, err
		}
		teamData, err := renderRecord(teamMeta, local.Body)
		if err != nil {
			return ops.Spec{}, err
		}
		local.Meta.LifecycleStatus = model.LifecyclePublished
		local.Meta.Status = model.LifecyclePublished
		local.Meta.UpdatedAt = now
		local.Meta.PublishedAt = &now
		if err := setContentHash(&local.Meta, local.Body); err != nil {
			return ops.Spec{}, err
		}
		localData, err := renderRecord(local.Meta, local.Body)
		if err != nil {
			return ops.Spec{}, err
		}
		writes := []ops.Write{
			{Path: relPath, Data: teamData, Mode: 0o644},
			{Path: local.RelPath, Data: localData, Mode: 0o600},
		}
		writes, err = appendEventWrite(root, writes, []Event{{
			Name: "handoff.publish", ID: id, Actor: withDefault(request.Actor, "handoff-publish"),
			Data: map[string]any{
				"path": relPath, "task_id": local.Meta.TaskID,
				"published_from": local.Meta.ID, "dirty": snapshot.Dirty,
			}, Time: now,
		}})
		if err != nil {
			return ops.Spec{}, err
		}
		return ops.Spec{Writes: writes}, nil
	})
	if err != nil {
		return Record{}, err
	}
	if err := operation.Commit(); err != nil {
		return Record{}, fmt.Errorf("commit handoff publish operation %s: %w", opID, err)
	}
	return Read(filepath.Join(root, filepath.FromSlash(relPath)))
}

func Close(env paths.Env, request CloseRequest) (Record, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	root, _, err := scopeRoot(env, request.Scope)
	if err != nil {
		return Record{}, err
	}
	opID, err := newOpaqueID("op_handoff_close")
	if err != nil {
		return Record{}, err
	}
	var recordPath string
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(opID, func() (ops.Spec, error) {
		record, err := Show(env, ShowRequest{Scope: request.Scope, ID: request.ID})
		if err != nil {
			return ops.Spec{}, err
		}
		if record.Meta.Visibility == model.VisibilityTeam {
			return ops.Spec{}, errors.New("team handoffs are immutable and cannot be closed in place")
		}
		now := time.Now().UTC()
		record.Meta.LifecycleStatus = model.LifecycleClosed
		record.Meta.Status = model.LifecycleClosed
		record.Meta.UpdatedAt = now
		record.Meta.ClosedAt = &now
		if err := setContentHash(&record.Meta, record.Body); err != nil {
			return ops.Spec{}, err
		}
		data, err := renderRecord(record.Meta, record.Body)
		if err != nil {
			return ops.Spec{}, err
		}
		writes := []ops.Write{{Path: record.RelPath, Data: data, Mode: 0o600}}
		writes, err = appendEventWrite(root, writes, []Event{{
			Name: "handoff.close", ID: record.Meta.ID, Actor: withDefault(request.Actor, "handoff-close"),
			Data: map[string]any{"path": record.RelPath, "task_id": record.Meta.TaskID}, Time: now,
		}})
		if err != nil {
			return ops.Spec{}, err
		}
		recordPath = record.Path
		return ops.Spec{Writes: writes}, nil
	})
	if err != nil {
		return Record{}, err
	}
	if err := operation.Commit(); err != nil {
		return Record{}, fmt.Errorf("commit handoff close operation %s: %w", opID, err)
	}
	return Read(recordPath)
}

func validateCreateRequest(request CreateRequest) error {
	if strings.TrimSpace(request.Summary) == "" {
		return errors.New("handoff summary is required")
	}
	bodyLower := strings.ToLower(request.Body)
	if strings.Contains(bodyLower, "## state snapshot") || strings.Contains(bodyLower, "## full snapshot") {
		return errors.New("handoff body must not embed a full state snapshot")
	}
	if !request.Complete && len(cleanNextSteps(request.NextSteps)) == 0 {
		return errors.New("non-complete handoff requires at least one next step")
	}
	for name, value := range map[string]string{"source_tool": request.SourceTool, "actor": request.Actor} {
		if strings.TrimSpace(value) != "" {
			if err := validateIdentity(name, value); err != nil {
				return err
			}
		}
	}
	for _, step := range request.NextSteps {
		if id := strings.TrimSpace(step.ID); id != "" {
			if err := validateIdentity("next step id", id); err != nil {
				return err
			}
		}
	}
	for _, validation := range request.Validation {
		if utf8.RuneCountInString(strings.TrimSpace(validation.Summary)) > 240 {
			return errors.New("validation summary exceeds 240 characters")
		}
		switch strings.TrimSpace(validation.Status) {
		case model.ValidationStatusUnknown, model.ValidationStatusPassed, model.ValidationStatusFailed, model.ValidationStatusSkipped:
		default:
			return fmt.Errorf("invalid validation status %q", validation.Status)
		}
	}
	return nil
}

func validateIdentity(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !opaqueIdentifierRE.MatchString(value) {
		return fmt.Errorf("%s must match %s", name, opaqueIdentifierRE.String())
	}
	return nil
}

var (
	opaqueIdentifierRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
	headCommitRE       = regexp.MustCompile(`^[0-9A-Fa-f]{7,64}$`)
)

func normalizeWorktree(snapshot *model.WorktreeSnapshot) error {
	if snapshot == nil {
		return nil
	}
	if commit := strings.TrimSpace(snapshot.HeadCommit); commit != "" && !headCommitRE.MatchString(commit) {
		return errors.New("worktree head_commit must be a hexadecimal revision")
	}
	switch strings.TrimSpace(snapshot.CodeAvailability) {
	case "", model.CodeAvailabilityAvailable, model.CodeAvailabilityUnavailable:
	default:
		return fmt.Errorf("invalid worktree code_availability %q", snapshot.CodeAvailability)
	}
	if len(snapshot.ChangedPaths) > worktreesnap.MaxChangedPaths {
		snapshot.ChangedPaths = append([]model.WorktreePathStatus(nil), snapshot.ChangedPaths[:worktreesnap.MaxChangedPaths]...)
	}
	for index := range snapshot.ChangedPaths {
		path := filepath.ToSlash(strings.TrimSpace(snapshot.ChangedPaths[index].Path))
		if path == "" || filepath.IsAbs(filepath.FromSlash(path)) || path == ".." || strings.HasPrefix(path, "../") {
			return fmt.Errorf("worktree path %q must be repository-relative", path)
		}
		snapshot.ChangedPaths[index].Path = path
	}
	if snapshot.ChangedPathCount < len(snapshot.ChangedPaths) {
		snapshot.ChangedPathCount = len(snapshot.ChangedPaths)
	}
	return nil
}

func sanitizeCreateRequest(request CreateRequest, profile textsafety.Profile) (CreateRequest, bool, error) {
	redacted := false
	process := func(value string) (string, error) {
		result, err := textsafety.Process(value, profile)
		if err != nil {
			return "", err
		}
		redacted = redacted || result.Redacted
		return strings.TrimSpace(result.Text), nil
	}
	var err error
	for _, target := range []*string{
		&request.Title, &request.Summary, &request.Body, &request.SourceTool, &request.Actor,
		&request.Worktree.Branch,
	} {
		*target, err = process(*target)
		if err != nil {
			return request, redacted, err
		}
	}
	for index := range request.Tags {
		request.Tags[index], err = process(request.Tags[index])
		if err != nil {
			return request, redacted, err
		}
	}
	for index := range request.NextSteps {
		request.NextSteps[index].Action, err = process(request.NextSteps[index].Action)
		if err != nil {
			return request, redacted, err
		}
		request.NextSteps[index].Owner, err = process(request.NextSteps[index].Owner)
		if err != nil {
			return request, redacted, err
		}
	}
	for _, values := range []*[]string{&request.OpenQuestions, &request.Risks} {
		for index := range *values {
			(*values)[index], err = process((*values)[index])
			if err != nil {
				return request, redacted, err
			}
		}
	}
	for index := range request.Validation {
		request.Validation[index].Command, err = process(request.Validation[index].Command)
		if err != nil {
			return request, redacted, err
		}
		request.Validation[index].Summary, err = process(request.Validation[index].Summary)
		if err != nil {
			return request, redacted, err
		}
	}
	for _, ref := range []*model.Ref{request.SourceState, request.Previous} {
		if ref == nil {
			continue
		}
		ref.RelPath, err = process(ref.RelPath)
		if err != nil {
			return request, redacted, err
		}
	}
	for index := range request.Worktree.ChangedPaths {
		request.Worktree.ChangedPaths[index].Path, err = process(request.Worktree.ChangedPaths[index].Path)
		if err != nil {
			return request, redacted, err
		}
		request.Worktree.ChangedPaths[index].Status, err = process(request.Worktree.ChangedPaths[index].Status)
		if err != nil {
			return request, redacted, err
		}
	}
	return request, redacted, nil
}

func validateFinalRecord(meta Metadata, body string, profile textsafety.Profile) error {
	result, err := textsafety.Process(finalSafetyText(meta, body), profile)
	if err != nil {
		return err
	}
	if result.Redacted {
		return errors.New("final handoff serialization contains unprocessed redactable content")
	}
	return nil
}

func finalSafetyText(meta Metadata, body string) string {
	values := []string{
		meta.ID,
		meta.ProjectID,
		meta.TaskID,
		meta.Title,
		meta.Summary,
		body,
		meta.SourceTool,
		meta.Actor,
		meta.Worktree.Branch,
	}
	values = append(values, meta.Tags...)
	for _, step := range meta.NextSteps {
		values = append(values, step.ID, step.Action, step.Owner)
	}
	values = append(values, meta.OpenQuestions...)
	values = append(values, meta.Risks...)
	for _, evidence := range meta.Validation {
		values = append(values, evidence.Command, evidence.Summary)
	}
	for _, changed := range meta.Worktree.ChangedPaths {
		values = append(values, changed.Path, changed.Status)
	}
	for _, ref := range []*model.Ref{meta.SourceState, meta.PreviousHandoff, meta.PublishedFrom} {
		if ref != nil {
			values = append(values, ref.Scope, ref.Kind, ref.ID, ref.RelPath)
		}
	}
	for _, refs := range [][]model.Ref{meta.Supersedes, meta.SupersededBy} {
		for _, ref := range refs {
			values = append(values, ref.Scope, ref.Kind, ref.ID, ref.RelPath)
		}
	}
	return strings.Join(values, "\n")
}

func validateSourceState(env paths.Env, scope, expectedProjectID, taskID string, ref *model.Ref) error {
	if ref == nil {
		return nil
	}
	cleaned := cleanRef(ref)
	if cleaned == nil {
		return errors.New("source_state id is required")
	}
	if cleaned.Scope != scope {
		return fmt.Errorf("source_state scope %q does not match handoff scope %q", cleaned.Scope, scope)
	}
	if cleaned.Kind != "state" {
		return fmt.Errorf("source_state kind must be %q", "state")
	}
	if err := validateIdentity("source state id", cleaned.ID); err != nil {
		return err
	}
	actualProjectID, err := projectID(env, scope)
	if err != nil {
		return err
	}
	if actualProjectID != strings.TrimSpace(expectedProjectID) {
		return fmt.Errorf("source_state project %q does not match handoff project %q", actualProjectID, expectedProjectID)
	}
	states, err := wtstate.List(env, wtstate.ListOptions{Scope: scope, Directory: "all"})
	if err != nil {
		return err
	}
	for _, candidate := range states {
		if candidate.Directory == wtstate.DirCheckpoints || candidate.State.ID != cleaned.ID {
			continue
		}
		if candidate.State.Scope != scope || wtstate.TaskID(candidate) != strings.TrimSpace(taskID) {
			return fmt.Errorf("source_state %q does not belong to task %q in scope %q", cleaned.ID, taskID, scope)
		}
		return nil
	}
	return fmt.Errorf("source_state %q was not found", cleaned.ID)
}

func renderBody(request CreateRequest) string {
	var body strings.Builder
	fmt.Fprintf(&body, "# Handoff: %s\n\n## Summary\n\n%s\n", withDefault(request.Title, "Handoff"), strings.TrimSpace(request.Summary))
	if len(request.NextSteps) > 0 {
		body.WriteString("\n## Next Steps\n\n")
		for _, step := range cleanNextSteps(request.NextSteps) {
			fmt.Fprintf(&body, "- %s", step.Action)
			if step.Owner != "" {
				fmt.Fprintf(&body, " (owner: %s)", step.Owner)
			}
			body.WriteByte('\n')
		}
	}
	writeListSection(&body, "Open Questions", request.OpenQuestions)
	writeListSection(&body, "Risks", request.Risks)
	if len(request.Validation) > 0 {
		body.WriteString("\n## Validation\n\n")
		for _, evidence := range cleanValidation(request.Validation) {
			fmt.Fprintf(&body, "- [%s]", evidence.Status)
			if evidence.Command != "" {
				fmt.Fprintf(&body, " `%s`", evidence.Command)
			}
			if evidence.Summary != "" {
				fmt.Fprintf(&body, " — %s", evidence.Summary)
			}
			body.WriteByte('\n')
		}
	}
	if request.SourceState != nil {
		body.WriteString("\n## References\n\n")
		fmt.Fprintf(&body, "- Source state: `%s`", request.SourceState.ID)
		if request.SourceState.RelPath != "" {
			fmt.Fprintf(&body, " (`%s`)", request.SourceState.RelPath)
		}
		body.WriteByte('\n')
	}
	return strings.TrimSpace(body.String())
}

func writeListSection(body *strings.Builder, title string, values []string) {
	values = cleanList(values)
	if len(values) == 0 {
		return
	}
	fmt.Fprintf(body, "\n## %s\n\n", title)
	for _, value := range values {
		fmt.Fprintf(body, "- %s\n", value)
	}
}

func safetyText(record Record, snapshot model.WorktreeSnapshot) string {
	var value strings.Builder
	value.WriteString(record.Meta.Title)
	value.WriteByte('\n')
	value.WriteString(record.Meta.Summary)
	value.WriteByte('\n')
	value.WriteString(record.Body)
	for _, step := range record.Meta.NextSteps {
		value.WriteByte('\n')
		value.WriteString(step.Action)
		value.WriteByte('\n')
		value.WriteString(step.Owner)
	}
	for _, item := range append(append([]string(nil), record.Meta.OpenQuestions...), record.Meta.Risks...) {
		value.WriteByte('\n')
		value.WriteString(item)
	}
	for _, evidence := range record.Meta.Validation {
		value.WriteByte('\n')
		value.WriteString(evidence.Command)
		value.WriteByte('\n')
		value.WriteString(evidence.Summary)
	}
	for _, changed := range snapshot.ChangedPaths {
		value.WriteByte('\n')
		value.WriteString(changed.Path)
	}
	return value.String()
}

func projectID(env paths.Env, scope string) (string, error) {
	configRoot := env.ProjectWT
	if scope == "user" {
		configRoot = env.ProjectWT
	}
	data, err := os.ReadFile(filepath.Join(configRoot, "config.json"))
	if err != nil {
		return "", fmt.Errorf("read stable project_id from config.json: %w", err)
	}
	var config struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return "", fmt.Errorf("decode config.json: %w", err)
	}
	if strings.TrimSpace(config.ProjectID) == "" {
		return "", errors.New("config.json is missing stable project_id")
	}
	return strings.TrimSpace(config.ProjectID), nil
}

func newOpaqueID(prefix string) (string, error) {
	random := make([]byte, 12)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return prefix + "_" + strconv.FormatInt(time.Now().UTC().UnixMilli(), 36) + "_" + hex.EncodeToString(random), nil
}

func NewTaskID() (string, error) {
	return newOpaqueID("task")
}
