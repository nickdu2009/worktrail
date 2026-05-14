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
	wtdistill "github.com/nickdu2009/worktrail/internal/distill"
	"github.com/nickdu2009/worktrail/internal/model"
)

func TestInitCreatesUserAndProjectRoots(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	for _, path := range []string{
		filepath.Join(home, "config.json"),
		filepath.Join(home, "logs", "events.jsonl"),
		filepath.Join(project, ".worktrail", "config.json"),
		filepath.Join(project, ".worktrail", "logs", "events.jsonl"),
		filepath.Join(project, ".gitignore"),
		filepath.Join(project, ".codex", "hooks.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(project, ".gitignore")); err != nil || !bytes.Contains(data, []byte(".codex/")) {
		t.Fatalf("expected project .gitignore to contain worktrail entries: %v %s", err, data)
	}
	for _, path := range []string{
		filepath.Join(project, "AGENTS.md"),
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(project, ".agents", "skills", "worktrail-state", "SKILL.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("did not expect init to create %s, err=%v", path, err)
		}
	}
}

func TestExtractionCandidateIDIncludesSourceAndOrdinal(t *testing.T) {
	got := extractionCandidateID("codex", "/tmp/session.jsonl", 1, "user")
	if got != "codex-02-user" {
		t.Fatalf("id = %s", got)
	}
	got = extractionCandidateID("", "/tmp/session.jsonl", 0, "")
	if got != "manual-01-session" {
		t.Fatalf("fallback id = %s", got)
	}
}

func TestImportCodexDiscoversAndExtractsProjectSessions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	session := filepath.Join(home, ".codex", "sessions", "2026", "05", "14", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"timestamp":"2026-05-14T00:00:00Z","type":"session_meta","payload":{"id":"session-1","cwd":"` + filepath.ToSlash(project) + `"}}` + "\n" +
		`{"timestamp":"2026-05-14T00:00:01Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Capture this workflow."}]}}` + "\n" +
		`{"timestamp":"2026-05-14T00:00:02Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Keep review candidates pending."}]}}` + "\n"
	if err := os.WriteFile(session, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import dry-run: %v stderr=%s", err, errb.String())
	}
	var dry importReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Matched != 1 || !dry.DryRun {
		t.Fatalf("unexpected dry-run report: %+v", dry)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"import", "codex"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import dry-run text: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "next steps:") || !strings.Contains(out.String(), "git guidance:") {
		t.Fatalf("import text output missing guidance:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"import", "codex", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import --all: %v stderr=%s", err, errb.String())
	}
	var report importReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Matched != 1 || report.Synced != 1 || report.Extracted != 1 || report.DryRun {
		t.Fatalf("unexpected import report: %+v", report)
	}
	candidates, err := filepath.Glob(filepath.Join(project, ".worktrail", "candidates", "project", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate files = %d, want 1", len(candidates))
	}
	candidateBody, err := os.ReadFile(candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(candidateBody, []byte(`"candidate_type": "transcript_notes"`)) {
		t.Fatalf("candidate is not transcript notes:\n%s", candidateBody)
	}
	if !bytes.Contains(candidateBody, []byte(`"target_path": "imports/transcripts/codex-01-session.md"`)) {
		t.Fatalf("candidate target is not transcript import path:\n%s", candidateBody)
	}
}

func TestImportKDDCreatesSemanticCandidates(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, "docs", "knowledge-driven-development")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(root, "README.md"), "# KDD Root\n\nSkipped root overview.")
	writeTextFile(t, filepath.Join(root, "project", "README.md"), "# Project KB\n\nShared overview.")
	writeTextFile(t, filepath.Join(root, "project", "active-knowledge-log.md"), "# Active Log\n\nUnverified finding.")
	writeTextFile(t, filepath.Join(root, "project", "architecture", "README.md"), "# Architecture\n\nDirectory guidance.")
	writeTextFile(t, filepath.Join(root, "project", "architecture", "system.md"), "# System Architecture\n\nArchitecture body.")
	writeTextFile(t, filepath.Join(root, "project", "architecture", "delivery-case-workbench-p0-implementation-alignment.md"), "# Alignment\n\nLong prefix body.")
	writeTextFile(t, filepath.Join(root, "project", "architecture", "delivery-case-workbench-p0-implementation-plan.md"), "# Plan\n\nLong prefix plan.")
	writeTextFile(t, filepath.Join(root, "project", "decisions", "choice.md"), "# Choice\n\nDecision body.")
	writeTextFile(t, filepath.Join(root, "project", "runbooks", "release.md"), "# Release\n\nRunbook body.")
	writeTextFile(t, filepath.Join(root, "project", "integrations", "api.md"), "# API\n\nIntegration body.")
	writeTextFile(t, filepath.Join(root, "project", "validation", "smoke.md"), "# Smoke\n\nValidation body.")
	writeTextFile(t, filepath.Join(root, "project", "glossary", "terms.md"), "# Terms\n\nGlossary body.")
	writeTextFile(t, filepath.Join(root, "project", "notes", "misc.md"), "# Misc\n\nMisc body.")
	writeTextFile(t, filepath.Join(root, "local", "active-knowledge-log.md"), "# Local\n\n/Users/example/private fixture.")
	writeTextFile(t, filepath.Join(root, "project", "validation", "blocked.md"), "# Blocked\n\n-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----\n")

	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"import", "kdd", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import kdd dry-run: %v stderr=%s", err, errb.String())
	}
	var dry kddImportReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Matched != 11 || dry.Skipped != 2 || dry.Blocked != 1 || dry.LocalSkipped != 1 {
		t.Fatalf("unexpected dry-run report: %+v", dry)
	}
	if !hasKDDSkippedPath(dry.Items, "project/architecture/README.md") {
		t.Fatalf("dry-run did not report skipped category README: %+v", dry.Items)
	}
	if hasDuplicateKDDCandidateIDs(dry.Items) {
		t.Fatalf("dry-run report has duplicate candidate ids: %+v", dry.Items)
	}

	missingRoot := filepath.Join(project, "missing-kdd")
	out.Reset()
	if err := Run(context.Background(), []string{"import", "kdd", "--root", missingRoot, "--format", "json"}, nil, &out, &errb); err == nil || !strings.Contains(err.Error(), "kdd root does not exist") {
		t.Fatalf("missing root error = %v", err)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"import", "kdd", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import kdd --all: %v stderr=%s", err, errb.String())
	}
	var report kddImportReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.DryRun || report.Created != 11 || report.Skipped != 2 || report.Blocked != 1 || report.LocalSkipped != 1 {
		t.Fatalf("unexpected import report: %+v", report)
	}

	archCandidate := filepath.Join(project, ".worktrail", "candidates", "project", "kdd-project-architecture-system.md")
	body, err := os.ReadFile(archCandidate)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"candidate_type": "architecture"`, `"target_path": "architecture/system.md"`, "Imported from KDD relative path: `project/architecture/system.md`"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("architecture candidate missing %q:\n%s", want, body)
		}
	}
	if bytes.Contains(body, []byte(".worktrail/architecture")) || bytes.Contains(body, []byte(root)) {
		t.Fatalf("candidate leaked disallowed path:\n%s", body)
	}
	activeLog, err := os.ReadFile(filepath.Join(project, ".worktrail", "candidates", "project", "kdd-project-active-knowledge-log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(activeLog, []byte(`"candidate_type": "lesson"`)) || !bytes.Contains(activeLog, []byte("Do not promote directly")) {
		t.Fatalf("active log candidate unexpected:\n%s", activeLog)
	}
	if !bytes.Contains(activeLog, []byte(`"kdd"`)) || !bytes.Contains(activeLog, []byte(`"split-source"`)) {
		t.Fatalf("active log candidate missing split-source tags:\n%s", activeLog)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"import", "kdd", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run duplicate import kdd --all: %v stderr=%s", err, errb.String())
	}
	var duplicate kddImportReport
	if err := json.Unmarshal(out.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Created != 0 || duplicate.Skipped < 11 {
		t.Fatalf("duplicate import report unexpected: %+v", duplicate)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"candidates", "list", "--semantic", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run semantic list: %v stderr=%s", err, errb.String())
	}
	var records []candidate.Record
	if err := json.Unmarshal(out.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if !hasCandidateType(records, "architecture") || !hasCandidateType(records, "integration") || !hasCandidateType(records, "validation") || !hasCandidateType(records, "glossary") || !hasCandidateType(records, "project") {
		t.Fatalf("semantic candidates missing new KDD types: %#v", records)
	}

	runApp(t, &out, &errb, "promote", "kdd-project-architecture-system")
	text := runApp(t, &out, &errb, "context", "architecture")
	if !strings.Contains(text, "Architecture") || !strings.Contains(text, "Architecture body.") {
		t.Fatalf("context missing promoted architecture:\n%s", text)
	}
}

func TestCandidatesListFiltersAndDistillTranscriptNotes(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "note-1", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-1.md", "--title", "Transcript Notes", "Evidence body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create transcript: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "note-2", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/note-2.md", "--title", "More Transcript Notes", "Second evidence body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create second transcript: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "rule-1", "--type", "rule", "--target", "rules/rule-1.md", "--title", "Rule", "Rule body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create rule: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "split-source", "--type", "lesson", "--target", "lessons/kdd-active-knowledge-log.md", "--title", "KDD Active Log", "--summary", "Do not promote directly", "--tags", "split-source", "Split source body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create split source: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "ordinary-lesson", "--type", "lesson", "--target", "lessons/ordinary.md", "--title", "Ordinary Lesson", "Ordinary lesson body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create ordinary lesson: %v stderr=%s", err, errb.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"candidates", "list", "--type", model.CandidateTypeTranscriptNotes, "--status", candidate.StatusPending, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates list: %v stderr=%s", err, errb.String())
	}
	var records []candidate.Record
	if err := json.Unmarshal(out.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Meta.ID != "note-1" || records[1].Meta.ID != "note-2" {
		t.Fatalf("filtered records = %#v", records)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"candidates", "list", "--semantic", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates list semantic: %v stderr=%s", err, errb.String())
	}
	records = nil
	if err := json.Unmarshal(out.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || !hasCandidateID(records, "rule-1") || !hasCandidateID(records, "split-source") || !hasCandidateID(records, "ordinary-lesson") {
		t.Fatalf("semantic records = %#v", records)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"review"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "note-1") || !strings.Contains(out.String(), "Hidden transcript evidence candidates: 2") {
		t.Fatalf("review did not hide transcript evidence:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"review", "--evidence"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review evidence: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "note-1") || strings.Contains(out.String(), "rule-1") {
		t.Fatalf("review evidence output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"context", "task"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "note-1") || strings.Contains(out.String(), "Evidence body.") || !strings.Contains(out.String(), "Hidden transcript evidence candidates: 2") || !strings.Contains(out.String(), "rule-1") {
		t.Fatalf("context did not hide transcript evidence:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"context", "--evidence", "task"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context evidence: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "note-1") || !strings.Contains(out.String(), "Evidence body.") || !strings.Contains(out.String(), "rule-1") || strings.Contains(out.String(), "Hidden transcript evidence candidates") {
		t.Fatalf("context evidence output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"context", "--format", "json", "task"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context json: %v stderr=%s", err, errb.String())
	}
	var contextJSON struct {
		HiddenEvidenceCandidates int `json:"hidden_evidence_candidates"`
	}
	if err := json.Unmarshal(out.Bytes(), &contextJSON); err != nil {
		t.Fatal(err)
	}
	if contextJSON.HiddenEvidenceCandidates != 2 {
		t.Fatalf("context json hidden_evidence_candidates = %d, want 2", contextJSON.HiddenEvidenceCandidates)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--limit", "1"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill pending: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "Evidence Candidate `note-1`") {
		t.Fatalf("distill pending output missing note-1:\n%s", out.String())
	}
	if strings.Contains(out.String(), "Evidence Candidate `note-2`") {
		t.Fatalf("distill pending ignored limit:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--limit", "1", "--offset", "1"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill pending offset: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "Evidence Candidate `note-1`") || !strings.Contains(out.String(), "Evidence Candidate `note-2`") {
		t.Fatalf("distill pending offset output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--all"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill pending all: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "Evidence candidates in this pack: 2") || !strings.Contains(out.String(), "Evidence Candidate `note-1`") || !strings.Contains(out.String(), "Evidence Candidate `note-2`") {
		t.Fatalf("distill pending all output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--all", "--summary"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill summary: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "Source Evidence") || !strings.Contains(out.String(), "evidence_candidates: 2") || strings.Contains(out.String(), "split-source") {
		t.Fatalf("distill summary output unexpected:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--split-sources", "--all", "--summary"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill split sources summary: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "evidence_candidates: 3") || !strings.Contains(out.String(), "candidate: split-source") || strings.Contains(out.String(), "ordinary-lesson") {
		t.Fatalf("distill split sources summary output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--all", "--json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill json: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), `"count":2`) {
		t.Fatalf("distill json output unexpected:\n%s", out.String())
	}

	pack := filepath.Join(project, "distill.md")
	out.Reset()
	if err := Run(context.Background(), []string{"distill", "--pending", "--all", "--write-pack", pack}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill write-pack: %v stderr=%s", err, errb.String())
	}
	packBody, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packBody, []byte("Evidence Candidate `note-2`")) || !strings.Contains(out.String(), "pack: "+pack) {
		t.Fatalf("write-pack output unexpected stdout=%s pack=%s", out.String(), packBody)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "note-1"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill: %v stderr=%s", err, errb.String())
	}
	text := out.String()
	for _, want := range []string{"Worktrail Distillation Pack", "Evidence body.", model.CandidateTypeTranscriptNotes, "Do not promote"} {
		if !strings.Contains(text, want) {
			t.Fatalf("distill output missing %q:\n%s", want, text)
		}
	}
	out.Reset()
	if err := Run(context.Background(), []string{"distill", "split-source"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill split source: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "Evidence Candidate `split-source`") || !strings.Contains(out.String(), "Split source body.") {
		t.Fatalf("distill split source output unexpected:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"distill", "ordinary-lesson"}, nil, &out, &errb); err == nil || !strings.Contains(err.Error(), "not a supported distillation source") {
		t.Fatalf("ordinary lesson distill error = %v stdout=%s", err, out.String())
	}
}

func TestDistillProposalValidateAndApplyPartial(t *testing.T) {
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
	runApp(t, &out, &errb, "candidates", "create", "--id", "same-target", "--type", "rule", "--target", "rules/existing.md", "--title", "Same Target", "Existing pending body.")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "existing.md"), "# Existing\n\nExisting formal rule.\n")

	proposal := filepath.Join(project, "proposal.json")
	writeTextFile(t, proposal, `{
  "schema": "worktrail.distill.proposal.v1",
  "source_candidate_ids": ["note-1"],
  "candidates": [
    {
      "candidate_type": "rule",
      "title": "Existing Rule",
      "summary": "Distilled rule.",
      "target_path": "rules/existing.md",
      "operation": "replace",
      "tags": ["distilled"],
      "evidence_label": "Pending Verification",
      "confidence": 0.8,
      "body": "# Existing Rule\n\nUse this rule."
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
      "title": "Bad Target",
      "target_path": ".worktrail/rules/bad.md",
      "operation": "replace",
      "body": "# Bad\n"
    },
    {
      "candidate_type": "decision",
      "title": "Wrong Target Type",
      "target_path": "rules/wrong-type.md",
      "operation": "replace",
      "body": "# Wrong Type\n"
    },
    {
      "candidate_type": "rule",
      "title": "Missing Source",
      "target_path": "rules/missing-source.md",
      "operation": "replace",
      "source_candidate_ids": ["missing-source"],
      "body": "# Missing Source\n"
    }
  ]
}`)

	badSchema := filepath.Join(project, "bad-schema.json")
	writeTextFile(t, badSchema, `{"schema":"wrong","source_candidate_ids":["note-1"],"candidates":[]}`)
	out.Reset()
	if err := Run(context.Background(), []string{"distill", "validate", badSchema, "--format", "json"}, nil, &out, &errb); err == nil || !strings.Contains(err.Error(), "proposal schema must be") {
		t.Fatalf("bad schema error = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "validate", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill validate: %v stderr=%s", err, errb.String())
	}
	var validation wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if validation.Valid || validation.Blocked != 1 || len(validation.Items) != 6 || validation.Items[0].Status != "valid" || validation.Items[1].Status != "error" || validation.Items[2].Status != "blocked" || validation.Items[3].Status != "error" || validation.Items[4].Status != "error" || validation.Items[5].Status != "error" {
		t.Fatalf("validation report unexpected: %+v", validation)
	}
	if !containsString(validation.Items[4].Errors, "candidate_type does not match target_path") {
		t.Fatalf("type-target mismatch errors unexpected: %+v", validation.Items[4])
	}
	if !containsString(validation.Items[5].Errors, "source candidate not found: missing-source") {
		t.Fatalf("missing source errors unexpected: %+v", validation.Items[5])
	}
	for _, want := range []string{"target_exists", "replace_target_exists", "same_target_pending:2"} {
		if !containsString(validation.Items[0].WarningCodes, want) {
			t.Fatalf("validation first item missing warning %q: %+v", want, validation.Items[0])
		}
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "apply", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill apply: %v stderr=%s", err, errb.String())
	}
	var applied wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if applied.Valid || applied.Created != 1 || applied.Blocked != 1 || len(applied.Items) != 6 || applied.Items[0].Status != "created" {
		t.Fatalf("apply report unexpected: %+v", applied)
	}
	createdID := applied.Items[0].CandidateID
	out.Reset()
	if err := Run(context.Background(), []string{"candidates", "show", createdID, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidate show: %v stderr=%s", err, errb.String())
	}
	var rec candidate.Record
	if err := json.Unmarshal(out.Bytes(), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.Meta.SourceCandidateIDs == nil || len(rec.Meta.SourceCandidateIDs) != 1 || rec.Meta.SourceCandidateIDs[0] != "note-1" || rec.Meta.EvidenceLabel != "Pending Verification" || rec.Meta.Confidence != 0.8 {
		t.Fatalf("created candidate metadata unexpected: %+v", rec.Meta)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"distill", "apply", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run duplicate distill apply: %v stderr=%s", err, errb.String())
	}
	var duplicate wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Created != 0 || duplicate.Skipped != 1 || duplicate.Items[0].Status != "skipped" {
		t.Fatalf("duplicate apply report unexpected: %+v", duplicate)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"candidates", "show", "note-1", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run source candidate show: %v stderr=%s", err, errb.String())
	}
	var source candidate.Record
	if err := json.Unmarshal(out.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if source.Meta.Status != candidate.StatusPending {
		t.Fatalf("source status changed: %+v", source.Meta)
	}
}

func TestDistillProposalEndToEndPromoteContextFromSplitSource(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "active-log", "--type", "lesson", "--target", "lessons/kdd-active-knowledge-log.md", "--title", "KDD Active Log", "--summary", "Do not promote directly", "--tags", "kdd,split-source", "Split source evidence body.")

	proposal := filepath.Join(project, "split-source-proposal.json")
	writeTextFile(t, proposal, `{
  "schema": "worktrail.distill.proposal.v1",
  "source_candidate_ids": ["active-log"],
  "candidates": [
    {
      "candidate_type": "glossary",
      "title": "Distilled Term",
      "summary": "A glossary item distilled from split-source evidence.",
      "target_path": "glossary/distilled-term.md",
      "operation": "replace",
      "tags": ["distilled"],
      "evidence_label": "User Confirmed",
      "body": "# Distilled Term\n\nDefinition from split source evidence."
    }
  ]
}`)

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "validate", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill validate split source: %v stderr=%s", err, errb.String())
	}
	var validation wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || len(validation.Items) != 1 || validation.Items[0].Status != "valid" {
		t.Fatalf("split source validation report unexpected: %+v", validation)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "apply", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill apply split source: %v stderr=%s", err, errb.String())
	}
	var applied wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.Valid || applied.Created != 1 || len(applied.Items) != 1 || applied.Items[0].Status != "created" {
		t.Fatalf("split source apply report unexpected: %+v", applied)
	}
	createdID := applied.Items[0].CandidateID

	text := runApp(t, &out, &errb, "review")
	if !strings.Contains(text, createdID) || !strings.Contains(text, "Distilled Term") || !strings.Contains(text, "Do not promote directly") {
		t.Fatalf("split source review output unexpected:\n%s", text)
	}

	runApp(t, &out, &errb, "promote", createdID)
	runApp(t, &out, &errb, "index", "rebuild")
	text = runApp(t, &out, &errb, "context", "distilled term")
	if !strings.Contains(text, "glossary/distilled-term.md") || !strings.Contains(text, "Definition from split source evidence.") {
		t.Fatalf("split source promoted context output unexpected:\n%s", text)
	}
}

func TestReviewShowsPendingSemanticWarnings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "existing.md"), "# Existing\n\nExisting formal rule.\n")
	runApp(t, &out, &errb, "candidates", "create", "--id", "replace-existing-1", "--type", "rule", "--target", "rules/existing.md", "--operation", "replace", "--title", "Replace Existing 1", "Rule body 1.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "replace-existing-2", "--type", "rule", "--target", "rules/existing.md", "--operation", "replace", "--title", "Replace Existing 2", "Rule body 2.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "merge-missing", "--type", "workflow", "--target", "workflows/missing.md", "--operation", "merge", "--title", "Merge Missing", "Workflow body.")

	text := runApp(t, &out, &errb, "review")
	for _, want := range []string{
		"warnings: target_exists, replace_target_exists, same_target_pending:2",
		"warnings: merge_target_missing",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("review output missing %q:\n%s", want, text)
		}
	}
}

func hasCandidateType(records []candidate.Record, typ string) bool {
	for _, rec := range records {
		if rec.Meta.CandidateType == typ {
			return true
		}
	}
	return false
}

func hasCandidateID(records []candidate.Record, id string) bool {
	for _, rec := range records {
		if rec.Meta.ID == id {
			return true
		}
	}
	return false
}

func hasDuplicateKDDCandidateIDs(items []kddImportItem) bool {
	seen := map[string]bool{}
	for _, item := range items {
		if item.CandidateID == "" {
			continue
		}
		if seen[item.CandidateID] {
			return true
		}
		seen[item.CandidateID] = true
	}
	return false
}

func hasKDDSkippedPath(items []kddImportItem, sourcePath string) bool {
	for _, item := range items {
		if item.SourcePath == sourcePath && item.SkipReason != "" {
			return true
		}
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestReviewWarnsWhenAppliedCandidateTargetMissing(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "rule-1", "--type", "rule", "--target", "rules/rule-1.md", "--title", "Rule", "Rule body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create rule: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"promote", "rule-1"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run promote: %v stderr=%s", err, errb.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"review"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "Applied candidate target warnings") {
		t.Fatalf("review warned while promoted target exists:\n%s", out.String())
	}

	if err := os.Remove(filepath.Join(project, ".worktrail", "rules", "rule-1.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"review"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review after target removal: %v stderr=%s", err, errb.String())
	}
	for _, want := range []string{
		"Applied candidate target warnings",
		"`rule-1` is promoted but `rules/rule-1.md` is missing",
		"context will not load it as formal knowledge",
		"worktrail restore <id>",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("review output missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	if err := Run(context.Background(), []string{"restore", "rule-1"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run restore: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "rule-1\trestored") {
		t.Fatalf("restore output unexpected:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"review"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review after restore: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "Applied candidate target warnings") {
		t.Fatalf("review still warned after restore:\n%s", out.String())
	}
}

func TestRetireClearsAppliedCandidateTargetWarning(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "retire-rule", "--type", "rule", "--target", "rules/retire-rule.md", "--title", "Retire Rule", "Retire body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create rule: %v stderr=%s", err, errb.String())
	}
	if err := Run(context.Background(), []string{"promote", "retire-rule"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run promote: %v stderr=%s", err, errb.String())
	}
	if err := os.Remove(filepath.Join(project, ".worktrail", "rules", "retire-rule.md")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"review"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review after target removal: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "worktrail retire <id> --reason <text>") {
		t.Fatalf("review did not suggest retire:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"retire", "retire-rule", "--reason", "smoke test cleanup"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run retire: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "retire-rule\tretired") {
		t.Fatalf("retire output unexpected:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"review"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review after retire: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "Applied candidate target warnings") {
		t.Fatalf("review still warned after retire:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"candidates", "list", "--status", "retired", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates list retired: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), `"id":"retire-rule"`) || !strings.Contains(out.String(), `"retire_reason":"smoke test cleanup"`) {
		t.Fatalf("retired list output unexpected:\n%s", out.String())
	}
}

func TestAppSmokeCoreCLILifecycle(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "smoke-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/smoke-note.md", "--title", "Smoke Transcript Notes", "Smoke evidence body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "smoke-rule", "--type", "rule", "--target", "rules/smoke-rule.md", "--title", "Smoke Rule", "Smoke rule body.")
	runApp(t, &out, &errb, "index", "rebuild")

	text := runApp(t, &out, &errb, "review")
	if !strings.Contains(text, "smoke-rule") || strings.Contains(text, "smoke-note") || !strings.Contains(text, "Hidden transcript evidence candidates: 1") {
		t.Fatalf("smoke review output unexpected:\n%s", text)
	}

	text = runApp(t, &out, &errb, "context", "smoke task")
	if !strings.Contains(text, "smoke-rule") || strings.Contains(text, "smoke-note") || strings.Contains(text, "Smoke evidence body.") || !strings.Contains(text, "Hidden transcript evidence candidates: 1") {
		t.Fatalf("smoke context output unexpected:\n%s", text)
	}

	text = runApp(t, &out, &errb, "context", "--evidence", "smoke task")
	if !strings.Contains(text, "smoke-rule") || !strings.Contains(text, "smoke-note") || !strings.Contains(text, "Smoke evidence body.") || strings.Contains(text, "Hidden transcript evidence candidates") {
		t.Fatalf("smoke context evidence output unexpected:\n%s", text)
	}

	runApp(t, &out, &errb, "promote", "smoke-rule")
	runApp(t, &out, &errb, "index", "rebuild")
	text = runApp(t, &out, &errb, "context", "promoted smoke rule")
	if !strings.Contains(text, "rules/smoke-rule.md") || !strings.Contains(text, "Smoke rule body.") {
		t.Fatalf("smoke promoted context output unexpected:\n%s", text)
	}

	target := filepath.Join(project, ".worktrail", "rules", "smoke-rule.md")
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	text = runApp(t, &out, &errb, "review")
	if !strings.Contains(text, "Applied candidate target warnings") || !strings.Contains(text, "worktrail restore <id>") || !strings.Contains(text, "worktrail retire <id> --reason <text>") {
		t.Fatalf("smoke missing-target review output unexpected:\n%s", text)
	}

	runApp(t, &out, &errb, "restore", "smoke-rule")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("restored target missing: %v", err)
	}
	text = runApp(t, &out, &errb, "review")
	if strings.Contains(text, "Applied candidate target warnings") {
		t.Fatalf("review still warned after restore:\n%s", text)
	}

	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	text = runApp(t, &out, &errb, "retire", "smoke-rule", "--reason", "smoke lifecycle retired")
	if !strings.Contains(text, "smoke-rule\tretired") {
		t.Fatalf("smoke retire output unexpected:\n%s", text)
	}
	text = runApp(t, &out, &errb, "review")
	if strings.Contains(text, "Applied candidate target warnings") {
		t.Fatalf("review still warned after retire:\n%s", text)
	}

	text = runApp(t, &out, &errb, "distill", "--pending", "--all", "--summary")
	if !strings.Contains(text, "evidence_candidates: 1") {
		t.Fatalf("smoke distill summary output unexpected:\n%s", text)
	}
	pack := filepath.Join(project, "smoke-distill.md")
	runApp(t, &out, &errb, "distill", "--pending", "--all", "--write-pack", pack)
	packBody, err := os.ReadFile(pack)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(packBody, []byte("Smoke evidence body.")) {
		t.Fatalf("smoke distill pack missing evidence body:\n%s", packBody)
	}
}

func TestCandidatesCreateHelpDoesNotRequireTarget(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"candidates", "create", "--help"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create help: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "usage: worktrail candidates create") || !strings.Contains(out.String(), "--target <path>") {
		t.Fatalf("help output unexpected:\n%s", out.String())
	}
}

func TestDistillAllLargeSetRequiresCompactOutput(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	for i := 0; i < 6; i++ {
		id := "note-large-" + string(rune('a'+i))
		if err := Run(context.Background(), []string{"candidates", "create", "--id", id, "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/" + id + ".md", "--title", "Transcript Notes", "Evidence body."}, nil, &out, &errb); err != nil {
			t.Fatalf("Run candidates create %s: %v stderr=%s", id, err, errb.String())
		}
	}
	out.Reset()
	err := Run(context.Background(), []string{"distill", "--pending", "--all"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "avoid flooding the terminal") {
		t.Fatalf("distill --all error = %v, stdout=%s", err, out.String())
	}
}

func runApp(t *testing.T, out, errb *bytes.Buffer, args ...string) string {
	t.Helper()
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), args, nil, out, errb); err != nil {
		t.Fatalf("Run %v: %v stderr=%s", args, err, errb.String())
	}
	return out.String()
}

func writeTextFile(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
