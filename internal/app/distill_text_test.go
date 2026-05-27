package app

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/model"
)

func TestDistillPendingSuggestsOtherScope(t *testing.T) {
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

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"distill", "--pending", "--summary"}, nil, &out, &errb)
	if err == nil {
		t.Fatal("distill --pending without matching project evidence returned nil error")
	}
	for _, want := range []string{
		"no pending transcript_notes candidates found in project scope",
		"user scope has 1 matching candidate(s)",
		"worktrail distill --pending --summary --scope user",
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("scope hint missing %q in error %q", want, err.Error())
		}
	}

	text := runApp(t, &out, &errb, "distill", "--pending", "--summary", "--scope", "user")
	if !strings.Contains(text, "evidence_candidates: 1") || !strings.Contains(text, "candidate: user-note") {
		t.Fatalf("user-scope distill summary unexpected:\n%s", text)
	}
}

func TestDistillSplitSourcesSummaryUsesSplitSourcesNextStep(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "migration-source", "--type", model.CandidateTypeMigrationSource, "--target", "imports/kdd/project/active-knowledge-log.md", "--title", "Migration Source", "Migration source body.")

	text := runApp(t, &out, &errb, "distill", "--pending", "--split-sources", "--summary")
	if !strings.Contains(text, "candidate: migration-source") {
		t.Fatalf("split-sources distill summary missing migration source:\n%s", text)
	}
	if !strings.Contains(text, "worktrail distill --pending --split-sources --limit 5 --offset <N>") {
		t.Fatalf("split-sources distill summary missing split-sources next step:\n%s", text)
	}
	if strings.Contains(text, "worktrail distill --pending --limit 5 --offset <N>` or `worktrail distill --pending --all") {
		t.Fatalf("split-sources distill summary leaked transcript-only next step:\n%s", text)
	}
}

func TestDistillApplyTextGroupsReportItems(t *testing.T) {
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

	proposal := filepath.Join(project, "mixed-proposal.json")
	writeTextFile(t, proposal, `{
  "schema": "worktrail.distill.proposal.v1",
  "source_candidate_ids": ["note-1"],
  "candidates": [
    {
      "candidate_type": "rule",
      "title": "Created Rule",
      "target_path": "rules/created.md",
      "operation": "replace",
      "body": "# Created Rule\n\nCreated body."
    },
    {
      "candidate_type": "validation",
      "title": "Invalid Confidence",
      "target_path": "validation/invalid.md",
      "operation": "replace",
      "confidence": 0,
      "body": "# Invalid\n"
    },
    {
      "candidate_type": "decision",
      "title": "Blocked Decision",
      "target_path": "decisions/blocked.md",
      "operation": "replace",
      "body": "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----"
    },
    {
      "candidate_type": "rule",
      "title": "Missing Source",
      "target_path": "rules/missing-source.md",
      "operation": "replace",
      "source_candidate_ids": ["note-404"],
      "body": "# Missing Source\n"
    }
  ]
}`)

	text := runApp(t, &out, &errb, "distill", "apply", proposal)
	for _, want := range []string{
		"Distill apply: partial success",
		"Summary: created=1 skipped=0 blocked=1 errors=2",
		"Created",
		"Blocked",
		"Errors",
		"-> rules/created.md (rule, replace)",
		"decisions/blocked.md (decision, replace)",
		"errors: confidence must be greater than 0 and less than or equal to 1",
		"errors: source candidate not found: note-404",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("distill apply text missing %q:\n%s", want, text)
		}
	}
	for _, forbidden := range []string{project, "BEGIN OPENSSH PRIVATE KEY"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("distill apply text leaked %q:\n%s", forbidden, text)
		}
	}
}

func TestDistillApplyTextNoChangesAndFatalFailure(t *testing.T) {
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

	proposal := filepath.Join(project, "duplicate-proposal.json")
	writeTextFile(t, proposal, `{
  "schema": "worktrail.distill.proposal.v1",
  "source_candidate_ids": ["note-1"],
  "candidates": [
    {
      "candidate_type": "rule",
      "title": "Duplicate Rule",
      "target_path": "rules/duplicate.md",
      "operation": "replace",
      "body": "# Duplicate Rule\n\nDuplicate body."
    }
  ]
}`)
	first := runApp(t, &out, &errb, "distill", "apply", proposal)
	if !strings.Contains(first, "Distill apply: success") {
		t.Fatalf("first apply text unexpected:\n%s", first)
	}
	second := runApp(t, &out, &errb, "distill", "apply", proposal)
	if !strings.Contains(second, "Distill apply: no changes") || !strings.Contains(second, "Skipped") {
		t.Fatalf("duplicate apply text unexpected:\n%s", second)
	}

	missing := filepath.Join(project, "missing-proposal.json")
	out.Reset()
	err := Run(context.Background(), []string{"distill", "apply", missing}, nil, &out, &errb)
	if err == nil {
		t.Fatal("missing proposal returned nil error")
	}
	if !strings.Contains(out.String(), "Distill apply: failed") || !strings.Contains(out.String(), "missing-proposal.json") {
		t.Fatalf("fatal apply text unexpected:\n%s", out.String())
	}
	if strings.Contains(out.String(), project) {
		t.Fatalf("fatal apply text leaked absolute path:\n%s", out.String())
	}
}
