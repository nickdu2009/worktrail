package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	wtdistill "github.com/nickdu2009/worktrail/internal/distill"
)

type distillFixtureExpected struct {
	Command             string                       `json:"command"`
	ExpectErrorContains string                       `json:"expect_error_contains"`
	Valid               *bool                        `json:"valid"`
	Created             *int                         `json:"created"`
	Skipped             *int                         `json:"skipped"`
	Blocked             *int                         `json:"blocked"`
	Items               []distillFixtureExpectedItem `json:"items"`
}

type distillFixtureExpectedItem struct {
	ProposalIndex int      `json:"proposal_index"`
	Status        string   `json:"status"`
	CandidateID   string   `json:"candidate_id"`
	WarningCodes  []string `json:"warning_codes"`
	Errors        []string `json:"errors"`
}

func TestDistillProposalFixtures(t *testing.T) {
	cases := []string{
		"valid-basic",
		"valid-split-source",
		"invalid-schema",
		"invalid-target-path",
		"invalid-confidence",
		"blocked-secret",
		"duplicate-id",
		"target-exists",
		"invalid-sources",
	}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			runDistillFixture(t, name)
		})
	}
}

func runDistillFixture(t *testing.T, name string) {
	t.Helper()
	fixtureRoot := filepath.Join("..", "testdata", "distill", name)
	expected := readDistillFixtureExpected(t, filepath.Join(fixtureRoot, "expected-report.json"))
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)

	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	copyDistillFixtureState(t, fixtureRoot, home, project)

	command := expected.Command
	if command == "" {
		command = "apply"
	}
	out.Reset()
	err := Run(context.Background(), []string{"distill", command, filepath.Join(fixtureRoot, "proposal.json"), "--format", "json"}, nil, &out, &errb)
	if expected.ExpectErrorContains != "" {
		if err != nil {
			t.Fatalf("expected JSON envelope failure, got err=%v stdout=%s", err, out.String())
		}
		assertCLIErrorEnvelope(t, out.String())
		if !strings.Contains(out.String(), expected.ExpectErrorContains) {
			t.Fatalf("expected envelope containing %q, got stdout=%s", expected.ExpectErrorContains, out.String())
		}
		return
	}
	if err != nil {
		t.Fatalf("Run fixture %s: %v stderr=%s stdout=%s", name, err, errb.String(), out.String())
	}
	var report wtdistill.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	assertDistillFixtureReport(t, expected, report)
}

func readDistillFixtureExpected(t *testing.T, path string) distillFixtureExpected {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var expected distillFixtureExpected
	if err := json.Unmarshal(b, &expected); err != nil {
		t.Fatal(err)
	}
	return expected
}

func copyDistillFixtureState(t *testing.T, fixtureRoot, home, project string) {
	t.Helper()
	projectCandidateDir := filepath.Join(project, ".worktrail", "candidates", "project")
	copyDistillFixtureDir(t, filepath.Join(fixtureRoot, "seed-candidates", "project"), projectCandidateDir)
	copyDistillFixtureDir(t, filepath.Join(fixtureRoot, "existing-candidates", "project"), projectCandidateDir)
	copyDistillFixtureDir(t, filepath.Join(fixtureRoot, "formal-targets", "project"), filepath.Join(project, ".worktrail"))
	copyDistillFixtureDir(t, filepath.Join(fixtureRoot, "seed-candidates", "user"), filepath.Join(home, "candidates", "user"))
	copyDistillFixtureDir(t, filepath.Join(fixtureRoot, "existing-candidates", "user"), filepath.Join(home, "candidates", "user"))
	copyDistillFixtureDir(t, filepath.Join(fixtureRoot, "formal-targets", "user"), home)
}

func copyDistillFixtureDir(t *testing.T, src, dst string) {
	t.Helper()
	if _, err := os.Stat(src); os.IsNotExist(err) {
		return
	} else if err != nil {
		t.Fatal(err)
	}
	if err := filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, b, 0o644)
	}); err != nil {
		t.Fatal(err)
	}
}

func assertDistillFixtureReport(t *testing.T, expected distillFixtureExpected, report wtdistill.Report) {
	t.Helper()
	if expected.Valid != nil && report.Valid != *expected.Valid {
		t.Fatalf("valid = %v, want %v; report=%+v", report.Valid, *expected.Valid, report)
	}
	if expected.Created != nil && report.Created != *expected.Created {
		t.Fatalf("created = %d, want %d; report=%+v", report.Created, *expected.Created, report)
	}
	if expected.Skipped != nil && report.Skipped != *expected.Skipped {
		t.Fatalf("skipped = %d, want %d; report=%+v", report.Skipped, *expected.Skipped, report)
	}
	if expected.Blocked != nil && report.Blocked != *expected.Blocked {
		t.Fatalf("blocked = %d, want %d; report=%+v", report.Blocked, *expected.Blocked, report)
	}
	if len(report.Items) != len(expected.Items) {
		t.Fatalf("items = %d, want %d; report=%+v", len(report.Items), len(expected.Items), report)
	}
	for i, want := range expected.Items {
		got := report.Items[i]
		if got.ProposalIndex != want.ProposalIndex {
			t.Fatalf("item %d proposal_index = %d, want %d; item=%+v", i, got.ProposalIndex, want.ProposalIndex, got)
		}
		if want.Status != "" && got.Status != want.Status {
			t.Fatalf("item %d status = %s, want %s; item=%+v", i, got.Status, want.Status, got)
		}
		if want.CandidateID != "" && got.CandidateID != want.CandidateID {
			t.Fatalf("item %d candidate_id = %s, want %s; item=%+v", i, got.CandidateID, want.CandidateID, got)
		}
		for _, warning := range want.WarningCodes {
			if !containsString(got.WarningCodes, warning) {
				t.Fatalf("item %d missing warning %q; item=%+v", i, warning, got)
			}
		}
		for _, expectedError := range want.Errors {
			if !containsStringContaining(got.Errors, expectedError) {
				t.Fatalf("item %d missing error containing %q; item=%+v", i, expectedError, got)
			}
		}
	}
}

func containsStringContaining(values []string, want string) bool {
	for _, value := range values {
		if strings.Contains(value, want) {
			return true
		}
	}
	return false
}
