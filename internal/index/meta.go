package index

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

type malformedMarkdownError struct {
	err error
}

func (e malformedMarkdownError) Error() string {
	return "parse markdown: " + e.err.Error()
}

func (e malformedMarkdownError) Unwrap() error {
	return e.err
}

type invalidV2MetadataError struct {
	err error
}

func (e invalidV2MetadataError) Error() string {
	return "normalize v2 metadata: " + e.err.Error()
}

func (e invalidV2MetadataError) Unwrap() error {
	return e.err
}

type contentHashMismatchError struct {
	got  string
	want string
}

func (e contentHashMismatchError) Error() string {
	return fmt.Sprintf("content hash mismatch: got %q, want %q", e.got, e.want)
}

func inferScope(root string) string {
	if b, err := os.ReadFile(filepath.Join(root, "config.json")); err == nil {
		var cfg struct {
			Scope string `json:"scope"`
		}
		if json.Unmarshal(b, &cfg) == nil && cfg.Scope != "" {
			return cfg.Scope
		}
	}
	if filepath.Base(root) == ".worktrail" {
		return "project"
	}
	return "user"
}

func inferType(rel string, meta map[string]any) string {
	if norm, err := model.NormalizeObjectMeta(rel, meta); err == nil {
		return entryTypeFromObject(norm, rel)
	}
	if typ := stringMeta(meta, "type", ""); typ != "" {
		return typ
	}
	if typ := stringMeta(meta, "candidate_type", ""); typ != "" {
		return "candidate"
	}
	switch {
	case strings.HasPrefix(rel, "candidates/"):
		return "candidate"
	case strings.HasPrefix(rel, "state/"):
		return "state"
	case strings.HasPrefix(rel, "architecture/"):
		return "architecture"
	case strings.HasPrefix(rel, "requirements/"):
		return "requirement"
	case strings.HasPrefix(rel, "decisions/"):
		return "decision"
	case strings.HasPrefix(rel, "glossary/"):
		return "glossary"
	case strings.HasPrefix(rel, "handoffs/"):
		return "handoff"
	case strings.HasPrefix(rel, "integrations/"):
		return "integration"
	case strings.HasPrefix(rel, "rules/"):
		return "rule"
	case strings.HasPrefix(rel, "validation/"):
		return "validation"
	case strings.HasPrefix(rel, "prompts/"):
		return "prompt"
	case strings.HasPrefix(rel, "profile/"):
		return "profile"
	case strings.HasPrefix(rel, "workflows/"):
		return "workflow"
	case strings.HasPrefix(rel, "lessons/"):
		return "lesson"
	case strings.HasPrefix(rel, "runtime/"):
		return "state"
	case rel == "project.md":
		return "project"
	case rel == "index.md":
		return "index"
	case rel == "log.md":
		return "log"
	default:
		return "knowledge"
	}
}

func entryTypeFromObject(meta model.ObjectMetaV2, rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if meta.IsHandoff() {
		return "handoff"
	}
	if strings.HasPrefix(rel, "state/active/") || meta.ResumePriority == model.ResumePriorityExplicitSession {
		return "state"
	}
	if strings.HasPrefix(rel, "runtime/") || strings.HasPrefix(rel, "state/checkpoints/") {
		return "state"
	}
	switch {
	case meta.IsKnowledgeDoc():
		if meta.KnowledgeType != "" {
			return model.CanonicalSemanticCandidateType(meta.KnowledgeType)
		}
		return knowledgeTypeFallback(rel)
	case meta.IsDraft(), meta.IsEvidence():
		return "candidate"
	case meta.IsRuntimeRecord():
		return "state"
	default:
		return knowledgeTypeFallback(rel)
	}
}

func entryCandidateTypeFromObject(meta model.ObjectMetaV2) string {
	switch {
	case meta.IsEvidence():
		switch meta.EvidenceType {
		case "transcript":
			return model.CandidateTypeTranscriptNotes
		case "migration_source":
			return model.CandidateTypeMigrationSource
		default:
			return meta.EvidenceType
		}
	case meta.IsDraft():
		if meta.ProposedKnowledgeType != "" {
			return model.CanonicalSemanticCandidateType(meta.ProposedKnowledgeType)
		}
		return meta.DraftKind
	default:
		return ""
	}
}

func activeEntryPath(rel string) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	return strings.HasPrefix(rel, "state/active/") && rel != "state/active/latest.md"
}

func activeEntry(meta model.ObjectMetaV2, rel string, fallback bool) bool {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if strings.HasPrefix(rel, "runtime/") || strings.HasPrefix(rel, "state/checkpoints/") {
		return false
	}
	if !strings.HasPrefix(rel, "state/active/") || rel == "state/active/latest.md" {
		return false
	}
	tool := strings.TrimSpace(meta.SourceTool)
	if tool != "" && tool != "worktrail" {
		return false
	}
	if meta.IsRuntimeRecord() && meta.ResumePriority == model.ResumePriorityHookRuntimeState {
		return false
	}
	return fallback
}

func normalizeEntryStatus(status string, meta model.ObjectMetaV2) string {
	status = strings.TrimSpace(status)
	if status != "" {
		return status
	}
	switch meta.LifecycleStatus {
	case model.LifecyclePendingReview, model.LifecyclePendingDistill:
		return "pending"
	case model.LifecyclePromoted:
		return "promoted"
	case model.LifecycleMerged:
		return "merged"
	case model.LifecycleDiscarded:
		return "discarded"
	case model.LifecycleRetired:
		return "retired"
	case model.LifecycleArchived:
		return "archived"
	default:
		return meta.LifecycleStatus
	}
}

func normalizeEntryLifecycle(meta model.ObjectMetaV2) string {
	if meta.LifecycleStatus == "" {
		return ""
	}
	switch meta.LifecycleStatus {
	case model.LifecyclePendingReview,
		model.LifecyclePendingDistill,
		model.LifecycleActive,
		model.LifecycleCurrent,
		model.LifecyclePromoted,
		model.LifecycleMerged,
		model.LifecyclePublished:
		return knowledge.LifecycleCurrent
	case model.LifecycleArchived, model.LifecycleClosed, model.LifecycleDiscarded:
		return knowledge.LifecycleHistorical
	case model.LifecycleRetired:
		return knowledge.LifecycleRetired
	case model.LifecycleSuperseded:
		return model.LifecycleSuperseded
	default:
		return meta.LifecycleStatus
	}
}

func knowledgeTypeFallback(rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	switch {
	case rel == "project.md":
		return "project"
	case rel == "index.md":
		return "index"
	case strings.HasPrefix(rel, "architecture/"):
		return "architecture"
	case strings.HasPrefix(rel, "requirements/"):
		return "requirement"
	case strings.HasPrefix(rel, "decisions/"):
		return "decision"
	case strings.HasPrefix(rel, "glossary/"):
		return "glossary"
	case strings.HasPrefix(rel, "handoffs/"):
		return "handoff"
	case strings.HasPrefix(rel, "integrations/"):
		return "integration"
	case strings.HasPrefix(rel, "rules/"):
		return "rule"
	case strings.HasPrefix(rel, "validation/"):
		return "validation"
	case strings.HasPrefix(rel, "prompts/"):
		return "prompt"
	case strings.HasPrefix(rel, "profile/"):
		return "profile"
	case strings.HasPrefix(rel, "workflows/"):
		return "workflow"
	case strings.HasPrefix(rel, "lessons/"):
		return "lesson"
	case strings.HasPrefix(rel, "runtime/"):
		return "state"
	case rel == "log.md":
		return "log"
	default:
		return "knowledge"
	}
}

func withFallback(current, fallback string) string {
	if strings.TrimSpace(current) != "" {
		return current
	}
	return strings.TrimSpace(fallback)
}

func InferType(rel string, meta map[string]any) string {
	return inferType(rel, meta)
}

func inferTitle(rel, body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	return strings.ReplaceAll(name, "-", " ")
}

func stringMeta(meta map[string]any, key, fallback string) string {
	v, ok := meta[key]
	if !ok {
		return fallback
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return fallback
		}
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fallback
	}
}

func stringSliceMeta(meta map[string]any, key string) []string {
	v, ok := meta[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringListMeta(meta map[string]any, key string) []string {
	v, ok := meta[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{filepath.ToSlash(strings.TrimSpace(x))}
	case []string:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, filepath.ToSlash(s))
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, filepath.ToSlash(s))
				}
			}
		}
		return out
	default:
		return nil
	}
}

func boolMeta(meta map[string]any, key string) bool {
	v, ok := meta[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	default:
		return false
	}
}

func timeMeta(meta map[string]any, key string, fallback time.Time) time.Time {
	if raw, ok := meta[key].(time.Time); ok {
		return raw
	}
	s := stringMeta(meta, key, "")
	if s == "" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fallback
	}
	return t
}

func buildEntry(root, path, rel, scope string) (Entry, bool, error) {
	b, info, err := readIndexRegularFile(root, path)
	if err != nil {
		return Entry{}, false, err
	}
	body := string(b)
	meta := map[string]any{}
	if strings.HasSuffix(strings.ToLower(path), ".md") {
		doc, parseErr := store.ParseMarkdown(b)
		if parseErr != nil {
			if bytes.HasPrefix(bytes.TrimSpace(b), []byte("---worktrail")) {
				return Entry{}, false, malformedMarkdownError{err: parseErr}
			}
		} else {
			meta = doc.Meta
			body = doc.Body
		}
	}
	normalized, normalizeErr := model.NormalizeObjectMeta(rel, meta)
	if normalizeErr != nil && isV2Schema(stringMeta(meta, "schema", "")) {
		return Entry{}, false, invalidV2MetadataError{err: normalizeErr}
	}
	if normalizeErr == nil {
		if err := verifyV2ContentHash(meta, body); err != nil {
			return Entry{}, false, err
		}
	}
	id := stringMeta(meta, "id", "")
	generatedID := id == ""
	if generatedID {
		id = util.Slug(strings.TrimSuffix(rel, filepath.Ext(rel)))
	}
	entry := Entry{
		Schema:        "worktrail.index.entry.v1",
		ID:            id,
		Scope:         stringMeta(meta, "scope", scope),
		ProjectID:     stringMeta(meta, "project_id", ""),
		TaskID:        stringMeta(meta, "task_id", ""),
		Visibility:    stringMeta(meta, "visibility", ""),
		Type:          inferType(rel, meta),
		Path:          rel,
		Title:         stringMeta(meta, "title", inferTitle(rel, body)),
		Status:        stringMeta(meta, "status", ""),
		Stage:         stringMeta(meta, "stage", ""),
		Lifecycle:     knowledge.NormalizeLifecycle(stringMeta(meta, "lifecycle", ""), stringMeta(meta, "stage", ""), stringMeta(meta, "status", "")),
		Topic:         stringMeta(meta, "topic", ""),
		SourceOfTruth: boolMeta(meta, "source_of_truth"),
		Supersedes:    stringListMeta(meta, "supersedes"),
		SupersededBy:  stringListMeta(meta, "superseded_by"),
		Tags:          stringSliceMeta(meta, "tags"),
		Content:       strings.TrimSpace(body),
		ExpiresAt:     timeMeta(meta, "expires_at", time.Time{}),
		CreatedAt:     timeMeta(meta, "created_at", time.Time{}),
		UpdatedAt:     timeMeta(meta, "updated_at", info.ModTime().UTC()),
		Active:        activeEntryPath(rel),
		generatedID:   generatedID,
	}
	entry.SourceSessions = stringSliceMeta(meta, "source_sessions")
	entry.CandidateType = model.CanonicalSemanticCandidateType(stringMeta(meta, "candidate_type", ""))
	if normalizeErr == nil {
		norm := normalized
		entry.ID = withFallback(entry.ID, norm.ID)
		entry.Scope = withFallback(entry.Scope, norm.Scope)
		entry.ProjectID = withFallback(entry.ProjectID, norm.ProjectID)
		entry.TaskID = withFallback(entry.TaskID, norm.TaskID)
		entry.Visibility = withFallback(entry.Visibility, norm.Visibility)
		entry.Type = entryTypeFromObject(norm, rel)
		entry.Title = withFallback(entry.Title, norm.Title)
		entry.Status = normalizeEntryStatus(entry.Status, norm)
		entry.Stage = withFallback(entry.Stage, norm.Stage)
		if lifecycle := normalizeEntryLifecycle(norm); lifecycle != "" {
			if entry.Lifecycle == "" || entry.Lifecycle == knowledge.LifecycleCurrent || lifecycle == model.LifecycleSuperseded {
				entry.Lifecycle = lifecycle
			}
		}
		entry.Topic = withFallback(entry.Topic, norm.Topic)
		entry.SourceOfTruth = entry.SourceOfTruth || norm.SourceOfTruth
		if len(entry.Supersedes) == 0 {
			entry.Supersedes = append([]string(nil), norm.Supersedes...)
		}
		if len(entry.SupersededBy) == 0 {
			entry.SupersededBy = append([]string(nil), norm.SupersededBy...)
		}
		if len(entry.Tags) == 0 {
			entry.Tags = append([]string(nil), norm.Tags...)
		}
		if entry.UpdatedAt.IsZero() && !norm.UpdatedAt.IsZero() {
			entry.UpdatedAt = norm.UpdatedAt
		}
		if entry.CandidateType == "" {
			entry.CandidateType = entryCandidateTypeFromObject(norm)
		}
		entry.CandidateType = model.CanonicalSemanticCandidateType(entry.CandidateType)
		entry.Active = activeEntry(norm, rel, entry.Active)
		if !norm.ExpiresAt.IsZero() {
			entry.ExpiresAt = norm.ExpiresAt
		}
		if norm.IsRuntimeRecord() {
			if entry.Visibility == "" {
				entry.Visibility = model.VisibilityLocal
			}
			if norm.IsHandoff() {
				entry.Content = norm.Summary
			} else {
				entry.Content = ""
			}
		}
	}
	if entry.Scope == "" {
		entry.Scope = scope
	}
	if strings.HasPrefix(filepath.ToSlash(rel), "runtime/") {
		if entry.Visibility == "" {
			entry.Visibility = model.VisibilityLocal
		}
		entry.Content = ""
	}
	if entry.Type == "config" {
		return Entry{}, false, nil
	}
	return entry, true, nil
}

func isMalformedMarkdown(err error) bool {
	var target malformedMarkdownError
	return errors.As(err, &target)
}

func isIgnoredEntryError(err error) bool {
	if isMalformedMarkdown(err) {
		return true
	}
	var invalidMeta invalidV2MetadataError
	var hashMismatch contentHashMismatchError
	return errors.As(err, &invalidMeta) || errors.As(err, &hashMismatch)
}

func isV2Schema(schema string) bool {
	switch strings.TrimSpace(schema) {
	case model.SchemaHandoffV2,
		model.SchemaKnowledgeV2,
		model.SchemaDraftV2,
		model.SchemaEvidenceV2,
		model.SchemaRuntimeV2:
		return true
	default:
		return false
	}
}

func verifyV2ContentHash(meta map[string]any, body string) error {
	if stringMeta(meta, "schema", "") != model.SchemaHandoffV2 {
		return nil
	}
	got := strings.TrimSpace(stringMeta(meta, "content_hash", ""))
	canonical := make(map[string]any, len(meta))
	for key, value := range meta {
		if key != "content_hash" {
			canonical[key] = value
		}
	}
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return invalidV2MetadataError{err: err}
	}
	canonicalBody := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\r", "\n"))
	sum := sha256.Sum256(append(append(canonicalJSON, '\n'), []byte(canonicalBody)...))
	want := hex.EncodeToString(sum[:])
	if got != want {
		return contentHashMismatchError{got: got, want: want}
	}
	return nil
}

func entryModTime(root string, entry Entry) (time.Time, error) {
	path := filepath.Join(root, filepath.FromSlash(entry.Path))
	if err := validateIndexPath(root, path); err != nil {
		return time.Time{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return time.Time{}, err
	}
	if !info.Mode().IsRegular() {
		return time.Time{}, fmt.Errorf("index source %q is not a regular file", path)
	}
	return info.ModTime().UTC(), nil
}
