package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/textsafety"
)

func TestBuildCLIErrorReportValidationError(t *testing.T) {
	err := textsafety.NewValidationError([]textsafety.Issue{
		{Field: "summary", Code: "summary_redactable_secret_or_pii", Message: "summary contains redactable secret or PII pattern"},
	})
	report := buildCLIErrorReport("worktrail note add", err)
	if report.Schema != cliErrorSchema || report.OK || report.Command != "worktrail note add" {
		t.Fatalf("unexpected report: %+v", report)
	}
	if !containsString(report.ErrorCodes, "summary_redactable_secret_or_pii") {
		t.Fatalf("error codes = %#v", report.ErrorCodes)
	}
	if len(report.Issues) != 1 || report.Issues[0].Field != "summary" {
		t.Fatalf("issues = %#v", report.Issues)
	}
}

func TestBuildCLIErrorReportBlocked(t *testing.T) {
	report := buildCLIErrorReport("worktrail promote", candidate.ErrBlocked)
	if !containsString(report.ErrorCodes, "body_blocked_sensitive_material") {
		t.Fatalf("error codes = %#v", report.ErrorCodes)
	}
}

func TestFailCLICommandJSONWritesEnvelopeAndReturnsNil(t *testing.T) {
	var out bytes.Buffer
	err := failCLICommand(IO{Out: &out}, "json", "worktrail note add", errors.New("note add requires --type"))
	if err != nil {
		t.Fatalf("failCLICommand returned %v", err)
	}
	assertCLIErrorEnvelope(t, out.String(), "cli_usage_error")
	if !strings.Contains(out.String(), "note add requires --type") {
		t.Fatalf("stdout = %s", out.String())
	}
}

func TestFailCLICommandTextReturnsError(t *testing.T) {
	var out bytes.Buffer
	err := failCLICommand(IO{Out: &out}, "text", "worktrail note add", errors.New("note add requires --type"))
	if err == nil || !strings.Contains(err.Error(), "note add requires --type") {
		t.Fatalf("failCLICommand text mode = %v stdout=%s", err, out.String())
	}
}

func TestNoteAddJSONFailureUsesCLIErrorEnvelope(t *testing.T) {
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

	err := Run(context.Background(), []string{
		"note", "add", "--format", "json",
		"--type", "workflow",
		"--target", "workflows/transcript.md",
		"--title", "Transcript Leak",
		"--summary", "Reach me at nick@example.com",
		"--evidence-label", "test",
		"# Transcript Leak\n\n- user: please fix the bug\n- assistant: here is the patch",
	}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("Run note add json failure = %v stderr=%s", err, errb.String())
	}
	assertCLIErrorEnvelope(t, out.String(), "summary_redactable_secret_or_pii", "body_raw_transcript_style_conversation")
}

func TestPathsDiscoverJSONFailureUsesCLIErrorEnvelope(t *testing.T) {
	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	brokenWD := filepath.Join(t.TempDir(), "broken-cwd")
	if err := os.MkdirAll(brokenWD, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(brokenWD); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(origWD)
	})
	if err := os.RemoveAll(brokenWD); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_PROJECT_ROOT", "")
	var out, errb bytes.Buffer
	err = Run(context.Background(), []string{"note", "add", "--format", "json"}, nil, &out, &errb)
	if err != nil {
		t.Fatalf("Run pre-dispatch json failure = %v stderr=%s", err, errb.String())
	}
	var report CLIErrorReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal envelope: %v stdout=%s", err, out.String())
	}
	if report.Schema != cliErrorSchema || report.OK {
		t.Fatalf("unexpected envelope: %+v", report)
	}
	if len(report.ErrorCodes) == 0 {
		t.Fatalf("expected error_codes in envelope: %+v", report)
	}
}

func assertCLIErrorEnvelope(t *testing.T, stdout string, wantCodes ...string) {
	t.Helper()
	var report CLIErrorReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("unmarshal envelope: %v stdout=%s", err, stdout)
	}
	if report.Schema != cliErrorSchema {
		t.Fatalf("schema = %q, want %q", report.Schema, cliErrorSchema)
	}
	if report.OK {
		t.Fatalf("ok = true, want false: %+v", report)
	}
	for _, want := range wantCodes {
		if !containsString(report.ErrorCodes, want) {
			t.Fatalf("missing error code %q in %+v", want, report)
		}
	}
}
