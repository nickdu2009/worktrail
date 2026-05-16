package triggereval

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCases(t *testing.T) {
	cases, err := LoadCases(filepath.Join("..", "..", "testdata", "trigger-eval", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cases) != 21 {
		t.Fatalf("len(cases) = %d, want 21", len(cases))
	}
	seen := map[string]bool{}
	for _, c := range cases {
		seen[c.ID] = true
	}
	for _, id := range []string{
		"codex-handoff-new-conversation",
		"codex-distill-validate-proposal",
		"codex-maintain-evidence-plan",
	} {
		if !seen[id] {
			t.Fatalf("missing case %s", id)
		}
	}
}

func TestValidateCasesReportsCoverageGaps(t *testing.T) {
	err := ValidateCases([]Case{
		{
			ID:                "one",
			Tool:              ToolCodex,
			Skill:             SkillHandoff,
			Prompt:            "handoff",
			ExpectedBehavior:  "run handoff",
			ExpectedCommands:  []string{"worktrail handoff"},
			NegativeCase:      false,
			ForbiddenPatterns: []string{"worktrail promote"},
		},
	})
	if err == nil {
		t.Fatal("ValidateCases returned nil error")
	}
	if !strings.Contains(err.Error(), SkillContext) {
		t.Fatalf("error does not mention missing skill coverage: %v", err)
	}
}
