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
	"github.com/nickdu2009/worktrail/internal/store"
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

func TestReviewPlanCompletelyExcludesLegacyHandoffCandidate(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	meta := model.Candidate{
		Schema: model.SchemaCandidate, ID: "legacy-handoff", Scope: "project",
		CandidateType: model.CandidateTypeHandoff, TargetPath: "handoffs/legacy.md",
		Title: "Legacy Handoff", Operation: candidate.OperationReplace, Status: candidate.StatusPending,
	}
	data, err := store.RenderMarkdown(meta, "# Legacy Handoff\n\nMigrate me.")
	if err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "candidates", "project", "legacy-handoff.md"), string(data))

	out.Reset()
	if err := Run(context.Background(), []string{"review", "plan", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	var plan reviewPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if plan.Summary.Total != 0 || plan.Summary.Excluded != 0 || len(plan.Diagnostics) != 0 {
		t.Fatalf("review plan did not exclude legacy handoff candidate: %+v", plan)
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
	if report.IndexRebuild == nil || report.IndexRebuild.Error != "" || report.IndexRebuild.Scope != "project" {
		t.Fatalf("apply-plan index rebuild unexpected: %+v", report.IndexRebuild)
	}
	assertCandidateStatus(t, manager, "apply-promote", candidate.StatusPromoted)
	assertCandidateStatus(t, manager, "apply-merge", candidate.StatusMerged)
	assertCandidateStatus(t, manager, "apply-discard", candidate.StatusArchived)
	assertCandidateStatus(t, manager, "apply-human", candidate.StatusPending)
	text := runApp(t, &out, &errb, "context", "Apply Promote")
	if !strings.Contains(text, "rules/apply-promote.md") {
		t.Fatalf("context did not see promoted doc after apply-plan rebuild:\n%s", text)
	}
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
	assertCandidateStatus(t, manager, "stale-promote", candidate.StatusArchived)
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "rules", "stale-promote.md")); !os.IsNotExist(err) {
		t.Fatalf("stale apply-plan should not create target, err=%v", err)
	}
}

func TestReviewApplyPlanReportsSemanticValidationErrorCodes(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

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
	rec := createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "unsafe-plan",
		CandidateType:      "workflow",
		TargetPath:         "workflows/unsafe-plan.md",
		Title:              "Unsafe Plan",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Unsafe Plan\n\nSafe body.\n",
	})
	replaceCandidateBody(t, rec.Path, rec.Body, "# Unsafe Plan\n\n- user: please paste the transcript\n- assistant: here is the transcript")

	planPath := writeReviewPlanFile(t, runReviewPlanJSON(t, &out, &errb))
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"review", "apply-plan", planPath, "--confirm", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review apply-plan unsafe body: %v stderr=%s", err, errb.String())
	}
	var report reviewApplyPlanReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Applied != 0 || report.Summary.Failed != 1 || len(report.Items) != 1 {
		t.Fatalf("unsafe apply-plan report unexpected: %+v", report)
	}
	item := report.Items[0]
	if item.Result != "failed" || !containsString(item.ReasonCodes, "apply_failed") || !containsString(item.ErrorCodes, "body_raw_transcript_style_conversation") || !strings.Contains(item.Error, "body contains raw transcript-style conversation") {
		t.Fatalf("unsafe apply-plan item unexpected: %+v", item)
	}
	assertCandidateStatus(t, manager, "unsafe-plan", candidate.StatusPending)
}

func TestReviewApplyPlanRejectsExplicitScopeMismatchAndUsesPlanScopeByDefault(t *testing.T) {
	t.Run("project plan rejects user scope", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		project := filepath.Join(t.TempDir(), "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("WORKTRAIL_HOME", home)
		t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

		var out, errb bytes.Buffer
		runApp(t, &out, &errb, "init")
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
			ID:                 "project-promote",
			CandidateType:      "rule",
			TargetPath:         "rules/project-promote.md",
			Title:              "Project Promote",
			Operation:          candidate.OperationReplace,
			SourceCandidateIDs: []string{"note-1"},
			Body:               "# Project Promote\n\nPromote this.",
		})
		planPath := writeReviewPlanFile(t, runReviewPlanJSONForScope(t, &out, &errb, "project"))

		out.Reset()
		err = Run(context.Background(), []string{"review", "apply-plan", planPath, "--confirm", "--scope", "user", "--format", "json"}, nil, &out, &errb)
		if err != nil {
			t.Fatalf("scope mismatch json failure = %v stdout=%s", err, out.String())
		}
		assertCLIErrorEnvelope(t, out.String(), "cli_scope_mismatch")
		if !strings.Contains(out.String(), "scope mismatch") {
			t.Fatalf("scope mismatch envelope missing message:\n%s", out.String())
		}
		assertCandidateStatusScoped(t, manager, "project", "project-promote", candidate.StatusPending)
	})

	t.Run("user plan rejects project scope and succeeds without explicit scope", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		project := filepath.Join(t.TempDir(), "project")
		if err := os.MkdirAll(project, 0o755); err != nil {
			t.Fatal(err)
		}
		t.Setenv("WORKTRAIL_HOME", home)
		t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

		var out, errb bytes.Buffer
		runApp(t, &out, &errb, "init")
		env, err := paths.Discover()
		if err != nil {
			t.Fatal(err)
		}
		manager := candidate.Manager{Env: env, Actor: "test"}
		createCandidate(t, manager, candidate.CreateRequest{
			Scope:         "user",
			ID:            "user-note",
			CandidateType: model.CandidateTypeTranscriptNotes,
			TargetPath:    "imports/transcripts/user-note.md",
			Title:         "User Transcript Notes",
			Body:          "User evidence body.",
		})
		createCandidate(t, manager, candidate.CreateRequest{
			Scope:              "user",
			ID:                 "user-promote",
			CandidateType:      "rule",
			TargetPath:         "rules/user-promote.md",
			Title:              "User Promote",
			Operation:          candidate.OperationReplace,
			SourceCandidateIDs: []string{"user-note"},
			Body:               "# User Promote\n\nPromote this.",
		})
		plan := runReviewPlanJSONForScope(t, &out, &errb, "user")
		userItem := mapReviewPlanItems(plan.Items)["user-promote"]
		for _, want := range []string{
			"worktrail candidates diff user-promote --scope user",
			"worktrail promote user-promote --scope user",
		} {
			if !containsString(userItem.Commands, want) {
				t.Fatalf("user review plan commands missing %q: %+v", want, userItem.Commands)
			}
		}
		planPath := writeReviewPlanFile(t, plan)

		out.Reset()
		err = Run(context.Background(), []string{"review", "apply-plan", planPath, "--confirm", "--scope", "project", "--format", "json"}, nil, &out, &errb)
		if err != nil {
			t.Fatalf("scope mismatch json failure = %v stdout=%s", err, out.String())
		}
		assertCLIErrorEnvelope(t, out.String(), "cli_scope_mismatch")
		if !strings.Contains(out.String(), "scope mismatch") {
			t.Fatalf("scope mismatch envelope missing message:\n%s", out.String())
		}
		assertCandidateStatusScoped(t, manager, "user", "user-promote", candidate.StatusPending)

		out.Reset()
		if err := Run(context.Background(), []string{"review", "apply-plan", planPath, "--confirm", "--format", "json"}, nil, &out, &errb); err != nil {
			t.Fatalf("Run user apply-plan without explicit scope: %v stderr=%s", err, errb.String())
		}
		assertCandidateStatusScoped(t, manager, "user", "user-promote", candidate.StatusPromoted)
	})
}

func TestReviewApplyCandidatesAppliesBatchActionsAndRebuildsIndex(t *testing.T) {
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
		ID:            "batch-promote-1",
		CandidateType: "rule",
		TargetPath:    "rules/batch-promote-1.md",
		Title:         "Batch Promote 1",
		Body:          "# Batch Promote 1\n\nPromoted body one.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "batch-promote-2",
		CandidateType: "rule",
		TargetPath:    "rules/batch-promote-2.md",
		Title:         "Batch Promote 2",
		Body:          "# Batch Promote 2\n\nPromoted body two.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "batch-merge",
		CandidateType: "workflow",
		TargetPath:    "workflows/merge-target.md",
		Title:         "Batch Merge",
		Operation:     candidate.OperationMerge,
		Body:          "Merged batch body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "batch-discard",
		CandidateType: "rule",
		TargetPath:    "rules/batch-discard.md",
		Title:         "Batch Discard",
		Body:          "Discard batch body.",
	})

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"review", "apply-candidates", "--promote", "batch-promote-1", "batch-promote-2", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run apply-candidates promote: %v stderr=%s", err, errb.String())
	}
	var promoteReport reviewApplyCandidatesReport
	if err := json.Unmarshal(out.Bytes(), &promoteReport); err != nil {
		t.Fatalf("promote report JSON invalid: %v\nstdout=%s\nstderr=%s", err, out.String(), errb.String())
	}
	if promoteReport.Schema != reviewApplyCandidatesReportSchema || promoteReport.Action != "promote" || promoteReport.Summary.Total != 2 || promoteReport.Summary.Applied != 2 || promoteReport.Summary.Failed != 0 {
		t.Fatalf("promote report unexpected: %+v", promoteReport)
	}
	if promoteReport.IndexRebuild == nil || promoteReport.IndexRebuild.Error != "" || promoteReport.IndexRebuild.Scope != "project" {
		t.Fatalf("promote index rebuild unexpected: %+v", promoteReport.IndexRebuild)
	}
	assertCandidateStatus(t, manager, "batch-promote-1", candidate.StatusPromoted)
	assertCandidateStatus(t, manager, "batch-promote-2", candidate.StatusPromoted)
	text := runApp(t, &out, &errb, "context", "Promoted body one")
	if !strings.Contains(text, "rules/batch-promote-1.md") {
		t.Fatalf("context did not see promoted doc after apply-candidates rebuild:\n%s", text)
	}

	text = runApp(t, &out, &errb, "review", "apply-candidates", "--merge", "batch-merge")
	if !strings.Contains(text, "index rebuilt\tproject") {
		t.Fatalf("merge text output missing index rebuild:\n%s", text)
	}
	assertCandidateStatus(t, manager, "batch-merge", candidate.StatusMerged)
	merged, err := os.ReadFile(filepath.Join(project, ".worktrail", "workflows", "merge-target.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(merged), "Existing workflow.") || !strings.Contains(string(merged), "Merged batch body.") {
		t.Fatalf("merge target content unexpected:\n%s", merged)
	}

	text = runApp(t, &out, &errb, "review", "apply-candidates", "--discard", "batch-discard")
	if !strings.Contains(text, "index rebuilt\tproject") {
		t.Fatalf("discard text output missing index rebuild:\n%s", text)
	}
	assertCandidateStatus(t, manager, "batch-discard", candidate.StatusArchived)
}

func TestReviewApplyCandidatesRejectsInvalidActionsBeforeMutation(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "invalid-promote",
		CandidateType: "rule",
		TargetPath:    "rules/invalid-promote.md",
		Title:         "Invalid Promote",
		Body:          "Invalid promote body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "invalid-discard",
		CandidateType: "rule",
		TargetPath:    "rules/invalid-discard.md",
		Title:         "Invalid Discard",
		Body:          "Invalid discard body.",
	})

	cases := [][]string{
		{"review", "apply-candidates", "--promote", "invalid-promote", "--discard", "invalid-discard"},
		{"review", "apply-candidates"},
		{"review", "apply-candidates", "--promote"},
	}
	for _, args := range cases {
		out.Reset()
		errb.Reset()
		if err := Run(context.Background(), args, nil, &out, &errb); err == nil {
			t.Fatalf("Run %v succeeded unexpectedly stdout=%s", args, out.String())
		}
	}
	assertCandidateStatus(t, manager, "invalid-promote", candidate.StatusPending)
	assertCandidateStatus(t, manager, "invalid-discard", candidate.StatusPending)
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "rules", "invalid-promote.md")); !os.IsNotExist(err) {
		t.Fatalf("invalid action should not promote target, err=%v", err)
	}
}

func TestReviewApplyCandidatesReportsFailuresAndBlocksEvidence(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	manager := candidate.Manager{Env: env, Actor: "test"}
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "ok-promote",
		CandidateType: "rule",
		TargetPath:    "rules/ok-promote.md",
		Title:         "OK Promote",
		Body:          "OK promote body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "evidence-note",
		CandidateType: model.CandidateTypeTranscriptNotes,
		TargetPath:    "imports/transcripts/evidence-note.md",
		Title:         "Evidence Note",
		Body:          "Evidence note body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "migration-source",
		CandidateType: model.CandidateTypeMigrationSource,
		TargetPath:    "imports/kdd/project/active-knowledge-log.md",
		Title:         "Migration Source",
		Body:          "Migration source body.",
	})
	createCandidate(t, manager, candidate.CreateRequest{
		Scope:         "project",
		ID:            "split-source",
		CandidateType: "lesson",
		TargetPath:    "lessons/kdd-active-knowledge-log.md",
		Title:         "KDD Active Log",
		Summary:       "Do not promote directly",
		Tags:          []string{"split-source"},
		Body:          "Do not promote directly.",
	})

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"review", "apply-candidates", "--promote", "ok-promote", "missing-candidate", "evidence-note", "migration-source", "split-source", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run apply-candidates mixed failures: %v stderr=%s", err, errb.String())
	}
	var report reviewApplyCandidatesReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Total != 5 || report.Summary.Applied != 1 || report.Summary.Failed != 4 {
		t.Fatalf("failure report unexpected: %+v", report)
	}
	assertCandidateStatus(t, manager, "ok-promote", candidate.StatusPromoted)
	for _, id := range []string{"evidence-note", "migration-source", "split-source"} {
		assertCandidateStatus(t, manager, id, candidate.StatusPending)
	}
	items := mapApplyCandidateItems(report.Items)
	for _, id := range []string{"evidence-note", "migration-source", "split-source"} {
		if items[id].Result != "failed" || !strings.Contains(items[id].Error, "blocks transcript_notes") {
			t.Fatalf("%s block item unexpected: %+v", id, items[id])
		}
	}
	if items["missing-candidate"].Result != "failed" || items["missing-candidate"].Error == "" {
		t.Fatalf("missing candidate item unexpected: %+v", items["missing-candidate"])
	}

	for _, action := range []string{"merge", "discard"} {
		out.Reset()
		errb.Reset()
		if err := Run(context.Background(), []string{"review", "apply-candidates", "--" + action, "evidence-note", "migration-source", "split-source", "--format", "json"}, nil, &out, &errb); err != nil {
			t.Fatalf("Run apply-candidates %s blocked candidates: %v stderr=%s", action, err, errb.String())
		}
		var blocked reviewApplyCandidatesReport
		if err := json.Unmarshal(out.Bytes(), &blocked); err != nil {
			t.Fatal(err)
		}
		if blocked.Summary.Applied != 0 || blocked.Summary.Failed != 3 {
			t.Fatalf("%s blocked candidates report unexpected: %+v", action, blocked)
		}
		for _, id := range []string{"evidence-note", "migration-source", "split-source"} {
			assertCandidateStatus(t, manager, id, candidate.StatusPending)
		}
	}
}

func TestReviewApplyCandidatesReportsBlockedSensitiveMaterialErrorCode(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

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
	rec := createCandidate(t, manager, candidate.CreateRequest{
		Scope:              "project",
		ID:                 "unsafe-blocked",
		CandidateType:      "rule",
		TargetPath:         "rules/unsafe-blocked.md",
		Title:              "Unsafe Blocked",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1"},
		Body:               "# Unsafe Blocked\n\nSafe body.\n",
	})
	replaceCandidateBody(t, rec.Path, rec.Body, "# Unsafe Blocked\n\n-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----")

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"review", "apply-candidates", "--promote", "unsafe-blocked", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review apply-candidates blocked body: %v stderr=%s", err, errb.String())
	}
	var report reviewApplyCandidatesReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Summary.Applied != 0 || report.Summary.Failed != 1 || len(report.Items) != 1 {
		t.Fatalf("unsafe apply-candidates report unexpected: %+v", report)
	}
	item := report.Items[0]
	if item.Result != "failed" || !containsString(item.ErrorCodes, "body_blocked_sensitive_material") || !strings.Contains(item.Error, "blocked sensitive material") {
		t.Fatalf("unsafe apply-candidates item unexpected: %+v", item)
	}
	assertCandidateStatus(t, manager, "unsafe-blocked", candidate.StatusPending)
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

func replaceCandidateBody(t *testing.T, path, oldBody, newBody string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), oldBody, newBody, 1)
	if updated == string(data) {
		t.Fatalf("candidate body replacement failed for %s", path)
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runReviewPlanJSON(t *testing.T, out, errb *bytes.Buffer) reviewPlan {
	t.Helper()
	return runReviewPlanJSONForScope(t, out, errb, "project")
}

func runReviewPlanJSONForScope(t *testing.T, out, errb *bytes.Buffer, scope string) reviewPlan {
	t.Helper()
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"review", "plan", "--format", "json", "--scope", scope}, nil, out, errb); err != nil {
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
	assertCandidateStatusScoped(t, manager, "project", id, want)
}

func assertCandidateStatusScoped(t *testing.T, manager candidate.Manager, scope, id, want string) {
	t.Helper()
	rec, err := manager.Show(scope, id)
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

func mapApplyCandidateItems(items []reviewApplyPlanItem) map[string]reviewApplyPlanItem {
	out := map[string]reviewApplyPlanItem{}
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
