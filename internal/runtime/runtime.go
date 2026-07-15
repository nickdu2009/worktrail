package runtime

import (
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	DirSessions    = "sessions"
	DirCheckpoints = "checkpoints"
	DirRecovery    = "recovery"

	RetentionWindow    = 14 * 24 * time.Hour
	LatestPerTaskLimit = 5
)

type Record struct {
	Meta        map[string]any
	Body        string
	Path        string
	ID          string
	ContentHash string
	RuntimeType string
	ProjectID   string
	TaskID      string
	ExpiresAt   time.Time
	UpdatedAt   time.Time
}

type WriteOptions struct {
	Scope          string
	Title          string
	Body           string
	RuntimeType    string
	ResumePriority string
	SessionID      string
	ProjectID      string
	TaskID         string
	SourceTool     string
	Event          string
	Tags           []string
	Actor          string
	FileSuffix     string
}

type Recorder struct {
	Root string
}

type Diagnostic struct {
	Path  string `json:"path"`
	Kind  string `json:"kind,omitempty"`
	Error string `json:"error"`
}

type malformedRuntimeError struct {
	err error
}

func (e malformedRuntimeError) Error() string { return "parse runtime markdown: " + e.err.Error() }
func (e malformedRuntimeError) Unwrap() error { return e.err }

type ListResult struct {
	Records     []Record     `json:"records,omitempty"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type PruneIdentity struct {
	ID          string `json:"id"`
	RuntimeType string `json:"runtime_type"`
	ProjectID   string `json:"project_id,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
}

type PruneItem struct {
	RelPath     string        `json:"relpath"`
	ContentHash string        `json:"content_hash"`
	Identity    PruneIdentity `json:"identity"`
	Reason      string        `json:"reason"`
	ExpiresAt   time.Time     `json:"expires_at,omitempty"`
}

type PruneProposal struct {
	Root        string      `json:"-"`
	Scope       string      `json:"scope"`
	GeneratedAt time.Time   `json:"generated_at"`
	Items       []PruneItem `json:"items,omitempty"`
}

type PruneResult struct {
	Deleted     int    `json:"deleted"`
	OperationID string `json:"operation_id,omitempty"`
}

func NewRecorder(root string) Recorder {
	return Recorder{Root: strings.TrimSpace(root)}
}

func EnsureDirs(root string) error {
	dirs := []string{
		filepath.Join(root, "runtime", DirSessions),
		filepath.Join(root, "runtime", DirCheckpoints),
		filepath.Join(root, "runtime", DirRecovery),
		filepath.Join(root, "raw", "cursor"),
		filepath.Join(root, "logs"),
	}
	for _, dir := range dirs {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		if _, err := EnsurePrivateDir(root, rel); err != nil {
			return err
		}
	}
	return nil
}

func (r Recorder) WriteSession(opts WriteOptions) (Record, error) {
	opts.RuntimeType = model.RuntimeTypeSessionState
	if opts.ResumePriority == "" {
		opts.ResumePriority = model.ResumePriorityHookRuntimeState
	}
	return r.write(filepath.Join("runtime", DirSessions), opts)
}

func (r Recorder) WriteCheckpoint(opts WriteOptions) (Record, error) {
	opts.RuntimeType = model.RuntimeTypeCheckpoint
	if opts.ResumePriority == "" {
		opts.ResumePriority = model.ResumePriorityRuntimeCheckpoint
	}
	return r.write(filepath.Join("runtime", DirCheckpoints), opts)
}

func (r Recorder) WriteTakeoverNote(opts WriteOptions) (Record, error) {
	opts.RuntimeType = model.RuntimeTypeTakeoverNote
	if opts.ResumePriority == "" {
		opts.ResumePriority = model.ResumePriorityRuntimeCheckpoint
	}
	return r.write(filepath.Join("runtime", DirCheckpoints), opts)
}

func (r Recorder) write(relDir string, opts WriteOptions) (Record, error) {
	if strings.TrimSpace(r.Root) == "" {
		return Record{}, errors.New("runtime recorder root is required")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Worktrail runtime"
	}
	now := time.Now().UTC()
	projectID := strings.TrimSpace(opts.ProjectID)
	if projectID == "" {
		projectID = projectIDFromRoot(r.Root)
	}
	taskID := strings.TrimSpace(opts.TaskID)
	unboundFields := make([]string, 0, 2)
	if projectID == "" {
		unboundFields = append(unboundFields, "project_id")
	}
	if taskID == "" {
		unboundFields = append(unboundFields, "task_id")
	}
	idPrefix := "rt"
	switch opts.RuntimeType {
	case model.RuntimeTypeCheckpoint:
		idPrefix = "chk"
	case model.RuntimeTypeTakeoverNote:
		idPrefix = "tko"
	case model.RuntimeTypeSessionState:
		idPrefix = "ses"
	}
	id := fmt.Sprintf("%s_%s_%s", idPrefix, now.Format("20060102_150405.000000000"), util.Slug(title))
	fileName := runtimeFileName(id, opts, now)
	meta := map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               id,
		"object_kind":      model.ObjectKindRuntime,
		"scope":            withDefault(opts.Scope, "project"),
		"runtime_type":     opts.RuntimeType,
		"title":            title,
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"resume_priority":  opts.ResumePriority,
		"session_id":       strings.TrimSpace(opts.SessionID),
		"project_id":       projectID,
		"task_id":          taskID,
		"binding_status":   bindingStatus(unboundFields),
		"unbound_fields":   unboundFields,
		"source_tool":      strings.TrimSpace(opts.SourceTool),
		"created_at":       now,
		"updated_at":       now,
		"expires_at":       now.Add(RetentionWindow),
		"tags":             cleanList(append([]string{opts.Event}, opts.Tags...)),
	}
	data, err := store.RenderMarkdown(meta, opts.Body)
	if err != nil {
		return Record{}, err
	}
	path, err := paths.SafeJoin(r.Root, relDir, fileName)
	if err != nil {
		return Record{}, err
	}
	if _, err := EnsurePrivateDir(r.Root, relDir); err != nil {
		return Record{}, err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return Record{}, fmt.Errorf("runtime output %q is not a regular file", path)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return Record{}, statErr
	}
	if err := util.AtomicWrite(path, data, 0o600); err != nil {
		return Record{}, err
	}
	actor := withDefault(opts.Actor, "runtime")
	if _, err := EnsurePrivateDir(r.Root, "logs"); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(r.Root, "runtime.write", id, actor, map[string]any{
		"path":            relToRoot(r.Root, path),
		"runtime_type":    opts.RuntimeType,
		"resume_priority": opts.ResumePriority,
	}); err != nil {
		return Record{}, err
	}
	return Record{
		Meta:        meta,
		Body:        opts.Body,
		Path:        path,
		ID:          id,
		ContentHash: hashBytes(data),
		RuntimeType: opts.RuntimeType,
		ProjectID:   projectID,
		TaskID:      taskID,
		ExpiresAt:   now.Add(RetentionWindow),
		UpdatedAt:   now,
	}, nil
}

func runtimeFileName(id string, opts WriteOptions, now time.Time) string {
	base := strings.TrimSpace(opts.FileSuffix)
	if base == "" {
		base = now.Format("20060102-150405.000000000")
	}
	parts := []string{base}
	if sessionKey := runtimeSessionKey(opts.SessionID); sessionKey != "" {
		parts = append(parts, sessionKey)
	}
	parts = append(parts, shortHash(id))
	return strings.Join(parts, "-") + ".md"
}

func runtimeSessionKey(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return "session-" + shortHash(sessionID)
}

func (r Recorder) WriteRecoveryDashboard(body string) (string, error) {
	now := time.Now().UTC()
	projectID := projectIDFromRoot(r.Root)
	unboundFields := []string{"task_id"}
	if projectID == "" {
		unboundFields = append([]string{"project_id"}, unboundFields...)
	}
	meta := map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "recovery_dashboard_" + now.Format("20060102_150405"),
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     "recovery_dashboard",
		"title":            "Recovery Dashboard",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleCurrent,
		"project_id":       projectID,
		"binding_status":   bindingStatus(unboundFields),
		"unbound_fields":   unboundFields,
		"created_at":       now,
		"updated_at":       now,
		"expires_at":       now.Add(RetentionWindow),
		"tags":             []string{"recovery", "dashboard"},
	}
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(r.Root, "runtime", DirRecovery, "current-state.md")
	if _, err := EnsurePrivateDir(r.Root, filepath.Join("runtime", DirRecovery)); err != nil {
		return "", err
	}
	if info, statErr := os.Lstat(path); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return "", fmt.Errorf("runtime output %q is not a regular file", path)
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", statErr
	}
	if err := util.AtomicWrite(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func List(env paths.Env, scope, dir string) ([]Record, error) {
	result, err := ListWithDiagnostics(env, scope, dir)
	if err != nil {
		return nil, err
	}
	return result.Records, nil
}

func ListWithDiagnostics(env paths.Env, scope, dir string) (ListResult, error) {
	root, _, err := scopeRoot(env, scope)
	if err != nil {
		return ListResult{}, err
	}
	items, diagnostics, err := listAllRoot(root, dir)
	if err != nil {
		return ListResult{}, err
	}
	recordDiagnostics(root, dir, diagnostics)
	return ListResult{
		Records:     latestValidPerTask(items, time.Now().UTC(), LatestPerTaskLimit),
		Diagnostics: diagnostics,
	}, nil
}

func listAll(env paths.Env, scope, dir string) ([]Record, []Diagnostic, error) {
	root, _, err := scopeRoot(env, scope)
	if err != nil {
		return nil, nil, err
	}
	items, diagnostics, err := listAllRoot(root, dir)
	recordDiagnostics(root, dir, diagnostics)
	return items, diagnostics, err
}

func listAllRoot(root, dir string) ([]Record, []Diagnostic, error) {
	switch dir {
	case DirSessions, DirCheckpoints, DirRecovery:
	default:
		return nil, nil, fmt.Errorf("unknown runtime directory %q", dir)
	}
	target, err := paths.SafeJoin(root, "runtime", dir)
	if err != nil {
		return nil, nil, err
	}
	if err := rejectSymlinkPath(root, target); errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	} else if err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, nil, err
	}
	var out []Record
	var diagnostics []Diagnostic
	for _, entry := range entries {
		path, err := paths.SafeJoin(target, entry.Name())
		if err != nil {
			return nil, nil, err
		}
		if err := rejectSymlinkPath(root, path); err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path: relToRoot(root, path), Kind: "symlink", Error: err.Error(),
			})
			continue
		}
		info, err := os.Lstat(path)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{
				Path: relToRoot(root, path), Kind: "read_error", Error: err.Error(),
			})
			continue
		}
		if info.IsDir() {
			continue
		}
		if !info.Mode().IsRegular() {
			diagnostics = append(diagnostics, Diagnostic{
				Path: relToRoot(root, path), Kind: "non_regular", Error: "runtime entry is not a regular file",
			})
			continue
		}
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		rec, err := Read(path)
		if err != nil {
			kind := "read_error"
			var malformed malformedRuntimeError
			if errors.As(err, &malformed) {
				kind = "malformed"
			}
			diagnostics = append(diagnostics, Diagnostic{
				Path:  relToRoot(root, path),
				Kind:  kind,
				Error: err.Error(),
			})
			continue
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].UpdatedAt.After(out[j].UpdatedAt)
		}
		return out[i].Path < out[j].Path
	})
	sort.Slice(diagnostics, func(i, j int) bool { return diagnostics[i].Path < diagnostics[j].Path })
	return out, diagnostics, nil
}

func Latest(env paths.Env, scope, dir string) (Record, error) {
	items, err := List(env, scope, dir)
	if err != nil {
		return Record{}, err
	}
	if len(items) == 0 {
		return Record{}, os.ErrNotExist
	}
	return items[0], nil
}

func LatestForTask(env paths.Env, scope, projectID, taskID string, now time.Time) (Record, error) {
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		return Record{}, errors.New("runtime task_id is required")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var matches []Record
	for _, dir := range []string{DirSessions, DirCheckpoints} {
		items, _, err := listAll(env, scope, dir)
		if err != nil {
			return Record{}, err
		}
		for _, item := range items {
			if item.TaskID != taskID || isExpired(item, now) {
				continue
			}
			if projectID != "" && item.ProjectID != strings.TrimSpace(projectID) {
				continue
			}
			matches = append(matches, item)
		}
	}
	sortRecords(matches)
	if len(matches) == 0 {
		return Record{}, os.ErrNotExist
	}
	return matches[0], nil
}

func PrunePlan(env paths.Env, scope string, now time.Time) (PruneProposal, error) {
	root, resolvedScope, err := scopeRoot(env, scope)
	if err != nil {
		return PruneProposal{}, err
	}
	return prunePlanRoot(root, resolvedScope, now)
}

func prunePlanRoot(root, scope string, now time.Time) (PruneProposal, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	var records []Record
	for _, dir := range []string{DirSessions, DirCheckpoints, DirRecovery} {
		items, diagnostics, err := listAllRoot(root, dir)
		if err != nil {
			return PruneProposal{}, err
		}
		for _, diagnostic := range diagnostics {
			if diagnostic.Kind == "symlink" || diagnostic.Kind == "non_regular" {
				return PruneProposal{}, fmt.Errorf("runtime prune refuses non-regular runtime file %q: %s", diagnostic.Path, diagnostic.Error)
			}
		}
		recordDiagnostics(root, dir, diagnostics)
		records = append(records, items...)
	}
	sortRecords(records)
	seenPerTask := map[string]int{}
	proposal := PruneProposal{Root: root, Scope: scope, GeneratedAt: now}
	for _, record := range records {
		reason := ""
		if isExpired(record, now) {
			reason = "expired"
		} else if key := recordTaskKey(record); key != "" {
			seenPerTask[key]++
			if seenPerTask[key] > LatestPerTaskLimit {
				reason = "exceeds_latest_per_task"
			}
		}
		if reason == "" {
			continue
		}
		rel, err := filepath.Rel(root, record.Path)
		if err != nil {
			return PruneProposal{}, err
		}
		proposal.Items = append(proposal.Items, PruneItem{
			RelPath:     filepath.ToSlash(rel),
			ContentHash: record.ContentHash,
			Identity:    pruneIdentity(record),
			Reason:      reason,
			ExpiresAt:   effectiveExpiry(record),
		})
	}
	sort.Slice(proposal.Items, func(i, j int) bool { return proposal.Items[i].RelPath < proposal.Items[j].RelPath })
	return proposal, nil
}

func ApplyPrune(plan PruneProposal) (PruneResult, error) {
	return applyPruneWithCoordinator(plan, ops.New(plan.Root))
}

func applyPruneWithCoordinator(plan PruneProposal, coordinator *ops.Coordinator) (PruneResult, error) {
	root := strings.TrimSpace(plan.Root)
	if root == "" {
		return PruneResult{}, errors.New("runtime prune plan root is required")
	}
	if coordinator == nil {
		return PruneResult{}, errors.New("runtime prune coordinator is required")
	}
	current, err := prunePlanRoot(root, plan.Scope, plan.GeneratedAt)
	if err != nil {
		return PruneResult{}, err
	}
	currentByPath := make(map[string]PruneItem, len(current.Items))
	for _, item := range current.Items {
		currentByPath[item.RelPath] = item
	}
	deletes := make([]string, 0, len(plan.Items))
	plannedByPath := make(map[string]PruneItem, len(plan.Items))
	for _, item := range plan.Items {
		rel := filepath.ToSlash(strings.TrimSpace(item.RelPath))
		if !strings.HasPrefix(rel, "runtime/") || filepath.Ext(rel) != ".md" {
			return PruneResult{}, fmt.Errorf("refusing to prune non-runtime path %q", item.RelPath)
		}
		if _, duplicate := plannedByPath[rel]; duplicate {
			return PruneResult{}, fmt.Errorf("runtime prune plan repeats %q", rel)
		}
		plannedByPath[rel] = item
		live, ok := currentByPath[rel]
		if !ok || live.Reason != item.Reason {
			path, pathErr := paths.SafeJoin(root, filepath.FromSlash(rel))
			if pathErr != nil {
				return PruneResult{}, pathErr
			}
			if info, statErr := os.Lstat(path); statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
				return PruneResult{}, fmt.Errorf("refusing to prune non-regular runtime file %q", rel)
			}
			return PruneResult{}, fmt.Errorf("runtime prune plan is stale for %q: record is no longer prune-eligible", rel)
		}
		path, err := paths.SafeJoin(root, filepath.FromSlash(rel))
		if err != nil {
			return PruneResult{}, err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return PruneResult{}, fmt.Errorf("verify runtime prune target %q: %w", rel, err)
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return PruneResult{}, fmt.Errorf("refusing to prune non-regular runtime file %q", rel)
		}
		record, err := Read(path)
		if err != nil {
			return PruneResult{}, fmt.Errorf("verify runtime prune target %q: %w", rel, err)
		}
		if record.ContentHash != item.ContentHash {
			return PruneResult{}, fmt.Errorf("runtime prune plan is stale for %q: content hash changed", rel)
		}
		if recordIdentity := pruneIdentity(record); recordIdentity != item.Identity {
			return PruneResult{}, fmt.Errorf("runtime prune plan is stale for %q: identity changed", rel)
		}
		if expiry := effectiveExpiry(record); !expiry.Equal(item.ExpiresAt) {
			return PruneResult{}, fmt.Errorf("runtime prune plan is stale for %q: expiry changed", rel)
		}
		deletes = append(deletes, rel)
	}
	if len(deletes) == 0 {
		return PruneResult{}, nil
	}
	operation, err := coordinator.Begin(ops.Spec{Deletes: deletes})
	if err != nil {
		return PruneResult{}, err
	}
	intent := operation.Intent()
	for _, action := range intent.Actions {
		item, ok := plannedByPath[action.Path]
		if !ok || action.BeforeHash != item.ContentHash {
			abortErr := operation.Abort()
			return PruneResult{}, errors.Join(
				fmt.Errorf("runtime prune plan is stale for %q: staged content hash changed", action.Path),
				abortErr,
			)
		}
	}
	result := PruneResult{OperationID: intent.ID}
	if err := operation.Commit(); err != nil {
		return result, err
	}
	result.Deleted = len(deletes)
	if _, err := EnsurePrivateDir(root, "logs"); err == nil {
		_ = wlog.Append(root, "runtime.prune", intent.ID, "runtime", map[string]any{"deleted": result.Deleted})
	}
	return result, nil
}

func Read(path string) (Record, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Record{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return Record{}, fmt.Errorf("runtime path %q is a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return Record{}, fmt.Errorf("runtime path %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return Record{}, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(info, after) {
		return Record{}, fmt.Errorf("runtime path %q changed while reading", path)
	}
	doc, err := store.ParseMarkdown(data)
	if err != nil {
		return Record{}, malformedRuntimeError{err: err}
	}
	runtimeType := stringField(doc.Meta, "runtime_type")
	updatedAt := timeField(doc.Meta, "updated_at")
	normalized, _ := model.NormalizeObjectMeta(filepath.ToSlash(path), doc.Meta)
	return Record{
		Meta:        doc.Meta,
		Body:        doc.Body,
		Path:        path,
		ID:          stringField(doc.Meta, "id"),
		ContentHash: hashBytes(data),
		RuntimeType: runtimeType,
		ProjectID:   normalized.ProjectID,
		TaskID:      normalized.TaskID,
		ExpiresAt:   normalized.ExpiresAt,
		UpdatedAt:   updatedAt,
	}, nil
}

func latestValidPerTask(items []Record, now time.Time, limit int) []Record {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if limit <= 0 {
		limit = LatestPerTaskLimit
	}
	sortRecords(items)
	counts := map[string]int{}
	out := make([]Record, 0, len(items))
	for _, item := range items {
		if isExpired(item, now) {
			continue
		}
		key := recordTaskKey(item)
		if key != "" {
			if counts[key] >= limit {
				continue
			}
			counts[key]++
		}
		out = append(out, item)
	}
	return out
}

func sortRecords(items []Record) {
	sort.Slice(items, func(i, j int) bool {
		if !items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		}
		return items[i].Path < items[j].Path
	})
}

func recordTaskKey(record Record) string {
	projectID := strings.TrimSpace(record.ProjectID)
	taskID := strings.TrimSpace(record.TaskID)
	if projectID == "" || taskID == "" {
		return ""
	}
	return projectID + "\x00" + taskID
}

func pruneIdentity(record Record) PruneIdentity {
	return PruneIdentity{
		ID:          strings.TrimSpace(record.ID),
		RuntimeType: strings.TrimSpace(record.RuntimeType),
		ProjectID:   strings.TrimSpace(record.ProjectID),
		TaskID:      strings.TrimSpace(record.TaskID),
	}
}

func recordDiagnostics(root, dir string, diagnostics []Diagnostic) {
	if len(diagnostics) == 0 {
		return
	}
	if _, err := EnsurePrivateDir(root, "logs"); err != nil {
		return
	}
	for _, diagnostic := range diagnostics {
		_ = wlog.Append(root, "runtime.read_diagnostic", "", "runtime", map[string]any{
			"directory": dir,
			"path":      diagnostic.Path,
			"error":     diagnostic.Error,
		})
	}
}

func isExpired(record Record, now time.Time) bool {
	expiresAt := effectiveExpiry(record)
	return !expiresAt.IsZero() && !expiresAt.After(now)
}

func effectiveExpiry(record Record) time.Time {
	return EffectiveExpiry(record.ExpiresAt, timeField(record.Meta, "created_at"))
}

// EffectiveExpiry is the shared runtime retention rule used by runtime reads
// and the derived index. Missing expires_at falls back only to created_at+14d.
func EffectiveExpiry(expiresAt, createdAt time.Time) time.Time {
	if !expiresAt.IsZero() {
		return expiresAt
	}
	if createdAt.IsZero() {
		return time.Time{}
	}
	return createdAt.Add(RetentionWindow)
}

func projectIDFromRoot(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		return ""
	}
	var cfg struct {
		ProjectID string `json:"project_id"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.ProjectID)
}

func bindingStatus(unboundFields []string) string {
	if len(unboundFields) > 0 {
		return "unbound"
	}
	return "bound"
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

func relToRoot(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return path
	}
	return filepath.ToSlash(rel)
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

func StringField(meta map[string]any, key string) string {
	return stringField(meta, key)
}

func stringField(meta map[string]any, key string) string {
	raw, ok := meta[key]
	if !ok {
		return ""
	}
	if s, ok := raw.(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func timeField(meta map[string]any, key string) time.Time {
	raw, ok := meta[key]
	if !ok {
		return time.Time{}
	}
	switch v := raw.(type) {
	case time.Time:
		return v
	case string:
		if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
			return t
		}
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			return t
		}
	}
	if b, err := json.Marshal(raw); err == nil {
		var t time.Time
		if json.Unmarshal(b, &t) == nil {
			return t
		}
	}
	return time.Time{}
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
