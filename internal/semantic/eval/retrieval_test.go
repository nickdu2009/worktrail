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

func TestReportRetrievalGatesGovernedMRRAndNDCGAgainstEntryFTS(t *testing.T) {
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
	if report.Passed {
		t.Fatalf("governed MRR/nDCG must fail when below entry FTS: %#v", report)
	}
	joined := strings.Join(report.FailureReasons, ",")
	if !strings.Contains(joined, "governed_below_entry_fts") {
		t.Fatalf("failure reasons = %v", report.FailureReasons)
	}
}

func TestEvidenceRecallRejectsGroupRangeForPrimaryRowLabel(t *testing.T) {
	labels, rankings := loadTableRetrievalFixtureSet(t)
	// Replace correct primary evidence with a whole-table group range only.
	rankings.Queries[0].Evidence = []RankedEntryEvidence{{
		Scope:   "project",
		EntryID: "e2e-architecture-table-hardening-matrix",
		Chunks: []RankedEvidenceChunk{{
			ChunkID:           "wrong-row",
			EvidenceRole:      EvidenceRoleMatch,
			StructuralGroupID: "table-matrix",
			RowKey:            "wrong-key",
			Primary:           EvalByteRange{StartByte: 900, EndByte: 980},
			Group:             &EvalByteRange{StartByte: 100, EndByte: 980},
		}},
	}}
	report, err := ReportRetrieval(labels, rankings, DefaultRetrievalThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed {
		t.Fatalf("group range must not satisfy primary row labels: %#v", report)
	}
	joined := strings.Join(report.FailureReasons, ",")
	if !strings.Contains(joined, "evidence_recall_at_k") && !strings.Contains(joined, "exact_row_key_coverage") {
		t.Fatalf("failure reasons = %v", report.FailureReasons)
	}
}

func TestReportTableRetrievalFixturePasses(t *testing.T) {
	labels, rankings := loadTableRetrievalFixtureSet(t)
	report, err := ReportRetrieval(labels, rankings, DefaultRetrievalThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed {
		t.Fatalf("table fixture failed: %#v", report)
	}
	if report.Evidence.EvidenceRecallAtK+1e-12 < 0.90 {
		t.Fatalf("evidence recall = %v", report.Evidence.EvidenceRecallAtK)
	}
	if report.Evidence.ExactRowKeyCoverage != 1 || report.Evidence.NeighborCoverage != 1 {
		t.Fatalf("coverage metrics = %#v", report.Evidence)
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
	return loadNamedRetrievalFixtureSet(t, "retrieval-labels-fixture.json", "retrieval-rankings-fixture.json")
}

func loadTableRetrievalFixtureSet(t *testing.T) (RetrievalLabels, RetrievalRankings) {
	t.Helper()
	return loadNamedRetrievalFixtureSet(t, "table-retrieval-labels-fixture.json", "table-retrieval-rankings-fixture.json")
}

func loadNamedRetrievalFixtureSet(t *testing.T, labelsName, rankingsName string) (RetrievalLabels, RetrievalRankings) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "semantic")
	labelsFile, err := os.Open(filepath.Join(root, labelsName))
	if err != nil {
		t.Fatal(err)
	}
	defer labelsFile.Close()
	rankingsFile, err := os.Open(filepath.Join(root, rankingsName))
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
