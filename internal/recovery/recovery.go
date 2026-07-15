package recovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

const (
	SchemaSelection = "worktrail.recovery.selection.v2"

	QualityPrimary  = "primary"
	QualityDegraded = "degraded"

	SourceLocalHandoff       = "local_handoff"
	SourceTeamHandoff        = "team_handoff"
	SourceExplicitState      = "explicit_state"
	SourceExplicitCheckpoint = "explicit_checkpoint"
	SourceRuntimeCheckpoint  = "runtime_checkpoint"
	SourceRuntimeSession     = "runtime_session"
)

const (
	priorityLocalHandoff = iota + 1
	priorityTeamHandoff
	priorityExplicitState
	priorityExplicitCheckpoint
	priorityRuntimeCheckpoint
	priorityRuntimeSession
)

type TaskSelector struct {
	TaskID string     `json:"task_id,omitempty"`
	Title  string     `json:"title,omitempty"`
	Ref    *model.Ref `json:"ref,omitempty"`
}

type TaskCandidate struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type AmbiguousTaskError struct {
	Candidates []TaskCandidate `json:"candidates"`
}

func (e *AmbiguousTaskError) Error() string {
	parts := make([]string, 0, len(e.Candidates))
	for _, item := range e.Candidates {
		parts = append(parts, fmt.Sprintf("%s (%s)", item.ID, item.Title))
	}
	return "task selector is ambiguous; choose --task-id or --ref from: " + strings.Join(parts, ", ")
}

type AmbiguousRefError struct {
	Requested model.Ref   `json:"requested"`
	Matches   []model.Ref `json:"matches"`
}

func (e *AmbiguousRefError) Error() string {
	parts := make([]string, 0, len(e.Matches))
	for _, ref := range e.Matches {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", ref.Scope, ref.Kind, ref.ID))
	}
	return fmt.Sprintf("recovery ref %q is ambiguous; specify scope and kind from: %s",
		e.Requested.ID, strings.Join(parts, ", "))
}

type Selection struct {
	Schema               string           `json:"schema"`
	Scope                string           `json:"scope"`
	ProjectID            string           `json:"project_id,omitempty"`
	TaskID               string           `json:"task_id,omitempty"`
	SourceKind           string           `json:"source_kind"`
	Priority             int              `json:"priority"`
	Quality              string           `json:"quality"`
	DegradedReason       string           `json:"degraded_reason,omitempty"`
	CodeAvailabilityHint string           `json:"code_availability_hint,omitempty"`
	Title                string           `json:"title"`
	SourceRef            model.Ref        `json:"source_ref"`
	SupportingRefs       []model.Ref      `json:"supporting_refs,omitempty"`
	Handoff              *handoff.Record  `json:"-"`
	State                *wtstate.Capsule `json:"-"`
	Runtime              *runtime.Record  `json:"-"`
	ActiveState          *wtstate.Capsule `json:"-"`
}

type TaskSummary struct {
	TaskID               string    `json:"task_id"`
	Title                string    `json:"title"`
	SourceKind           string    `json:"source_kind"`
	Priority             int       `json:"priority"`
	Quality              string    `json:"quality"`
	SourceRef            model.Ref `json:"source_ref"`
	CodeAvailabilityHint string    `json:"code_availability_hint,omitempty"`
	LifecycleTime        time.Time `json:"lifecycle_time"`
}

type TaskScopedResolver struct {
	Env paths.Env
	Now time.Time
}

func NewTaskScopedResolver(env paths.Env) TaskScopedResolver {
	return TaskScopedResolver{Env: env}
}

// Select keeps the pre-V2 call surface while enforcing task-scoped selection.
func Select(env paths.Env, scope string) (Selection, error) {
	return NewTaskScopedResolver(env).Resolve(scope, TaskSelector{})
}

func (r TaskScopedResolver) Resolve(scope string, selector TaskSelector) (Selection, error) {
	scope = normalizeScope(scope)
	if err := validateSelector(selector); err != nil {
		return Selection{}, err
	}
	candidates, projectID, err := r.loadCandidates(scope)
	if err != nil {
		return Selection{}, err
	}
	if selector.Ref != nil {
		matches := findByRef(candidates, *selector.Ref)
		if len(matches) == 0 {
			return Selection{}, os.ErrNotExist
		}
		if len(matches) > 1 {
			refs := make([]model.Ref, 0, len(matches))
			for _, match := range matches {
				refs = append(refs, match.ref)
			}
			sort.Slice(refs, func(i, j int) bool {
				if refs[i].Scope != refs[j].Scope {
					return refs[i].Scope < refs[j].Scope
				}
				if refs[i].Kind != refs[j].Kind {
					return refs[i].Kind < refs[j].Kind
				}
				return refs[i].ID < refs[j].ID
			})
			return Selection{}, &AmbiguousRefError{Requested: *selector.Ref, Matches: refs}
		}
		match := matches[0]
		related := []sourceCandidate{match}
		if match.taskID != "" {
			related = related[:0]
			for _, item := range candidates {
				if item.taskID == match.taskID {
					related = append(related, item)
				}
			}
		}
		return r.selection(scope, projectID, match, related), nil
	}

	taskID, err := selectTaskID(candidates, selector)
	if err != nil {
		return Selection{}, err
	}
	var taskCandidates []sourceCandidate
	for _, item := range candidates {
		if item.taskID == taskID {
			taskCandidates = append(taskCandidates, item)
		}
	}
	if len(taskCandidates) == 0 {
		return Selection{}, os.ErrNotExist
	}
	sortCandidates(taskCandidates)
	return r.selection(scope, projectID, taskCandidates[0], taskCandidates), nil
}

func (r TaskScopedResolver) ListTasks(scope string) ([]TaskSummary, error) {
	scope = normalizeScope(scope)
	candidates, projectID, err := r.loadCandidates(scope)
	if err != nil {
		return nil, err
	}
	byTask := map[string][]sourceCandidate{}
	for _, item := range candidates {
		if item.taskID == "" || !item.bound {
			continue
		}
		byTask[item.taskID] = append(byTask[item.taskID], item)
	}
	taskIDs := make([]string, 0, len(byTask))
	for taskID := range byTask {
		taskIDs = append(taskIDs, taskID)
	}
	sort.Strings(taskIDs)
	summaries := make([]TaskSummary, 0, len(taskIDs))
	for _, taskID := range taskIDs {
		items := byTask[taskID]
		sortCandidates(items)
		sel := r.selection(scope, projectID, items[0], items)
		summaries = append(summaries, TaskSummary{
			TaskID:               taskID,
			Title:                sel.Title,
			SourceKind:           sel.SourceKind,
			Priority:             sel.Priority,
			Quality:              sel.Quality,
			SourceRef:            sel.SourceRef,
			CodeAvailabilityHint: sel.CodeAvailabilityHint,
			LifecycleTime:        items[0].lifecycleTime,
		})
	}
	return summaries, nil
}

func (r TaskScopedResolver) selection(scope, projectID string, selected sourceCandidate, candidates []sourceCandidate) Selection {
	sortCandidates(candidates)
	sel := Selection{
		Schema:               SchemaSelection,
		Scope:                scope,
		ProjectID:            projectID,
		TaskID:               selected.taskID,
		SourceKind:           selected.sourceKind,
		Priority:             selected.priority,
		Quality:              selected.quality,
		DegradedReason:       selected.degradedReason,
		CodeAvailabilityHint: selected.codeAvailabilityHint,
		Title:                selected.title,
		SourceRef:            selected.ref,
		Handoff:              selected.handoff,
		State:                selected.state,
		Runtime:              selected.runtime,
	}
	seen := map[string]bool{}
	for _, item := range candidates {
		key := item.ref.Scope + "\x00" + item.ref.Kind + "\x00" + item.ref.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		sel.SupportingRefs = append(sel.SupportingRefs, item.ref)
		if sel.ActiveState == nil && item.state != nil && item.state.Directory == wtstate.DirActive && isExplicitState(*item.state) {
			state := *item.state
			sel.ActiveState = &state
		}
	}
	return sel
}

func (r TaskScopedResolver) loadCandidates(scope string) ([]sourceCandidate, string, error) {
	projectID := stableProjectID(r.Env)
	now := r.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var candidates []sourceCandidate

	handoffScopes := []string{scope}
	if scope == "project" && strings.TrimSpace(r.Env.UserRoot) != "" {
		handoffScopes = append(handoffScopes, "user")
	}
	for _, handoffScope := range handoffScopes {
		visibility := ""
		if handoffScope == "user" {
			visibility = model.VisibilityLocal
		}
		result, err := handoff.ListWithDiagnostics(r.Env, handoff.ListOptions{Scope: handoffScope, Visibility: visibility})
		if err != nil {
			return nil, "", err
		}
		for i := range result.Records {
			record := result.Records[i]
			if strings.TrimSpace(record.Meta.ProjectID) == "" || record.Meta.ProjectID != projectID || strings.TrimSpace(record.Meta.TaskID) == "" {
				continue
			}
			candidates = append(candidates, candidateFromHandoff(record, handoffScope))
		}
	}

	states, err := wtstate.List(r.Env, wtstate.ListOptions{Scope: scope, Directory: "all"})
	if err != nil {
		return nil, "", err
	}
	for i := range states {
		state := states[i]
		if strings.TrimSpace(wtstate.TaskID(state)) == "" || !isExplicitState(state) {
			continue
		}
		candidates = append(candidates, candidateFromState(r.Env, scope, state))
	}

	for _, dir := range []string{runtime.DirCheckpoints, runtime.DirSessions} {
		records, err := runtime.List(r.Env, scope, dir)
		if err != nil {
			return nil, "", err
		}
		for i := range records {
			record := records[i]
			if runtimeExpired(record, now) {
				continue
			}
			bound := strings.TrimSpace(record.ProjectID) != "" && strings.TrimSpace(record.TaskID) != ""
			if bound && record.ProjectID != projectID {
				continue
			}
			if !bound && strings.TrimSpace(record.ProjectID) != "" && record.ProjectID != projectID {
				continue
			}
			candidates = append(candidates, candidateFromRuntime(r.Env, scope, record, dir, bound))
		}
	}
	return candidates, projectID, nil
}

func RenderRecoveryDashboard(sel Selection) string {
	return renderTaskDashboard([]TaskSummary{{
		TaskID:               sel.TaskID,
		Title:                sel.Title,
		SourceKind:           sel.SourceKind,
		Priority:             sel.Priority,
		Quality:              sel.Quality,
		SourceRef:            sel.SourceRef,
		CodeAvailabilityHint: sel.CodeAvailabilityHint,
	}})
}

func RenderTaskDashboard(tasks []TaskSummary) string {
	return renderTaskDashboard(tasks)
}

func renderTaskDashboard(tasks []TaskSummary) string {
	var b strings.Builder
	b.WriteString("# Worktrail Recovery Dashboard\n\n")
	b.WriteString("Schema: `worktrail.recovery.dashboard.v2`\n\n")
	b.WriteString("| Task | Title | Source | Priority | Quality | Ref |\n")
	b.WriteString("| --- | --- | --- | ---: | --- | --- |\n")
	for _, task := range tasks {
		fmt.Fprintf(&b, "| `%s` | %s | `%s` | %d | `%s` | `%s:%s` |\n",
			task.TaskID, escapeTable(task.Title), task.SourceKind, task.Priority, task.Quality, task.SourceRef.Kind, task.SourceRef.ID)
		if task.CodeAvailabilityHint != "" {
			fmt.Fprintf(&b, "\n> `%s`: %s\n", task.TaskID, task.CodeAvailabilityHint)
		}
	}
	if len(tasks) == 0 {
		b.WriteString("| — | No recoverable tasks | — | — | — | — |\n")
	}
	return b.String()
}

func WriteRecoveryDashboard(env paths.Env, scope string) (string, error) {
	resolver := NewTaskScopedResolver(env)
	tasks, err := resolver.ListTasks(scope)
	if err != nil {
		return "", err
	}
	root, err := env.ScopeRoot(normalizeScope(scope))
	if err != nil {
		return "", err
	}
	return runtime.NewRecorder(root).WriteRecoveryDashboard(RenderTaskDashboard(tasks))
}

type sourceCandidate struct {
	sourceKind           string
	priority             int
	quality              string
	degradedReason       string
	codeAvailabilityHint string
	projectID            string
	taskID               string
	title                string
	ref                  model.Ref
	lifecycleTime        time.Time
	createdAt            time.Time
	bound                bool
	handoff              *handoff.Record
	state                *wtstate.Capsule
	runtime              *runtime.Record
}

func candidateFromHandoff(record handoff.Record, scope string) sourceCandidate {
	sourceKind := SourceLocalHandoff
	priority := priorityLocalHandoff
	if record.Meta.Visibility == model.VisibilityTeam {
		sourceKind = SourceTeamHandoff
		priority = priorityTeamHandoff
	}
	quality := QualityPrimary
	degradedReason := ""
	if lifecycle := strings.TrimSpace(record.Meta.LifecycleStatus); lifecycle != "" && lifecycle != model.LifecycleCurrent && lifecycle != model.LifecyclePublished {
		quality = QualityDegraded
		degradedReason = fmt.Sprintf("Recovery uses a %s handoff because no higher-priority current source was selected.", lifecycle)
	}
	hint := ""
	if record.Meta.Visibility == model.VisibilityTeam && record.Meta.Worktree.CodeAvailability == model.CodeAvailabilityUnavailable {
		hint = "Team handoff code is unavailable; fetch or restore the referenced revision before continuing implementation."
	}
	copy := record
	return sourceCandidate{
		sourceKind:           sourceKind,
		priority:             priority,
		quality:              quality,
		degradedReason:       degradedReason,
		codeAvailabilityHint: hint,
		projectID:            record.Meta.ProjectID,
		taskID:               record.Meta.TaskID,
		title:                withDefault(record.Meta.Title, record.Meta.TaskID),
		ref:                  model.Ref{Scope: scope, Kind: "handoff", ID: record.Meta.ID, RelPath: cleanRelPath(record.RelPath)},
		lifecycleTime:        record.Meta.UpdatedAt,
		createdAt:            record.Meta.CreatedAt,
		bound:                true,
		handoff:              &copy,
	}
}

func candidateFromState(env paths.Env, scope string, state wtstate.Capsule) sourceCandidate {
	sourceKind := SourceExplicitState
	priority := priorityExplicitState
	refKind := "state"
	if state.Directory == wtstate.DirCheckpoints {
		sourceKind = SourceExplicitCheckpoint
		priority = priorityExplicitCheckpoint
		refKind = "checkpoint"
	}
	copy := state
	return sourceCandidate{
		sourceKind:    sourceKind,
		priority:      priority,
		quality:       QualityPrimary,
		taskID:        wtstate.TaskID(state),
		title:         withDefault(state.State.Title, wtstate.TaskID(state)),
		ref:           model.Ref{Scope: scope, Kind: refKind, ID: state.State.ID, RelPath: relativeRef(env, scope, state.Path)},
		lifecycleTime: state.State.UpdatedAt,
		createdAt:     state.State.CreatedAt,
		bound:         true,
		state:         &copy,
	}
}

func candidateFromRuntime(env paths.Env, scope string, record runtime.Record, dir string, bound bool) sourceCandidate {
	sourceKind := SourceRuntimeSession
	priority := priorityRuntimeSession
	refKind := "runtime_session"
	reason := "Recovery uses a runtime session because no durable handoff, explicit state, or checkpoint was selected."
	if dir == runtime.DirCheckpoints {
		sourceKind = SourceRuntimeCheckpoint
		priority = priorityRuntimeCheckpoint
		refKind = "runtime_checkpoint"
		reason = "Recovery uses a runtime checkpoint because no durable handoff or explicit state/checkpoint was selected."
	}
	copy := record
	return sourceCandidate{
		sourceKind:     sourceKind,
		priority:       priority,
		quality:        QualityDegraded,
		degradedReason: reason,
		projectID:      record.ProjectID,
		taskID:         record.TaskID,
		title:          withDefault(titleFromRuntime(record), runtime.StringField(record.Meta, "id")),
		ref: model.Ref{
			Scope:   scope,
			Kind:    refKind,
			ID:      runtime.StringField(record.Meta, "id"),
			RelPath: relativeRef(env, scope, record.Path),
		},
		lifecycleTime: record.UpdatedAt,
		createdAt:     metadataTime(record.Meta, "created_at"),
		bound:         bound,
		runtime:       &copy,
	}
}

func selectTaskID(candidates []sourceCandidate, selector TaskSelector) (string, error) {
	if taskID := strings.TrimSpace(selector.TaskID); taskID != "" {
		for _, item := range candidates {
			if item.bound && item.taskID == taskID {
				return taskID, nil
			}
		}
		return "", os.ErrNotExist
	}
	title := strings.TrimSpace(selector.Title)
	byTask := map[string][]sourceCandidate{}
	for _, item := range candidates {
		if !item.bound || item.taskID == "" {
			continue
		}
		if title != "" && !strings.EqualFold(strings.TrimSpace(item.title), title) {
			continue
		}
		byTask[item.taskID] = append(byTask[item.taskID], item)
	}
	if len(byTask) == 0 {
		return "", os.ErrNotExist
	}
	if len(byTask) == 1 {
		for taskID := range byTask {
			return taskID, nil
		}
	}
	items := make([]TaskCandidate, 0, len(byTask))
	for taskID, sources := range byTask {
		sortCandidates(sources)
		items = append(items, TaskCandidate{ID: taskID, Title: sources[0].title})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return "", &AmbiguousTaskError{Candidates: items}
}

func findByRef(candidates []sourceCandidate, ref model.Ref) []sourceCandidate {
	var matches []sourceCandidate
	for i := range candidates {
		item := candidates[i]
		if strings.TrimSpace(ref.ID) != item.ref.ID {
			continue
		}
		if scope := strings.TrimSpace(ref.Scope); scope != "" && scope != item.ref.Scope {
			continue
		}
		if kind := strings.TrimSpace(ref.Kind); kind != "" && kind != item.ref.Kind {
			continue
		}
		matches = append(matches, item)
	}
	return matches
}

func validateSelector(selector TaskSelector) error {
	count := 0
	if strings.TrimSpace(selector.TaskID) != "" {
		count++
	}
	if strings.TrimSpace(selector.Title) != "" {
		count++
	}
	if selector.Ref != nil && strings.TrimSpace(selector.Ref.ID) != "" {
		count++
	}
	if count > 1 {
		return errors.New("choose exactly one task selector: task_id, title, or ref")
	}
	if selector.Ref != nil && strings.TrimSpace(selector.Ref.ID) == "" {
		return errors.New("task ref id is required")
	}
	return nil
}

func sortCandidates(items []sourceCandidate) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.priority != b.priority {
			return a.priority < b.priority
		}
		if !a.lifecycleTime.Equal(b.lifecycleTime) {
			return a.lifecycleTime.After(b.lifecycleTime)
		}
		if !a.createdAt.Equal(b.createdAt) {
			return a.createdAt.After(b.createdAt)
		}
		return a.ref.ID < b.ref.ID
	})
}

func stableProjectID(env paths.Env) string {
	data, err := os.ReadFile(filepath.Join(env.ProjectWT, "config.json"))
	if err != nil {
		return ""
	}
	var config struct {
		ProjectID string `json:"project_id"`
	}
	if json.Unmarshal(data, &config) != nil {
		return ""
	}
	return strings.TrimSpace(config.ProjectID)
}

func isExplicitState(state wtstate.Capsule) bool {
	tool := strings.TrimSpace(state.State.SourceTool)
	return tool == "" || tool == "worktrail"
}

func runtimeExpired(record runtime.Record, now time.Time) bool {
	expiresAt := record.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = metadataTime(record.Meta, "created_at")
		if !expiresAt.IsZero() {
			expiresAt = expiresAt.Add(runtime.RetentionWindow)
		}
	}
	return !expiresAt.IsZero() && !expiresAt.After(now)
}

func metadataTime(meta map[string]any, key string) time.Time {
	raw, ok := meta[key]
	if !ok {
		return time.Time{}
	}
	switch value := raw.(type) {
	case time.Time:
		return value.UTC()
	case string:
		parsed, _ := time.Parse(time.RFC3339Nano, value)
		return parsed.UTC()
	default:
		return time.Time{}
	}
}

func relativeRef(env paths.Env, scope, path string) string {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

func cleanRelPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" || filepath.IsAbs(filepath.FromSlash(path)) || path == ".." || strings.HasPrefix(path, "../") {
		return ""
	}
	return path
}

func titleFromRuntime(record runtime.Record) string {
	if title := runtime.StringField(record.Meta, "title"); title != "" {
		return title
	}
	return "Runtime recovery artifact"
}

func normalizeScope(scope string) string {
	if strings.TrimSpace(scope) == "" {
		return "project"
	}
	return strings.TrimSpace(scope)
}

func escapeTable(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "|", "\\|")
}

func withDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return strings.TrimSpace(fallback)
	}
	return strings.TrimSpace(value)
}
