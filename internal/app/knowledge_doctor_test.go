package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestRequirementHeadingSignalCount(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "headings carry mvp scope and acceptance criteria",
			body: "# Title\n\n## MVP Constraints Preserved\n\nbody\n\n## Acceptance Criteria\n\nbody\n",
			want: 2,
		},
		{
			name: "single signal repeated across multiple headings",
			body: "# Title\n\n## 15. MVP 范围\n\nx\n\n### 15.1 MVP 必须做\n\ny\n\n### 15.2 MVP 可以不做\n\nz\n\n## MVP 边界\n\nw\n",
			want: 4,
		},
		{
			name: "signals only in body do not count",
			body: "# Title\n\nAll agents must preserve the MVP scope guard. The primary user is X. Acceptance criteria are documented elsewhere.\n\n## Implementation\n\nbody\n",
			want: 0,
		},
		{
			name: "persona substring should not match personal-accident or personality",
			body: "# Title\n\n## personal-accident product\n\nbody\n\n## personality-driven branding\n\nbody\n",
			want: 0,
		},
		{
			name: "persona as a real word in heading counts",
			body: "# Title\n\n## User personas\n\nbody\n",
			want: 1,
		},
		{
			name: "H1 title is ignored",
			body: "# Delivery Case As Primary User Entrypoint\n\nbody\n",
			want: 0,
		},
		{
			name: "H5 and deeper headings are ignored",
			body: "# Title\n\n##### MVP details\n\nbody\n",
			want: 0,
		},
		{
			name: "table column user goal in body does not count",
			body: "# Title\n\n## Workflow\n\n| Step | User goal | AI behavior |\n| --- | --- | --- |\n| 1 | win | ack |\n",
			want: 0,
		},
		{
			name: "out-of-scope and out of scope both detected",
			body: "# Title\n\n## Out of scope\n\nbody\n\n## Out-of-scope clarifications\n\nbody\n",
			want: 2,
		},
		{
			name: "heading containing both mvp and acceptance criteria counts once",
			body: "# Title\n\n## MVP acceptance criteria summary\n\nbody\n",
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requirementHeadingSignalCount(tc.body)
			if got != tc.want {
				t.Fatalf("requirementHeadingSignalCount: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequirementHeadingSignalCountTolerantOfCaseAndWhitespace(t *testing.T) {
	body := "# Title\n\n##    MVP   Scope   \n\nbody\n\n##\tAcceptance Criteria\n\nbody\n"
	if got := requirementHeadingSignalCount(body); got != 2 {
		t.Fatalf("expected 2 heading signal hits, got %d", got)
	}
}

func TestKnowledgeDoctorScanExcludesHandoffRuntimeSurface(t *testing.T) {
	root := t.TempDir()
	for path, body := range map[string]string{
		"rules/visible.md":        "# Visible\n\nRule.",
		"handoffs/local/local.md": "# Local\n\nRuntime handoff.",
		"handoffs/team/team.md":   "# Team\n\nRuntime handoff.",
		"handoffs/legacy-root.md": "# Legacy\n\nV1 handoff.",
	} {
		target := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	docs, err := scanKnowledgeDocs(root, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Path != "rules/visible.md" {
		t.Fatalf("knowledge doctor scanned runtime handoffs: %+v", docs)
	}
}

func TestKnowledgeDoctorDoesNotScanOrExposeLegacyHandoffCandidate(t *testing.T) {
	project := t.TempDir()
	env := paths.Env{
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	meta := model.Candidate{
		Schema: model.SchemaCandidate, ID: "legacy-handoff", Scope: "project",
		CandidateType: model.CandidateTypeHandoff, TargetPath: "handoffs/legacy.md",
		Title: "Legacy Handoff", Operation: candidate.OperationReplace, Status: candidate.StatusPending,
	}
	data, err := store.RenderMarkdown(meta, "# Legacy Handoff\n\nMigrate me.")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(env.ProjectWT, "candidates", "project", "legacy-handoff.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	report := buildKnowledgeDoctorReport(env, "project", false)
	for _, finding := range report.Findings {
		if finding.Code == "HANDOFF001" || strings.Contains(finding.Path, "legacy-handoff") ||
			strings.Contains(finding.Message, "handoff candidate") {
			t.Fatalf("knowledge doctor exposed migration-only handoff candidate: %+v", finding)
		}
	}
}
