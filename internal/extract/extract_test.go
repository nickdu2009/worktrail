package extract

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestManualProviderCreatesStructuredCandidates(t *testing.T) {
	provider := ManualProvider{}
	out, err := provider.Extract(Input{
		Scope:          "project",
		Text:           `{"candidates":[{"title":"Add rule","summary":"Keep tests focused","candidate_type":"rule","target_path":"rules/testing.md","tags":["testing"]}]}`,
		SourceSessions: []string{"session-1"},
		Now:            time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	}, Schema{Name: "candidate"})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(out.Candidates))
	}
	candidate := out.Candidates[0]
	if candidate.Scope != "project" || candidate.CandidateType != "rule" || candidate.Status != "pending" {
		t.Fatalf("unexpected candidate: %+v", candidate)
	}
	if candidate.RedactionStatus != "unreviewed" || len(candidate.SourceSessions) != 1 {
		t.Fatalf("candidate missing safety metadata: %+v", candidate)
	}
}

func TestManualProviderCreatesTranscriptNotesCandidate(t *testing.T) {
	provider := ManualProvider{}
	out, err := provider.Extract(Input{
		Scope: "project",
		Text:  "- user: What remains for codexlite?\n- assistant: Add integration coverage.\n",
		Now:   time.Date(2026, 5, 3, 0, 0, 0, 0, time.UTC),
	}, Schema{Name: "candidate"})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(out.Candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(out.Candidates))
	}
	candidate := out.Candidates[0]
	if candidate.ID != "" || candidate.CandidateType != "lesson" || candidate.Title != "Transcript notes" {
		t.Fatalf("unexpected transcript candidate: %+v", candidate)
	}
	if !strings.Contains(candidate.Summary, "Add integration coverage") {
		t.Fatalf("candidate summary did not include transcript content: %s", candidate.Summary)
	}
}

func TestUnavailableProvidersReturnExplicitErrors(t *testing.T) {
	for _, provider := range []Provider{CodexProvider{}, ClaudeProvider{}} {
		_, err := provider.Extract(Input{Text: "anything"}, Schema{Name: "candidate"})
		if !errors.Is(err, ErrProviderUnavailable) {
			t.Fatalf("%s error = %v, want ErrProviderUnavailable", provider.Name(), err)
		}
	}
}
