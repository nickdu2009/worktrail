package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestReviewPlanRecommendsConservativeActions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeTextFile(t, filepath.Join(project, ".worktrail", "workflows", "merge-target.md"), "# Merge Target\n\nExisting workflow.\n")

	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 5, 15, 0, 0, 0, 0, time.UTC)
	ordinal := 0
	manager := candidate.Manager{
		Env:   env,
		Actor: "test",
		Now: func() time.Time {
			ordinal++
			return now.Add(time.Duration(ordinal) * time.Second)
		},
	}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "note-1",
		CandidateType: model.CandidateTypeTranscriptNotes,
		TargetPath:    "imports/transcripts/note-1.md",
		Title:         "Transcript Notes",
		Body:          "Evidence body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "clean-promote",
		CandidateType:      "rule",
		TargetPath:         "rules/clean-promote.md",
		Title:              "Clean Promote",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Clean Promote\n\nPromote this.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "clean-merge",
		CandidateType:      "workflow",
		TargetPath:         "workflows/merge-target.md",
		Title:              "Clean Merge",
		Operation:          candidate.OperationMerge,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Merge Addition\n\nMerge this.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "empty-body",
		CandidateType: "rule",
		TargetPath:    "rules/empty.md",
		Title:         "Empty Body",
		Operation:     candidate.OperationReplace,
		Body:          " \n\t",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "duplicate-old",
		CandidateType:      "decision",
		TargetPath:         "decisions/duplicate.md",
		Title:              "Duplicate Old",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Duplicate\n\nSame body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "duplicate-new",
		CandidateType:      "decision",
		TargetPath:         "decisions/duplicate.md",
		Title:              "Duplicate New",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Duplicate\n\nSame body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "missing-source",
		CandidateType:      "rule",
		TargetPath:         "rules/missing-source.md",
		Title:              "Missing Source",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-404"},
		Body:               "# Missing Source\n\nNeeds review.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "split-source",
		CandidateType: "lesson",
		TargetPath:    "lessons/kdd-active-knowledge-log.md",
		Title:         "KDD Active Log",
		Summary:       "Do not promote directly",
		Operation:     candidate.OperationReplace,
		Tags:          []string{"kdd", "split-source"},
		Body:          "# KDD Active Log\n\nDo not promote directly.",
	})

	textBefore := runApp(t, &out, &errb, "candidates", "show", "clean-promote", "--format", "json")
	out.Reset()
	if err := Run(context.Background(), []string{"review", "plan", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review plan: %v stderr=%s", err, errb.String())
	}
	planBytes := append([]byte(nil), out.Bytes()...)
	textAfter := runApp(t, &out, &errb, "candidates", "show", "clean-promote", "--format", "json")
	if textBefore != textAfter {
		t.Fatalf("review plan mutated candidate\nbefore=%s\nafter=%s", textBefore, textAfter)
	}

	var plan reviewPlan
	if err := json.Unmarshal(planBytes, &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Schema != reviewPlanSchema || plan.Summary.Total != 7 {
		t.Fatalf("review plan header unexpected: %+v", plan)
	}
	items := mapReviewPlanItems(plan.Items)
	assertReviewAction(t, items, "clean-promote", "promote", "worktrail promote clean-promote")
	assertReviewAction(t, items, "clean-merge", "merge", "worktrail merge clean-merge")
	assertReviewAction(t, items, "empty-body", "discard", "worktrail discard empty-body")
	assertReviewAction(t, items, "duplicate-new", "discard", "worktrail discard duplicate-new")
	assertReviewAction(t, items, "duplicate-old", "needs_human_review", "")
	assertReviewAction(t, items, "missing-source", "needs_human_review", "")
	assertReviewAction(t, items, "split-source", "needs_human_review", "")
	if !containsString(items["split-source"].ReasonCodes, "kdd_split_source_not_promotable") || !containsString(items["split-source"].ReasonCodes, "defer_evidence_cleanup") {
		t.Fatalf("split source reason codes unexpected: %+v", items["split-source"].ReasonCodes)
	}
	if !containsString(items["missing-source"].ReasonCodes, "source_missing") || len(items["missing-source"].SourceStatuses) != 1 || items["missing-source"].SourceStatuses[0].Exists {
		t.Fatalf("missing source status unexpected: %+v", items["missing-source"])
	}
	if !strings.HasPrefix(items["clean-promote"].Snapshot.CandidateBodyHash, "sha256:") || !strings.HasPrefix(items["clean-promote"].Snapshot.CandidateMetadataHash, "sha256:") {
		t.Fatalf("snapshot hashes missing: %+v", items["clean-promote"].Snapshot)
	}
}

func TestReviewShowsEmptySourceCandidateWarning(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "rule-no-source", "--type", "rule", "--target", "rules/no-source.md", "--title", "Rule No Source", "Rule body.")

	text := runApp(t, &out, &errb, "review")
	if !strings.Contains(text, "source_candidate_ids_empty") {
		t.Fatalf("review output missing empty source warning:\n%s", text)
	}
}

func TestReviewApplyPlanAppliesFreshActionsAndSkipsHumanReview(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeTextFile(t, filepath.Join(project, ".worktrail", "workflows", "merge-target.md"), "# Merge Target\n\nExisting workflow.\n")

	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "note-1",
		CandidateType: model.CandidateTypeTranscriptNotes,
		TargetPath:    "imports/transcripts/note-1.md",
		Title:         "Transcript Notes",
		Body:          "Evidence body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "apply-promote",
		CandidateType:      "rule",
		TargetPath:         "rules/apply-promote.md",
		Title:              "Apply Promote",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Apply Promote\n\nPromoted from plan.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "apply-merge",
		CandidateType:      "workflow",
		TargetPath:         "workflows/merge-target.md",
		Title:              "Apply Merge",
		Operation:          candidate.OperationMerge,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Merge Addition\n\nMerged from plan.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "apply-discard",
		CandidateType: "rule",
		TargetPath:    "rules/apply-discard.md",
		Title:         "Apply Discard",
		Operation:     candidate.OperationReplace,
		Body:          "",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "apply-human",
		CandidateType:      "rule",
		TargetPath:         "rules/apply-human.md",
		Title:              "Apply Human",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"missing-note"},
		Body:               "# Apply Human\n\nNeeds human review.",
	})

	plan := runReviewPlanJSON(t, &out, &errb)
	planPath := writeReviewPlanFile(t, plan)
	out.Reset()
	err = Run(context.Background(), []string{"review", "apply-plan", planPath}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("apply-plan without confirm error = %v stdout=%s", err, out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"review", "apply-plan", planPath, "--confirm", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review apply-plan: %v stderr=%s", err, errb.String())
	}
	var report reviewApplyPlanReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != reviewApplyPlanReportSchema || report.Summary.Applied != 3 || report.Summary.Skipped != 1 || report.Summary.Stale != 0 || report.Summary.Failed != 0 {
		t.Fatalf("apply-plan report unexpected: %+v", report)
	}
	assertCandidateStatus(t, manager, "apply-promote", candidate.StatusPromoted)
	assertCandidateStatus(t, manager, "apply-merge", candidate.StatusMerged)
	assertCandidateStatus(t, manager, "apply-discard", candidate.StatusDiscarded)
	assertCandidateStatus(t, manager, "apply-human", candidate.StatusPending)
	merged, err := os.ReadFile(filepath.Join(project, ".worktrail", "workflows", "merge-target.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "Existing workflow.") || !strings.Contains(string(merged), "Merged from plan.") {
		t.Fatalf("merge target content unexpected:\n%s", merged)
	}
}

func TestReviewApplyPlanRejectsStaleSnapshot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "note-1", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-1.md", "--title", "Transcript Notes", "Evidence body.")
	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "stale-promote",
		CandidateType:      "rule",
		TargetPath:         "rules/stale-promote.md",
		Title:              "Stale Promote",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Stale Promote\n\nPromote this.",
	})
	planPath := writeReviewPlanFile(t, runReviewPlanJSON(t, &out, &errb))
	runApp(t, &out, &errb, "discard", "stale-promote")

	out.Reset()
	if err := Run(context.Background(), []string{"review", "apply-plan", planPath, "--confirm", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run stale apply-plan: %v stderr=%s", err, errb.String())
	}
	var report reviewApplyPlanReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Stale != 1 || len(report.Items) != 1 || report.Items[0].Result != "stale" {
		t.Fatalf("stale report unexpected: %+v", report)
	}
	if !containsString(report.Items[0].ReasonCodes, "candidate_status_changed") || !containsString(report.Items[0].ReasonCodes, "candidate_metadata_hash_changed") {
		t.Fatalf("stale reason codes unexpected: %+v", report.Items[0].ReasonCodes)
	}
	assertCandidateStatus(t, manager, "stale-promote", candidate.StatusDiscarded)
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "rules", "stale-promote.md")); !os.IsNotExist(err) {
		t.Fatalf("stale apply-plan should not create target, err=%v", err)
	}
}

func TestReviewPlanSnapshotMismatchesCoverStaleFields(t *testing.T) {
	base := reviewPlanSnapshot{
		CandidateStatus:          candidate.StatusPending,
		CandidateOperation:       candidate.OperationReplace,
		CandidateTargetPath:      "rules/base.md",
		CandidateRedactionStatus: "clean",
		CandidateCreatedAt:       "2026-05-15T00:00:00Z",
		CandidateUpdatedAt:       "2026-05-15T00:00:00Z",
		CandidateBodyHash:        "sha256:body",
		CandidateMetadataHash:    "sha256:meta",
		TargetExists:             false,
		SourceCandidateIDsHash:   "sha256:sources",
	}
	cases := []struct {
		name string
		mut  func(*reviewPlanSnapshot)
		want string
	}{
		{"status", func(s *reviewPlanSnapshot) { s.CandidateStatus = candidate.StatusPromoted }, "candidate_status_changed"},
		{"operation", func(s *reviewPlanSnapshot) { s.CandidateOperation = candidate.OperationMerge }, "candidate_operation_changed"},
		{"target path", func(s *reviewPlanSnapshot) { s.CandidateTargetPath = "rules/other.md" }, "candidate_target_path_changed"},
		{"source ids", func(s *reviewPlanSnapshot) { s.SourceCandidateIDsHash = "sha256:other-sources" }, "source_candidate_ids_hash_changed"},
		{"body hash", func(s *reviewPlanSnapshot) { s.CandidateBodyHash = "sha256:other-body" }, "candidate_body_hash_changed"},
		{"metadata hash", func(s *reviewPlanSnapshot) { s.CandidateMetadataHash = "sha256:other-meta" }, "candidate_metadata_hash_changed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			current := base
			tc.mut(&current)
			if got := reviewPlanSnapshotMismatches(base, current); !containsString(got, tc.want) {
				t.Fatalf("mismatches = %+v, want %s", got, tc.want)
			}
		})
	}
}

func createCandidate(t *testing.T, manager candidate.Manager, req candidate.CreateRequest) candidate.Record {
	t.Helper()
	rec, err := manager.Create(req)
	if err != nil {
		t.Fatalf("Create %s: %v", req.ID, err)
	}
	return rec
}

func runReviewPlanJSON(t *testing.T, out, errb *bytes.Buffer) reviewPlan {
	t.Helper()
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"review", "plan", "--format", "json"}, nil, out, errb); err != nil {
		t.Fatalf("Run review plan: %v stderr=%s", err, errb.String())
	}
	var plan reviewPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	return plan
}

func writeReviewPlanFile(t *testing.T, plan reviewPlan) string {
	t.Helper()
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "review-plan.json")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertCandidateStatus(t *testing.T, manager candidate.Manager, id, want string) {
	t.Helper()
	rec, err := manager.Show("project", id)
	if err != nil {
		t.Fatalf("Show %s: %v", id, err)
	}
	if rec.Meta.Status != want {
		t.Fatalf("%s status = %s, want %s", id, rec.Meta.Status, want)
	}
}

func mapReviewPlanItems(items []reviewPlanItem) map[string]reviewPlanItem {
	out := map[string]reviewPlanItem{}
	for _, item := range items {
		out[item.CandidateID] = item
	}
	return out
}

func assertReviewAction(t *testing.T, items map[string]reviewPlanItem, id, action, command string) {
	t.Helper()
	item, ok := items[id]
	if !ok {
		t.Fatalf("missing review plan item %s", id)
	}
	if item.RecommendedAction != action {
		t.Fatalf("%s recommended_action = %s, want %s; item=%+v", id, item.RecommendedAction, action, item)
	}
	if len(item.Commands) == 0 || item.Commands[0] != "worktrail candidates diff "+id {
		t.Fatalf("%s commands missing diff first: %+v", id, item.Commands)
	}
	if command != "" && !containsString(item.Commands, command) {
		t.Fatalf("%s commands missing %q: %+v", id, command, item.Commands)
	}
	if command == "" {
		for _, got := range item.Commands[1:] {
			if strings.Contains(got, "promote ") || strings.Contains(got, "merge ") || strings.Contains(got, "discard ") {
				t.Fatalf("%s has state-changing command for %s: %+v", id, action, item.Commands)
			}
		}
	}
}
