// Package retrieve defines the dependency-injected semantic recall facade.
// It deliberately owns no database or application wiring.
package retrieve

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

// PolicyVersion identifies the ranking and governance policy.
const PolicyVersion = "semantic-retrieve-v1"

// Policy configures reciprocal-rank fusion and result governance.
type Policy struct {
	Version        string
	RRFK           int
	LaneTopK       int
	MaxPerDocument int
	FinalLimit     int
}

// DefaultPolicy returns a fresh copy of the stable initial semantic recall
// policy so callers cannot mutate the package default.
func DefaultPolicy() Policy {
	return Policy{
		Version:        PolicyVersion,
		RRFK:           60,
		LaneTopK:       50,
		MaxPerDocument: 2,
		FinalLimit:     10,
	}
}

// Request is one recall request. Scope is used only to make a rebuild next
// step actionable; it is not used to access any index.
type Request struct {
	Query string
	Mode  contracts.Mode
	Scope string
}

// LaneHit identifies one chunk ranked by one retrieval lane. Rank is
// one-based and is the only value used by reciprocal-rank fusion.
type LaneHit struct {
	ChunkID string
	EntryID string
	Rank    int
}

// Candidate is a fused chunk hit enriched with entry governance metadata.
// EntryHydrator is responsible for supplying DocumentID and the metadata.
type Candidate struct {
	ChunkID       string
	EntryID       string
	DocumentID    string
	Score         float64
	SourceOfTruth bool
	Active        bool
	Lifecycle     string
	Superseded    bool
}

// Lane describes the observable outcome of a retrieval lane.
type Lane struct {
	Name     string
	Degraded bool
	Reason   contracts.ReasonCode
}

// Response contains only recall-stage data. Callers decide how to present it.
type Response struct {
	PolicyVersion string
	Candidates    []Candidate
	Lanes         []Lane
	Degraded      bool
	Reason        contracts.ReasonCode
	NextStep      string
}

// Error exposes a stable semantic failure reason for errors.As callers.
type Error struct {
	Code    contracts.ReasonCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ChunkFTS retrieves lexical chunk hits from the injected backend.
type ChunkFTS interface {
	SearchChunks(ctx context.Context, query string, limit int) ([]LaneHit, error)
}

// VectorKNN retrieves vector chunk hits from the injected backend.
type VectorKNN interface {
	SearchChunks(ctx context.Context, embedding []float32, limit int) ([]LaneHit, error)
}

// QueryEmbedder creates a query vector for VectorKNN.
type QueryEmbedder interface {
	EmbedQuery(ctx context.Context, query string) ([]float32, error)
}

// EntryHydrator attaches entry metadata to fused candidates.
type EntryHydrator interface {
	Hydrate(ctx context.Context, candidates []Candidate) ([]Candidate, error)
}

// GenerationGate checks semantic-generation availability before any embedding
// work. A non-empty reason must accompany a non-nil error.
type GenerationGate interface {
	Check(ctx context.Context) (reason contracts.ReasonCode, err error)
}

// LegacyLexicalFallback runs the pre-semantic lexical recall path after a
// semantic generation gate failure. It returns presentation-ready candidates
// and is intentionally injected so this package does not depend on app or
// index implementations.
type LegacyLexicalFallback func(ctx context.Context, query string, limit int) ([]Candidate, error)

// Facade coordinates injected recall dependencies without opening a generation,
// querying SQL, or depending on the application command layer.
type Facade struct {
	Policy                Policy
	ChunkFTS              ChunkFTS
	VectorKNN             VectorKNN
	Embedder              QueryEmbedder
	Hydrator              EntryHydrator
	Gate                  GenerationGate
	LegacyLexicalFallback LegacyLexicalFallback
}

// Recall executes the selected recall mode.
func (f Facade) Recall(ctx context.Context, request Request) (Response, error) {
	policy, err := f.policy()
	if err != nil {
		return Response{}, err
	}
	if !request.Mode.Valid() {
		return Response{}, fmt.Errorf("invalid semantic mode %q", request.Mode)
	}

	if request.Mode != contracts.ModeLexical {
		reason, err := f.checkGeneration(ctx)
		if err != nil {
			if request.Mode == contracts.ModeRequired {
				return Response{}, &Error{
					Code:    reason,
					Message: "semantic generation is unavailable",
					Err:     err,
				}
			}
			return f.legacyLexicalFallback(ctx, request, policy, reason), nil
		}
	}

	lexicalHits, lexicalLane := f.searchLexical(ctx, request.Query, policy.LaneTopK)
	if request.Mode == contracts.ModeLexical {
		return f.finish(ctx, policy, lexicalHits, nil, []Lane{lexicalLane})
	}

	embedding, err := f.embed(ctx, request.Query)
	if err != nil {
		return f.finish(ctx, policy, lexicalHits, nil, []Lane{
			lexicalLane,
			degradedLane("vector_knn", contracts.ReasonRuntimeUnavailable),
		})
	}
	vectorHits, vectorLane := f.searchVector(ctx, embedding, policy.LaneTopK)
	return f.finish(ctx, policy, lexicalHits, vectorHits, []Lane{lexicalLane, vectorLane})
}

func (f Facade) policy() (Policy, error) {
	policy := f.Policy
	if policy == (Policy{}) {
		return DefaultPolicy(), nil
	}
	if policy.Version == "" {
		policy.Version = PolicyVersion
	}
	if policy.RRFK <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: RRFK must be positive")
	}
	if policy.LaneTopK <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: LaneTopK must be positive")
	}
	if policy.MaxPerDocument <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: MaxPerDocument must be positive")
	}
	if policy.FinalLimit <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: FinalLimit must be positive")
	}
	return policy, nil
}

func (f Facade) checkGeneration(ctx context.Context) (contracts.ReasonCode, error) {
	if f.Gate == nil {
		return contracts.ReasonRuntimeUnavailable, errors.New("semantic generation gate is not configured")
	}
	reason, err := f.Gate.Check(ctx)
	if err == nil {
		return "", nil
	}
	if reason == "" {
		reason = contracts.ReasonRuntimeUnavailable
	}
	return reason, err
}

func (f Facade) searchLexical(ctx context.Context, query string, limit int) ([]LaneHit, Lane) {
	if f.ChunkFTS == nil {
		return nil, degradedLane("chunk_fts", contracts.ReasonRuntimeUnavailable)
	}
	hits, err := f.ChunkFTS.SearchChunks(ctx, query, limit)
	if err != nil {
		return nil, degradedLane("chunk_fts", contracts.ReasonRuntimeUnavailable)
	}
	return hits, Lane{Name: "chunk_fts"}
}

func (f Facade) embed(ctx context.Context, query string) ([]float32, error) {
	if f.Embedder == nil {
		return nil, errors.New("query embedder is not configured")
	}
	return f.Embedder.EmbedQuery(ctx, query)
}

func (f Facade) searchVector(ctx context.Context, embedding []float32, limit int) ([]LaneHit, Lane) {
	if f.VectorKNN == nil {
		return nil, degradedLane("vector_knn", contracts.ReasonRuntimeUnavailable)
	}
	hits, err := f.VectorKNN.SearchChunks(ctx, embedding, limit)
	if err != nil {
		return nil, degradedLane("vector_knn", contracts.ReasonRuntimeUnavailable)
	}
	return hits, Lane{Name: "vector_knn"}
}

func (f Facade) finish(ctx context.Context, policy Policy, lexical, vector []LaneHit, lanes []Lane) (Response, error) {
	response := Response{
		PolicyVersion: policy.Version,
		Lanes:         lanes,
	}
	for _, lane := range lanes {
		if lane.Degraded {
			response.Degraded = true
			if response.Reason == "" {
				response.Reason = lane.Reason
			}
		}
	}

	candidates := fuse(policy.RRFK, lexical, vector)
	if len(candidates) == 0 {
		return response, nil
	}
	if f.Hydrator == nil {
		return Response{}, errors.New("entry hydrator is not configured")
	}
	hydrated, err := f.Hydrator.Hydrate(ctx, candidates)
	if err != nil {
		return Response{}, err
	}
	response.Candidates = govern(hydrated, policy)
	return response, nil
}

func (f Facade) legacyLexicalFallback(ctx context.Context, request Request, policy Policy, reason contracts.ReasonCode) Response {
	response := Response{
		PolicyVersion: policy.Version,
		Degraded:      true,
		Reason:        reason,
		NextStep:      rebuildNextStep(request.Scope),
	}
	if f.LegacyLexicalFallback == nil {
		response.Lanes = []Lane{degradedLane("legacy_lexical", contracts.ReasonRuntimeUnavailable)}
		return response
	}

	candidates, err := f.LegacyLexicalFallback(ctx, request.Query, policy.FinalLimit)
	if err != nil {
		response.Lanes = []Lane{degradedLane("legacy_lexical", contracts.ReasonRuntimeUnavailable)}
		return response
	}
	response.Candidates = candidates
	response.Lanes = []Lane{{Name: "legacy_lexical"}}
	return response
}

func rebuildNextStep(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "all"
	}
	return "worktrail semantic rebuild --scope " + scope
}

func degradedLane(name string, reason contracts.ReasonCode) Lane {
	return Lane{Name: name, Degraded: true, Reason: reason}
}

// FuseRanks merges lane hits with reciprocal-rank fusion. Rank is the only
// signal; raw scores are ignored. Exposed for release-engineering collectors.
func FuseRanks(k int, lanes ...[]LaneHit) []Candidate {
	return fuse(k, lanes...)
}

// ApplyGovernance filters superseded/non-current candidates and applies
// source-of-truth / active preference plus per-document diversity.
func ApplyGovernance(candidates []Candidate, policy Policy) []Candidate {
	return govern(candidates, policy)
}

func fuse(k int, lanes ...[]LaneHit) []Candidate {
	byChunk := make(map[string]Candidate)
	for _, hits := range lanes {
		for _, hit := range hits {
			if hit.ChunkID == "" || hit.EntryID == "" || hit.Rank <= 0 {
				continue
			}
			candidate := byChunk[hit.ChunkID]
			if candidate.ChunkID == "" {
				candidate.ChunkID = hit.ChunkID
				candidate.EntryID = hit.EntryID
			}
			candidate.Score += 1 / float64(k+hit.Rank)
			byChunk[hit.ChunkID] = candidate
		}
	}
	out := make([]Candidate, 0, len(byChunk))
	for _, candidate := range byChunk {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ChunkID < out[j].ChunkID
	})
	return out
}

func govern(candidates []Candidate, policy Policy) []Candidate {
	filtered := candidates[:0]
	for _, candidate := range candidates {
		if candidate.Superseded || isNonCurrent(candidate.Lifecycle) {
			continue
		}
		filtered = append(filtered, candidate)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].SourceOfTruth != filtered[j].SourceOfTruth {
			return filtered[i].SourceOfTruth
		}
		if filtered[i].Active != filtered[j].Active {
			return filtered[i].Active
		}
		return false
	})

	perDocument := make(map[string]int)
	out := make([]Candidate, 0, min(len(filtered), policy.FinalLimit))
	for _, candidate := range filtered {
		documentID := candidate.DocumentID
		if documentID == "" {
			documentID = candidate.EntryID
		}
		if perDocument[documentID] >= policy.MaxPerDocument {
			continue
		}
		perDocument[documentID]++
		out = append(out, candidate)
		if len(out) == policy.FinalLimit {
			break
		}
	}
	return out
}

func isNonCurrent(lifecycle string) bool {
	switch strings.ToLower(strings.TrimSpace(lifecycle)) {
	case "", "current":
		return false
	default:
		return true
	}
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
