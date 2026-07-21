package eval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReportRetrievalFixturePasses(t *testing.T) {
	labels, rankings := loadRetrievalFixtureSet(t)
	report, err := ReportRetrieval(labels, rankings, DefaultRetrievalThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.FailureReasons) != 0 {
		t.Fatalf("unexpected failed report: %#v", report)
	}
	if report.Schema != RetrievalReportSchema || report.Queries != 2 {
		t.Fatalf("unexpected report metadata: %#v", report)
	}
	for _, lane := range report.Lanes {
		if lane.Lane != LaneRRF && lane.Lane != LaneGoverned {
			continue
		}
		if lane.VsEntryFTS != "ge" {
			t.Fatalf("lane %s vs entry_fts = %q, want ge", lane.Lane, lane.VsEntryFTS)
		}
	}
	// Governed gate is recall-focused; RRF still carries full ranking thresholds.
	for _, lane := range report.Lanes {
		if lane.Lane == LaneRRF && (lane.MRR < 0.9 || lane.NDCGAtK < 0.9) {
			t.Fatalf("rrf ranking metrics too low: %#v", lane)
		}
		if lane.Lane == LaneGoverned && lane.RecallAtK < 0.9 {
			t.Fatalf("governed recall too low: %#v", lane)
		}
	}
}

func TestReportRetrievalRejectsMissingLane(t *testing.T) {
	labels, rankings := loadRetrievalFixtureSet(t)
	delete(rankings.Queries[0].Lanes, LaneDense)
	if _, err := ReportRetrieval(labels, rankings, DefaultRetrievalThresholds()); err == nil {
		t.Fatal("expected missing lane error")
	}
}

func TestReportRetrievalFlagsBelowBaseline(t *testing.T) {
	labels, rankings := loadRetrievalFixtureSet(t)
	rankings.Queries[0].Lanes[LaneGoverned] = []ScopedEntryID{{Scope: "project", EntryID: "unrelated-entry"}}
	rankings.Queries[1].Lanes[LaneGoverned] = []ScopedEntryID{{Scope: "project", EntryID: "unrelated-entry"}}
	report, err := ReportRetrieval(labels, rankings, DefaultRetrievalThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatalf("expected failure, got %#v", report)
	}
	joined := strings.Join(report.FailureReasons, ",")
	if !strings.Contains(joined, "governed_below_entry_fts") {
		t.Fatalf("failure reasons = %v", report.FailureReasons)
	}
}

func TestReportRetrievalDoesNotGateGovernedMRROrNDCG(t *testing.T) {
	labels, rankings := loadRetrievalFixtureSet(t)
	for i := range rankings.Queries {
		rankings.Queries[i].Lanes[LaneGoverned] = append(
			[]ScopedEntryID{{Scope: "project", EntryID: "unrelated-entry"}},
			rankings.Queries[i].Lanes[LaneGoverned]...,
		)
	}

	report, err := ReportRetrieval(labels, rankings, DefaultRetrievalThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("governed MRR/nDCG must not fail the report: %#v", report)
	}
	if report.Thresholds.Governed.MRRAndNDCGPolicy != "reported_not_gated" {
		t.Fatalf("governed policy = %q", report.Thresholds.Governed.MRRAndNDCGPolicy)
	}
}

func TestRankedScopedEntryIDsDedupes(t *testing.T) {
	got := RankedScopedEntryIDs([]ScopedEntryID{
		{Scope: "project", EntryID: "a"},
		{Scope: "user", EntryID: "a"},
		{Scope: "project", EntryID: "a"},
		{Scope: "project", EntryID: "b"},
	})
	if len(got) != 3 || got[0].Scope != "project" || got[0].EntryID != "a" || got[1].Scope != "user" || got[2].EntryID != "b" {
		t.Fatalf("RankedScopedEntryIDs() = %#v", got)
	}
}

func loadRetrievalFixtureSet(t *testing.T) (RetrievalLabels, RetrievalRankings) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "semantic")
	labelsFile, err := os.Open(filepath.Join(root, "retrieval-labels-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer labelsFile.Close()
	rankingsFile, err := os.Open(filepath.Join(root, "retrieval-rankings-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer rankingsFile.Close()

	labels, err := DecodeRetrievalLabels(labelsFile)
	if err != nil {
		t.Fatal(err)
	}
	rankings, err := DecodeRetrievalRankings(rankingsFile)
	if err != nil {
		t.Fatal(err)
	}
	return labels, rankings
}
