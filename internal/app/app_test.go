package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	wtdistill "github.com/nickdu2009/worktrail/internal/distill"
	"github.com/nickdu2009/worktrail/internal/index"
	kddmigration "github.com/nickdu2009/worktrail/internal/migration/kdd"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
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
		filepath.Join(project, ".worktrail", "requirements"),
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

func TestPreviewBuildsProjectKnowledgeHTML(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "decisions", "choice.md"), "# Choice\n\nDecision body.")
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "preview-note", "--type", "rule", "--target", "rules/preview-note.md", "--title", "Preview Candidate", "# Preview Candidate\n\nPending body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create: %v stderr=%s", err, errb.String())
	}
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"preview", "--no-open"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run preview --no-open: %v stderr=%s", err, errb.String())
	}
	indexPath := parseTabValue(t, out.String(), "index")
	body, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read preview HTML: %v", err)
	}
	for _, want := range []string{"Project Knowledge", "Pending Candidates", "Preview Candidate", "sections/decisions.html", "candidates/index.html"} {
		if !bytes.Contains(body, []byte(want)) {
			t.Fatalf("preview HTML missing %q:\n%s", want, body)
		}
	}
	for _, absent := range []string{"Decision body.", "Pending body."} {
		if bytes.Contains(body, []byte(absent)) {
			t.Fatalf("preview entry page should not inline %q:\n%s", absent, body)
		}
	}
	if !strings.Contains(indexPath, filepath.Join(".worktrail", ".cache", "preview", "index.html")) {
		t.Fatalf("preview index path should live in cache, got %s", indexPath)
	}

	docBody, err := os.ReadFile(filepath.Join(filepath.Dir(indexPath), "docs", "decisions-choice.html"))
	if err != nil {
		t.Fatalf("read document page: %v", err)
	}
	if !bytes.Contains(docBody, []byte("Decision body.")) {
		t.Fatalf("document page missing full body:\n%s", docBody)
	}

	candidatesBody, err := os.ReadFile(filepath.Join(filepath.Dir(indexPath), "candidates", "index.html"))
	if err != nil {
		t.Fatalf("read candidates page: %v", err)
	}
	if !bytes.Contains(candidatesBody, []byte("Preview Candidate")) {
		t.Fatalf("candidates page missing candidate title:\n%s", candidatesBody)
	}

	candidateBody, err := os.ReadFile(filepath.Join(filepath.Dir(indexPath), "candidates", "preview-note.html"))
	if err != nil {
		t.Fatalf("read candidate detail page: %v", err)
	}
	if !bytes.Contains(candidateBody, []byte("Pending body.")) {
		t.Fatalf("candidate detail page missing body:\n%s", candidateBody)
	}
}

func TestPreviewIgnoresGeneratedStorageDirectories(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "visible.md"), "# Visible Rule\n\nVisible rule body.")
	writeTextFile(t, filepath.Join(project, ".worktrail", "staging", "drafts", "noise.md"), "# Staging Noise\n\nShould not render.")
	writeTextFile(t, filepath.Join(project, ".worktrail", "runtime", "checkpoints", "noise.md"), "# Runtime Noise\n\nShould not render.")
	writeTextFile(t, filepath.Join(project, ".worktrail", "derived", "exports", "noise.md"), "# Derived Noise\n\nShould not render.")

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"preview", "--no-open"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run preview --no-open: %v stderr=%s", err, errb.String())
	}
	indexPath := parseTabValue(t, out.String(), "index")
	cacheRoot := filepath.Dir(indexPath)
	var rendered []string
	if err := filepath.Walk(cacheRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".html" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rendered = append(rendered, string(data))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(rendered, "\n")
	if !strings.Contains(all, "Visible Rule") {
		t.Fatalf("preview should include visible rule:\n%s", all)
	}
	for _, absent := range []string{"Staging Noise", "Runtime Noise", "Derived Noise"} {
		if strings.Contains(all, absent) {
			t.Fatalf("preview should ignore generated storage content %q:\n%s", absent, all)
		}
	}
}

func TestPreviewScopeUserAndClearCache(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"preview", "--scope", "user", "--no-open"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run preview user --no-open: %v stderr=%s", err, errb.String())
	}
	indexPath := parseTabValue(t, out.String(), "index")
	if _, err := os.Stat(indexPath); err != nil {
		t.Fatalf("user preview file missing: %v", err)
	}
	if !strings.HasSuffix(indexPath, filepath.Join(".cache", "preview", "index.html")) {
		t.Fatalf("user preview should use stable index.html, got %s", indexPath)
	}
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"preview", "--scope", "user", "--clear-cache"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run preview clear-cache: %v stderr=%s", err, errb.String())
	}
	cleared := parseTabValue(t, out.String(), "cleared")
	if !strings.Contains(cleared, filepath.Join(".worktrail", ".cache", "preview")) {
		t.Fatalf("cleared path unexpected: %s", cleared)
	}
	if _, err := os.Stat(cleared); !os.IsNotExist(err) {
		t.Fatalf("cache dir should be removed, err=%v", err)
	}
}

func TestPreviewRejectsLegacyFlagsAndTargets(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	for _, args := range [][]string{
		{"preview", "project.md"},
		{"preview", "--candidate", "note-1"},
		{"preview", "--render-only"},
		{"preview", "--open"},
	} {
		out.Reset()
		errb.Reset()
		err := Run(context.Background(), args, nil, &out, &errb)
		if err == nil {
			t.Fatalf("Run %v expected error", args)
		}
	}
}

func TestPreviewHelpShowsNewContract(t *testing.T) {
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"preview", "--help"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run preview help: %v stderr=%s", err, errb.String())
	}
	for _, want := range []string{
		"usage: worktrail preview [--scope project|user] [--no-open]",
		"worktrail preview --clear-cache [--scope project|user]",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("preview help missing %q:\n%s", want, out.String())
		}
	}
}

func TestPreviewRenderOnlyRendersCandidate(t *testing.T) {
	t.Skip("preview command was redesigned to whole-knowledge rendering")
}

func TestPreviewFlagParsingAndErrors(t *testing.T) {
	t.Skip("preview command was redesigned to whole-knowledge rendering")
}

func TestPreviewRenderOnlyRendersProjectDocument(t *testing.T) {
	t.Skip("preview command was redesigned to whole-knowledge rendering")
}

func TestTopLevelHelpIncludesCandidateApplyCommands(t *testing.T) {
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run help: %v stderr=%s", err, errb.String())
	}
	for _, want := range []string{
		"worktrail preview [--scope project|user] [--no-open]",
		"worktrail search <keyword>",
		"worktrail state start <title>",
		"worktrail state update <note>",
		"worktrail handoff <summary>",
		"worktrail resume [<task>]",
		"worktrail doctor knowledge",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("top-level help missing %q:\n%s", want, out.String())
		}
	}
}

func TestCLIHelpSmokeDoesNotMutateState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "context", args: []string{"context", "--help"}, want: "usage: worktrail context"},
		{name: "state", args: []string{"state", "--help"}, want: "usage: worktrail state"},
		{name: "state help", args: []string{"state", "help"}, want: "start <title>"},
		{name: "state short help", args: []string{"state", "-h"}, want: "checkpoint"},
		{name: "state start", args: []string{"state", "start", "--help"}, want: "inject"},
		{name: "state update", args: []string{"state", "update", "--help"}, want: "update"},
		{name: "state checkpoint", args: []string{"state", "checkpoint", "--help"}, want: "checkpoint"},
		{name: "state inject", args: []string{"state", "inject", "--help"}, want: "inject"},
		{name: "handoff", args: []string{"handoff", "--help"}, want: "usage: worktrail handoff"},
		{name: "resume", args: []string{"resume", "--help"}, want: "usage: worktrail resume"},
		{name: "import", args: []string{"import", "--help"}, want: "usage: worktrail import codex"},
		{name: "review", args: []string{"review", "--help"}, want: "usage: worktrail review"},
		{name: "distill", args: []string{"distill", "--help"}, want: "usage: worktrail distill"},
		{name: "evidence", args: []string{"evidence", "--help"}, want: "usage: worktrail evidence"},
		{name: "evidence plan", args: []string{"evidence", "plan", "--help"}, want: "usage: worktrail evidence"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errb bytes.Buffer
			if err := Run(context.Background(), tc.args, nil, &out, &errb); err != nil {
				t.Fatalf("Run %v: %v stderr=%s", tc.args, err, errb.String())
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("help output missing %q:\n%s", tc.want, out.String())
			}
		})
	}

	if _, err := os.Stat(filepath.Join(project, ".worktrail", "candidates")); !os.IsNotExist(err) {
		t.Fatalf("help commands should not create candidates directory, err=%v", err)
	}

	var out, errb bytes.Buffer
	err := Run(context.Background(), []string{"state", "unknown"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), `unknown state subcommand "unknown"`) {
		t.Fatalf("unknown state subcommand error = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"context", "help", "debug", "this"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context with help task word: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "usage: worktrail context") || !strings.Contains(out.String(), "help debug this") {
		t.Fatalf("context task word help was treated as help output:\n%s", out.String())
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"handoff", "need", "help", "later"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run handoff with help summary word: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "usage: worktrail handoff") || !strings.Contains(out.String(), filepath.Join(".worktrail", "handoffs")) {
		t.Fatalf("handoff summary word help was treated as help output:\n%s", out.String())
	}
}

func TestCandidateActionHelpDoesNotLookupCandidates(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	for _, command := range []string{"promote", "merge", "discard"} {
		t.Run(command, func(t *testing.T) {
			var out, errb bytes.Buffer
			if err := Run(context.Background(), []string{command, "--help"}, nil, &out, &errb); err != nil {
				t.Fatalf("Run %s --help: %v stderr=%s", command, err, errb.String())
			}
			if strings.Contains(out.String(), "candidate not found") || strings.Contains(errb.String(), "candidate not found") {
				t.Fatalf("%s --help performed candidate lookup:\nstdout=%s\nstderr=%s", command, out.String(), errb.String())
			}
			if !strings.Contains(out.String(), "usage: worktrail "+command) {
				t.Fatalf("%s --help missing usage:\n%s", command, out.String())
			}
		})
	}
}

func TestReviewHelpIncludesApplyCandidates(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"review", "--help"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review --help: %v stderr=%s", err, errb.String())
	}
	for _, want := range []string{
		"worktrail review apply-candidates --promote <id...>",
		"worktrail review apply-candidates --merge <id...>",
		"worktrail review apply-candidates --discard <id...>",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("review help missing %q:\n%s", want, out.String())
		}
	}
}

func TestCandidateActionIndexRebuildFailureReportsNextStep(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "json-promote", "--type", "rule", "--target", "rules/json-promote.md", "--title", "JSON Promote", "JSON promote body.")
	runApp(t, &out, &errb, "candidates", "create", "--id", "text-discard", "--type", "rule", "--target", "rules/text-discard.md", "--title", "Text Discard", "Text discard body.")
	if err := os.RemoveAll(filepath.Join(project, ".worktrail", "index")); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "index"), "not a directory\n")

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"promote", "json-promote", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run promote json: %v stderr=%s", err, errb.String())
	}
	var result candidate.ApplyResult
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("promote JSON stdout invalid: %v\nstdout=%s\nstderr=%s", err, out.String(), errb.String())
	}
	if strings.Contains(out.String(), "next:") {
		t.Fatalf("JSON stdout was polluted by next-step:\n%s", out.String())
	}
	if !strings.Contains(errb.String(), "next: worktrail index rebuild --scope project") {
		t.Fatalf("JSON stderr missing rebuild next-step:\n%s", errb.String())
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"discard", "text-discard"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run discard text: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "next: worktrail index rebuild --scope project") {
		t.Fatalf("text stdout missing rebuild next-step:\nstdout=%s\nstderr=%s", out.String(), errb.String())
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

func TestInstallAllIncludesCursorAndPreservesCompatibleSkills(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("HOME", home)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"install", "all", "--project"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run install all: %v stderr=%s", err, errb.String())
	}
	for _, path := range []string{
		filepath.Join(project, ".codex", "hooks.json"),
		filepath.Join(project, ".claude", "settings.json"),
		filepath.Join(project, ".cursor", "hooks.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected install all file %s: %v", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(project, "AGENTS.md"),
		filepath.Join(project, "CLAUDE.md"),
		filepath.Join(project, ".cursor", "rules", "worktrail.mdc"),
		filepath.Join(project, ".agents", "skills", "worktrail-state", "SKILL.md"),
		filepath.Join(project, ".claude", "skills", "worktrail-state", "SKILL.md"),
		filepath.Join(project, ".cursor", "skills", "worktrail-state", "SKILL.md"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("project install should not create agent instruction file %s, err=%v", path, err)
		}
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

func TestImportCursorUsesObservedRegistryAndDoesNotScanLogs(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(t.TempDir(), "cursor-session.jsonl")
	body := `{"role":"user","content":"Capture Cursor workflow."}` + "\n" +
		`{"role":"assistant","content":"Keep Cursor evidence pending."}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"init"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run init: %v stderr=%s", err, errb.String())
	}
	observedDir := filepath.Join(project, ".worktrail", "raw", "cursor")
	writeTextFile(t, filepath.Join(observedDir, "observed-test.metadata.json"), `{
  "schema": "worktrail.cursor_observed_transcript.v1",
  "id": "observed-test",
  "source": "cursor",
  "path": "`+filepath.ToSlash(transcriptPath)+`",
  "path_basename": "cursor-session.jsonl",
  "created_at": "2026-05-15T00:00:00Z"
}
`)
	out.Reset()
	if err := Run(context.Background(), []string{"import", "cursor", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import cursor dry-run: %v stderr=%s", err, errb.String())
	}
	var dry importReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Matched != 1 || dry.Observed != 1 || !dry.DryRun {
		t.Fatalf("unexpected cursor dry-run report: %+v", dry)
	}
	out.Reset()
	if err := Run(context.Background(), []string{"import", "cursor", "--all", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import cursor --all: %v stderr=%s", err, errb.String())
	}
	var report importReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Matched != 1 || report.Synced != 1 || report.Extracted != 1 || report.Blocked != 0 || report.DryRun {
		t.Fatalf("unexpected cursor import report: %+v", report)
	}
	if matches, err := filepath.Glob(filepath.Join(project, ".worktrail", "raw", "cursor", "observed-*.metadata.json")); err != nil || len(matches) != 1 {
		t.Fatalf("observed metadata should remain distinct, matches=%v err=%v", matches, err)
	}
	if matches, err := filepath.Glob(filepath.Join(project, ".worktrail", "raw", "cursor", "cursor-session-*.metadata.json")); err != nil || len(matches) != 1 {
		t.Fatalf("synced metadata should be distinct, matches=%v err=%v", matches, err)
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
}

func TestImportHelpDoesNotExposeKDDMigration(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	if err := Run(context.Background(), []string{"import", "--help"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run import help: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "import kdd") {
		t.Fatalf("import help exposed legacy KDD migration:\n%s", out.String())
	}
}

func TestImportSinceAcceptsDayShorthand(t *testing.T) {
	d, err := parseImportSince("14d")
	if err != nil {
		t.Fatalf("parseImportSince: %v", err)
	}
	if d.Hours() != 14*24 {
		t.Fatalf("duration = %s, want 336h", d)
	}
}

func TestNoteAddCreatesPendingCandidateOnly(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	out.Reset()
	if err := Run(context.Background(), []string{
		"note", "add",
		"--type", "rule",
		"--target", "rules/note-rule.md",
		"--title", "Note Rule",
		"--summary", "Capture a validated rule.",
		"--evidence-label", "dogfood-analysis",
		"--confidence", "0.8",
		"Keep knowledge writes candidate-based.",
	}, nil, &out, &errb); err != nil {
		t.Fatalf("Run note add: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "pending") || !strings.Contains(out.String(), "next: worktrail review plan --format json --scope project") {
		t.Fatalf("note add output unexpected:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "rules", "note-rule.md")); !os.IsNotExist(err) {
		t.Fatalf("note add should not write formal knowledge, err=%v", err)
	}
	candidates, err := filepath.Glob(filepath.Join(project, ".worktrail", "candidates", "project", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate files = %d, want 1", len(candidates))
	}
	body, err := os.ReadFile(candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"candidate_type": "rule"`, `"target_path": "rules/note-rule.md"`, `"evidence_label": "dogfood-analysis"`, `"confidence": 0.8`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("candidate missing %q:\n%s", want, body)
		}
	}
}

func TestNoteAddRejectsUnsafeOrIncompleteInput(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	err := Run(context.Background(), []string{"note", "add", "--type", "rule", "--target", "rules/missing.md"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "note add requires") {
		t.Fatalf("missing fields error = %v stdout=%s", err, out.String())
	}
	err = Run(context.Background(), []string{
		"note", "add",
		"--type", "decision",
		"--target", "rules/wrong.md",
		"--title", "Wrong Target",
		"--summary", "Wrong target.",
		"--evidence-label", "test",
		"Body.",
	}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "does not match target path") {
		t.Fatalf("target mismatch error = %v stdout=%s", err, out.String())
	}
	err = Run(context.Background(), []string{
		"note", "add",
		"--type", "workflow",
		"--target", "workflows/transcript.md",
		"--title", "Transcript Leak",
		"--summary", "Reach me at nick@example.com",
		"--evidence-label", "test",
		"# Transcript Leak\n\n- user: please fix the bug\n- assistant: here is the patch",
	}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "summary contains redactable secret or PII pattern") || !strings.Contains(err.Error(), "body contains raw transcript-style conversation") {
		t.Fatalf("unsafe note add error = %v stdout=%s", err, out.String())
	}
}

func TestHandoffWriteFailureReportsWorktrailWriteDirs(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	if err := os.RemoveAll(filepath.Join(project, ".worktrail", "handoffs")); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "handoffs"), "not a directory\n")

	err := Run(context.Background(), []string{"handoff", "cannot write handoff"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("handoff unexpectedly succeeded")
	}
	for _, want := range []string{"handoff write failed", filepath.Join(project, ".worktrail", "handoffs"), filepath.Join(project, ".worktrail", "logs")} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("handoff error missing %q:\n%v", want, err)
		}
	}
}

func TestStateCloseToHandoffFailureKeepsActiveState(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "state", "start", "Close Safety")
	if err := os.RemoveAll(filepath.Join(project, ".worktrail", "handoffs")); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "handoffs"), "not a directory\n")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"state", "close", "--to", "handoff", "need durable handoff"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("state close unexpectedly succeeded")
	}
	active := nonAliasStateFiles(t, filepath.Join(project, ".worktrail", "state", "active", "*.md"))
	if len(active) != 1 {
		t.Fatalf("active state files = %v, want 1 active state", active)
	}
	archived := nonAliasStateFiles(t, filepath.Join(project, ".worktrail", "state", "archived", "*.md"))
	if len(archived) != 0 {
		t.Fatalf("archived state files = %v, want none", archived)
	}
}

func TestResumeFailureKeepsSourceStateActive(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "state", "start", "Resume Safety")

	latestPath := filepath.Join(project, ".worktrail", "state", "active", "latest.md")
	if err := os.Remove(latestPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(latestPath, 0o755); err != nil {
		t.Fatal(err)
	}

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"resume", "Resume Safety Continued"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("resume unexpectedly succeeded")
	}
	active := nonAliasStateFiles(t, filepath.Join(project, ".worktrail", "state", "active", "*.md"))
	if len(active) != 1 {
		t.Fatalf("active state files = %v, want source state only", active)
	}
	archived := nonAliasStateFiles(t, filepath.Join(project, ".worktrail", "state", "archived", "*.md"))
	if len(archived) != 0 {
		t.Fatalf("archived state files = %v, want none", archived)
	}
}

func TestMaintainKnowledgeValidateAndApplyProposal(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	proposal := filepath.Join(project, "maintain-proposal.json")
	writeTextFile(t, proposal, `{
  "schema": "worktrail.knowledge.maintenance.proposal.v1",
  "actions": [
    {
      "action": "create_candidate",
      "candidate_type": "rule",
      "target_path": "rules/maintained.md",
      "title": "Maintained Rule",
      "summary": "Created through maintenance proposal.",
      "evidence_label": "maintain-smoke",
      "confidence": 0.7,
      "body": "# Maintained Rule\n\nUse proposals for maintenance writes.",
      "reason": "Capture a reviewed maintenance finding."
    }
  ]
}`)
	out.Reset()
	if err := Run(context.Background(), []string{"maintain", "knowledge", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run maintain knowledge: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), `"schema":"worktrail.knowledge.maintenance.report.v1"`) {
		t.Fatalf("maintain knowledge report unexpected:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"maintain", "validate", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run maintain validate: %v stderr=%s stdout=%s", err, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), `"valid":true`) {
		t.Fatalf("maintain validate should be valid:\n%s", out.String())
	}
	out.Reset()
	if err := Run(context.Background(), []string{"maintain", "apply", proposal, "--confirm", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run maintain apply: %v stderr=%s stdout=%s", err, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), `"applied":1`) {
		t.Fatalf("maintain apply output unexpected:\n%s", out.String())
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "rules", "maintained.md")); !os.IsNotExist(err) {
		t.Fatalf("maintain create_candidate should not write formal target, err=%v", err)
	}
	candidates, err := filepath.Glob(filepath.Join(project, ".worktrail", "candidates", "project", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate files = %d, want 1", len(candidates))
	}
}

func TestMaintainRejectsUnsafeProposalActions(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	bad := filepath.Join(project, "bad-maintain-proposal.json")
	writeTextFile(t, bad, `{
  "schema": "worktrail.knowledge.maintenance.proposal.v1",
  "actions": [
    {"action": "update_index", "target_path": "index.md", "reason": "direct index mutation"},
    {"action": "create_candidate", "candidate_type": "decision", "target_path": "rules/wrong.md", "title": "Wrong", "summary": "Wrong", "body": "# Wrong"},
    {"action": "create_candidate", "candidate_type": "rule", "target_path": "rules/leak.md", "title": "Leak", "summary": "Contact me at nick@example.com", "body": "# Leak\n\nPath: /Users/tester/private.txt"},
    {"action": "create_candidate", "candidate_type": "workflow", "target_path": "workflows/raw-transcript.md", "title": "Transcript Leak", "summary": "Transcript excerpt", "body": "# Transcript Leak\n\n- user: please fix the bug\n- assistant: here is the patch"},
    {"action": "archive_evidence", "candidate_id": "missing-evidence"}
  ]
}`)
	out.Reset()
	err := Run(context.Background(), []string{"maintain", "validate", bad, "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("maintain validate error = %v stdout=%s", err, out.String())
	}
	if !strings.Contains(out.String(), `"valid":false`) ||
		!strings.Contains(out.String(), "unsupported action type") ||
		!strings.Contains(out.String(), "candidate_type does not match target_path") ||
		!strings.Contains(out.String(), "summary contains redactable secret or PII pattern") ||
		!strings.Contains(out.String(), "body contains local absolute path") ||
		!strings.Contains(out.String(), "summary_redactable_secret_or_pii") ||
		!strings.Contains(out.String(), "body_local_absolute_path") ||
		!strings.Contains(out.String(), "body contains raw transcript-style conversation") ||
		!strings.Contains(out.String(), "body_raw_transcript_style_conversation") ||
		!strings.Contains(out.String(), "reason_required") ||
		!strings.Contains(out.String(), "archive_evidence requires reason") {
		t.Fatalf("maintain validation output missing errors:\n%s", out.String())
	}
}

func TestMigrateKDDCreatesProjectAndUserCandidates(t *testing.T) {
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
	if err := Run(context.Background(), []string{"migrate", "kdd", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run migrate kdd dry-run: %v stderr=%s", err, errb.String())
	}
	var dry kddmigration.Report
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.Matched != 12 || dry.Skipped != 2 || dry.Blocked != 1 || dry.ProjectItems != 11 || dry.LocalItems != 1 {
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
	if err := Run(context.Background(), []string{"migrate", "kdd", "--root", missingRoot, "--format", "json"}, nil, &out, &errb); err == nil || !strings.Contains(err.Error(), "kdd root does not exist") {
		t.Fatalf("missing root error = %v", err)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"migrate", "kdd", "--write-candidates", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run migrate kdd --write-candidates: %v stderr=%s", err, errb.String())
	}
	var report kddmigration.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.DryRun || report.Created != 12 || report.Skipped != 2 || report.Blocked != 1 || report.ProjectItems != 11 || report.LocalItems != 1 {
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
	if !bytes.Contains(activeLog, []byte(`"candidate_type": "migration_source"`)) || !bytes.Contains(activeLog, []byte(`"target_path": "imports/kdd/project/active-knowledge-log.md"`)) {
		t.Fatalf("active log candidate unexpected:\n%s", activeLog)
	}
	if !bytes.Contains(activeLog, []byte(`"kdd"`)) || !bytes.Contains(activeLog, []byte(`"migration_source"`)) {
		t.Fatalf("active log candidate missing migration tags:\n%s", activeLog)
	}
	localActiveLog, err := os.ReadFile(filepath.Join(home, "candidates", "user", "kdd-local-active-knowledge-log.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(localActiveLog, []byte(`"candidate_type": "migration_source"`)) || !bytes.Contains(localActiveLog, []byte(`"target_path": "imports/kdd/local/active-knowledge-log.md"`)) || !bytes.Contains(localActiveLog, []byte(`"local_path_detected"`)) {
		t.Fatalf("local active log candidate unexpected:\n%s", localActiveLog)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"migrate", "kdd", "--write-candidates", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run duplicate migrate kdd --write-candidates: %v stderr=%s", err, errb.String())
	}
	var duplicate kddmigration.Report
	if err := json.Unmarshal(out.Bytes(), &duplicate); err != nil {
		t.Fatal(err)
	}
	if duplicate.Created != 0 || duplicate.Skipped < 12 {
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
	if !strings.Contains(text, "Architecture") || !strings.Contains(text, "architecture/system.md") {
		t.Fatalf("context missing promoted architecture:\n%s", text)
	}
}

func TestDoctorMigrationDetectsRisksAndCleanupClearsLegacyRoot(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, "docs", "knowledge-driven-development")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeWorktrailGovernance(t, project)
	writeTextFile(t, filepath.Join(root, "project", "README.md"), "# Legacy\n")
	writeTextFile(t, filepath.Join(project, ".worktrail", "runbooks", "legacy.md"), "# Legacy Runbook\n")
	runApp(t, &out, &errb, "candidates", "create", "--id", "migration-source", "--type", model.CandidateTypeMigrationSource, "--target", "imports/kdd/project/active-knowledge-log.md", "--title", "Migration Source", "--tags", "kdd-migration", "Mixed evidence.")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"doctor", "migration", "--format", "json"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("doctor migration unexpectedly passed:\n%s", out.String())
	}
	var report migrationDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"SRC001", "KDD002", "REV001"} {
		if !hasMigrationFinding(report, code) {
			t.Fatalf("doctor report missing %s: %+v", code, report.Findings)
		}
	}

	runApp(t, &out, &errb, "discard", "migration-source")
	if err := os.RemoveAll(filepath.Join(project, ".worktrail", "runbooks")); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"migrate", "kdd", "--cleanup-legacy", "--confirm", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("cleanup legacy: %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("legacy root still exists or stat failed: %v", err)
	}
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"doctor", "migration", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("doctor after cleanup: %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
}

func TestMigrateKDDCleanupArchivePathSafety(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, "docs", "knowledge-driven-development")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeWorktrailGovernance(t, project)
	writeTextFile(t, filepath.Join(root, "project", "README.md"), "# Legacy\n")

	unsafeArchive := filepath.Join(root, "archive")
	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"migrate", "kdd", "--cleanup-legacy", "--confirm", "--archive-path", unsafeArchive, "--format", "json"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "archive path must be outside legacy KDD root") {
		t.Fatalf("unsafe archive path error = %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("legacy root should remain after blocked archive cleanup: %v", err)
	}

	archive := filepath.Join(project, "archives", "legacy-kdd")
	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"migrate", "kdd", "--cleanup-legacy", "--confirm", "--archive-path", archive, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("archive cleanup: %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	var report legacyCleanupReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Action != "archived" || report.ArchivePath != archive {
		t.Fatalf("archive cleanup report unexpected: %+v", report)
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("legacy root should be gone after archive cleanup, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(archive, "project", "README.md")); err != nil {
		t.Fatalf("archived legacy content missing: %v", err)
	}
}

func TestDoctorMigrationReportsTrackedRuntimeState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeWorktrailGovernance(t, project)
	writeTextFile(t, filepath.Join(project, ".worktrail", "index", "index.sqlite"), "SQLite format 3\x00")
	if err := exec.Command("git", "-C", project, "init").Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	if err := exec.Command("git", "-C", project, "add", "-f", ".worktrail/index/index.sqlite").Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"doctor", "migration", "--format", "json"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("doctor migration unexpectedly passed:\n%s", out.String())
	}
	var report migrationDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !hasMigrationFinding(report, "GIT001") {
		t.Fatalf("doctor report missing GIT001: %+v", report.Findings)
	}
}

func TestDoctorKnowledgeDetectsGovernanceRisks(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	writeTextFile(t, filepath.Join(project, ".worktrail", "index.md"), "# Worktrail Project Index\n\n- `architecture/old.md`\n")
	writeTextFile(t, filepath.Join(project, ".worktrail", "architecture", "old.md"), `# Old Architecture

## MVP scope

PRD notes, MVP boundary, user problem mixed into architecture.

## Acceptance criteria

Detailed acceptance criteria belong in requirements/, not architecture/.
`)
	writeTextFile(t, filepath.Join(project, ".worktrail", "requirements", "new.md"), `---worktrail
{
  "stage": "requirements",
  "topic": "delivery",
  "source_of_truth": true,
  "supersedes": ["architecture/old.md"]
}
---

# New Requirement

Current requirement-level source of truth.
`)
	writeTextFile(t, filepath.Join(project, ".worktrail", "requirements", "other.md"), `---worktrail
{
  "stage": "requirements",
  "topic": "delivery",
  "source_of_truth": true
}
---

# Other Requirement

Conflicting source of truth.
`)
	writeTextFile(t, filepath.Join(project, ".worktrail", "decisions", "mixed.md"), `---worktrail
{
  "stage": "planning"
}
---

# Mixed Decision

## PRD context

This describes MVP boundary, out-of-scope behavior, user goal, and acceptance criteria.

## Acceptance criteria

Acceptance criteria belong in requirements/, not decisions/.
`)

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("doctor knowledge unexpectedly passed:\n%s", out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, code := range []string{"SOT001", "STAGE001", "ARCH001", "DEC001", "REQ001", "SUPER001"} {
		if !hasKnowledgeFinding(report, code) {
			t.Fatalf("doctor knowledge report missing %s: %+v", code, report.Findings)
		}
	}
}

func TestDoctorKnowledgeDetectsFormalWriteEscapes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	if err := exec.Command("git", "-C", project, "init").Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	runApp(t, &out, &errb, "candidates", "create", "--id", "promoted-rule", "--type", "rule", "--target", "rules/promoted.md", "--title", "Promoted Rule", "Promoted rule body.")
	runApp(t, &out, &errb, "promote", "promoted-rule")
	if err := exec.Command("git", "-C", project, "add", ".worktrail").Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "direct.md"), "# Direct\n\nDirect formal edit.")

	out.Reset()
	err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("doctor knowledge should not fail on warnings without strict: %v stdout=%s", err, out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !hasKnowledgeFindingFor(report, "ESCAPE001", "rules/direct.md") || !hasKnowledgeFindingFor(report, "ESCAPE003", "rules/direct.md") {
		t.Fatalf("doctor report missing direct edit escapes: %+v", report.Findings)
	}
	if hasKnowledgeFindingFor(report, "ESCAPE003", "rules/promoted.md") {
		t.Fatalf("promoted formal file should have candidate trail: %+v", report.Findings)
	}
}

func TestDoctorKnowledgeDetectsFormalDeletionWhenGitReportsIt(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	if err := exec.Command("git", "-C", project, "init").Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	path := filepath.Join(project, ".worktrail", "rules", "deleted.md")
	writeTextFile(t, path, "# Deleted\n\nDeleted formal knowledge.")
	if err := exec.Command("git", "-C", project, "add", ".worktrail/rules/deleted.md").Run(); err != nil {
		t.Skipf("git add failed: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("doctor knowledge should not fail on warnings without strict: %v stdout=%s", err, out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !hasKnowledgeFindingFor(report, "ESCAPE005", "rules/deleted.md") {
		t.Fatalf("doctor report missing deletion escape: %+v", report.Findings)
	}
}

func TestDoctorKnowledgeSkipsBootstrapAndHandoffEscapeNoise(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	if err := exec.Command("git", "-C", project, "init").Run(); err != nil {
		t.Skipf("git init failed: %v", err)
	}
	runApp(t, &out, &errb, "handoff", "bootstrap handoff")
	runApp(t, &out, &errb, "index", "rebuild")

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("doctor knowledge should ignore bootstrap escape noise: %v stdout=%s", err, out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"project.md",
		"index.md",
		"rules/coding-rules.md",
		"rules/security-rules.md",
		"rules/testing-rules.md",
		"prompts/generate-config-draft.md",
		"prompts/project-review.md",
	} {
		if hasKnowledgeFindingFor(report, "ESCAPE001", path) || hasKnowledgeFindingFor(report, "ESCAPE003", path) {
			t.Fatalf("bootstrap doc should not raise escape warning for %s: %+v", path, report.Findings)
		}
	}
	for _, finding := range report.Findings {
		if strings.HasPrefix(finding.Path, "handoffs/") && (finding.Code == "ESCAPE001" || finding.Code == "ESCAPE003") {
			t.Fatalf("handoff should not raise escape noise: %+v", finding)
		}
	}
}

func TestDoctorKnowledgeSkipsTopicWarningForDecisionRuleAndLogDocs(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	writeTextFile(t, filepath.Join(project, ".worktrail", "decisions", "choice.md"), "# Choice\n\n## Decision\n\nAdopt the current path.\n")
	writeTextFile(t, filepath.Join(project, ".worktrail", "log.md"), "# Log\n\nOperational notes.\n")
	runApp(t, &out, &errb, "index", "rebuild")

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("doctor knowledge should not fail for topic-noise test: %v stdout=%s", err, out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"decisions/choice.md",
		"log.md",
		"rules/coding-rules.md",
		"rules/security-rules.md",
		"rules/testing-rules.md",
	} {
		if hasKnowledgeFindingFor(report, "TOPIC001", path) {
			t.Fatalf("doctor knowledge should not require topic for %s: %+v", path, report.Findings)
		}
	}
}

func TestDoctorKnowledgeIncludesHintsAndIndexFindings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	deletedPath := filepath.Join(project, ".worktrail", "decisions", "deleted.md")
	writeTextFile(t, filepath.Join(project, ".worktrail", "decisions", "choice.md"), "# Choice\n\nNo decision heading here.")
	writeTextFile(t, deletedPath, "# Deleted\n\nOld decision.")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "invalid-lifecycle.md"), "---worktrail\n{\n  \"title\": \"Invalid Lifecycle\",\n  \"type\": \"rule\",\n  \"lifecycle\": \"historic\"\n}\n---\n\nRule body.\n")
	runApp(t, &out, &errb, "index", "rebuild")
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "new.md"), "# New Rule\n\nFresh rule.")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("doctor knowledge should fail when lifecycle metadata is invalid:\n%s", out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	dec := knowledgeFindingByCode(report, "DEC001")
	if dec == nil || dec.Hint == "" {
		t.Fatalf("DEC001 should include a hint: %+v", report.Findings)
	}
	idxDeleted := knowledgeFindingByCode(report, "IDX001")
	if idxDeleted == nil || len(idxDeleted.Commands) == 0 || idxDeleted.Commands[0] != "worktrail index rebuild --scope project" {
		t.Fatalf("IDX001 should include rebuild guidance: %+v", report.Findings)
	}
	if !hasKnowledgeFindingFor(report, "IDX002", "rules/new.md") {
		t.Fatalf("doctor report missing IDX002 for unindexed rule: %+v", report.Findings)
	}
	if !hasKnowledgeFindingFor(report, "LIFE001", "rules/invalid-lifecycle.md") {
		t.Fatalf("doctor report missing LIFE001 for invalid lifecycle: %+v", report.Findings)
	}
}

func TestDoctorKnowledgeIgnoresGeneratedStorageDirectories(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	invalid := "---worktrail\n{\n  \"title\": \"Ignored Noise\",\n  \"stage\": \"planning\",\n  \"lifecycle\": \"historic\"\n}\n---\n\nShould be ignored.\n"
	writeTextFile(t, filepath.Join(project, ".worktrail", "staging", "drafts", "noise.md"), invalid)
	writeTextFile(t, filepath.Join(project, ".worktrail", "runtime", "checkpoints", "noise.md"), invalid)
	writeTextFile(t, filepath.Join(project, ".worktrail", "derived", "exports", "noise.md"), invalid)

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"doctor", "knowledge", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("doctor knowledge should ignore generated storage dirs: %v stdout=%s", err, out.String())
	}
	var report knowledgeDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"staging/drafts/noise.md", "runtime/checkpoints/noise.md", "derived/exports/noise.md"} {
		if hasKnowledgeFindingFor(report, "STAGE001", path) || hasKnowledgeFindingFor(report, "LIFE001", path) {
			t.Fatalf("doctor knowledge should ignore %s: %+v", path, report.Findings)
		}
	}
}

func TestDraftCreateValidatesSemanticContract(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"draft", "create", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/noise.md", "--title", "Noise", "--summary", "summary", "body"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "semantic candidate type") {
		t.Fatalf("draft create should reject evidence types: err=%v stdout=%s stderr=%s", err, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"draft", "create", "--type", "rule", "--target", "decisions/noise.md", "--title", "Noise", "--summary", "summary", "body"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "does not match target path") {
		t.Fatalf("draft create should reject mismatched target paths: err=%v stdout=%s stderr=%s", err, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"draft", "create", "--id", "draft-rule", "--type", "rule", "--target", "rules/draft-rule.md", "--title", "Draft Rule", "--summary", "summary", "Draft body."}, nil, &out, &errb); err != nil {
		t.Fatalf("draft create valid path failed: %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "candidates", "project", "draft-rule.md")); err != nil {
		t.Fatalf("draft create should write candidate file: %v", err)
	}

	out.Reset()
	errb.Reset()
	err = Run(context.Background(), []string{"draft", "create", "--type", "workflow", "--target", "workflows/draft-transcript.md", "--title", "Draft Transcript", "--summary", "summary", "# Draft\n\n- user: hi\n- assistant: hello"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "body contains raw transcript-style conversation") {
		t.Fatalf("draft create should reject transcript-style body: err=%v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
}

func TestCandidatesCreateRejectsUnsafeSemanticText(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	err := Run(context.Background(), []string{"candidates", "create", "--id", "unsafe-rule", "--type", "rule", "--target", "rules/unsafe-rule.md", "--title", "Unsafe Rule", "--summary", "Reach me at nick@example.com", "# Unsafe Rule\n\n- user: paste the transcript\n- assistant: pasted"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "summary contains redactable secret or PII pattern") || !strings.Contains(err.Error(), "body contains raw transcript-style conversation") {
		t.Fatalf("candidates create should reject unsafe semantic text: err=%v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
}

func TestDoctorDeleteClassifiesBlockersAndWarnings(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	target := "rules/target.md"
	writeTextFile(t, filepath.Join(project, ".worktrail", "project.md"), "# Project\n\nSee [Target](rules/target.md).")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "target.md"), "# Target\n\nRule body.")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "mention.md"), "# Mention\n\nPlain text rules/target.md reference.")
	writeWorktrailGovernance(t, project)
	writeTextFile(t, filepath.Join(project, "AGENTS.md"), "See rules/target.md before editing.\n")
	runApp(t, &out, &errb, "candidates", "create", "--id", "pending-target", "--type", "rule", "--target", target, "--title", "Pending Target", "See rules/target.md in body.")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"doctor", "delete", "--format", "json", target}, nil, &out, &errb)
	var report deleteDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if err == nil {
		t.Fatalf("doctor delete should fail when blockers exist: %+v", report)
	}
	if report.Safe {
		t.Fatalf("doctor delete unexpectedly marked report safe: %+v", report)
	}
	if !hasDeleteFindingKind(report.Blockers, "starter_link") || !hasDeleteFindingKind(report.Blockers, "candidate_target") {
		t.Fatalf("doctor delete missing blockers: %+v", report)
	}
	if !hasDeleteFindingKind(report.Warnings, "candidate_body") || !hasDeleteFindingKind(report.Warnings, "governance_text") || !hasDeleteFindingKind(report.Warnings, "body_text") {
		t.Fatalf("doctor delete missing warnings: %+v", report)
	}
	for _, finding := range append(append([]deleteDoctorFinding{}, report.Blockers...), report.Warnings...) {
		if finding.Scope == "" {
			t.Fatalf("doctor delete finding missing scope: %+v", report)
		}
	}
}

func TestDoctorDeleteScopeAllPreservesFindingScope(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, ".worktrail"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	writeTextFile(t, filepath.Join(home, ".worktrail", "project.md"), "# User Root\n\nSee [Target](rules/shared.md).")
	writeTextFile(t, filepath.Join(project, ".worktrail", "project.md"), "# Project Root\n\nSee [Target](rules/shared.md).")
	writeTextFile(t, filepath.Join(project, ".worktrail", "rules", "shared.md"), "# Shared\n\nRule body.")
	writeTextFile(t, filepath.Join(home, ".worktrail", "rules", "shared.md"), "# Shared\n\nRule body.")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"doctor", "delete", "--scope", "all", "--format", "json", "rules/shared.md"}, nil, &out, &errb)
	if err == nil {
		t.Fatalf("doctor delete --scope all should fail with blockers")
	}
	var report deleteDoctorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !hasDeleteFindingScope(report.Blockers, "project") || !hasDeleteFindingScope(report.Blockers, "user") {
		t.Fatalf("doctor delete all-scope report missing scope attribution: %+v", report)
	}
}

func TestContextLifecycleIncludeAndLegacyStageAlias(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	writeTextFile(t, filepath.Join(project, ".worktrail", "requirements", "current.md"), "---worktrail\n{\n  \"title\": \"Current Requirement\",\n  \"stage\": \"requirements\"\n}\n---\n\nCurrent body.\n")
	writeTextFile(t, filepath.Join(project, ".worktrail", "requirements", "historical.md"), "---worktrail\n{\n  \"title\": \"Historical Requirement\",\n  \"stage\": \"historical\"\n}\n---\n\nHistorical body.\n")

	defaultText := runApp(t, &out, &errb, "context", "lifecycle test")
	if strings.Contains(defaultText, "Historical Requirement") {
		t.Fatalf("default context should hide historical requirement:\n%s", defaultText)
	}
	historicalOnly := runApp(t, &out, &errb, "context", "--include-lifecycle", "historical", "lifecycle test")
	if !strings.Contains(historicalOnly, "Historical Requirement") || strings.Contains(historicalOnly, "Current Requirement") {
		t.Fatalf("include-lifecycle output unexpected:\n%s", historicalOnly)
	}
	legacyAlias := runApp(t, &out, &errb, "context", "--stage", "historical", "lifecycle test")
	if !strings.Contains(legacyAlias, "Historical Requirement") || strings.Contains(legacyAlias, "Current Requirement") {
		t.Fatalf("legacy stage alias output unexpected:\n%s", legacyAlias)
	}
}

func TestContextRejectsInvalidIncludeLifecycle(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	out.Reset()
	errb.Reset()
	err := Run(context.Background(), []string{"context", "--include-lifecycle", "historical,historcal", "lifecycle test"}, nil, &out, &errb)
	if err == nil {
		t.Fatal("context should reject invalid include-lifecycle values")
	}
	if !strings.Contains(err.Error(), `invalid lifecycle "historcal"`) {
		t.Fatalf("unexpected error: %v", err)
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
	if err := Run(context.Background(), []string{"candidates", "create", "--id", "handoff-noise", "--type", "handoff", "--target", "handoffs/noise.md", "--title", "Handoff Noise", "Operational handoff body."}, nil, &out, &errb); err != nil {
		t.Fatalf("Run candidates create handoff noise: %v stderr=%s", err, errb.String())
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
	if strings.Contains(out.String(), "note-1") || strings.Contains(out.String(), "handoff-noise") || !strings.Contains(out.String(), "Hidden evidence candidates: 2") || !strings.Contains(out.String(), "Hidden non-semantic pending candidates: 1") {
		t.Fatalf("review did not hide evidence or non-semantic candidates:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"review", "--evidence"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review evidence: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "note-1") || strings.Contains(out.String(), "rule-1") || strings.Contains(out.String(), "handoff-noise") {
		t.Fatalf("review evidence output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"review", "--all"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run review all: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "note-1") || !strings.Contains(out.String(), "rule-1") || !strings.Contains(out.String(), "handoff-noise") || strings.Contains(out.String(), "Hidden evidence candidates") || strings.Contains(out.String(), "Hidden non-semantic pending candidates") {
		t.Fatalf("review all output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"context", "task"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context: %v stderr=%s", err, errb.String())
	}
	if strings.Contains(out.String(), "note-1") || strings.Contains(out.String(), "Evidence body.") || !strings.Contains(out.String(), "Hidden evidence candidates: 2") || !strings.Contains(out.String(), "rule-1") {
		t.Fatalf("context did not hide transcript evidence:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"context", "--evidence", "task"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context evidence: %v stderr=%s", err, errb.String())
	}
	if !strings.Contains(out.String(), "note-1") || !strings.Contains(out.String(), "rule-1") || strings.Contains(out.String(), "Evidence body.") || strings.Contains(out.String(), "Hidden evidence candidates") {
		t.Fatalf("context evidence output unexpected:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"context", "--format", "json", "task"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run context json: %v stderr=%s", err, errb.String())
	}
	var contextJSON struct {
		HiddenEvidenceCandidates int `json:"hidden_evidence_candidates"`
		Maintenance              struct {
			PendingEvidenceCandidates   int      `json:"pending_evidence_candidates"`
			PendingSemanticCandidates   int      `json:"pending_semantic_candidates"`
			EvidenceLifecycleCandidates int      `json:"evidence_lifecycle_candidates"`
			NextSteps                   []string `json:"next_steps"`
		} `json:"maintenance"`
	}
	if err := json.Unmarshal(out.Bytes(), &contextJSON); err != nil {
		t.Fatal(err)
	}
	if contextJSON.HiddenEvidenceCandidates != 2 {
		t.Fatalf("context json hidden_evidence_candidates = %d, want 2", contextJSON.HiddenEvidenceCandidates)
	}
	if contextJSON.Maintenance.PendingEvidenceCandidates != 2 || contextJSON.Maintenance.PendingSemanticCandidates != 3 {
		t.Fatalf("context json maintenance counts unexpected: %+v", contextJSON.Maintenance)
	}
	if !containsString(contextJSON.Maintenance.NextSteps, "worktrail distill --pending --summary") || !containsString(contextJSON.Maintenance.NextSteps, "worktrail review plan --format json") {
		t.Fatalf("context json maintenance next_steps unexpected: %+v", contextJSON.Maintenance.NextSteps)
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
      "candidate_type": "workflow",
      "title": "Transcript Leak",
      "summary": "Conversation excerpt.",
      "target_path": "workflows/transcript-leak.md",
      "operation": "replace",
      "body": "# Transcript Leak\n\n- user: please fix the bug\n- assistant: here is the patch"
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
	if err := Run(context.Background(), []string{"distill", "validate", badSchema, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("bad schema json failure = %v stderr=%s", err, errb.String())
	}
	assertCLIErrorEnvelope(t, out.String(), "cli_usage_error")
	if !strings.Contains(out.String(), "proposal schema must be") {
		t.Fatalf("bad schema envelope missing message:\n%s", out.String())
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "validate", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill validate: %v stderr=%s", err, errb.String())
	}
	var validation wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if validation.Valid || validation.Blocked != 1 || len(validation.Items) != 7 || validation.Items[0].Status != "valid" || validation.Items[1].Status != "error" || validation.Items[2].Status != "blocked" || validation.Items[3].Status != "error" || validation.Items[4].Status != "error" || validation.Items[5].Status != "error" || validation.Items[6].Status != "error" {
		t.Fatalf("validation report unexpected: %+v", validation)
	}
	if !containsString(validation.Items[4].Errors, "candidate_type does not match target_path") {
		t.Fatalf("type-target mismatch errors unexpected: %+v", validation.Items[4])
	}
	if !containsString(validation.Items[5].Errors, "body contains raw transcript-style conversation") {
		t.Fatalf("transcript-style errors unexpected: %+v", validation.Items[5])
	}
	if !containsString(validation.Items[5].ErrorCodes, "body_raw_transcript_style_conversation") {
		t.Fatalf("transcript-style error codes unexpected: %+v", validation.Items[5])
	}
	if !containsString(validation.Items[6].Errors, "source candidate not found: missing-source") {
		t.Fatalf("missing source errors unexpected: %+v", validation.Items[6])
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
	if applied.Valid || applied.Created != 1 || applied.Blocked != 1 || len(applied.Items) != 7 || applied.Items[0].Status != "created" {
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

func TestDistillProposalEndToEndPromoteContextFromMigrationSource(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	runApp(t, &out, &errb, "candidates", "create", "--id", "active-log", "--type", model.CandidateTypeMigrationSource, "--target", "imports/kdd/project/active-knowledge-log.md", "--title", "KDD Active Log", "--tags", "kdd,migration_source", "Migration source evidence body.")

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
		t.Fatalf("Run distill validate migration source: %v stderr=%s", err, errb.String())
	}
	var validation wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &validation); err != nil {
		t.Fatal(err)
	}
	if !validation.Valid || len(validation.Items) != 1 || validation.Items[0].Status != "valid" {
		t.Fatalf("migration source validation report unexpected: %+v", validation)
	}

	out.Reset()
	if err := Run(context.Background(), []string{"distill", "apply", proposal, "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatalf("Run distill apply migration source: %v stderr=%s", err, errb.String())
	}
	var applied wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.Valid || applied.Created != 1 || len(applied.Items) != 1 || applied.Items[0].Status != "created" {
		t.Fatalf("migration source apply report unexpected: %+v", applied)
	}
	createdID := applied.Items[0].CandidateID

	text := runApp(t, &out, &errb, "review")
	if !strings.Contains(text, createdID) || !strings.Contains(text, "Distilled Term") || !strings.Contains(text, "migration_source, pending") {
		t.Fatalf("migration source review output unexpected:\n%s", text)
	}

	runApp(t, &out, &errb, "promote", createdID)
	runApp(t, &out, &errb, "index", "rebuild")
	text = runApp(t, &out, &errb, "context", "distilled term")
	if !strings.Contains(text, "glossary/distilled-term.md") || strings.Contains(text, "Definition from split source evidence.") {
		t.Fatalf("migration source promoted context output unexpected:\n%s", text)
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

func TestReviewShowsSourceCandidateIDsAndWarnings(t *testing.T) {
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
	runApp(t, &out, &errb, "candidates", "create", "--id", "old-note", "--type", model.CandidateTypeTranscriptNotes, "--target", "imports/transcripts/old-note.md", "--title", "Old Transcript Notes", "Old evidence body.")
	runApp(t, &out, &errb, "discard", "old-note")
	runApp(t, &out, &errb, "candidates", "create", "--id", "ordinary-lesson", "--type", "lesson", "--target", "lessons/ordinary.md", "--title", "Ordinary Lesson", "Ordinary lesson body.")

	env, err := paths.Discover()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := (candidate.Manager{Env: env, Actor: "test"}).Create(candidate.CreateRequest{
		Scope:              "project",
		ID:                 "source-aware-rule",
		CandidateType:      "rule",
		TargetPath:         "rules/source-aware.md",
		Title:              "Source Aware Rule",
		Summary:            "Review should show source evidence details.",
		Operation:          candidate.OperationReplace,
		SourceCandidateIDs: []string{"note-1", "old-note", "ordinary-lesson", "missing-source"},
		Body:               "# Source Aware Rule\n\nReview source visibility.",
	}); err != nil {
		t.Fatal(err)
	}

	text := runApp(t, &out, &errb, "review")
	for _, want := range []string{
		"source_candidate_ids: `note-1` (transcript_notes, pending, redaction=clean), `old-note` (transcript_notes, discarded, redaction=clean), `ordinary-lesson` (lesson, pending, redaction=clean), `missing-source` (missing)",
		"warnings: source_not_pending:old-note, source_not_evidence:ordinary-lesson, source_missing:missing-source",
		"next: worktrail candidates diff source-aware-rule",
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

func hasMigrationFinding(report migrationDoctorReport, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func hasKnowledgeFinding(report knowledgeDoctorReport, code string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}

func hasKnowledgeFindingFor(report knowledgeDoctorReport, code, path string) bool {
	for _, finding := range report.Findings {
		if finding.Code == code && finding.Path == path {
			return true
		}
	}
	return false
}

func hasDuplicateKDDCandidateIDs(items []kddmigration.Item) bool {
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

func knowledgeFindingByCode(report knowledgeDoctorReport, code string) *knowledgeFinding {
	for i := range report.Findings {
		if report.Findings[i].Code == code {
			return &report.Findings[i]
		}
	}
	return nil
}

func hasDeleteFindingKind(findings []deleteDoctorFinding, kind string) bool {
	for _, finding := range findings {
		if finding.Kind == kind {
			return true
		}
	}
	return false
}

func hasDeleteFindingScope(findings []deleteDoctorFinding, scope string) bool {
	for _, finding := range findings {
		if finding.Scope == scope {
			return true
		}
	}
	return false
}

func writeWorktrailGovernance(t *testing.T, project string) {
	t.Helper()
	body := "Use worktrail context before work, worktrail review before applying candidates, and worktrail handoff before ending a session.\n"
	writeTextFile(t, filepath.Join(project, "AGENTS.md"), body)
	writeTextFile(t, filepath.Join(project, "CLAUDE.md"), body)
}

func hasKDDSkippedPath(items []kddmigration.Item, sourcePath string) bool {
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
	if !strings.Contains(text, "smoke-rule") || strings.Contains(text, "smoke-note") || !strings.Contains(text, "Hidden evidence candidates: 1") {
		t.Fatalf("smoke review output unexpected:\n%s", text)
	}

	text = runApp(t, &out, &errb, "context", "smoke task")
	if !strings.Contains(text, "smoke-rule") || strings.Contains(text, "smoke-note") || strings.Contains(text, "Smoke evidence body.") || !strings.Contains(text, "Hidden evidence candidates: 1") {
		t.Fatalf("smoke context output unexpected:\n%s", text)
	}

	text = runApp(t, &out, &errb, "context", "--evidence", "smoke task")
	if !strings.Contains(text, "smoke-rule") || !strings.Contains(text, "smoke-note") || strings.Contains(text, "Smoke evidence body.") || strings.Contains(text, "Hidden evidence candidates") {
		t.Fatalf("smoke context evidence output unexpected:\n%s", text)
	}

	runApp(t, &out, &errb, "promote", "smoke-rule")
	runApp(t, &out, &errb, "index", "rebuild")
	text = runApp(t, &out, &errb, "context", "promoted smoke rule")
	if !strings.Contains(text, "rules/smoke-rule.md") || strings.Contains(text, "Smoke rule body.") {
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

func TestIndexDiffCommandReportsDeletedUnindexedAndChanged(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	currentPath := filepath.Join(project, ".worktrail", "rules", "current.md")
	deletedPath := filepath.Join(project, ".worktrail", "rules", "deleted.md")
	newPath := filepath.Join(project, ".worktrail", "rules", "new.md")
	writeTextFile(t, currentPath, "# Current\n\ncurrent body")
	writeTextFile(t, deletedPath, "# Deleted\n\ndeleted body")
	runApp(t, &out, &errb, "index", "rebuild")

	db, err := index.Load(filepath.Join(project, ".worktrail"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, currentPath, "# Current\n\nchanged body")
	writeTextFile(t, newPath, "# New\n\nnew body")
	later := db.GeneratedAt.Add(time.Hour)
	if err := os.Chtimes(currentPath, later, later); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(newPath, later, later); err != nil {
		t.Fatal(err)
	}

	text := runApp(t, &out, &errb, "index", "diff")
	for _, want := range []string{
		"scope: project",
		"stale: true",
		"deleted: 1",
		"unindexed: 1",
		"new: 1",
		"changed: 1",
		"rules/deleted.md",
		"rules/current.md",
		"rules/new.md",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("index diff missing %q:\n%s", want, text)
		}
	}
}

func TestSearchScopeAllRanksGlobally(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	for i := 0; i < 15; i++ {
		writeTextFile(t, filepath.Join(project, ".worktrail", "rules", fmt.Sprintf("project-%02d.md", i)),
			fmt.Sprintf("# Project %02d\n\nneedle shared keyword", i))
		writeTextFile(t, filepath.Join(home, "rules", fmt.Sprintf("user-%02d.md", i)),
			fmt.Sprintf("# User %02d\n\nneedle shared keyword", i))
	}
	runApp(t, &out, &errb, "index", "rebuild", "--scope", "project")
	runApp(t, &out, &errb, "index", "rebuild", "--scope", "user")

	out.Reset()
	errb.Reset()
	if err := Run(context.Background(), []string{"search", "--scope", "all", "--format", "json", "needle"}, nil, &out, &errb); err != nil {
		t.Fatalf("search scope all: %v stderr=%s", err, errb.String())
	}
	var results []index.Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("search JSON invalid: %v\n%s", err, out.String())
	}
	if len(results) != 20 {
		t.Fatalf("scope=all should return global top 20, got %d", len(results))
	}
	for i := 1; i < len(results); i++ {
		if results[i-1].Score < results[i].Score {
			t.Fatalf("scope=all results not globally ranked: %+v", results)
		}
	}
}

func TestSearchSkipsStaleDeletedAndChangedEntries(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	deletedPath := filepath.Join(project, ".worktrail", "rules", "deleted.md")
	changedPath := filepath.Join(project, ".worktrail", "rules", "changed.md")
	writeTextFile(t, deletedPath, "# Deleted\n\nneedle")
	writeTextFile(t, changedPath, "# Changed\n\nneedle")
	runApp(t, &out, &errb, "index", "rebuild")

	db, err := index.Load(filepath.Join(project, ".worktrail"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := os.Remove(deletedPath); err != nil {
		t.Fatal(err)
	}
	writeTextFile(t, changedPath, "# Changed\n\nneedle updated")
	later := db.GeneratedAt.Add(time.Hour)
	if err := os.Chtimes(changedPath, later, later); err != nil {
		t.Fatal(err)
	}

	text := runApp(t, &out, &errb, "search", "needle")
	if strings.Contains(text, "Deleted") {
		t.Fatalf("search returned deleted stale entry:\n%s", text)
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

func parseTabValue(t *testing.T, text, key string) string {
	t.Helper()
	prefix := key + "\t"
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	t.Fatalf("output missing %q in:\n%s", key, text)
	return ""
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

func nonAliasStateFiles(t *testing.T, pattern string) []string {
	t.Helper()
	matches, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if filepath.Base(match) == "latest.md" {
			continue
		}
		out = append(out, match)
	}
	return out
}
