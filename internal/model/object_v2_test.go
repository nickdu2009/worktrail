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

func TestNormalizeObjectMetaLegacyADRAlias(t *testing.T) {
	meta := map[string]any{
		"schema":           SchemaCandidate,
		"id":               "adr-1",
		"scope":            "project",
		"candidate_type":   "adr",
		"target_path":      "decisions/ADR-0001-choice.md",
		"title":            "Choice",
		"operation":        "replace",
		"status":           "pending",
		"redaction_status": "clean",
	}
	obj, err := NormalizeObjectMeta("candidates/project/adr-1.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.ProposedKnowledgeType != "decision" || obj.DraftKind != DraftKindSemantic {
		t.Fatalf("unexpected normalized ADR alias: %+v", obj)
	}
}

func TestNormalizeObjectMetaDecisionStatusUsesExplicitLifecycle(t *testing.T) {
	meta := map[string]any{
		"schema":    SchemaKnowledge,
		"id":        "ADR-0001",
		"scope":     "project",
		"type":      "decision",
		"title":     "Choice",
		"status":    "accepted",
		"lifecycle": "current",
	}
	obj, err := NormalizeObjectMeta("decisions/ADR-0001-choice.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.KnowledgeType != "decision" || obj.LifecycleStatus != LifecycleCurrent {
		t.Fatalf("unexpected normalized decision metadata: %+v", obj)
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
	if obj.Schema != SchemaRuntimeV2 || obj.ObjectKind != ObjectKindRuntime {
		t.Fatalf("unexpected normalized schema/kind: %+v", obj)
	}
	if obj.RuntimeType != RuntimeTypeHandoff || !obj.IsHandoff() {
		t.Fatalf("unexpected handoff runtime classification: %+v", obj)
	}
	if obj.Durability != DurabilityEphemeral || obj.Visibility != VisibilityLocal {
		t.Fatalf("Durability = %q", obj.Durability)
	}
	if obj.LegacySchema != SchemaHandoff {
		t.Fatalf("LegacySchema = %q", obj.LegacySchema)
	}
}

func TestNormalizeObjectMetaHandoffV2(t *testing.T) {
	meta := map[string]any{
		"schema":           SchemaHandoffV2,
		"id":               "ho_1",
		"scope":            "project",
		"object_kind":      ObjectKindRuntime,
		"title":            "Handoff",
		"project_id":       "project_1",
		"task_id":          "task_1",
		"runtime_type":     RuntimeTypeHandoff,
		"summary":          "Continue the task",
		"visibility":       VisibilityTeam,
		"storage_class":    StorageClassTeam,
		"lifecycle_status": LifecycleCurrent,
		"resume_priority":  ResumePriorityManualHandoff,
		"content_hash":     "sha256:abc",
		"format_version":   2,
		"supersedes": []map[string]any{
			{"scope": "project", "kind": "handoff", "id": "ho_0", "rel_path": "handoffs/team/ho_0.md"},
		},
		"worktree": map[string]any{
			"branch":            "feat/handoff-v2",
			"head_commit":       "abc123",
			"dirty":             false,
			"code_availability": CodeAvailabilityAvailable,
		},
	}
	obj, err := NormalizeObjectMeta("handoffs/team/ho_1.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.Schema != SchemaHandoffV2 || !obj.IsHandoff() {
		t.Fatalf("unexpected normalized schema/kind: %+v", obj)
	}
	if obj.ProjectID != "project_1" || obj.TaskID != "task_1" {
		t.Fatalf("unexpected identity: %+v", obj)
	}
	if obj.Visibility != VisibilityTeam || obj.StorageClass != StorageClassTeam || obj.Durability != DurabilityDurable {
		t.Fatalf("unexpected storage classification: %+v", obj)
	}
	if obj.ResumePriority != ResumePriorityManualHandoff || obj.Summary != "Continue the task" {
		t.Fatalf("unexpected recovery metadata: %+v", obj)
	}
	if len(obj.Supersedes) != 1 || obj.Supersedes[0] != "ho_0" {
		t.Fatalf("Supersedes = %+v", obj.Supersedes)
	}
	if obj.CodeAvailability != CodeAvailabilityAvailable {
		t.Fatalf("CodeAvailability = %q", obj.CodeAvailability)
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

func TestNormalizeObjectMetaExplicitCheckpoint(t *testing.T) {
	meta := map[string]any{
		"schema":      SchemaState,
		"id":          "cp_1",
		"scope":       "project",
		"type":        "checkpoint",
		"title":       "Checkpoint",
		"status":      "active",
		"source_tool": "worktrail",
	}
	obj, err := NormalizeObjectMeta("state/checkpoints/cp_1.md", meta)
	if err != nil {
		t.Fatalf("NormalizeObjectMeta() error = %v", err)
	}
	if obj.RuntimeType != RuntimeTypeCheckpoint || obj.ResumePriority != ResumePriorityExplicitCheckpoint {
		t.Fatalf("unexpected explicit checkpoint metadata: %+v", obj)
	}
}

func TestNormalizeObjectMetaHandoffPathFallbacks(t *testing.T) {
	tests := []struct {
		path           string
		wantSchema     string
		wantLegacy     string
		wantVisibility string
		wantDurability string
	}{
		{path: "handoffs/local/ho_local.md", wantSchema: SchemaHandoffV2, wantVisibility: VisibilityLocal, wantDurability: DurabilityEphemeral},
		{path: "handoffs/team/ho_team.md", wantSchema: SchemaHandoffV2, wantVisibility: VisibilityTeam, wantDurability: DurabilityDurable},
		{path: "handoffs/legacy.md", wantSchema: SchemaRuntimeV2, wantLegacy: SchemaHandoff, wantVisibility: VisibilityLocal, wantDurability: DurabilityEphemeral},
	}
	for _, tc := range tests {
		obj, err := NormalizeObjectMeta(tc.path, map[string]any{"id": "ho", "scope": "project"})
		if err != nil {
			t.Fatalf("%s: NormalizeObjectMeta() error = %v", tc.path, err)
		}
		if !obj.IsHandoff() || obj.Schema != tc.wantSchema || obj.LegacySchema != tc.wantLegacy {
			t.Fatalf("%s: unexpected handoff classification: %+v", tc.path, obj)
		}
		if obj.Visibility != tc.wantVisibility || obj.Durability != tc.wantDurability {
			t.Fatalf("%s: unexpected storage classification: %+v", tc.path, obj)
		}
	}
	if _, err := NormalizeObjectMeta("handoffs/archive/legacy.md", map[string]any{}); err == nil {
		t.Fatal("nested legacy handoff path should not be treated as live handoff")
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
