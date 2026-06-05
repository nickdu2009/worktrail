package model

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

const (
	ObjectKindKnowledgeDoc = "knowledge_doc"
	ObjectKindDraft        = "draft"
	ObjectKindEvidence     = "evidence"
	ObjectKindRuntime      = "runtime_record"
)

const (
	DraftKindSemantic    = "semantic"
	DraftKindOperational = "operational"
)

const (
	DurabilityDurable   = "durable"
	DurabilityEphemeral = "ephemeral"
	DurabilityTemporary = "temporary"
)

const (
	ReviewPolicyExplicit = "explicit_review"
)

const (
	RuntimeTypeSessionState = "session_state"
	RuntimeTypeCheckpoint   = "checkpoint"
	RuntimeTypeTakeoverNote = "takeover_note"
	RuntimeTypeHandoffDraft = "handoff_draft"
)

const (
	LifecyclePendingReview  = "pending_review"
	LifecyclePendingDistill = "pending_distill"
	LifecycleActive         = "active"
	LifecycleCurrent        = "current"
	LifecyclePromoted       = "promoted"
	LifecycleMerged         = "merged"
	LifecycleDiscarded      = "discarded"
	LifecycleRetired        = "retired"
	LifecycleArchived       = "archived"
)

type BaseMetaV2 struct {
	Schema     string    `json:"schema"`
	ID         string    `json:"id"`
	Scope      string    `json:"scope"`
	ObjectKind string    `json:"object_kind"`
	Title      string    `json:"title"`
	Tags       []string  `json:"tags,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type KnowledgeMetaV2 struct {
	BaseMetaV2
	KnowledgeType   string    `json:"knowledge_type"`
	Topic           string    `json:"topic,omitempty"`
	Stage           string    `json:"stage,omitempty"`
	Durability      string    `json:"durability"`
	LifecycleStatus string    `json:"lifecycle_status"`
	SourceOfTruth   bool      `json:"source_of_truth,omitempty"`
	ReviewPolicy    string    `json:"review_policy,omitempty"`
	Supersedes      []string  `json:"supersedes,omitempty"`
	SupersededBy    []string  `json:"superseded_by,omitempty"`
	RelatedTopics   []string  `json:"related_topics,omitempty"`
	Owners          []string  `json:"owners,omitempty"`
	LastVerifiedAt  time.Time `json:"last_verified_at,omitempty"`
}

type DraftMetaV2 struct {
	BaseMetaV2
	DraftKind             string   `json:"draft_kind"`
	ProposedKnowledgeType string   `json:"proposed_knowledge_type"`
	Topic                 string   `json:"topic,omitempty"`
	TargetPath            string   `json:"target_path,omitempty"`
	Operation             string   `json:"operation,omitempty"`
	LifecycleStatus       string   `json:"lifecycle_status"`
	SourceEvidenceIDs     []string `json:"source_evidence_ids,omitempty"`
	Confidence            float64  `json:"confidence,omitempty"`
	ReviewPolicy          string   `json:"review_policy,omitempty"`
	RedactionStatus       string   `json:"redaction_status,omitempty"`
}

type EvidenceMetaV2 struct {
	BaseMetaV2
	EvidenceType    string   `json:"evidence_type"`
	Topic           string   `json:"topic,omitempty"`
	SourceURI       string   `json:"source_uri,omitempty"`
	RedactionStatus string   `json:"redaction_status,omitempty"`
	RetentionPolicy string   `json:"retention_policy,omitempty"`
	LifecycleStatus string   `json:"lifecycle_status"`
	DerivedDraftIDs []string `json:"derived_draft_ids,omitempty"`
}

type RuntimeMetaV2 struct {
	BaseMetaV2
	RuntimeType     string    `json:"runtime_type"`
	Topic           string    `json:"topic,omitempty"`
	TaskID          string    `json:"task_id,omitempty"`
	SessionID       string    `json:"session_id,omitempty"`
	Durability      string    `json:"durability"`
	LifecycleStatus string    `json:"lifecycle_status"`
	ResumePriority  string    `json:"resume_priority,omitempty"`
	ExpiresAt       time.Time `json:"expires_at,omitempty"`
	SourceTool      string    `json:"source_tool,omitempty"`
}

type ObjectMetaV2 struct {
	BaseMetaV2
	LegacySchema          string
	KnowledgeType         string
	DraftKind             string
	ProposedKnowledgeType string
	EvidenceType          string
	RuntimeType           string
	Topic                 string
	Stage                 string
	Durability            string
	LifecycleStatus       string
	ReviewPolicy          string
	SourceOfTruth         bool
	Supersedes            []string
	SupersededBy          []string
	TargetPath            string
	Operation             string
	SourceEvidenceIDs     []string
	DerivedDraftIDs       []string
	SourceURI             string
	RedactionStatus       string
	RetentionPolicy       string
	TaskID                string
	SessionID             string
	ResumePriority        string
	ExpiresAt             time.Time
	SourceTool            string
}

func (m ObjectMetaV2) IsKnowledgeDoc() bool  { return m.ObjectKind == ObjectKindKnowledgeDoc }
func (m ObjectMetaV2) IsDraft() bool         { return m.ObjectKind == ObjectKindDraft }
func (m ObjectMetaV2) IsEvidence() bool      { return m.ObjectKind == ObjectKindEvidence }
func (m ObjectMetaV2) IsRuntimeRecord() bool { return m.ObjectKind == ObjectKindRuntime }

func NormalizeObjectMeta(path string, raw map[string]any) (ObjectMetaV2, error) {
	schema := strings.TrimSpace(stringField(raw["schema"]))
	switch schema {
	case SchemaKnowledgeV2:
		var meta KnowledgeMetaV2
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return fromKnowledgeMetaV2(meta), nil
	case SchemaDraftV2:
		var meta DraftMetaV2
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return fromDraftMetaV2(meta), nil
	case SchemaEvidenceV2:
		var meta EvidenceMetaV2
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return fromEvidenceMetaV2(meta), nil
	case SchemaRuntimeV2:
		var meta RuntimeMetaV2
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return fromRuntimeMetaV2(meta), nil
	case SchemaKnowledge:
		var meta Knowledge
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return normalizeLegacyKnowledge(meta), nil
	case SchemaCandidate:
		var meta Candidate
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return normalizeLegacyCandidate(meta), nil
	case SchemaState:
		var meta State
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return normalizeLegacyState(meta, path), nil
	case SchemaHandoff:
		var meta legacyHandoffMeta
		if err := decodeRawMeta(raw, &meta); err != nil {
			return ObjectMetaV2{}, err
		}
		return normalizeLegacyHandoff(meta), nil
	default:
		return normalizePathOnly(path, raw)
	}
}

func NormalizeKnowledgeMeta(meta Knowledge) ObjectMetaV2 {
	return normalizeLegacyKnowledge(meta)
}

func NormalizeCandidateMeta(meta Candidate) ObjectMetaV2 {
	return normalizeLegacyCandidate(meta)
}

func NormalizeStateMeta(meta State, path string) ObjectMetaV2 {
	return normalizeLegacyState(meta, path)
}

type legacyHandoffMeta struct {
	Schema    string    `json:"schema"`
	ID        string    `json:"id"`
	Scope     string    `json:"scope"`
	Type      string    `json:"type"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	TaskID    string    `json:"task_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Tags      []string  `json:"tags,omitempty"`
}

func decodeRawMeta(raw map[string]any, out any) error {
	b, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return err
	}
	return nil
}

func fromKnowledgeMetaV2(meta KnowledgeMetaV2) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2:      meta.BaseMetaV2,
		LegacySchema:    meta.Schema,
		KnowledgeType:   strings.TrimSpace(meta.KnowledgeType),
		Topic:           strings.TrimSpace(meta.Topic),
		Stage:           strings.TrimSpace(meta.Stage),
		Durability:      withDefault(strings.TrimSpace(meta.Durability), DurabilityDurable),
		LifecycleStatus: strings.TrimSpace(meta.LifecycleStatus),
		ReviewPolicy:    strings.TrimSpace(meta.ReviewPolicy),
		SourceOfTruth:   meta.SourceOfTruth,
		Supersedes:      cleanList(meta.Supersedes),
		SupersededBy:    cleanList(meta.SupersededBy),
	}
}

func fromDraftMetaV2(meta DraftMetaV2) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2:            meta.BaseMetaV2,
		LegacySchema:          meta.Schema,
		DraftKind:             withDefault(strings.TrimSpace(meta.DraftKind), DraftKindSemantic),
		ProposedKnowledgeType: strings.TrimSpace(meta.ProposedKnowledgeType),
		Topic:                 strings.TrimSpace(meta.Topic),
		TargetPath:            strings.TrimSpace(meta.TargetPath),
		Operation:             strings.TrimSpace(meta.Operation),
		LifecycleStatus:       strings.TrimSpace(meta.LifecycleStatus),
		SourceEvidenceIDs:     cleanList(meta.SourceEvidenceIDs),
		ReviewPolicy:          strings.TrimSpace(meta.ReviewPolicy),
		RedactionStatus:       strings.TrimSpace(meta.RedactionStatus),
	}
}

func fromEvidenceMetaV2(meta EvidenceMetaV2) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2:      meta.BaseMetaV2,
		LegacySchema:    meta.Schema,
		EvidenceType:    strings.TrimSpace(meta.EvidenceType),
		Topic:           strings.TrimSpace(meta.Topic),
		SourceURI:       strings.TrimSpace(meta.SourceURI),
		RedactionStatus: strings.TrimSpace(meta.RedactionStatus),
		RetentionPolicy: strings.TrimSpace(meta.RetentionPolicy),
		LifecycleStatus: strings.TrimSpace(meta.LifecycleStatus),
		DerivedDraftIDs: cleanList(meta.DerivedDraftIDs),
	}
}

func fromRuntimeMetaV2(meta RuntimeMetaV2) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2:      meta.BaseMetaV2,
		LegacySchema:    meta.Schema,
		RuntimeType:     strings.TrimSpace(meta.RuntimeType),
		Topic:           strings.TrimSpace(meta.Topic),
		TaskID:          strings.TrimSpace(meta.TaskID),
		SessionID:       strings.TrimSpace(meta.SessionID),
		Durability:      withDefault(strings.TrimSpace(meta.Durability), DurabilityEphemeral),
		LifecycleStatus: strings.TrimSpace(meta.LifecycleStatus),
		ResumePriority:  strings.TrimSpace(meta.ResumePriority),
		ExpiresAt:       meta.ExpiresAt,
		SourceTool:      strings.TrimSpace(meta.SourceTool),
	}
}

func normalizeLegacyKnowledge(meta Knowledge) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2: BaseMetaV2{
			Schema:     SchemaKnowledgeV2,
			ID:         strings.TrimSpace(meta.ID),
			Scope:      strings.TrimSpace(meta.Scope),
			ObjectKind: ObjectKindKnowledgeDoc,
			Title:      strings.TrimSpace(meta.Title),
			Tags:       cleanList(meta.Tags),
			CreatedAt:  meta.CreatedAt,
			UpdatedAt:  meta.UpdatedAt,
		},
		LegacySchema:    SchemaKnowledge,
		KnowledgeType:   strings.TrimSpace(meta.Type),
		Topic:           strings.TrimSpace(meta.Topic),
		Stage:           strings.TrimSpace(meta.Stage),
		Durability:      DurabilityDurable,
		LifecycleStatus: withDefault(strings.TrimSpace(meta.Status), LifecycleActive),
		SourceOfTruth:   meta.SourceOfTruth,
		Supersedes:      cleanList(meta.Supersedes),
		SupersededBy:    cleanList(meta.SupersededBy),
	}
}

func normalizeLegacyCandidate(meta Candidate) ObjectMetaV2 {
	base := BaseMetaV2{
		ID:        strings.TrimSpace(meta.ID),
		Scope:     strings.TrimSpace(meta.Scope),
		Title:     strings.TrimSpace(meta.Title),
		Tags:      cleanList(meta.Tags),
		CreatedAt: meta.CreatedAt,
		UpdatedAt: meta.UpdatedAt,
	}
	if IsEvidenceCandidateType(meta.CandidateType) {
		base.Schema = SchemaEvidenceV2
		base.ObjectKind = ObjectKindEvidence
		lifecycle := normalizeLifecycleStatus(meta.Status)
		if lifecycle == LifecyclePendingReview {
			lifecycle = LifecyclePendingDistill
		}
		return ObjectMetaV2{
			BaseMetaV2:      base,
			LegacySchema:    SchemaCandidate,
			EvidenceType:    normalizeEvidenceType(meta.CandidateType),
			Topic:           withDefault(strings.TrimSpace(meta.Topic), inferTopicFromTarget(meta.TargetPath)),
			SourceURI:       strings.Join(cleanList(meta.SourceSessions), ","),
			RedactionStatus: strings.TrimSpace(meta.RedactionStatus),
			RetentionPolicy: "keep_until_distilled",
			LifecycleStatus: lifecycle,
			DerivedDraftIDs: cleanList(meta.SourceCandidateIDs),
		}
	}
	base.Schema = SchemaDraftV2
	base.ObjectKind = ObjectKindDraft
	return ObjectMetaV2{
		BaseMetaV2:            base,
		LegacySchema:          SchemaCandidate,
		DraftKind:             normalizeDraftKind(meta.CandidateType, meta.TargetPath, meta.Tags),
		ProposedKnowledgeType: normalizeKnowledgeTypeFromCandidate(meta.CandidateType, meta.TargetPath),
		Topic:                 withDefault(strings.TrimSpace(meta.Topic), inferTopicFromTarget(meta.TargetPath)),
		TargetPath:            strings.TrimSpace(meta.TargetPath),
		Operation:             strings.TrimSpace(meta.Operation),
		LifecycleStatus:       normalizeLifecycleStatus(meta.Status),
		SourceEvidenceIDs:     cleanList(meta.SourceCandidateIDs),
		ReviewPolicy:          ReviewPolicyExplicit,
		RedactionStatus:       strings.TrimSpace(meta.RedactionStatus),
	}
}

func normalizeLegacyState(meta State, path string) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2: BaseMetaV2{
			Schema:     SchemaRuntimeV2,
			ID:         strings.TrimSpace(meta.ID),
			Scope:      strings.TrimSpace(meta.Scope),
			ObjectKind: ObjectKindRuntime,
			Title:      strings.TrimSpace(meta.Title),
			Tags:       cleanList(meta.Tags),
			CreatedAt:  meta.CreatedAt,
			UpdatedAt:  meta.UpdatedAt,
		},
		LegacySchema:    SchemaState,
		RuntimeType:     runtimeTypeFromLegacyState(meta.Type, path),
		Durability:      DurabilityEphemeral,
		LifecycleStatus: normalizeLifecycleStatus(meta.Status),
		TaskID:          "",
		SessionID:       strings.Join(cleanList(meta.SourceSessions), ","),
		SourceTool:      strings.TrimSpace(meta.SourceTool),
	}
}

func normalizeLegacyHandoff(meta legacyHandoffMeta) ObjectMetaV2 {
	return ObjectMetaV2{
		BaseMetaV2: BaseMetaV2{
			Schema:     SchemaKnowledgeV2,
			ID:         strings.TrimSpace(meta.ID),
			Scope:      strings.TrimSpace(meta.Scope),
			ObjectKind: ObjectKindKnowledgeDoc,
			Title:      strings.TrimSpace(meta.Title),
			Tags:       cleanList(meta.Tags),
			CreatedAt:  meta.CreatedAt,
			UpdatedAt:  meta.UpdatedAt,
		},
		LegacySchema:    SchemaHandoff,
		KnowledgeType:   "handoff",
		Durability:      DurabilityDurable,
		LifecycleStatus: withDefault(strings.TrimSpace(meta.Status), LifecycleCurrent),
		ReviewPolicy:    ReviewPolicyExplicit,
		TaskID:          strings.TrimSpace(meta.TaskID),
	}
}

func normalizePathOnly(path string, raw map[string]any) (ObjectMetaV2, error) {
	rel := filepath.ToSlash(strings.TrimSpace(path))
	if rel == "" {
		return ObjectMetaV2{}, fmt.Errorf("cannot normalize metadata without schema or path")
	}
	id := strings.TrimSpace(stringField(raw["id"]))
	title := strings.TrimSpace(stringField(raw["title"]))
	scope := strings.TrimSpace(stringField(raw["scope"]))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	}
	base := BaseMetaV2{
		ID:        id,
		Scope:     scope,
		Title:     title,
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
	switch {
	case strings.HasPrefix(rel, "handoffs/"):
		base.Schema = SchemaKnowledgeV2
		base.ObjectKind = ObjectKindKnowledgeDoc
		return ObjectMetaV2{BaseMetaV2: base, KnowledgeType: "handoff", Durability: DurabilityDurable, LifecycleStatus: LifecycleCurrent}, nil
	case strings.HasPrefix(rel, "state/") || rel == "current-state.md":
		base.Schema = SchemaRuntimeV2
		base.ObjectKind = ObjectKindRuntime
		return ObjectMetaV2{BaseMetaV2: base, RuntimeType: runtimeTypeFromPath(rel), Durability: DurabilityEphemeral, LifecycleStatus: LifecycleActive}, nil
	case strings.HasPrefix(rel, "candidates/"):
		base.Schema = SchemaDraftV2
		base.ObjectKind = ObjectKindDraft
		return ObjectMetaV2{BaseMetaV2: base, DraftKind: DraftKindSemantic, LifecycleStatus: LifecyclePendingReview}, nil
	case rel == "project.md" || rel == "index.md" || strings.HasPrefix(rel, "architecture/") || strings.HasPrefix(rel, "decisions/") || strings.HasPrefix(rel, "requirements/") || strings.HasPrefix(rel, "workflows/") || strings.HasPrefix(rel, "validation/") || strings.HasPrefix(rel, "integrations/") || strings.HasPrefix(rel, "glossary/") || strings.HasPrefix(rel, "rules/") || strings.HasPrefix(rel, "lessons/") || strings.HasPrefix(rel, "prompts/"):
		base.Schema = SchemaKnowledgeV2
		base.ObjectKind = ObjectKindKnowledgeDoc
		return ObjectMetaV2{BaseMetaV2: base, KnowledgeType: knowledgeTypeFromPath(rel), Durability: DurabilityDurable, LifecycleStatus: LifecycleActive}, nil
	default:
		return ObjectMetaV2{}, fmt.Errorf("unsupported metadata without schema at %q", rel)
	}
}

func normalizeKnowledgeTypeFromCandidate(candidateType, targetPath string) string {
	candidateType = strings.TrimSpace(candidateType)
	if candidateType == "" || candidateType == "knowledge" || candidateType == "manual" {
		return knowledgeTypeFromPath(targetPath)
	}
	switch candidateType {
	case "adr":
		return "decision"
	case "project":
		return "project"
	case "index":
		return "index"
	default:
		return candidateType
	}
}

func normalizeEvidenceType(candidateType string) string {
	switch strings.TrimSpace(candidateType) {
	case CandidateTypeTranscriptNotes:
		return "transcript"
	case CandidateTypeMigrationSource:
		return "migration_source"
	default:
		return strings.TrimSpace(candidateType)
	}
}

func normalizeDraftKind(candidateType, targetPath string, tags []string) string {
	candidateType = strings.TrimSpace(candidateType)
	if CandidateSurface(candidateType) == CandidateSurfaceSemantic {
		return DraftKindSemantic
	}
	if candidateType == "handoff" {
		return DraftKindOperational
	}
	if knowledgeTypeFromPath(targetPath) != "knowledge" {
		return DraftKindSemantic
	}
	return DraftKindOperational
}

func normalizeLifecycleStatus(status string) string {
	switch strings.TrimSpace(status) {
	case StatusPending():
		return LifecyclePendingReview
	case StatusPromoted():
		return LifecyclePromoted
	case StatusMerged():
		return LifecycleMerged
	case StatusDiscarded():
		return LifecycleDiscarded
	case StatusRetired():
		return LifecycleRetired
	case StatusArchived():
		return LifecycleArchived
	case "active", "current":
		return strings.TrimSpace(status)
	default:
		return strings.TrimSpace(status)
	}
}

func runtimeTypeFromPath(path string) string {
	rel := filepath.ToSlash(strings.TrimSpace(path))
	switch {
	case strings.Contains(rel, "/checkpoints/"):
		return RuntimeTypeCheckpoint
	case strings.HasPrefix(rel, "state/checkpoints/"):
		return RuntimeTypeCheckpoint
	case strings.HasPrefix(rel, "state/"):
		return RuntimeTypeSessionState
	case rel == "current-state.md":
		return RuntimeTypeSessionState
	default:
		return RuntimeTypeSessionState
	}
}

func runtimeTypeFromLegacyState(metaType, path string) string {
	switch strings.TrimSpace(metaType) {
	case "checkpoint":
		return RuntimeTypeCheckpoint
	case "takeover_note":
		return RuntimeTypeTakeoverNote
	case "handoff_draft":
		return RuntimeTypeHandoffDraft
	case "session", "session_state":
		return RuntimeTypeSessionState
	default:
		return runtimeTypeFromPath(path)
	}
}

func inferTopicFromTarget(targetPath string) string {
	targetPath = filepath.ToSlash(strings.TrimSpace(targetPath))
	switch {
	case strings.HasPrefix(targetPath, "architecture/"), strings.HasPrefix(targetPath, "requirements/"), strings.HasPrefix(targetPath, "decisions/"), strings.HasPrefix(targetPath, "workflows/"), strings.HasPrefix(targetPath, "validation/"), strings.HasPrefix(targetPath, "integrations/"), strings.HasPrefix(targetPath, "glossary/"), strings.HasPrefix(targetPath, "rules/"), strings.HasPrefix(targetPath, "lessons/"), strings.HasPrefix(targetPath, "prompts/"):
		return strings.TrimSuffix(filepath.Base(targetPath), filepath.Ext(targetPath))
	default:
		return ""
	}
}

func knowledgeTypeFromPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	switch {
	case path == "project.md":
		return "project"
	case path == "index.md":
		return "index"
	case strings.HasPrefix(path, "architecture/"):
		return "architecture"
	case strings.HasPrefix(path, "decisions/"):
		return "decision"
	case strings.HasPrefix(path, "requirements/"):
		return "requirement"
	case strings.HasPrefix(path, "workflows/"):
		return "workflow"
	case strings.HasPrefix(path, "validation/"):
		return "validation"
	case strings.HasPrefix(path, "integrations/"):
		return "integration"
	case strings.HasPrefix(path, "glossary/"):
		return "glossary"
	case strings.HasPrefix(path, "rules/"):
		return "rule"
	case strings.HasPrefix(path, "lessons/"):
		return "lesson"
	case strings.HasPrefix(path, "prompts/"):
		return "prompt"
	case strings.HasPrefix(path, "handoffs/"):
		return "handoff"
	default:
		return "knowledge"
	}
}

func stringField(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}

func cleanList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func withDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

// Candidate status helpers live here temporarily to avoid import cycles while the
// old candidate package is being adapted to V2 object semantics.
func StatusPending() string   { return "pending" }
func StatusPromoted() string  { return "promoted" }
func StatusMerged() string    { return "merged" }
func StatusDiscarded() string { return "discarded" }
func StatusRetired() string   { return "retired" }
func StatusArchived() string  { return "archived" }
