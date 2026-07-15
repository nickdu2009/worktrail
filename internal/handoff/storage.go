package handoff

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

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

type Diagnostic struct {
	Code       string `json:"code"`
	Path       string `json:"path,omitempty"`
	ID         string `json:"id,omitempty"`
	Message    string `json:"message"`
	Repairable bool   `json:"repairable,omitempty"`
}

type ListOptions struct {
	Scope      string `json:"scope,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	TaskID     string `json:"task_id,omitempty"`
}

type ListResult struct {
	Records     []Record     `json:"records"`
	Diagnostics []Diagnostic `json:"diagnostics,omitempty"`
}

type ShowRequest struct {
	Scope      string `json:"scope,omitempty"`
	ID         string `json:"id"`
	Visibility string `json:"visibility,omitempty"`
}

var ErrMigrationRequired = errors.New("legacy handoff migration required")

type MigrationRequiredError struct {
	Paths []string
}

func (e *MigrationRequiredError) Error() string {
	return fmt.Sprintf("%v before normal handoff reads: %s", ErrMigrationRequired, strings.Join(e.Paths, ", "))
}

func (e *MigrationRequiredError) Unwrap() error {
	return ErrMigrationRequired
}

func Latest(env paths.Env, scope string) (Record, error) {
	records, err := List(env, scope)
	if err != nil {
		return Record{}, err
	}
	if len(records) == 0 {
		return Record{}, os.ErrNotExist
	}
	return records[0], nil
}

// List preserves the additive pre-cutover API. ListWithDiagnostics is the V2
// fail-soft interface and should be used by CLI and resolver code.
func List(env paths.Env, scope string) ([]Record, error) {
	result, err := ListWithDiagnostics(env, ListOptions{Scope: scope})
	return result.Records, err
}

func ListWithDiagnostics(env paths.Env, options ListOptions) (ListResult, error) {
	return listWithDiagnostics(env, options, false)
}

func listWithDiagnostics(env paths.Env, options ListOptions, allowLegacy bool) (ListResult, error) {
	root, scope, err := scopeRoot(env, options.Scope)
	if err != nil {
		return ListResult{}, err
	}
	if !allowLegacy {
		legacyPaths, err := legacyRootHandoffPaths(root)
		if err != nil {
			return ListResult{}, err
		}
		if len(legacyPaths) > 0 {
			return ListResult{}, &MigrationRequiredError{Paths: legacyPaths}
		}
	}
	visibility := strings.TrimSpace(options.Visibility)
	if visibility != "" && visibility != model.VisibilityLocal && visibility != model.VisibilityTeam {
		return ListResult{}, fmt.Errorf("visibility must be local or team")
	}
	type scanRoot struct {
		rel        string
		visibility string
		legacy     bool
	}
	roots := []scanRoot{
		{rel: "handoffs/local", visibility: model.VisibilityLocal},
		{rel: "handoffs/team", visibility: model.VisibilityTeam},
		{rel: "handoffs", visibility: model.VisibilityLocal, legacy: true},
	}
	var result ListResult
	for _, candidate := range roots {
		if visibility != "" && visibility != candidate.visibility {
			continue
		}
		dir := filepath.Join(root, filepath.FromSlash(candidate.rel))
		entries, readErr := os.ReadDir(dir)
		if errors.Is(readErr, os.ErrNotExist) {
			continue
		}
		if readErr != nil {
			return result, readErr
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			record, recordErr := readRecord(path, true)
			if recordErr != nil {
				repairable := candidate.visibility == model.VisibilityLocal &&
					!candidate.legacy &&
					!errors.Is(recordErr, errUnsafeStoragePath)
				result.Diagnostics = append(result.Diagnostics, Diagnostic{
					Code:       diagnosticCode(recordErr),
					Path:       filepath.ToSlash(filepath.Join(candidate.rel, entry.Name())),
					Message:    recordErr.Error(),
					Repairable: repairable,
				})
				continue
			}
			if candidate.legacy && record.Meta.Schema == Schema {
				continue
			}
			if record.Meta.Scope == "" {
				record.Meta.Scope = scope
			}
			if taskID := strings.TrimSpace(options.TaskID); taskID != "" && record.Meta.TaskID != taskID {
				continue
			}
			result.Records = append(result.Records, record)
		}
	}
	sort.Slice(result.Records, func(left, right int) bool {
		a, b := result.Records[left], result.Records[right]
		if lifecycleRank(a.Meta.LifecycleStatus) != lifecycleRank(b.Meta.LifecycleStatus) {
			return lifecycleRank(a.Meta.LifecycleStatus) < lifecycleRank(b.Meta.LifecycleStatus)
		}
		if a.Meta.Visibility != b.Meta.Visibility {
			return a.Meta.Visibility == model.VisibilityLocal
		}
		if !a.Meta.UpdatedAt.Equal(b.Meta.UpdatedAt) {
			return a.Meta.UpdatedAt.After(b.Meta.UpdatedAt)
		}
		return a.Meta.ID < b.Meta.ID
	})
	sort.Slice(result.Diagnostics, func(left, right int) bool {
		return result.Diagnostics[left].Path < result.Diagnostics[right].Path
	})
	return result, nil
}

func legacyRootHandoffPaths(root string) ([]string, error) {
	dir := filepath.Join(root, "handoffs")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			paths = append(paths, filepath.ToSlash(filepath.Join("handoffs", entry.Name())))
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func Show(env paths.Env, request ShowRequest) (Record, error) {
	if err := validateID(request.ID); err != nil {
		return Record{}, err
	}
	result, err := ListWithDiagnostics(env, ListOptions{Scope: request.Scope, Visibility: request.Visibility})
	if err != nil {
		return Record{}, err
	}
	var matches []Record
	for _, record := range result.Records {
		if record.Meta.ID == request.ID {
			matches = append(matches, record)
		}
	}
	if len(matches) == 0 {
		return Record{}, os.ErrNotExist
	}
	if len(matches) > 1 {
		return Record{}, fmt.Errorf("handoff id %q is ambiguous across storage classes", request.ID)
	}
	return matches[0], nil
}

func Read(path string) (Record, error) {
	if filepath.Base(filepath.Dir(filepath.Clean(path))) == "handoffs" {
		return Record{}, &MigrationRequiredError{Paths: []string{filepath.ToSlash(filepath.Join("handoffs", filepath.Base(path)))}}
	}
	return readRecord(path, true)
}

func readRecord(path string, verifyHash bool) (Record, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return Record{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Record{}, fmt.Errorf("%w: handoff path %q is not a regular non-symlink file", errUnsafeStoragePath, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	doc, err := store.ParseMarkdown(data)
	if err != nil {
		if isV2StoragePath(path) {
			return Record{}, fmt.Errorf("parse V2 handoff: %w", err)
		}
		return legacyRecord(path, string(data))
	}
	schema, _ := doc.Meta["schema"].(string)
	if schema != Schema {
		if isV2StoragePath(path) {
			return Record{}, fmt.Errorf("unexpected handoff schema %q in V2 storage", schema)
		}
		return legacyRecord(path, string(data))
	}
	var meta model.HandoffMetaV2
	raw, err := json.Marshal(doc.Meta)
	if err != nil {
		return Record{}, err
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Record{}, fmt.Errorf("decode handoff metadata: %w", err)
	}
	if err := validateV2Metadata(meta); err != nil {
		return Record{}, err
	}
	compat := Metadata{HandoffMetaV2: meta}
	projectRoot := inferScopeRootFromPath(path)
	relPath := filepath.ToSlash(filepath.Base(path))
	if projectRoot != "" {
		if rel, relErr := filepath.Rel(projectRoot, path); relErr == nil {
			relPath = filepath.ToSlash(rel)
		}
	}
	projectCompatibility(&compat)
	record := Record{
		Meta:        compat,
		Body:        normalizeNewlines(doc.Body),
		RelPath:     relPath,
		Path:        path,
		MetadataMap: doc.Meta,
	}
	if verifyHash {
		expected, err := contentHash(record.Meta, record.Body)
		if err != nil {
			return Record{}, err
		}
		if expected != record.Meta.ContentHash {
			return Record{}, fmt.Errorf("%w: got %s, want %s", errContentHashMismatch, record.Meta.ContentHash, expected)
		}
	}
	return record, nil
}

func legacyRecord(path, body string) (Record, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Record{}, err
	}
	title := inferTitle(path, body)
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	meta := Metadata{HandoffMetaV2: model.HandoffMetaV2{
		BaseMetaV2: model.BaseMetaV2{
			Schema:     model.SchemaHandoff,
			ID:         id,
			ObjectKind: model.ObjectKindRuntime,
			Title:      title,
			CreatedAt:  info.ModTime().UTC(),
			UpdatedAt:  info.ModTime().UTC(),
		},
		RuntimeType:     model.RuntimeTypeHandoff,
		Visibility:      model.VisibilityLocal,
		StorageClass:    model.StorageClassLocal,
		Durability:      model.DurabilityEphemeral,
		LifecycleStatus: model.LifecycleCurrent,
		ResumePriority:  model.ResumePriorityManualHandoff,
	}}
	if doc, parseErr := store.ParseMarkdown([]byte(body)); parseErr == nil {
		raw, marshalErr := json.Marshal(doc.Meta)
		if marshalErr == nil {
			var legacy struct {
				ID                string    `json:"id"`
				Scope             string    `json:"scope"`
				Title             string    `json:"title"`
				Summary           string    `json:"summary"`
				Status            string    `json:"status"`
				TaskID            string    `json:"task_id"`
				SourceStateID     string    `json:"source_state_id"`
				PreviousHandoffID string    `json:"previous_handoff_id"`
				CreatedAt         time.Time `json:"created_at"`
				UpdatedAt         time.Time `json:"updated_at"`
			}
			if json.Unmarshal(raw, &legacy) == nil {
				meta.ID = withDefault(legacy.ID, meta.ID)
				meta.Scope = legacy.Scope
				meta.Title = withDefault(legacy.Title, meta.Title)
				meta.Summary = legacy.Summary
				meta.TaskID = legacy.TaskID
				meta.LifecycleStatus = withDefault(legacy.Status, model.LifecycleCurrent)
				meta.CreatedAt = nonZeroTime(legacy.CreatedAt, meta.CreatedAt)
				meta.UpdatedAt = nonZeroTime(legacy.UpdatedAt, meta.UpdatedAt)
				meta.SourceStateID = legacy.SourceStateID
				meta.PreviousHandoffID = legacy.PreviousHandoffID
				body = doc.Body
			}
		}
	}
	projectCompatibility(&meta)
	root := inferScopeRootFromPath(path)
	relPath := filepath.ToSlash(filepath.Base(path))
	if root != "" {
		if rel, relErr := filepath.Rel(root, path); relErr == nil {
			relPath = filepath.ToSlash(rel)
		}
	}
	return Record{Meta: meta, Body: body, RelPath: relPath, Path: path}, nil
}

func renderRecord(meta Metadata, body string) ([]byte, error) {
	return store.RenderMarkdown(meta.HandoffMetaV2, normalizeNewlines(body))
}

func setContentHash(meta *Metadata, body string) error {
	hash, err := contentHash(*meta, body)
	if err != nil {
		return err
	}
	meta.ContentHash = hash
	return nil
}

func contentHash(meta Metadata, body string) (string, error) {
	raw, err := json.Marshal(meta.HandoffMetaV2)
	if err != nil {
		return "", err
	}
	canonical := map[string]any{}
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return "", err
	}
	delete(canonical, "content_hash")
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	canonicalBody := strings.TrimSpace(normalizeNewlines(body))
	sum := sha256.Sum256(append(append(canonicalJSON, '\n'), []byte(canonicalBody)...))
	return hex.EncodeToString(sum[:]), nil
}

func appendEventWrite(root string, writes []ops.Write, events []Event) ([]ops.Write, error) {
	if len(events) == 0 {
		return writes, nil
	}
	relPath := "logs/events.jsonl"
	path := filepath.Join(root, filepath.FromSlash(relPath))
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if len(existing) > 0 && existing[len(existing)-1] != '\n' {
		existing = append(existing, '\n')
	}
	for _, event := range events {
		when := event.Time
		if when.IsZero() {
			when = time.Now().UTC()
		}
		data, err := json.Marshal(model.Event{
			Time:  when.UTC(),
			Event: event.Name,
			ID:    event.ID,
			Actor: event.Actor,
			Data:  event.Data,
		})
		if err != nil {
			return nil, err
		}
		existing = append(existing, data...)
		existing = append(existing, '\n')
	}
	return append(writes, ops.Write{Path: relPath, Data: existing, Mode: 0o600}), nil
}

func validateV2Metadata(meta model.HandoffMetaV2) error {
	if meta.Schema != Schema {
		return fmt.Errorf("unexpected handoff schema %q", meta.Schema)
	}
	if err := validateID(meta.ID); err != nil {
		return err
	}
	if err := validateIdentity("project_id", meta.ProjectID); err != nil {
		return err
	}
	if err := validateIdentity("task_id", meta.TaskID); err != nil {
		return err
	}
	if strings.TrimSpace(meta.Summary) == "" {
		return errors.New("handoff metadata requires summary")
	}
	if meta.Visibility != model.VisibilityLocal && meta.Visibility != model.VisibilityTeam {
		return fmt.Errorf("invalid handoff visibility %q", meta.Visibility)
	}
	if meta.ContentHash == "" {
		return errors.New("handoff metadata requires content_hash")
	}
	for name, value := range map[string]string{"source_tool": meta.SourceTool, "actor": meta.Actor} {
		if strings.TrimSpace(value) != "" {
			if err := validateIdentity(name, value); err != nil {
				return err
			}
		}
	}
	for _, step := range meta.NextSteps {
		if strings.TrimSpace(step.ID) != "" {
			if err := validateIdentity("next step id", step.ID); err != nil {
				return err
			}
		}
	}
	for _, item := range []struct {
		name string
		kind string
		ref  *model.Ref
	}{
		{name: "source_state", kind: "state", ref: meta.SourceState},
		{name: "previous_handoff", kind: "handoff", ref: meta.PreviousHandoff},
		{name: "published_from", kind: "handoff", ref: meta.PublishedFrom},
	} {
		if err := validateStoredRef(item.name, meta.Scope, item.kind, item.ref); err != nil {
			return err
		}
	}
	for _, refs := range [][]model.Ref{meta.Supersedes, meta.SupersededBy} {
		for index := range refs {
			if err := validateStoredRef("handoff reference", meta.Scope, "handoff", &refs[index]); err != nil {
				return err
			}
		}
	}
	snapshot := meta.Worktree
	if err := normalizeWorktree(&snapshot); err != nil {
		return err
	}
	return nil
}

func validateStoredRef(name, scope, kind string, ref *model.Ref) error {
	if ref == nil {
		return nil
	}
	if ref.Scope != scope {
		return fmt.Errorf("%s scope %q does not match record scope %q", name, ref.Scope, scope)
	}
	if ref.Kind != kind {
		return fmt.Errorf("%s kind %q does not match %q", name, ref.Kind, kind)
	}
	if err := validateIdentity(name+" id", ref.ID); err != nil {
		return err
	}
	rel := filepath.ToSlash(strings.TrimSpace(ref.RelPath))
	if rel != "" && (filepath.IsAbs(filepath.FromSlash(rel)) || rel == ".." || strings.HasPrefix(rel, "../")) {
		return fmt.Errorf("%s rel_path must be scope-root-relative", name)
	}
	return nil
}

func scopeRoot(env paths.Env, scope string) (string, string, error) {
	scope = withDefault(scope, "project")
	root, err := env.ScopeRoot(scope)
	return root, scope, err
}

func handoffRelPath(visibility, id string) string {
	return filepath.ToSlash(filepath.Join("handoffs", visibility, id+".md"))
}

func validateID(id string) error {
	return validateIdentity("handoff id", id)
}

func inferScopeRootFromPath(path string) string {
	path = filepath.Clean(path)
	dir := filepath.Dir(path)
	for {
		if filepath.Base(dir) == ".worktrail" || filepath.Base(dir) == ".worktrail-user" {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func isV2StoragePath(path string) bool {
	slashed := filepath.ToSlash(filepath.Clean(path))
	return strings.Contains(slashed, "/handoffs/local/") || strings.Contains(slashed, "/handoffs/team/")
}

func projectCompatibility(meta *Metadata) {
	meta.Status = meta.LifecycleStatus
	if meta.SourceState != nil {
		meta.SourceStateID = meta.SourceState.ID
	}
	if meta.PreviousHandoff != nil {
		meta.PreviousHandoffID = meta.PreviousHandoff.ID
	}
}

func refForRecord(record Record) model.Ref {
	return model.Ref{
		Scope:   record.Meta.Scope,
		Kind:    "handoff",
		ID:      record.Meta.ID,
		RelPath: record.RelPath,
	}
}

func cleanRef(ref *model.Ref) *model.Ref {
	if ref == nil || strings.TrimSpace(ref.ID) == "" {
		return nil
	}
	result := *ref
	result.Scope = strings.TrimSpace(result.Scope)
	result.Kind = strings.TrimSpace(result.Kind)
	result.ID = strings.TrimSpace(result.ID)
	result.RelPath = filepath.ToSlash(strings.TrimSpace(result.RelPath))
	if filepath.IsAbs(filepath.FromSlash(result.RelPath)) || result.RelPath == ".." || strings.HasPrefix(result.RelPath, "../") {
		result.RelPath = ""
	}
	return &result
}

func refWithoutPath(ref *model.Ref) *model.Ref {
	ref = cleanRef(ref)
	if ref == nil {
		return nil
	}
	ref.RelPath = ""
	return ref
}

func validateLocalPrevious(scope, taskID string, ref model.Ref, records []Record) (model.Ref, error) {
	for _, record := range records {
		if record.Meta.ID != ref.ID {
			continue
		}
		if record.Meta.Schema != Schema || record.Meta.Visibility != model.VisibilityLocal || record.Meta.TaskID != taskID {
			return model.Ref{}, errors.New("previous handoff must be local and belong to the same task")
		}
		return model.Ref{Scope: scope, Kind: "handoff", ID: record.Meta.ID, RelPath: record.RelPath}, nil
	}
	return model.Ref{}, fmt.Errorf("previous local handoff %q was not found for task %q", ref.ID, taskID)
}

func currentLocalRecords(records []Record) []Record {
	var current []Record
	for _, record := range records {
		if record.Meta.Schema == Schema && record.Meta.Visibility == model.VisibilityLocal && record.Meta.LifecycleStatus == model.LifecycleCurrent {
			current = append(current, record)
		}
	}
	return current
}

func teamHeads(records []Record) []Record {
	superseded := map[string]bool{}
	for _, record := range records {
		for _, ref := range record.Meta.Supersedes {
			superseded[ref.ID] = true
		}
	}
	var heads []Record
	for _, record := range records {
		if record.Meta.Visibility == model.VisibilityTeam && !superseded[record.Meta.ID] {
			heads = append(heads, record)
		}
	}
	sort.Slice(heads, func(left, right int) bool {
		if !heads[left].Meta.CreatedAt.Equal(heads[right].Meta.CreatedAt) {
			return heads[left].Meta.CreatedAt.After(heads[right].Meta.CreatedAt)
		}
		return heads[left].Meta.ID < heads[right].Meta.ID
	})
	return heads
}

func resolveTeamSupersedes(scope, taskID string, requested []string, heads, records []Record) ([]model.Ref, error) {
	requested = cleanList(requested)
	if len(requested) == 0 {
		switch len(heads) {
		case 0:
			return nil, nil
		case 1:
			return []model.Ref{refForRecord(heads[0])}, nil
		default:
			return nil, fmt.Errorf("task %q has %d team heads; publish a reconciliation handoff with explicit --supersedes ids", taskID, len(heads))
		}
	}
	byID := map[string]Record{}
	for _, record := range records {
		byID[record.Meta.ID] = record
	}
	seen := map[string]bool{}
	refs := make([]model.Ref, 0, len(requested))
	for _, id := range requested {
		if seen[id] {
			continue
		}
		record, ok := byID[id]
		if !ok || record.Meta.Visibility != model.VisibilityTeam || record.Meta.TaskID != taskID {
			return nil, fmt.Errorf("team supersedes id %q was not found for task %q", id, taskID)
		}
		seen[id] = true
		refs = append(refs, model.Ref{Scope: scope, Kind: "handoff", ID: id, RelPath: record.RelPath})
	}
	var missingHeads []string
	for _, head := range heads {
		if !seen[head.Meta.ID] {
			missingHeads = append(missingHeads, head.Meta.ID)
		}
	}
	if len(missingHeads) > 0 {
		sort.Strings(missingHeads)
		return nil, fmt.Errorf("team reconciliation must supersede every current head; missing: %s", strings.Join(missingHeads, ", "))
	}
	return refs, nil
}

func cleanNextSteps(values []model.NextStep) []model.NextStep {
	var result []model.NextStep
	for _, value := range values {
		value.ID = strings.TrimSpace(value.ID)
		value.Action = strings.TrimSpace(value.Action)
		value.Owner = strings.TrimSpace(value.Owner)
		if value.Action != "" {
			result = append(result, value)
		}
	}
	return result
}

func cleanValidation(values []model.ValidationEvidence) []model.ValidationEvidence {
	var result []model.ValidationEvidence
	for _, value := range values {
		value.Command = strings.TrimSpace(value.Command)
		value.Status = strings.TrimSpace(value.Status)
		value.Summary = strings.TrimSpace(value.Summary)
		result = append(result, value)
	}
	return result
}

func cleanList(values []string) []string {
	var result []string
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func withDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func redactionStatus(redacted bool) string {
	if redacted {
		return "redacted"
	}
	return "clean"
}

func lifecycleRank(status string) int {
	switch strings.TrimSpace(status) {
	case model.LifecycleCurrent:
		return 0
	case model.LifecyclePublished:
		return 1
	case model.LifecycleClosed:
		return 2
	case model.LifecycleSuperseded:
		return 3
	default:
		return 4
	}
}

func inferTitle(path, body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	return strings.ReplaceAll(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), "-", " ")
}

func nonZeroTime(value, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func diagnosticCode(err error) string {
	if errors.Is(err, errContentHashMismatch) {
		return "content_hash_mismatch"
	}
	return "invalid_handoff"
}

var errContentHashMismatch = errors.New("handoff content hash mismatch")
var errUnsafeStoragePath = errors.New("unsafe handoff storage path")
