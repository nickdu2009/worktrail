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
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestADRCreateTemplateUsesDecisionCandidate(t *testing.T) {
	project := setupADRTestProject(t)
	rec := runADRJSON(t, nil, "adr", "create", "--adr-id", "ADR-0001", "--title", "Choose Storage", "--format", "json")

	if rec.Meta.CandidateType != "decision" {
		t.Fatalf("candidate type = %q", rec.Meta.CandidateType)
	}
	if rec.Meta.Operation != candidate.OperationReplace {
		t.Fatalf("operation = %q", rec.Meta.Operation)
	}
	if rec.Meta.TargetPath != "decisions/ADR-0001-choose-storage.md" {
		t.Fatalf("target path = %q", rec.Meta.TargetPath)
	}
	doc, err := store.ParseMarkdown([]byte(rec.Body))
	if err != nil {
		t.Fatal(err)
	}
	if doc.Meta["status"] != "proposed" || doc.Meta["lifecycle"] != "current" || doc.Meta["type"] != "decision" {
		t.Fatalf("unexpected ADR frontmatter: %#v", doc.Meta)
	}
	if !strings.Contains(doc.Body, "# ADR-0001: Choose Storage") {
		t.Fatalf("unexpected ADR body:\n%s", doc.Body)
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "decisions", "ADR-0001-choose-storage.md")); !os.IsNotExist(err) {
		t.Fatalf("adr create wrote formal knowledge or unexpected stat error: %v", err)
	}
}

func TestADRCreateFromStdinValidatesStatusAndStructure(t *testing.T) {
	setupADRTestProject(t)
	body := completeADR("ADR-0002", "Choose Queue", "Accepted", "")
	rec := runADRJSON(t, strings.NewReader(body), "adr", "create", "--stdin", "--title", "Choose Queue", "--decision-status", "Accepted", "--format", "json")
	if rec.Meta.TargetPath != "decisions/ADR-0002-choose-queue.md" {
		t.Fatalf("target path = %q", rec.Meta.TargetPath)
	}

	var out, errb bytes.Buffer
	err := Run(context.Background(), []string{"adr", "create", "--stdin", "--title", "Broken"}, strings.NewReader("# ADR-0003: Broken\n\n- Status: Proposed\n\n## Context\n\nContext.\n"), &out, &errb)
	if err == nil || !strings.Contains(err.Error(), `section "Decision"`) {
		t.Fatalf("expected missing Decision error, got %v", err)
	}
	emptyConsequences := "# ADR-0003: Broken\n\n- Status: Proposed\n\n## Context\n\nContext.\n\n## Decision\n\nDecision.\n\n## Consequences\n\n### Positive\n\n### Negative\n"
	err = Run(context.Background(), []string{"adr", "create", "--stdin"}, strings.NewReader(emptyConsequences), &out, &errb)
	if err == nil || !strings.Contains(err.Error(), `section "Consequences"`) {
		t.Fatalf("expected empty Consequences error, got %v", err)
	}
}

func TestADRCreateFromFileRejectsConflictingInputs(t *testing.T) {
	setupADRTestProject(t)
	path := filepath.Join(t.TempDir(), "ADR-0006.md")
	if err := os.WriteFile(path, []byte(completeADR("ADR-0006", "Choose Log", "Proposed", "")), 0o644); err != nil {
		t.Fatal(err)
	}
	rec := runADRJSON(t, nil, "adr", "create", "--from-file", path, "--adr-id", "ADR-0006", "--format", "json")
	if rec.Meta.TargetPath != "decisions/ADR-0006-choose-log.md" {
		t.Fatalf("target path = %q", rec.Meta.TargetPath)
	}

	var out, errb bytes.Buffer
	err := Run(context.Background(), []string{"adr", "create", "--from-file", path, "--stdin"}, strings.NewReader("unused"), &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected mutually exclusive input error, got %v", err)
	}
	err = Run(context.Background(), []string{"adr", "create", "--from-file", path, "--adr-id", "ADR-0007"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "does not match ADR heading id") {
		t.Fatalf("expected ADR id conflict, got %v", err)
	}
}

func TestADRCreateRejectsDerivedSupersededByFrontmatter(t *testing.T) {
	setupADRTestProject(t)
	rendered, err := store.RenderMarkdown(map[string]any{
		"schema":        model.SchemaKnowledge,
		"id":            "ADR-0008",
		"scope":         "project",
		"type":          "decision",
		"title":         "Injected Reverse",
		"status":        "accepted",
		"lifecycle":     "current",
		"stage":         "decision",
		"superseded_by": []string{"decisions/fake.md"},
	}, completeADR("ADR-0008", "Injected Reverse", "Accepted", ""))
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	err = Run(context.Background(), []string{"adr", "create", "--stdin"}, bytes.NewReader(rendered), &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "superseded_by is derived") {
		t.Fatalf("expected derived superseded_by rejection, got %v", err)
	}
}

func TestADRSupersedesRequiresAcceptedStatusAndMatchingDecision(t *testing.T) {
	project := setupADRTestProject(t)
	oldPath := filepath.Join(project, ".worktrail", "decisions", "ADR-0001-old-choice.md")
	writeTextFile(t, oldPath, completeADR("ADR-0001", "Old Choice", "Accepted", ""))

	accepted := completeADR("ADR-0002", "New Choice", "Accepted", "- Supersedes: ADR-0001")
	rec := runADRJSON(t, strings.NewReader(accepted), "adr", "create", "--stdin", "--supersedes", "decisions/ADR-0001-old-choice.md", "--format", "json")
	doc, err := store.ParseMarkdown([]byte(rec.Body))
	if err != nil {
		t.Fatal(err)
	}
	got, err := adrMetaStringList(doc.Meta["supersedes"])
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "decisions/ADR-0001-old-choice.md" {
		t.Fatalf("supersedes = %#v", got)
	}

	proposed := completeADR("ADR-0003", "Future Choice", "Proposed", "- Proposes to supersede: ADR-0001")
	var out, errb bytes.Buffer
	err = Run(context.Background(), []string{"adr", "create", "--stdin", "--supersedes", "decisions/ADR-0001-old-choice.md"}, strings.NewReader(proposed), &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "only valid for Accepted") {
		t.Fatalf("expected Proposed supersedes rejection, got %v", err)
	}
}

func TestADRProposedToAcceptedUsesStableReplacementTarget(t *testing.T) {
	project := setupADRTestProject(t)
	proposed := completeADR("ADR-0004", "Choose Cache", "Proposed", "")
	first := runADRJSON(t, strings.NewReader(proposed), "adr", "create", "--stdin", "--id", "proposed-cache", "--format", "json")

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "promote", first.Meta.ID)
	formalPath := filepath.Join(project, ".worktrail", filepath.FromSlash(first.Meta.TargetPath))
	data, err := os.ReadFile(formalPath)
	if err != nil {
		t.Fatal(err)
	}
	formal, err := store.ParseMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if formal.Meta["status"] != "proposed" || formal.Meta["lifecycle"] != "current" || !strings.Contains(formal.Body, "- Status: Proposed") {
		t.Fatalf("promote changed Proposed ADR status or lifecycle: meta=%#v body=%s", formal.Meta, formal.Body)
	}

	accepted := completeADR("ADR-0004", "Choose Cache", "Accepted", "")
	second := runADRJSON(t, strings.NewReader(accepted), "adr", "create", "--stdin", "--id", "accepted-cache", "--format", "json")
	if second.Meta.TargetPath != first.Meta.TargetPath {
		t.Fatalf("replacement target = %q, want %q", second.Meta.TargetPath, first.Meta.TargetPath)
	}
	if second.Meta.Operation != candidate.OperationReplace || second.Meta.Status != candidate.StatusPending {
		t.Fatalf("unexpected replacement metadata: %+v", second.Meta)
	}
	runApp(t, &out, &errb, "promote", second.Meta.ID)
	data, err = os.ReadFile(formalPath)
	if err != nil {
		t.Fatal(err)
	}
	formal, err = store.ParseMarkdown(data)
	if err != nil {
		t.Fatal(err)
	}
	if formal.Meta["status"] != "accepted" || formal.Meta["lifecycle"] != "current" || !strings.Contains(formal.Body, "- Status: Accepted") {
		t.Fatalf("promote changed Accepted ADR status or lifecycle: meta=%#v body=%s", formal.Meta, formal.Body)
	}
}

func TestADRDateIDDoesNotDuplicateSlug(t *testing.T) {
	setupADRTestProject(t)
	rec := runADRJSON(t, nil, "adr", "create", "--adr-id", "ADR-20260714-choose-cache", "--title", "Choose Cache", "--format", "json")
	if rec.Meta.TargetPath != "decisions/ADR-20260714-choose-cache.md" {
		t.Fatalf("target path = %q", rec.Meta.TargetPath)
	}
}

func TestLegacyADRAliasListsAndReviewsAsDecision(t *testing.T) {
	project := setupADRTestProject(t)
	now := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	meta := model.Candidate{
		Schema:          model.SchemaCandidate,
		ID:              "legacy-adr",
		Scope:           "project",
		CandidateType:   "adr",
		TargetPath:      "decisions/ADR-0005-legacy.md",
		Title:           "Legacy ADR",
		Summary:         "Legacy decision candidate.",
		Operation:       candidate.OperationReplace,
		Status:          candidate.StatusPending,
		RedactionStatus: "clean",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	data, err := store.RenderMarkdown(meta, completeADR("ADR-0005", "Legacy ADR", "Proposed", ""))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(project, ".worktrail", "candidates", "project", "legacy-adr.md")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "candidates", "list", "--type", "decision", "--format", "json")
	var records []candidate.Record
	if err := json.Unmarshal(out.Bytes(), &records); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Meta.CandidateType != "decision" {
		t.Fatalf("unexpected canonical list: %+v", records)
	}

	runApp(t, &out, &errb, "review", "plan", "--format", "json")
	var plan reviewPlan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.Items) != 1 || plan.Items[0].CandidateType != "decision" {
		t.Fatalf("unexpected canonical review plan: %+v", plan.Items)
	}
}

func setupADRTestProject(t *testing.T) string {
	t.Helper()
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	return project
}

func runADRJSON(t *testing.T, in *strings.Reader, args ...string) candidate.Record {
	t.Helper()
	var reader *strings.Reader
	if in != nil {
		reader = in
	}
	var out, errb bytes.Buffer
	if err := Run(context.Background(), args, reader, &out, &errb); err != nil {
		t.Fatalf("Run %v: %v stderr=%s", args, err, errb.String())
	}
	var rec candidate.Record
	if err := json.Unmarshal(out.Bytes(), &rec); err != nil {
		t.Fatalf("decode candidate: %v\n%s", err, out.String())
	}
	return rec
}

func completeADR(id, title, status, links string) string {
	if links == "" {
		links = "- Related: ADR-0000"
	}
	return "# " + id + ": " + title + "\n\n" +
		"- Status: " + status + "\n" +
		"- Date: 2026-07-14\n\n" +
		"## Context\n\nContext.\n\n" +
		"## Decision Drivers\n\nDriver.\n\n" +
		"## Considered Alternatives\n\nAlternative.\n\n" +
		"## Decision\n\nDecision.\n\n" +
		"## Consequences\n\n### Positive\n\nPositive.\n\n### Negative\n\nNegative.\n\n" +
		"## Revisit Conditions\n\nRevisit.\n\n" +
		"## Links\n\n" + links + "\n"
}
