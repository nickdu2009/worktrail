package eval

import (
	"fmt"
	"strings"
)

const (
	RangeRolePrimary = "primary"
	RangeRoleContext = "context"
	RangeRoleGroup   = "group"

	EvidenceRoleMatch    = "match"
	EvidenceRoleNeighbor = "neighbor"
)

// EvalByteRange is a zero-based, half-open UTF-8 byte range.
type EvalByteRange struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

func (r EvalByteRange) valid() bool {
	return r.StartByte >= 0 && r.EndByte >= r.StartByte
}

func (r EvalByteRange) covers(target EvalByteRange) bool {
	return r.valid() && target.valid() && r.StartByte <= target.StartByte && r.EndByte >= target.EndByte
}

// RequiredEvidence labels one evidence span that governed top-K must cover.
// range_role=primary|context|group selects which source range may satisfy the
// label. Group ranges must never satisfy a primary/context row/cell label.
type RequiredEvidence struct {
	Scope                string `json:"scope"`
	EntryID              string `json:"entry_id"`
	RangeRole            string `json:"range_role"`
	AllowedEvidenceRoles []string `json:"allowed_evidence_roles"`
	StartByte            int    `json:"start_byte"`
	EndByte              int    `json:"end_byte"`
	RowKey               string `json:"row_key,omitempty"`
	RequireNeighbor      bool   `json:"require_neighbor,omitempty"`
	StructuralGroupID    string `json:"structural_group_id,omitempty"`
}

func (r RequiredEvidence) target() EvalByteRange {
	return EvalByteRange{StartByte: r.StartByte, EndByte: r.EndByte}
}

func (r RequiredEvidence) valid() error {
	if r.Scope == "" || r.EntryID == "" {
		return fmt.Errorf("required evidence needs scope and entry_id")
	}
	switch r.RangeRole {
	case RangeRolePrimary, RangeRoleContext, RangeRoleGroup:
	default:
		return fmt.Errorf("required evidence range_role must be primary|context|group")
	}
	if !r.target().valid() || r.StartByte == r.EndByte {
		return fmt.Errorf("required evidence needs a non-empty byte range")
	}
	if len(r.AllowedEvidenceRoles) == 0 {
		return fmt.Errorf("required evidence needs allowed_evidence_roles")
	}
	for _, role := range r.AllowedEvidenceRoles {
		if role != EvidenceRoleMatch && role != EvidenceRoleNeighbor {
			return fmt.Errorf("unsupported evidence role %q", role)
		}
	}
	return nil
}

// RankedEvidenceChunk is one governed evidence chunk collected for evaluation.
type RankedEvidenceChunk struct {
	ChunkID           string         `json:"chunk_id"`
	EvidenceRole      string         `json:"evidence_role"`
	StructuralGroupID string         `json:"structural_group_id,omitempty"`
	RowKey            string         `json:"row_key,omitempty"`
	TokenCount        int            `json:"token_count,omitempty"`
	Primary           EvalByteRange  `json:"primary_source_range"`
	Context           *EvalByteRange `json:"context_source_range,omitempty"`
	Group             *EvalByteRange `json:"structural_group_source_range,omitempty"`
}

// RankedEntryEvidence is governed evidence for one final entry.
type RankedEntryEvidence struct {
	Scope   string                `json:"scope"`
	EntryID string                `json:"entry_id"`
	Chunks  []RankedEvidenceChunk `json:"chunks"`
}

// QueryDiagnostics captures structural and refill diagnostics for one query.
type QueryDiagnostics struct {
	DuplicateEntries             int            `json:"duplicate_entries"`
	DuplicateChunkEvidence       int            `json:"duplicate_chunk_evidence"`
	CrossGroupNeighborViolations int            `json:"cross_group_neighbor_violations"`
	RangeViolations              int            `json:"range_violations"`
	HardMaxViolations            int            `json:"hard_max_violations"`
	MaxEntryOccupancy            int            `json:"max_entry_occupancy"`
	EntryOccupancy               map[string]int `json:"entry_occupancy,omitempty"`
	RefillRounds                 int            `json:"refill_rounds"`
	WindowSaturated              bool           `json:"window_saturated"`
}

// EvidenceThresholds configures table evidence release gates.
type EvidenceThresholds struct {
	MinEvidenceRecallAtK         float64 `json:"min_evidence_recall_at_k"`
	RequireExactRowKey           bool    `json:"require_exact_row_key"`
	RequireNeighborCoverage      bool    `json:"require_neighbor_coverage"`
	MaxDuplicateEntries          int     `json:"max_duplicate_entries"`
	MaxDuplicateChunkEvidence    int     `json:"max_duplicate_chunk_evidence"`
	MaxCrossGroupNeighbor        int     `json:"max_cross_group_neighbor_violations"`
	MaxRangeViolations           int     `json:"max_range_violations"`
	MaxHardMaxViolations         int     `json:"max_hard_max_violations"`
	ForbidUnexpectedSaturation   bool    `json:"forbid_unexpected_saturation"`
	RequireAdversarialSaturation bool    `json:"require_adversarial_saturation"`
}

// EvidenceMetrics summarizes governed evidence quality across labeled queries.
type EvidenceMetrics struct {
	EvidenceRecallAtK      float64 `json:"evidence_recall_at_k"`
	ExactRowKeyCoverage    float64 `json:"exact_row_key_coverage"`
	NeighborCoverage       float64 `json:"neighbor_coverage"`
	DuplicateEntries       int     `json:"duplicate_entries"`
	DuplicateChunkEvidence int     `json:"duplicate_chunk_evidence"`
	CrossGroupViolations   int     `json:"cross_group_neighbor_violations"`
	RangeViolations        int     `json:"range_violations"`
	HardMaxViolations      int     `json:"hard_max_violations"`
	UnexpectedSaturation   int     `json:"unexpected_saturation"`
	MissingSaturation      int     `json:"missing_adversarial_saturation"`
	EvidenceQueries        int     `json:"evidence_queries"`
	RowKeyQueries          int     `json:"row_key_queries"`
	NeighborQueries        int     `json:"neighbor_queries"`
}

func DefaultEvidenceThresholds() EvidenceThresholds {
	return EvidenceThresholds{
		MinEvidenceRecallAtK:         0.90,
		RequireExactRowKey:           true,
		RequireNeighborCoverage:      true,
		MaxDuplicateEntries:          0,
		MaxDuplicateChunkEvidence:    0,
		MaxCrossGroupNeighbor:        0,
		MaxRangeViolations:           0,
		MaxHardMaxViolations:         0,
		ForbidUnexpectedSaturation:   true,
		RequireAdversarialSaturation: true,
	}
}

func validateEvidenceThresholds(t EvidenceThresholds) error {
	if t.MinEvidenceRecallAtK < 0 || t.MinEvidenceRecallAtK > 1 {
		return fmt.Errorf("evidence recall threshold must be between zero and one")
	}
	for _, value := range []int{
		t.MaxDuplicateEntries,
		t.MaxDuplicateChunkEvidence,
		t.MaxCrossGroupNeighbor,
		t.MaxRangeViolations,
		t.MaxHardMaxViolations,
	} {
		if value < 0 {
			return fmt.Errorf("evidence violation thresholds must not be negative")
		}
	}
	return nil
}

func scoreEvidence(
	labels RetrievalLabels,
	rankings RetrievalRankings,
	k int,
	thresholds EvidenceThresholds,
) (EvidenceMetrics, []string, error) {
	if err := validateEvidenceThresholds(thresholds); err != nil {
		return EvidenceMetrics{}, nil, err
	}
	byQuery := make(map[string]RetrievalQueryRankings, len(rankings.Queries))
	for _, item := range rankings.Queries {
		byQuery[item.QueryID] = item
	}

	var (
		metrics       EvidenceMetrics
		recallSum     float64
		rowKeySum     float64
		neighborSum   float64
		failureReasons []string
	)

	for _, query := range labels.Queries {
		item := byQuery[query.ID]
		governed := item.Lanes[LaneGoverned]
		if governed == nil {
			return EvidenceMetrics{}, nil, fmt.Errorf("query %q missing governed lane", query.ID)
		}
		top := governed
		if len(top) > k {
			top = top[:k]
		}
		evidenceByEntry := indexEntryEvidence(item.Evidence, top)
		diag := item.Diagnostics
		if diag == nil {
			diag = &QueryDiagnostics{}
		}
		metrics.DuplicateEntries += diag.DuplicateEntries
		metrics.DuplicateChunkEvidence += diag.DuplicateChunkEvidence
		metrics.CrossGroupViolations += diag.CrossGroupNeighborViolations
		metrics.RangeViolations += diag.RangeViolations
		metrics.HardMaxViolations += diag.HardMaxViolations

		expectSaturation := strings.EqualFold(query.Category, "adversarial_saturation")
		if expectSaturation {
			if thresholds.RequireAdversarialSaturation && !diag.WindowSaturated {
				metrics.MissingSaturation++
			}
		} else if thresholds.ForbidUnexpectedSaturation && diag.WindowSaturated {
			metrics.UnexpectedSaturation++
		}

		if len(query.RequiredEvidence) > 0 {
			metrics.EvidenceQueries++
			hits := 0
			for _, required := range query.RequiredEvidence {
				if evidenceSatisfies(required, evidenceByEntry[scopedKey(required.Scope, required.EntryID)]) {
					hits++
				}
			}
			recallSum += float64(hits) / float64(len(query.RequiredEvidence))
		}

		rowKeys := query.ExactRowKeys
		if len(rowKeys) == 0 {
			for _, required := range query.RequiredEvidence {
				if required.RowKey != "" {
					rowKeys = append(rowKeys, required.RowKey)
				}
			}
		}
		if len(rowKeys) > 0 {
			metrics.RowKeyQueries++
			found := 0
			for _, key := range rowKeys {
				if evidenceHasRowKey(evidenceByEntry, key) {
					found++
				}
			}
			rowKeySum += float64(found) / float64(len(rowKeys))
		}

		neighborNeeded := query.RequireNeighborCoverage
		for _, required := range query.RequiredEvidence {
			if required.RequireNeighbor {
				neighborNeeded = true
				break
			}
		}
		if neighborNeeded {
			metrics.NeighborQueries++
			if neighborCoverageOK(query.RequiredEvidence, evidenceByEntry) {
				neighborSum++
			}
		}
	}

	if metrics.EvidenceQueries > 0 {
		metrics.EvidenceRecallAtK = recallSum / float64(metrics.EvidenceQueries)
	}
	if metrics.RowKeyQueries > 0 {
		metrics.ExactRowKeyCoverage = rowKeySum / float64(metrics.RowKeyQueries)
	} else {
		metrics.ExactRowKeyCoverage = 1
	}
	if metrics.NeighborQueries > 0 {
		metrics.NeighborCoverage = neighborSum / float64(metrics.NeighborQueries)
	} else {
		metrics.NeighborCoverage = 1
	}

	if metrics.EvidenceQueries > 0 && metrics.EvidenceRecallAtK+1e-12 < thresholds.MinEvidenceRecallAtK {
		failureReasons = append(failureReasons, "evidence_recall_at_k")
	}
	if thresholds.RequireExactRowKey && metrics.RowKeyQueries > 0 && metrics.ExactRowKeyCoverage+1e-12 < 1 {
		failureReasons = append(failureReasons, "exact_row_key_coverage")
	}
	if thresholds.RequireNeighborCoverage && metrics.NeighborQueries > 0 && metrics.NeighborCoverage+1e-12 < 1 {
		failureReasons = append(failureReasons, "neighbor_coverage")
	}
	if metrics.DuplicateEntries > thresholds.MaxDuplicateEntries {
		failureReasons = append(failureReasons, "duplicate_entries")
	}
	if metrics.DuplicateChunkEvidence > thresholds.MaxDuplicateChunkEvidence {
		failureReasons = append(failureReasons, "duplicate_chunk_evidence")
	}
	if metrics.CrossGroupViolations > thresholds.MaxCrossGroupNeighbor {
		failureReasons = append(failureReasons, "cross_group_neighbor_violations")
	}
	if metrics.RangeViolations > thresholds.MaxRangeViolations {
		failureReasons = append(failureReasons, "range_violations")
	}
	if metrics.HardMaxViolations > thresholds.MaxHardMaxViolations {
		failureReasons = append(failureReasons, "hard_max_violations")
	}
	if metrics.UnexpectedSaturation > 0 {
		failureReasons = append(failureReasons, "unexpected_saturation")
	}
	if metrics.MissingSaturation > 0 {
		failureReasons = append(failureReasons, "missing_adversarial_saturation")
	}
	return metrics, failureReasons, nil
}

func indexEntryEvidence(evidence []RankedEntryEvidence, top []ScopedEntryID) map[string][]RankedEvidenceChunk {
	allowed := make(map[string]bool, len(top))
	for _, id := range top {
		allowed[id.Key()] = true
	}
	out := make(map[string][]RankedEvidenceChunk)
	for _, entry := range evidence {
		key := scopedKey(entry.Scope, entry.EntryID)
		if !allowed[key] {
			continue
		}
		out[key] = append(out[key], entry.Chunks...)
	}
	return out
}

func evidenceSatisfies(required RequiredEvidence, chunks []RankedEvidenceChunk) bool {
	if len(chunks) == 0 {
		return false
	}
	target := required.target()
	for _, chunk := range chunks {
		if !roleAllowed(required.AllowedEvidenceRoles, chunk.EvidenceRole) {
			continue
		}
		if required.StructuralGroupID != "" && chunk.StructuralGroupID != "" &&
			chunk.StructuralGroupID != required.StructuralGroupID {
			continue
		}
		if required.RowKey != "" && chunk.RowKey != "" && chunk.RowKey != required.RowKey {
			continue
		}
		switch required.RangeRole {
		case RangeRolePrimary:
			// Primary/context row labels may only be satisfied by the primary
			// range. Group ranges must never mask a wrong-row miss.
			if chunk.Primary.covers(target) {
				return true
			}
		case RangeRoleContext:
			if chunk.Context != nil && chunk.Context.covers(target) {
				return true
			}
		case RangeRoleGroup:
			if chunk.Group != nil && chunk.Group.covers(target) {
				return true
			}
		}
	}
	return false
}

func evidenceHasRowKey(evidence map[string][]RankedEvidenceChunk, rowKey string) bool {
	for _, chunks := range evidence {
		for _, chunk := range chunks {
			if chunk.RowKey == rowKey && chunk.Primary.valid() {
				return true
			}
		}
	}
	return false
}

func neighborCoverageOK(required []RequiredEvidence, evidence map[string][]RankedEvidenceChunk) bool {
	checked := false
	for _, item := range required {
		if !item.RequireNeighbor {
			continue
		}
		checked = true
		chunks := evidence[scopedKey(item.Scope, item.EntryID)]
		foundNeighbor := false
		for _, chunk := range chunks {
			if chunk.EvidenceRole != EvidenceRoleNeighbor {
				continue
			}
			if item.StructuralGroupID != "" && chunk.StructuralGroupID != item.StructuralGroupID {
				continue
			}
			foundNeighbor = true
			break
		}
		if !foundNeighbor {
			return false
		}
	}
	if checked {
		return true
	}
	// Category-level neighbor requirement: any governed evidence entry must
	// expose at least one same-group neighbor chunk.
	for _, chunks := range evidence {
		for _, chunk := range chunks {
			if chunk.EvidenceRole == EvidenceRoleNeighbor {
				return true
			}
		}
	}
	return false
}

func roleAllowed(allowed []string, role string) bool {
	for _, item := range allowed {
		if item == role {
			return true
		}
	}
	return false
}

func scopedKey(scope, entryID string) string {
	return scope + "\x00" + entryID
}
