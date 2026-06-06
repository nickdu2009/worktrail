package model

import (
	"testing"
	"time"
)

func TestNormalizeObjectMetaLegacyCandidateSemantic(t *testing.T) {
	meta := map[string]any{
		"schema":           SchemaCandidate,
		"id":               "cand-1",
		"scope":            "project",
		"candidate_type":   "architecture",
		"target_path":      "architecture/example.md",
		"title":            "Example",
		"operation":        "merge",
		"status":           "pending",
		"redaction_status": "clean",
		"created_at":       time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"updated_at":       time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	obj, err := NormalizeObjectMeta("candidates/project/cand-1.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaDraftV2 || obj.ObjectKind != ObjectKindDraft {
		t.Fatalf("unexpected normalized schema/kind: %+v", obj)
	}
	if obj.DraftKind != DraftKindSemantic {
		t.Fatalf("DraftKind = %q", obj.DraftKind)
	}
	if obj.ProposedKnowledgeType != "architecture" {
		t.Fatalf("ProposedKnowledgeType = %q", obj.ProposedKnowledgeType)
	}
	if obj.LifecycleStatus != LifecyclePendingReview {
		t.Fatalf("LifecycleStatus = %q", obj.LifecycleStatus)
	}
}

func TestNormalizeObjectMetaLegacyCandidateEvidence(t *testing.T) {
	meta := map[string]any{
		"schema":           SchemaCandidate,
		"id":               "cand-2",
		"scope":            "project",
		"candidate_type":   CandidateTypeTranscriptNotes,
		"title":            "Transcript",
		"status":           "pending",
		"redaction_status": "clean",
		"created_at":       time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"updated_at":       time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	obj, err := NormalizeObjectMeta("candidates/project/cand-2.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaEvidenceV2 || obj.ObjectKind != ObjectKindEvidence {
		t.Fatalf("unexpected normalized schema/kind: %+v", obj)
	}
	if obj.EvidenceType != "transcript" {
		t.Fatalf("EvidenceType = %q", obj.EvidenceType)
	}
	if obj.LifecycleStatus != LifecyclePendingDistill {
		t.Fatalf("LifecycleStatus = %q", obj.LifecycleStatus)
	}
}

func TestNormalizeObjectMetaLegacyHandoff(t *testing.T) {
	meta := map[string]any{
		"schema":     SchemaHandoff,
		"id":         "handoff_1",
		"scope":      "project",
		"type":       "handoff",
		"title":      "Handoff",
		"status":     "current",
		"created_at": time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"updated_at": time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	obj, err := NormalizeObjectMeta("handoffs/x.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaKnowledgeV2 || obj.ObjectKind != ObjectKindKnowledgeDoc {
		t.Fatalf("unexpected normalized schema/kind: %+v", obj)
	}
	if obj.KnowledgeType != "handoff" {
		t.Fatalf("KnowledgeType = %q", obj.KnowledgeType)
	}
	if obj.Durability != DurabilityDurable {
		t.Fatalf("Durability = %q", obj.Durability)
	}
}

func TestNormalizeObjectMetaLegacyState(t *testing.T) {
	meta := map[string]any{
		"schema":      SchemaState,
		"id":          "st_1",
		"scope":       "project",
		"type":        "session",
		"title":       "State",
		"status":      "active",
		"source_tool": "worktrail",
		"created_at":  time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"updated_at":  time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	obj, err := NormalizeObjectMeta("state/active/current.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaState {
		t.Fatalf("unexpected normalized schema: %+v", obj)
	}
	if obj.ResumePriority != ResumePriorityExplicitSession || obj.SourceTool != "worktrail" {
		t.Fatalf("unexpected explicit session metadata: %+v", obj)
	}
}

func TestNormalizeObjectMetaLegacyHookCheckpoint(t *testing.T) {
	meta := map[string]any{
		"schema":      SchemaState,
		"id":          "chk_1",
		"scope":       "project",
		"type":        "checkpoint",
		"title":       "Checkpoint",
		"status":      "active",
		"source_tool": "cursor",
		"created_at":  time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		"updated_at":  time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	obj, err := NormalizeObjectMeta("state/checkpoints/20260605-stop.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaRuntimeV2 || obj.ObjectKind != ObjectKindRuntime {
		t.Fatalf("unexpected normalized schema/kind: %+v", obj)
	}
	if obj.RuntimeType != RuntimeTypeCheckpoint || obj.ResumePriority != ResumePriorityRuntimeCheckpoint {
		t.Fatalf("unexpected checkpoint metadata: %+v", obj)
	}
}

func TestNormalizeObjectMetaPathFallback(t *testing.T) {
	obj, err := NormalizeObjectMeta("requirements/example.md", map[string]any{
		"id":    "req",
		"title": "Example requirement",
		"scope": "project",
	})
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaKnowledgeV2 || obj.KnowledgeType != "requirement" {
		t.Fatalf("unexpected fallback object: %+v", obj)
	}
}
