package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestEvidencePlanReportsReferenceLifecycle(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "note-keep", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-keep.md", "--title", "Keep Evidence", "Keep evidence body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "note-archive", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-archive.md", "--title", "Archive Evidence", "Archive evidence body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "empty-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/empty-note.md", "--title", "Empty Evidence")
	runApp(t, &out, &errb, "candidates", "create", "--id", "discarded-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/discarded-note.md", "--title", "Discarded Evidence", "Discarded evidence body.")
	runApp(t, &out, &errb, "discard", "discarded-note")
	runApp(t, &out, &errb, "candidates", "create", "--id", "split-source", "--type", "lesson", "--target", "lessons/kdd-active-knowledge-log.md", "--title", "KDD Active Log", "--summary", "Do not promote directly", "--tags", "kdd,split-source", "Do not promote directly.")

	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "pending-rule",
		CandidateType:      "rule",
		TargetPath:         "rules/pending-rule.md",
		Title:              "Pending Rule",
		SourceCandidateIDs: []string{"note-keep"},
		Operation:          candidate.OperationReplace,
		Body:               "# Pending Rule\n\nReferences active evidence.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "applied-rule",
		CandidateType:      "rule",
		TargetPath:         "rules/applied-rule.md",
		Title:              "Applied Rule",
		SourceCandidateIDs: []string{"note-archive"},
		Operation:          candidate.OperationReplace,
		Body:               "# Applied Rule\n\nReferences archived-ready evidence.",
	})
	runApp(t, &out, &errb, "promote", "applied-rule")
	writeTextFile(t, filepath.Join(project, ".worktrail", "candidates", "project", "archived-note.md"), archivedEvidenceCandidateBody())

	plan := runEvidencePlanJSON(t, &out, &errb, "active")
	if plan.Schema != evidencePlanSchema {
		t.Fatalf("schema = %s", plan.Schema)
	}
	items := mapEvidencePlanItems(plan.Items)
	assertEvidenceAction(t, items, "note-keep", "keep")
	assertEvidenceAction(t, items, "note-archive", "archive")
	assertEvidenceAction(t, items, "empty-note", "discard")
	assertEvidenceAction(t, items, "split-source", "needs_human_review")
	if _, ok := items["archived-note"]; ok {
		t.Fatalf("active plan included archived evidence: %+v", items["archived-note"])
	}
	if _, ok := items["discarded-note"]; ok {
		t.Fatalf("active plan included discarded evidence: %+v", items["discarded-note"])
	}
	if items["note-keep"].PendingSemanticReferences != 1 || !items["note-keep"].NeededForActiveReview {
		t.Fatalf("note-keep references unexpected: %+v", items["note-keep"])
	}
	if items["note-archive"].AppliedSemanticReferences != 1 {
		t.Fatalf("note-archive references unexpected: %+v", items["note-archive"])
	}
	if !containsString(items["split-source"].ReasonCodes, "defer_evidence_cleanup") {
		t.Fatalf("split source reason codes unexpected: %+v", items["split-source"].ReasonCodes)
	}

	archived := runEvidencePlanJSON(t, &out, &errb, "archived")
	archivedItems := mapEvidencePlanItems(archived.Items)
	if len(archivedItems) != 1 {
		t.Fatalf("archived items = %+v", archivedItems)
	}
	assertEvidenceAction(t, archivedItems, "archived-note", "keep")

	all := runEvidencePlanJSON(t, &out, &errb, "all")
	allItems := mapEvidencePlanItems(all.Items)
	if _, ok := allItems["archived-note"]; !ok {
		t.Fatalf("all plan missing archived evidence: %+v", allItems)
	}
	if _, ok := allItems["discarded-note"]; ok {
		t.Fatalf("all plan included discarded evidence: %+v", allItems)
	}

	text := runApp(t, &out, &errb, "evidence", "plan")
	if !strings.Contains(text, "Worktrail Evidence Plan") || !strings.Contains(text, "Archive") || !strings.Contains(text, "Discard") {
		t.Fatalf("evidence plan text unexpected:\n%s", text)
	}
}

func TestEvidencePlanTextSuggestsOtherScope(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--scope", "user", "--id", "user-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/user-note.md", "--title", "User Evidence", "User evidence body.")

	text := runApp(t, &out, &errb, "evidence", "plan")
	for _, want := range []string{
		"Summary: total=0",
		"No evidence lifecycle candidates matched in project scope",
		"user scope has 1 matching candidate(s)",
		"worktrail evidence plan --format json --scope user",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("evidence plan text missing %q:\n%s", want, text)
		}
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"evidence", "plan", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run evidence plan JSON: %v stderr=%s", err, errb.String())
	}
	var plan evidencePlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Total != 0 || strings.Contains(out.String(), "scope has") {
		t.Fatalf("JSON plan leaked text hint or non-empty project items:\n%s", out.String())
	}
}

func TestEvidenceArchiveAndDiscardRequireConfirmationAndPlanRecommendation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "note-keep", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-keep.md", "--title", "Keep Evidence", "Keep evidence body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "note-archive", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-archive.md", "--title", "Archive Evidence", "Archive evidence body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "empty-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/empty-note.md", "--title", "Empty Evidence")

	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "pending-rule",
		CandidateType:      "rule",
		TargetPath:         "rules/pending-rule.md",
		Title:              "Pending Rule",
		SourceCandidateIDs: []string{"note-keep"},
		Operation:          candidate.OperationReplace,
		Body:               "# Pending Rule\n\nReferences active evidence.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "applied-rule",
		CandidateType:      "rule",
		TargetPath:         "rules/applied-rule.md",
		Title:              "Applied Rule",
		SourceCandidateIDs: []string{"note-archive"},
		Operation:          candidate.OperationReplace,
		Body:               "# Applied Rule\n\nReferences archive-ready evidence.",
	})
	runApp(t, &out, &errb, "promote", "applied-rule")

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"evidence", "archive", "note-archive", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("archive without confirm json failure = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	assertCLIErrorEnvelope(t, out.String(), "cli_confirmation_required")

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"evidence", "archive", "note-keep", "--confirm", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("archive keep evidence json failure = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	assertCLIErrorEnvelope(t, out.String(), "cli_command_failed")
	if !strings.Contains(out.String(), "recommended for keep, not archive") {
		t.Fatalf("archive keep evidence envelope = %s", out.String())
	}

	text := runApp(t, &out, &errb, "evidence", "archive", "note-archive", "--confirm", "--reason", "applied knowledge keeps traceability")
	if !strings.Contains(text, "note-archive\tarchived") {
		t.Fatalf("archive output unexpected:\n%s", text)
	}
	archived := runEvidencePlanJSON(t, &out, &errb, "archived")
	assertEvidenceAction(t, mapEvidencePlanItems(archived.Items), "note-archive", "keep")

	text = runApp(t, &out, &errb, "evidence", "discard", "empty-note", "--confirm", "--reason", "empty evidence")
	if !strings.Contains(text, "empty-note\tdiscarded") {
		t.Fatalf("discard output unexpected:\n%s", text)
	}
	all := runEvidencePlanJSON(t, &out, &errb, "all")
	if _, ok := mapEvidencePlanItems(all.Items)["empty-note"]; ok {
		t.Fatalf("discarded evidence appeared in all plan: %+v", all.Items)
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "candidates", "project", "empty-note.md")); err != nil {
		t.Fatalf("discard removed candidate file: %v", err)
	}
}

func TestEvidenceActionsRequireNonEmptySafeReason(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "note-archive", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-archive.md", "--title", "Archive Evidence", "Archive evidence body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "empty-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/empty-note.md", "--title", "Empty Evidence")

	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "applied-rule",
		CandidateType:      "rule",
		TargetPath:         "rules/applied-rule.md",
		Title:              "Applied Rule",
		SourceCandidateIDs: []string{"note-archive"},
		Operation:          candidate.OperationReplace,
		Body:               "# Applied Rule\n\nReferences archive-ready evidence.",
	})
	runApp(t, &out, &errb, "promote", "applied-rule")

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"evidence", "archive", "note-archive", "--confirm"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "evidence lifecycle reason is required") {
		t.Fatalf("archive without reason error = %v stdout=%s", err, out.String())
	}

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"evidence", "archive", "note-archive", "--confirm", "--reason", "/Users/tester/private.txt cleanup", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("archive unsafe reason json failure = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	assertCLIErrorEnvelope(t, out.String(), "reason_local_absolute_path")

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"evidence", "discard", "empty-note", "--confirm", "--reason", "Reach me at nick@example.com", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("discard unsafe reason json failure = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	assertCLIErrorEnvelope(t, out.String(), "reason_redactable_secret_or_pii")
}

func runEvidencePlanJSON(t *testing.T, out, errb *bytes.Buffer, status string) evidencePlan {
	t.Helper()
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"evidence", "plan", "--status", status, "--format", "json"}, nil, out, errb); err != nil {
		t.Fatalf("Run evidence plan: %v stderr=%s", err, errb.String())
	}
	var plan evidencePlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func mapEvidencePlanItems(items []evidencePlanItem) map[string]evidencePlanItem {
	out := map[string]evidencePlanItem{}
	for _, item := range items {
		out[item.CandidateID] = item
	}
	return out
}

func assertEvidenceAction(t *testing.T, items map[string]evidencePlanItem, id, action string) {
	t.Helper()
	item, ok := items[id]
	if !ok {
		t.Fatalf("missing evidence item %s; items=%+v", id, items)
	}
	if item.RecommendedAction != action {
		t.Fatalf("%s action = %s, want %s; item=%+v", id, item.RecommendedAction, action, item)
	}
}

func archivedEvidenceCandidateBody() string {
	return `---worktrail
{
  "schema": "worktrail.candidate.v1",
  "id": "archived-note",
  "scope": "project",
  "candidate_type": "transcript_notes",
  "target_path": "imports/transcripts/archived-note.md",
  "title": "Archived Evidence",
  "summary": "Already archived evidence.",
  "operation": "replace",
  "status": "archived",
  "redaction_status": "clean",
  "created_at": "2026-05-15T00:00:00Z",
  "updated_at": "2026-05-15T00:00:00Z"
}
---

# Archived Evidence

Already archived evidence body.
`
}
