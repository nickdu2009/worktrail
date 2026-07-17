package eval

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
)

const (
	RetrievalLabelsSchema   = "worktrail.semantic.eval.retrieval-labels.v1"
	RetrievalRankingsSchema = "worktrail.semantic.eval.retrieval-rankings.v1"
	RetrievalReportSchema   = "worktrail.semantic.eval.retrieval-report.v1"

	LaneEntryFTS = "entry_fts"
	LaneChunkFTS = "chunk_fts"
	LaneDense    = "dense"
	LaneRRF      = "rrf"
	LaneGoverned = "governed"
)

// RetrievalLabels is the release-engineering labeled query set.
type RetrievalLabels struct {
	Schema  string           `json:"schema"`
	Queries []RetrievalQuery `json:"queries"`
}

// RetrievalQuery names one labeled query and its relevant entry IDs.
type RetrievalQuery struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Category    string   `json:"category,omitempty"`
	RelevantIDs []string `json:"relevant_ids"`
}

// RetrievalRankings carries explicit per-lane entry ID rankings.
// Rankings must be collected from lane APIs; they must not be inferred from
// public search JSON envelopes.
type RetrievalRankings struct {
	Schema  string                   `json:"schema"`
	Queries []RetrievalQueryRankings `json:"queries"`
}

// RetrievalQueryRankings holds one query's lane rankings as ordered entry IDs.
type RetrievalQueryRankings struct {
	QueryID string              `json:"query_id"`
	Lanes   map[string][]string `json:"lanes"`
}

// RetrievalThresholds configures lane-specific metric gates for the retrieval report.
type RetrievalThresholds struct {
	K        int                         `json:"k"`
	RRF      RRFThresholds               `json:"rrf"`
	Governed GovernedRetrievalThresholds `json:"governed"`
}

// RRFThresholds require fused ranking quality to preserve every reported
// ranking metric relative to entry FTS and meet absolute thresholds.
type RRFThresholds struct {
	RequireVsEntryFTS bool    `json:"require_vs_entry_fts"`
	MinRecallAtK      float64 `json:"min_recall_at_k"`
	MinMRR            float64 `json:"min_mrr"`
	MinNDCGAtK        float64 `json:"min_ndcg_at_k"`
}

// GovernedRetrievalThresholds intentionally gate recall only. Governance may
// promote current source-of-truth records and therefore change MRR/nDCG.
// Those metrics remain reported for diagnosis but are not release thresholds.
type GovernedRetrievalThresholds struct {
	RequireRecallVsEntryFTS bool    `json:"require_recall_vs_entry_fts"`
	MinRecallAtK            float64 `json:"min_recall_at_k"`
	MRRAndNDCGPolicy        string  `json:"mrr_and_ndcg_at_k_policy"`
}

// DefaultRetrievalThresholds returns the initial release gate thresholds.
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
			RequireRecallVsEntryFTS: true,
			MinRecallAtK:            0.9,
			MRRAndNDCGPolicy:        "reported_not_gated",
		},
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
			relevant[id] = true
		}
		for _, name := range laneNames {
			ranked := item.Lanes[name]
			if ranked == nil {
				return RetrievalReport{}, fmt.Errorf("query %q missing lane %q", query.ID, name)
			}
			limit := min(thresholds.K, len(ranked))
			cutoff := ranked[:limit]
			a := accum[name]
			a.queries++
			a.recall += recallAtK(cutoff, relevant)
			a.mrr += reciprocalRank(ranked, relevant)
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
			// Fusion must beat or match lexical entry FTS on every ranking metric.
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
			// Governance may intentionally prefer active/source-of-truth docs and
			// reorder MRR/nDCG; still require relevant docs remain in the top-K
			// at least as often as entry FTS.
			if thresholds.Governed.RequireRecallVsEntryFTS {
				if metrics.RecallAtK+1e-12 >= entry.RecallAtK {
					metrics.VsEntryFTS = "ge"
				} else {
					metrics.VsEntryFTS = "lt"
					report.FailureReasons = append(report.FailureReasons, name+"_below_entry_fts")
				}
			}
			if metrics.RecallAtK < thresholds.Governed.MinRecallAtK {
				report.FailureReasons = append(report.FailureReasons, name+"_recall_at_k")
			}
		}
		report.Lanes = append(report.Lanes, metrics)
	}
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
			if id == "" {
				return fmt.Errorf("query %q has an empty relevant id", query.ID)
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
			if _, ok := item.Lanes[name]; !ok {
				return fmt.Errorf("query %q missing lane %q", item.QueryID, name)
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
	} {
		if value < 0 || value > 1 {
			return errors.New("retrieval quality thresholds must be between zero and one")
		}
	}
	if t.Governed.MRRAndNDCGPolicy != "reported_not_gated" {
		return errors.New("governed MRR/nDCG policy must be reported_not_gated")
	}
	return nil
}

// EncodeJSON writes indented JSON ending with a newline.
func EncodeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// RankedEntryIDs collapses chunk-level hits to first-seen entry IDs.
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
func SortedLaneNames(lanes map[string][]string) []string {
	names := make([]string, 0, len(lanes))
	for name := range lanes {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
