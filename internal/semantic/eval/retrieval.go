package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	RetrievalLabelsSchema   = "worktrail.semantic.eval.retrieval-labels.v2"
	RetrievalRankingsSchema = "worktrail.semantic.eval.retrieval-rankings.v2"
	RetrievalReportSchema   = "worktrail.semantic.eval.retrieval-report.v1"

	LaneEntryFTS = "entry_fts"
	LaneChunkFTS = "chunk_fts"
	LaneDense    = "dense"
	LaneRRF      = "rrf"
	LaneGoverned = "governed"
)

// ScopedEntryID is a scope-qualified knowledge identity used by retrieval eval v2.
type ScopedEntryID struct {
	Scope   string `json:"scope"`
	EntryID string `json:"entry_id"`
}

func (s ScopedEntryID) Key() string {
	return s.Scope + "\x00" + s.EntryID
}

func (s ScopedEntryID) valid() bool {
	return s.Scope != "" && s.EntryID != ""
}

// RetrievalLabels is the release-engineering labeled query set.
type RetrievalLabels struct {
	Schema  string           `json:"schema"`
	Queries []RetrievalQuery `json:"queries"`
}

// RetrievalQuery names one labeled query and its relevant scoped entry IDs.
type RetrievalQuery struct {
	ID                      string             `json:"id"`
	Text                    string             `json:"text"`
	Category                string             `json:"category,omitempty"`
	RelevantIDs             []ScopedEntryID    `json:"relevant_ids"`
	RequiredEvidence        []RequiredEvidence `json:"required_evidence,omitempty"`
	ExactRowKeys            []string           `json:"exact_row_keys,omitempty"`
	RequireNeighborCoverage bool               `json:"require_neighbor_coverage,omitempty"`
}

// RetrievalRankings carries explicit per-lane scoped entry ID rankings.
// Rankings must be collected from lane APIs; they must not be inferred from
// public search JSON envelopes.
type RetrievalRankings struct {
	Schema  string                   `json:"schema"`
	Queries []RetrievalQueryRankings `json:"queries"`
}

// RetrievalQueryRankings holds one query's lane rankings as ordered scoped IDs.
type RetrievalQueryRankings struct {
	QueryID     string                     `json:"query_id"`
	Lanes       map[string][]ScopedEntryID `json:"lanes"`
	Evidence    []RankedEntryEvidence      `json:"evidence,omitempty"`
	Diagnostics *QueryDiagnostics          `json:"diagnostics,omitempty"`
}

// RetrievalThresholds configures lane-specific metric gates for the retrieval report.
type RetrievalThresholds struct {
	K        int                         `json:"k"`
	RRF      RRFThresholds               `json:"rrf"`
	Governed GovernedRetrievalThresholds `json:"governed"`
	Evidence EvidenceThresholds          `json:"evidence"`
}

// RRFThresholds require fused ranking quality to preserve every reported
// ranking metric relative to entry FTS and meet absolute thresholds.
type RRFThresholds struct {
	RequireVsEntryFTS bool    `json:"require_vs_entry_fts"`
	MinRecallAtK      float64 `json:"min_recall_at_k"`
	MinMRR            float64 `json:"min_mrr"`
	MinNDCGAtK        float64 `json:"min_ndcg_at_k"`
}

// GovernedRetrievalThresholds gate Recall@K/MRR/nDCG@K against entry FTS for
// table-hardening release freeze, matching the RRF entry-lane contract.
type GovernedRetrievalThresholds struct {
	RequireVsEntryFTS bool    `json:"require_vs_entry_fts"`
	MinRecallAtK      float64 `json:"min_recall_at_k"`
	MinMRR            float64 `json:"min_mrr"`
	MinNDCGAtK        float64 `json:"min_ndcg_at_k"`
}

// DefaultRetrievalThresholds returns the table-hardening release gate thresholds.
func DefaultRetrievalThresholds() RetrievalThresholds {
	return RetrievalThresholds{
		K: 10,
		RRF: RRFThresholds{
			RequireVsEntryFTS: true,
			MinRecallAtK:      0.9,
			MinMRR:            0.9,
			MinNDCGAtK:        0.9,
		},
		Governed: GovernedRetrievalThresholds{
			RequireVsEntryFTS: true,
			MinRecallAtK:      0.9,
			MinMRR:            0.9,
			MinNDCGAtK:        0.9,
		},
		Evidence: DefaultEvidenceThresholds(),
	}
}

// LaneMetrics summarizes one retrieval lane across the labeled query set.
type LaneMetrics struct {
	Lane       string  `json:"lane"`
	RecallAtK  float64 `json:"recall_at_k"`
	MRR        float64 `json:"mrr"`
	NDCGAtK    float64 `json:"ndcg_at_k"`
	Queries    int     `json:"queries"`
	VsEntryFTS string  `json:"vs_entry_fts,omitempty"`
}

// RetrievalReport is the release-engineering retrieval quality report.
type RetrievalReport struct {
	Schema         string              `json:"schema"`
	Thresholds     RetrievalThresholds `json:"thresholds"`
	Queries        int                 `json:"queries"`
	Lanes          []LaneMetrics       `json:"lanes"`
	Evidence       EvidenceMetrics     `json:"evidence"`
	Passed         bool                `json:"passed"`
	FailureReasons []string            `json:"failure_reasons,omitempty"`
}

// DecodeRetrievalLabels reads and validates a labeled query set.
func DecodeRetrievalLabels(r io.Reader) (RetrievalLabels, error) {
	var labels RetrievalLabels
	if err := decodeOne(r, &labels); err != nil {
		return RetrievalLabels{}, err
	}
	if err := validateRetrievalLabels(labels); err != nil {
		return RetrievalLabels{}, err
	}
	return labels, nil
}

// DecodeRetrievalRankings reads and validates explicit lane rankings.
func DecodeRetrievalRankings(r io.Reader) (RetrievalRankings, error) {
	var rankings RetrievalRankings
	if err := decodeOne(r, &rankings); err != nil {
		return RetrievalRankings{}, err
	}
	if err := validateRetrievalRankings(rankings); err != nil {
		return RetrievalRankings{}, err
	}
	return rankings, nil
}

// ReportRetrieval computes Recall@K, MRR, and nDCG@K from explicit lane rankings.
func ReportRetrieval(labels RetrievalLabels, rankings RetrievalRankings, thresholds RetrievalThresholds) (RetrievalReport, error) {
	if err := validateRetrievalLabels(labels); err != nil {
		return RetrievalReport{}, err
	}
	if err := validateRetrievalRankings(rankings); err != nil {
		return RetrievalReport{}, err
	}
	if err := validateRetrievalThresholds(thresholds); err != nil {
		return RetrievalReport{}, err
	}

	byQuery := make(map[string]RetrievalQueryRankings, len(rankings.Queries))
	for _, item := range rankings.Queries {
		if _, exists := byQuery[item.QueryID]; exists {
			return RetrievalReport{}, fmt.Errorf("duplicate rankings for query %q", item.QueryID)
		}
		byQuery[item.QueryID] = item
	}
	for _, query := range labels.Queries {
		if _, ok := byQuery[query.ID]; !ok {
			return RetrievalReport{}, fmt.Errorf("missing rankings for query %q", query.ID)
		}
	}

	laneNames := []string{LaneEntryFTS, LaneChunkFTS, LaneDense, LaneRRF, LaneGoverned}
	accum := make(map[string]*laneAccum, len(laneNames))
	for _, name := range laneNames {
		accum[name] = &laneAccum{}
	}

	for _, query := range labels.Queries {
		item := byQuery[query.ID]
		relevant := make(map[string]bool, len(query.RelevantIDs))
		for _, id := range query.RelevantIDs {
			relevant[id.Key()] = true
		}
		for _, name := range laneNames {
			ranked := item.Lanes[name]
			if ranked == nil {
				return RetrievalReport{}, fmt.Errorf("query %q missing lane %q", query.ID, name)
			}
			keys := scopedKeys(ranked)
			limit := min(thresholds.K, len(keys))
			cutoff := keys[:limit]
			a := accum[name]
			a.queries++
			a.recall += recallAtK(cutoff, relevant)
			a.mrr += reciprocalRank(keys, relevant)
			a.ndcg += ndcgAtK(cutoff, relevant)
		}
	}

	report := RetrievalReport{
		Schema:     RetrievalReportSchema,
		Thresholds: thresholds,
		Queries:    len(labels.Queries),
	}
	entry := metricsFromAccum(LaneEntryFTS, accum[LaneEntryFTS])
	for _, name := range laneNames {
		metrics := metricsFromAccum(name, accum[name])
		switch name {
		case LaneRRF:
			if thresholds.RRF.RequireVsEntryFTS {
				metrics.VsEntryFTS = compareAgainstBaseline(metrics, entry)
				if metrics.VsEntryFTS != "ge" {
					report.FailureReasons = append(report.FailureReasons, name+"_below_entry_fts")
				}
			}
			if metrics.RecallAtK < thresholds.RRF.MinRecallAtK {
				report.FailureReasons = append(report.FailureReasons, name+"_recall_at_k")
			}
			if metrics.MRR < thresholds.RRF.MinMRR {
				report.FailureReasons = append(report.FailureReasons, name+"_mrr")
			}
			if metrics.NDCGAtK < thresholds.RRF.MinNDCGAtK {
				report.FailureReasons = append(report.FailureReasons, name+"_ndcg_at_k")
			}
		case LaneGoverned:
			if thresholds.Governed.RequireVsEntryFTS {
				metrics.VsEntryFTS = compareAgainstBaseline(metrics, entry)
				if metrics.VsEntryFTS != "ge" {
					report.FailureReasons = append(report.FailureReasons, name+"_below_entry_fts")
				}
			}
			if metrics.RecallAtK < thresholds.Governed.MinRecallAtK {
				report.FailureReasons = append(report.FailureReasons, name+"_recall_at_k")
			}
			if metrics.MRR < thresholds.Governed.MinMRR {
				report.FailureReasons = append(report.FailureReasons, name+"_mrr")
			}
			if metrics.NDCGAtK < thresholds.Governed.MinNDCGAtK {
				report.FailureReasons = append(report.FailureReasons, name+"_ndcg_at_k")
			}
		}
		report.Lanes = append(report.Lanes, metrics)
	}

	evidence, evidenceFailures, err := scoreEvidence(labels, rankings, thresholds.K, thresholds.Evidence)
	if err != nil {
		return RetrievalReport{}, err
	}
	report.Evidence = evidence
	report.FailureReasons = append(report.FailureReasons, evidenceFailures...)
	report.Passed = len(report.FailureReasons) == 0
	return report, nil
}

type laneAccum struct {
	queries int
	recall  float64
	mrr     float64
	ndcg    float64
}

func metricsFromAccum(name string, a *laneAccum) LaneMetrics {
	if a.queries == 0 {
		return LaneMetrics{Lane: name}
	}
	n := float64(a.queries)
	return LaneMetrics{
		Lane:      name,
		RecallAtK: a.recall / n,
		MRR:       a.mrr / n,
		NDCGAtK:   a.ndcg / n,
		Queries:   a.queries,
	}
}

func compareAgainstBaseline(lane, baseline LaneMetrics) string {
	if lane.RecallAtK+1e-12 >= baseline.RecallAtK &&
		lane.MRR+1e-12 >= baseline.MRR &&
		lane.NDCGAtK+1e-12 >= baseline.NDCGAtK {
		return "ge"
	}
	return "lt"
}

func validateRetrievalLabels(labels RetrievalLabels) error {
	if labels.Schema != RetrievalLabelsSchema {
		return fmt.Errorf("unexpected retrieval labels schema %q", labels.Schema)
	}
	if len(labels.Queries) == 0 {
		return errors.New("retrieval labels require queries")
	}
	seen := map[string]bool{}
	for _, query := range labels.Queries {
		if query.ID == "" || query.Text == "" || len(query.RelevantIDs) == 0 || seen[query.ID] {
			return fmt.Errorf("invalid or duplicate retrieval query %q", query.ID)
		}
		seen[query.ID] = true
		for _, id := range query.RelevantIDs {
			if !id.valid() {
				return fmt.Errorf("query %q has an invalid relevant id", query.ID)
			}
		}
		for _, evidence := range query.RequiredEvidence {
			if err := evidence.valid(); err != nil {
				return fmt.Errorf("query %q required evidence: %w", query.ID, err)
			}
		}
	}
	return nil
}

func validateRetrievalRankings(rankings RetrievalRankings) error {
	if rankings.Schema != RetrievalRankingsSchema {
		return fmt.Errorf("unexpected retrieval rankings schema %q", rankings.Schema)
	}
	if len(rankings.Queries) == 0 {
		return errors.New("retrieval rankings require queries")
	}
	seen := map[string]bool{}
	required := []string{LaneEntryFTS, LaneChunkFTS, LaneDense, LaneRRF, LaneGoverned}
	for _, item := range rankings.Queries {
		if item.QueryID == "" || seen[item.QueryID] {
			return fmt.Errorf("invalid or duplicate ranking query %q", item.QueryID)
		}
		seen[item.QueryID] = true
		if item.Lanes == nil {
			return fmt.Errorf("query %q has no lanes", item.QueryID)
		}
		for _, name := range required {
			ids, ok := item.Lanes[name]
			if !ok {
				return fmt.Errorf("query %q missing lane %q", item.QueryID, name)
			}
			for _, id := range ids {
				if !id.valid() {
					return fmt.Errorf("query %q lane %q has an invalid scoped id", item.QueryID, name)
				}
			}
		}
	}
	return nil
}

func validateRetrievalThresholds(t RetrievalThresholds) error {
	if t.K <= 0 {
		return errors.New("retrieval threshold k must be positive")
	}
	for _, value := range []float64{
		t.RRF.MinRecallAtK,
		t.RRF.MinMRR,
		t.RRF.MinNDCGAtK,
		t.Governed.MinRecallAtK,
		t.Governed.MinMRR,
		t.Governed.MinNDCGAtK,
	} {
		if value < 0 || value > 1 {
			return errors.New("retrieval quality thresholds must be between zero and one")
		}
	}
	return validateEvidenceThresholds(t.Evidence)
}

// EncodeJSON writes indented JSON ending with a newline.
func EncodeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// RankedScopedEntryIDs collapses hits to first-seen scope-qualified entry IDs.
func RankedScopedEntryIDs(ids []ScopedEntryID) []ScopedEntryID {
	seen := map[string]bool{}
	out := make([]ScopedEntryID, 0, len(ids))
	for _, id := range ids {
		if !id.valid() || seen[id.Key()] {
			continue
		}
		seen[id.Key()] = true
		out = append(out, id)
	}
	return out
}

// RankedEntryIDs collapses unscoped entry IDs for compatibility helpers.
func RankedEntryIDs(entryIDs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(entryIDs))
	for _, id := range entryIDs {
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// SortedLaneNames returns a stable lane name order for reports.
func SortedLaneNames(lanes map[string][]ScopedEntryID) []string {
	names := make([]string, 0, len(lanes))
	for name := range lanes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func scopedKeys(ids []ScopedEntryID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Key()
	}
	return out
}
