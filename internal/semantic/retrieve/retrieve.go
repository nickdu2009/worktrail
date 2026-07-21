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
const PolicyVersion = "semantic-retrieve-v2"

const (
	LaneNameChunkFTS  = "chunk_fts"
	LaneNameVectorKNN = "vector_knn"
	LaneNameLegacyLex = "legacy_lexical"
)

// Policy configures reciprocal-rank fusion, bounded refill, and result limits.
type Policy struct {
	Version             string
	RRFK                int
	InitialWindow       int
	WindowGrowth        int
	HardCap             int
	EligibleEntryTarget int
	FinalLimit          int
}

// DefaultPolicy returns a fresh copy of the stable semantic-retrieve-v2 policy
// so callers cannot mutate the package default.
func DefaultPolicy() Policy {
	return Policy{
		Version:             PolicyVersion,
		RRFK:                60,
		InitialWindow:       50,
		WindowGrowth:        2,
		HardCap:             200,
		EligibleEntryTarget: 20,
		FinalLimit:          10,
	}
}

// ExactFilters are user-supplied exact predicates applied before lane entry ranks.
type ExactFilters struct {
	Type  string
	Topic string
	Tag   string
}

// Request is one recall request. Scope may be project, user, or all and is also
// used to make rebuild next steps actionable.
type Request struct {
	Query   string
	Mode    contracts.Mode
	Scope   string
	Filters ExactFilters
}

// RawChunkHit is one backend chunk before exact-filter collapse.
type RawChunkHit struct {
	ChunkID       string
	EntryID       string
	Rank          int
	DocumentType  string
	DocumentTopic string
	Tags          []string
}

// LaneHit identifies one scope-qualified entry ranked by one retrieval lane.
// Rank is the consecutive lane_entry_rank used by reciprocal-rank fusion.
type LaneHit struct {
	Scope         string
	ChunkID       string
	EntryID       string
	Rank          int
	BestChunkRank int
}

// Candidate is a fused entry hit enriched with entry governance metadata.
type Candidate struct {
	Scope         string
	ChunkID       string
	EntryID       string
	DocumentID    string
	Score         float64
	SourceOfTruth bool
	Active        bool
	Lifecycle     string
	Superseded    bool
}

// Lane describes the observable outcome of one (scope, lane) retrieval path.
type Lane struct {
	Scope            string
	Name             string
	Degraded         bool
	Reason           contracts.ReasonCode
	RawHits          int
	FilterRejections int
	EligibleEntries  int
	RefillRounds     int
	HardCap          int
	WindowSaturated  bool
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
	SearchChunks(ctx context.Context, query string, limit int) ([]RawChunkHit, error)
}

// VectorKNN retrieves vector chunk hits from the injected backend.
type VectorKNN interface {
	SearchChunks(ctx context.Context, embedding []float32, limit int) ([]RawChunkHit, error)
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
// semantic generation gate failure or total lane failure in auto mode.
type LegacyLexicalFallback func(ctx context.Context, query string, limit int) ([]Candidate, error)

// ScopeBackend binds one knowledge scope to its lane backends and hydrator.
type ScopeBackend struct {
	Scope     string
	ChunkFTS  ChunkFTS
	VectorKNN VectorKNN
	Hydrator  EntryHydrator
	// FiltersPushed reports whether ChunkFTS already applied exact filters in SQL.
	FiltersPushed bool
}

// Facade coordinates injected recall dependencies without opening a generation,
// querying SQL, or depending on the application command layer.
type Facade struct {
	Policy                Policy
	Backends              []ScopeBackend
	Embedder              QueryEmbedder
	Gate                  GenerationGate
	LegacyLexicalFallback LegacyLexicalFallback
}

// Recall executes the selected recall mode across configured scope backends.
func (f Facade) Recall(ctx context.Context, request Request) (Response, error) {
	policy, err := f.policy()
	if err != nil {
		return Response{}, err
	}
	if !request.Mode.Valid() {
		return Response{}, fmt.Errorf("invalid semantic mode %q", request.Mode)
	}
	if len(f.Backends) == 0 {
		return Response{}, errors.New("semantic recall requires at least one scope backend")
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

	var embedding []float32
	var embedErr error
	needsVector := request.Mode != contracts.ModeLexical
	if needsVector {
		embedding, embedErr = f.embed(ctx, request.Query)
	}

	type laneResult struct {
		hits []LaneHit
		lane Lane
		err  error
	}
	results := make([]laneResult, 0, len(f.Backends)*2)
	for _, backend := range f.Backends {
		lexical, lane, err := f.collectLexicalLane(ctx, backend, request, policy)
		results = append(results, laneResult{hits: lexical, lane: lane, err: err})
		if request.Mode == contracts.ModeLexical {
			continue
		}
		vector, vectorLane, vectorErr := f.collectVectorLane(ctx, backend, request, policy, embedding, embedErr)
		results = append(results, laneResult{hits: vector, lane: vectorLane, err: vectorErr})
	}

	lanes := make([]Lane, 0, len(results))
	var fusedLanes [][]LaneHit
	var laneErrors []error
	var firstLaneReason contracts.ReasonCode
	available := 0
	for _, result := range results {
		lanes = append(lanes, result.lane)
		if result.lane.Degraded {
			if firstLaneReason == "" {
				firstLaneReason = result.lane.Reason
			}
			laneErrors = append(laneErrors, result.err)
			continue
		}
		available++
		if len(result.hits) > 0 {
			fusedLanes = append(fusedLanes, result.hits)
		}
	}

	if request.Mode == contracts.ModeRequired {
		for _, result := range results {
			if result.lane.Degraded {
				reason := result.lane.Reason
				if reason == "" {
					reason = contracts.ReasonRuntimeUnavailable
				}
				return Response{}, &Error{
					Code:    reason,
					Message: "requested semantic lane failed",
					Err:     result.err,
				}
			}
		}
	}

	if request.Mode != contracts.ModeLexical && available == 0 {
		reason := firstLaneReason
		if reason == "" {
			reason = contracts.ReasonRuntimeUnavailable
		}
		if request.Mode == contracts.ModeAuto {
			return f.legacyLexicalFallback(ctx, request, policy, reason), nil
		}
	}

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

	candidates := FuseRanks(policy.RRFK, fusedLanes...)
	if len(candidates) == 0 {
		return response, nil
	}
	hydrated, err := f.hydrateAll(ctx, candidates)
	if err != nil {
		return Response{}, err
	}
	response.Candidates = ApplyGovernance(hydrated, policy)
	return response, nil
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
	if policy.InitialWindow <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: InitialWindow must be positive")
	}
	if policy.WindowGrowth < 2 {
		return Policy{}, errors.New("invalid retrieval policy: WindowGrowth must be >= 2")
	}
	if policy.HardCap < policy.InitialWindow {
		return Policy{}, errors.New("invalid retrieval policy: HardCap must be >= InitialWindow")
	}
	if policy.EligibleEntryTarget <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: EligibleEntryTarget must be positive")
	}
	if policy.FinalLimit <= 0 {
		return Policy{}, errors.New("invalid retrieval policy: FinalLimit must be positive")
	}
	return policy, nil
}

func (f Facade) checkGeneration(ctx context.Context) (contracts.ReasonCode, error) {
	if f.Gate == nil {
		return "", nil
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

func (f Facade) embed(ctx context.Context, query string) ([]float32, error) {
	if f.Embedder == nil {
		return nil, errors.New("query embedder is not configured")
	}
	return f.Embedder.EmbedQuery(ctx, query)
}

func (f Facade) collectLexicalLane(ctx context.Context, backend ScopeBackend, request Request, policy Policy) ([]LaneHit, Lane, error) {
	lane := Lane{Scope: backend.Scope, Name: LaneNameChunkFTS, HardCap: policy.HardCap}
	if backend.ChunkFTS == nil {
		lane.Degraded = true
		lane.Reason = contracts.ReasonRuntimeUnavailable
		return nil, lane, errors.New("chunk FTS backend is not configured")
	}
	provider := newPrefixProvider(func(limit int) ([]RawChunkHit, error) {
		return backend.ChunkFTS.SearchChunks(ctx, request.Query, limit)
	})
	hits, diagnostics, err := collectEntryLane(provider, policy, backend.Scope, request.Filters, !backend.FiltersPushed)
	lane.RawHits = diagnostics.RawHits
	lane.FilterRejections = diagnostics.FilterRejections
	lane.EligibleEntries = diagnostics.EligibleEntries
	lane.RefillRounds = diagnostics.RefillRounds
	lane.WindowSaturated = diagnostics.WindowSaturated
	if err != nil {
		lane.Degraded = true
		lane.Reason = classifyLaneError(err, true)
		return nil, lane, err
	}
	return hits, lane, nil
}

func (f Facade) collectVectorLane(
	ctx context.Context,
	backend ScopeBackend,
	request Request,
	policy Policy,
	embedding []float32,
	embedErr error,
) ([]LaneHit, Lane, error) {
	lane := Lane{Scope: backend.Scope, Name: LaneNameVectorKNN, HardCap: policy.HardCap}
	if embedErr != nil {
		lane.Degraded = true
		lane.Reason = classifyLaneError(embedErr, false)
		return nil, lane, embedErr
	}
	if backend.VectorKNN == nil {
		lane.Degraded = true
		lane.Reason = contracts.ReasonRuntimeUnavailable
		return nil, lane, errors.New("vector KNN backend is not configured")
	}
	provider := newPrefixProvider(func(limit int) ([]RawChunkHit, error) {
		return backend.VectorKNN.SearchChunks(ctx, embedding, limit)
	})
	hits, diagnostics, err := collectEntryLane(provider, policy, backend.Scope, request.Filters, true)
	lane.RawHits = diagnostics.RawHits
	lane.FilterRejections = diagnostics.FilterRejections
	lane.EligibleEntries = diagnostics.EligibleEntries
	lane.RefillRounds = diagnostics.RefillRounds
	lane.WindowSaturated = diagnostics.WindowSaturated
	if err != nil {
		lane.Degraded = true
		lane.Reason = classifyLaneError(err, false)
		return nil, lane, err
	}
	return hits, lane, nil
}

func (f Facade) hydrateAll(ctx context.Context, candidates []Candidate) ([]Candidate, error) {
	byScope := make(map[string][]Candidate)
	order := make([]string, 0, len(f.Backends))
	seenScope := make(map[string]bool)
	for _, backend := range f.Backends {
		if !seenScope[backend.Scope] {
			order = append(order, backend.Scope)
			seenScope[backend.Scope] = true
		}
	}
	for _, candidate := range candidates {
		byScope[candidate.Scope] = append(byScope[candidate.Scope], candidate)
	}

	hydrators := make(map[string]EntryHydrator, len(f.Backends))
	for _, backend := range f.Backends {
		hydrators[backend.Scope] = backend.Hydrator
	}

	hydratedByKey := make(map[string]Candidate, len(candidates))
	for _, scope := range order {
		scopeCandidates := byScope[scope]
		if len(scopeCandidates) == 0 {
			continue
		}
		hydrator := hydrators[scope]
		if hydrator == nil {
			return nil, fmt.Errorf("entry hydrator is not configured for scope %q", scope)
		}
		hydrated, err := hydrator.Hydrate(ctx, scopeCandidates)
		if err != nil {
			return nil, err
		}
		for _, candidate := range hydrated {
			hydratedByKey[candidateKey(candidate.Scope, candidate.EntryID)] = candidate
		}
	}

	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		hydrated, ok := hydratedByKey[candidateKey(candidate.Scope, candidate.EntryID)]
		if !ok {
			continue
		}
		hydrated.Score = candidate.Score
		hydrated.ChunkID = candidate.ChunkID
		out = append(out, hydrated)
	}
	return out, nil
}

func (f Facade) legacyLexicalFallback(ctx context.Context, request Request, policy Policy, reason contracts.ReasonCode) Response {
	response := Response{
		PolicyVersion: policy.Version,
		Degraded:      true,
		Reason:        reason,
		NextStep:      rebuildNextStep(request.Scope),
	}
	if f.LegacyLexicalFallback == nil {
		response.Lanes = []Lane{degradedLane("", LaneNameLegacyLex, contracts.ReasonRuntimeUnavailable)}
		return response
	}

	candidates, err := f.LegacyLexicalFallback(ctx, request.Query, policy.FinalLimit)
	if err != nil {
		response.Lanes = []Lane{degradedLane("", LaneNameLegacyLex, contracts.ReasonRuntimeUnavailable)}
		return response
	}
	response.Candidates = candidates
	response.Lanes = []Lane{{Name: LaneNameLegacyLex}}
	return response
}

func rebuildNextStep(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "all"
	}
	return "worktrail semantic rebuild --scope " + scope
}

func degradedLane(scope, name string, reason contracts.ReasonCode) Lane {
	return Lane{Scope: scope, Name: name, Degraded: true, Reason: reason}
}

func classifyLaneError(err error, lexical bool) contracts.ReasonCode {
	if err == nil {
		return contracts.ReasonRuntimeUnavailable
	}
	var typed *Error
	if errors.As(err, &typed) && typed.Code != "" {
		return typed.Code
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "fts"):
		return contracts.ReasonFTSQueryFailed
	case strings.Contains(message, "sqlite-vec") || strings.Contains(message, "sqlite_vec") || strings.Contains(message, "chunk_vec"):
		return contracts.ReasonSQLiteVecUnavailable
	case strings.Contains(message, "profile mismatch across scopes"):
		return contracts.ReasonProfileMismatchAcrossScopes
	case strings.Contains(message, "profile") && strings.Contains(message, "stale"):
		return contracts.ReasonProfileStale
	case strings.Contains(message, "generation"):
		return contracts.ReasonGenerationMissing
	case !lexical && (strings.Contains(message, "embed") || strings.Contains(message, "daemon") || strings.Contains(message, "runtime")):
		return contracts.ReasonRuntimeUnavailable
	case lexical:
		return contracts.ReasonFTSQueryFailed
	default:
		return contracts.ReasonRuntimeUnavailable
	}
}

// FuseRanks merges entry-ranked lane hits with reciprocal-rank fusion. Rank is
// the only signal; raw scores are ignored. Keys are (scope, entry_id).
func FuseRanks(k int, lanes ...[]LaneHit) []Candidate {
	return fuse(k, lanes...)
}

// ApplyGovernance filters superseded/non-current candidates and applies
// source-of-truth / active preference. Result slots are unique entries.
func ApplyGovernance(candidates []Candidate, policy Policy) []Candidate {
	return govern(candidates, policy)
}

func fuse(k int, lanes ...[]LaneHit) []Candidate {
	byKey := make(map[string]Candidate)
	for _, hits := range lanes {
		for _, hit := range hits {
			if hit.Scope == "" || hit.EntryID == "" || hit.Rank <= 0 {
				continue
			}
			key := candidateKey(hit.Scope, hit.EntryID)
			candidate := byKey[key]
			if candidate.EntryID == "" {
				candidate.Scope = hit.Scope
				candidate.EntryID = hit.EntryID
				candidate.ChunkID = hit.ChunkID
			}
			candidate.Score += 1 / float64(k+hit.Rank)
			byKey[key] = candidate
		}
	}
	out := make([]Candidate, 0, len(byKey))
	for _, candidate := range byKey {
		out = append(out, candidate)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Scope != out[j].Scope {
			return out[i].Scope < out[j].Scope
		}
		return out[i].EntryID < out[j].EntryID
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

	seen := make(map[string]bool)
	out := make([]Candidate, 0, min(len(filtered), policy.FinalLimit))
	for _, candidate := range filtered {
		key := candidateKey(candidate.Scope, candidate.EntryID)
		if seen[key] {
			continue
		}
		seen[key] = true
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

func candidateKey(scope, entryID string) string {
	return scope + "\x00" + entryID
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
