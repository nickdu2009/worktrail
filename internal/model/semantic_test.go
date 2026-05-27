package model

import "testing"

func TestIsSemanticCandidateType(t *testing.T) {
	cases := []struct {
		typ  string
		want bool
	}{
		{"project", true},
		{"index", true},
		{"architecture", true},
		{"decision", true},
		{"glossary", true},
		{"integration", true},
		{"lesson", true},
		{"prompt", true},
		{"requirement", true},
		{"rule", true},
		{"validation", true},
		{"workflow", true},
		{"", false},
		{"unknown", false},
		{"transcript_notes", false},
		{"migration_source", false},
	}
	for _, c := range cases {
		if got := IsSemanticCandidateType(c.typ); got != c.want {
			t.Errorf("IsSemanticCandidateType(%q) = %v, want %v", c.typ, got, c.want)
		}
	}
}

func TestSemanticTargetPathMatches(t *testing.T) {
	cases := []struct {
		typ, target string
		want        bool
	}{
		{"project", "project.md", true},
		{"project", "project", false},
		{"project", "project.md.bak", false},
		{"index", "index.md", true},
		{"index", "index", false},
		{"index", "architecture/index.md", false},
		{"architecture", "architecture/foo.md", true},
		{"architecture", "decisions/foo.md", false},
		{"decision", "decisions/foo.md", true},
		{"prompt", "prompts/x.md", true},
		{"prompt", "prompt/x.md", false},
		{"unknown", "anything.md", false},
	}
	for _, c := range cases {
		if got := SemanticTargetPathMatches(c.typ, c.target); got != c.want {
			t.Errorf("SemanticTargetPathMatches(%q, %q) = %v, want %v", c.typ, c.target, got, c.want)
		}
	}
}
