package app

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestLegacyHandoffCandidateIsHiddenFromCandidateAndReviewSurfaces(t *testing.T) {
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

	for _, args := range [][]string{
		{"candidates", "list"},
		{"candidates", "list", "--type", "handoff"},
		{"candidates", "list", "--format", "json"},
		{"review", "--all"},
	} {
		out.Reset()
		errb.Reset()
		if err := Run(context.Background(), args, nil, &out, &errb); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(out.String(), "legacy-handoff") {
			t.Fatalf("%v leaked handoff candidate:\n%s", args, out.String())
		}
	}

	for _, args := range [][]string{
		{"candidates", "show", "legacy-handoff"},
		{"candidates", "diff", "legacy-handoff"},
		{"promote", "legacy-handoff"},
		{"merge", "legacy-handoff"},
	} {
		out.Reset()
		errb.Reset()
		err := Run(context.Background(), args, nil, &out, &errb)
		if !errors.Is(err, candidate.ErrHandoffCandidateMigrationRequired) {
			t.Fatalf("%v error = %v, want migration required", args, err)
		}
	}
}
