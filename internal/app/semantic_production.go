package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/retrieve"
)

// productionSemanticSearcher constructs the local semantic runtime only for a
// semantic request. It never rebuilds an index or changes generation state.
type productionSemanticSearcher struct {
	env  paths.Env
	deps semanticProductionDependencies
}

type semanticProductionDependencies struct {
	discoverRoots func() (paths.SemanticRoots, error)
	build         func(composition.Input) (composition.Result, error)
	prepare       func(context.Context, composition.Result, paths.Env, SemanticSearchRequest) (productionSemanticRuntime, error)
}

type productionSemanticRuntime interface {
	Recall(context.Context, SemanticSearchRequest) (SemanticSearchResponse, error)
	Close() error
}

func newProductionSemanticSearcher(env paths.Env) SemanticSearcher {
	return productionSemanticSearcher{
		env:  env,
		deps: productionSemanticProductionDependencies(),
	}
}

func productionSemanticProductionDependencies() semanticProductionDependencies {
	return semanticProductionDependencies{
		discoverRoots: paths.DiscoverSemanticRoots,
		build:         composition.Build,
		prepare:       prepareProductionSemanticRuntime,
	}
}

func (s productionSemanticSearcher) Search(ctx context.Context, request SemanticSearchRequest) (SemanticSearchResponse, error) {
	roots, err := s.deps.discoverRoots()
	if err != nil {
		return s.unavailable(request, contracts.ReasonRuntimeUnavailable)
	}
	composed, err := s.deps.build(composition.Input{
		Roots:    roots,
		Versions: composition.DefaultSubsystemVersions(),
	})
	if err != nil {
		return s.unavailable(request, semanticProductionReason(err))
	}

	runtime, err := s.deps.prepare(ctx, composed, s.env, request)
	if err != nil {
		return s.unavailable(request, semanticProductionReason(err))
	}
	defer func() { _ = runtime.Close() }()

	response, err := runtime.Recall(ctx, request)
	if err != nil {
		return s.unavailable(request, semanticProductionReason(err))
	}
	return response, nil
}

func (s productionSemanticSearcher) unavailable(request SemanticSearchRequest, reason contracts.ReasonCode) (SemanticSearchResponse, error) {
	if reason == "" {
		reason = contracts.ReasonRuntimeUnavailable
	}
	if request.Mode == contracts.ModeRequired {
		return SemanticSearchResponse{}, &SemanticSearchError{Code: reason}
	}
	return SemanticSearchResponse{
		Degraded:        true,
		DegradedReasons: []contracts.ReasonCode{reason},
		NextSteps:       []string{semanticRepairNextStep(reason, request.Scope)},
	}, nil
}

func semanticRequestScopes(scope string) ([]string, error) {
	switch scope {
	case "project", "user":
		return []string{scope}, nil
	case "all":
		return []string{"user", "project"}, nil
	default:
		return nil, fmt.Errorf("invalid semantic search scope")
	}
}

func firstSemanticReason(response SemanticSearchResponse) contracts.ReasonCode {
	if len(response.DegradedReasons) > 0 && response.DegradedReasons[0] != "" {
		return response.DegradedReasons[0]
	}
	for _, lane := range response.Lanes {
		if lane.Degraded && lane.Reason != "" {
			return lane.Reason
		}
	}
	return contracts.ReasonRuntimeUnavailable
}

func semanticProductionReason(err error) contracts.ReasonCode {
	var compositionErr *composition.Error
	if errors.As(err, &compositionErr) && compositionErr.Code != "" {
		return compositionErr.Code
	}
	var generationErr *generation.Error
	if errors.As(err, &generationErr) && generationErr.Code != "" {
		return generationErr.Code
	}
	var daemonErr *daemon.Error
	if errors.As(err, &daemonErr) && daemonErr.Code != "" {
		return daemonErr.Code
	}
	var retrieveErr *retrieve.Error
	if errors.As(err, &retrieveErr) && retrieveErr.Code != "" {
		return retrieveErr.Code
	}
	if errors.Is(err, generation.ErrSourcesChanged) {
		return contracts.ReasonProfileStale
	}
	return contracts.ReasonRuntimeUnavailable
}

func prepareProductionSemanticRuntime(
	ctx context.Context,
	composed composition.Result,
	env paths.Env,
	request SemanticSearchRequest,
) (productionSemanticRuntime, error) {
	scopes, err := semanticRequestScopes(request.Scope)
	if err != nil {
		return nil, err
	}

	metadata, err := generation.NewRebuildMetadata(
		composed.Identity.RecallProfileID,
		composed.Identity.ModelSpaceID,
		composed.Identity.RecallProfile.SQLiteVecVersion,
		composed.Identity.ModelSpace.Dimension,
	)
	if err != nil {
		return nil, err
	}

	// Verify every participating scope before daemon startup so scope=all never
	// returns a partial semantic result after a later open/profile failure.
	opened := make([]productionOpenedScope, 0, len(scopes))
	closeOpened := func() {
		for i := len(opened) - 1; i >= 0; i-- {
			_ = opened[i].active.Close()
		}
	}

	var profiles []string
	for _, scope := range scopes {
		semanticDir, err := env.SemanticIndexRoot(scope)
		if err != nil {
			closeOpened()
			return nil, err
		}
		pointer, err := generation.ReadActive(semanticDir)
		if err != nil {
			closeOpened()
			return nil, err
		}
		profiles = append(profiles, pointer.RecallProfileID)
	}
	if len(profiles) > 1 {
		for i := 1; i < len(profiles); i++ {
			if profiles[i] != profiles[0] {
				closeOpened()
				return nil, &retrieve.Error{
					Code:    contracts.ReasonProfileMismatchAcrossScopes,
					Message: "semantic recall profiles differ across scopes",
				}
			}
		}
	}

	for _, scope := range scopes {
		root, err := env.ScopeRoot(scope)
		if err != nil {
			closeOpened()
			return nil, err
		}
		semanticDir, err := env.SemanticIndexRoot(scope)
		if err != nil {
			closeOpened()
			return nil, err
		}
		active, err := generation.OpenActive(ctx, semanticDir, metadata)
		if err != nil {
			closeOpened()
			return nil, err
		}
		query := index.Query{
			Scope: scope,
			Type:  request.Type,
			Topic: request.Topic,
			Tag:   request.Tag,
		}
		opened = append(opened, productionOpenedScope{
			scope:    scope,
			root:     root,
			active:   active,
			hydrator: retrieve.IndexEntryHydrator{Root: root, Query: query},
			query:    query,
		})
	}

	// Starting after all active generations have been opened prevents an all
	// scope request from returning a partial semantic result.
	if _, err := composed.Controller.Start(ctx); err != nil {
		closeOpened()
		return nil, err
	}

	filters := retrieve.ExactFilters{
		Type:  request.Type,
		Topic: request.Topic,
		Tag:   request.Tag,
	}
	backends := make([]retrieve.ScopeBackend, 0, len(opened))
	legacyAdapters := make([]retrieve.LegacyLexicalAdapter, 0, len(opened))
	for _, item := range opened {
		backends = append(backends, retrieve.ScopeBackend{
			Scope: item.scope,
			ChunkFTS: retrieve.GenerationChunkFTS{
				Active:    item.active,
				Tokenizer: index.NewTokenizer(),
				Filters:   filters,
				Scope:     item.scope,
			},
			VectorKNN: retrieve.GenerationVectorKNN{
				Active: item.active,
				Scope:  item.scope,
			},
			Hydrator:      item.hydrator,
			ChunkLoader:   retrieve.GenerationChunkLoader{Active: item.active},
			FiltersPushed: true,
		})
		legacyAdapters = append(legacyAdapters, retrieve.LegacyLexicalAdapter{
			Root:  item.root,
			Query: item.query,
		})
	}

	facade := retrieve.Facade{
		Policy:   retrieve.DefaultPolicy(),
		Backends: backends,
		Embedder: retrieve.DaemonQueryEmbedder{
			Embedder:    composed.Client,
			Credentials: composed.Store,
		},
		Gate:                  activeGenerationGate{},
		LegacyLexicalFallback: multiScopeLegacyLexicalFallback(legacyAdapters).Recall,
	}
	return &productionSemanticMultiRecall{
		opened:    opened,
		facade:    facade,
		profileID: composed.Identity.RecallProfileID,
	}, nil
}

type activeGenerationGate struct{}

func (activeGenerationGate) Check(context.Context) (contracts.ReasonCode, error) {
	return "", nil
}

type productionOpenedScope struct {
	scope    string
	root     string
	active   *generation.Active
	hydrator retrieve.IndexEntryHydrator
	query    index.Query
}

type productionSemanticMultiRecall struct {
	opened    []productionOpenedScope
	facade    retrieve.Facade
	profileID string
}

func (s *productionSemanticMultiRecall) Recall(ctx context.Context, request SemanticSearchRequest) (SemanticSearchResponse, error) {
	response, err := s.facade.Recall(ctx, retrieve.Request{
		Query: request.Query,
		Mode:  request.Mode,
		Scope: request.Scope,
		Filters: retrieve.ExactFilters{
			Type:  request.Type,
			Topic: request.Topic,
			Tag:   request.Tag,
		},
	})
	if err != nil {
		return SemanticSearchResponse{}, err
	}

	results, err := mapProductionCandidates(ctx, s.opened, response.Candidates)
	if err != nil {
		return SemanticSearchResponse{}, err
	}
	details := mapProductionEvidence(response.Evidence)
	if request.Limit > 0 && len(results) > request.Limit {
		results = results[:request.Limit]
		if len(details) > request.Limit {
			details = details[:request.Limit]
		}
	}

	lanes := make([]SemanticSearchLane, len(response.Lanes))
	for i, lane := range response.Lanes {
		lanes[i] = SemanticSearchLane{
			Scope:            lane.Scope,
			Name:             lane.Name,
			Degraded:         lane.Degraded,
			Reason:           lane.Reason,
			RawHits:          lane.RawHits,
			FilterRejections: lane.FilterRejections,
			EligibleEntries:  lane.EligibleEntries,
			RefillRounds:     lane.RefillRounds,
			HardCap:          lane.HardCap,
			WindowSaturated:  lane.WindowSaturated,
		}
	}
	searchResponse := SemanticSearchResponse{
		Results:  results,
		Details:  details,
		Policy:   response.PolicyVersion,
		Profile:  s.profileID,
		Lanes:    lanes,
		Degraded: response.Degraded,
		NextSteps: func() []string {
			if response.NextStep == "" {
				return nil
			}
			return []string{response.NextStep}
		}(),
	}
	if response.Reason != "" {
		searchResponse.DegradedReasons = []contracts.ReasonCode{response.Reason}
	} else {
		for _, lane := range lanes {
			if lane.Degraded && lane.Reason != "" {
				searchResponse.DegradedReasons = append(searchResponse.DegradedReasons, lane.Reason)
			}
		}
	}
	return searchResponse, nil
}

func (s *productionSemanticMultiRecall) Close() error {
	var first error
	for i := len(s.opened) - 1; i >= 0; i-- {
		if err := s.opened[i].active.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func mapProductionEvidence(evidence []retrieve.EntryEvidence) []SemanticSearchResultDetail {
	if len(evidence) == 0 {
		return nil
	}
	out := make([]SemanticSearchResultDetail, len(evidence))
	for i, item := range evidence {
		detail := SemanticSearchResultDetail{
			Ranks: SemanticSearchRanks{Final: item.FinalRank},
		}
		if item.LexicalRank > 0 {
			rank := item.LexicalRank
			detail.Ranks.Lexical = &rank
		}
		if item.SemanticRank > 0 {
			rank := item.SemanticRank
			detail.Ranks.Semantic = &rank
		}
		if len(item.Chunks) > 0 {
			detail.ChunkMatches = make([]SemanticChunkMatch, len(item.Chunks))
			for j, chunk := range item.Chunks {
				match := SemanticChunkMatch{
					ChunkID:           chunk.ChunkID,
					ChunkKind:         chunk.ChunkKind,
					StructuralGroupID: chunk.StructuralGroupID,
					HeadingBreadcrumb: append([]string(nil), chunk.HeadingBreadcrumb...),
					EvidenceRole:      chunk.EvidenceRole,
					Lanes:             append([]string(nil), chunk.Lanes...),
					PrimarySourceRange: SemanticByteRange{
						StartByte: chunk.PrimarySourceRange.StartByte,
						EndByte:   chunk.PrimarySourceRange.EndByte,
					},
				}
				if len(chunk.BestChunkRanks) > 0 {
					match.BestChunkRanks = make(map[string]int, len(chunk.BestChunkRanks))
					for lane, rank := range chunk.BestChunkRanks {
						match.BestChunkRanks[lane] = rank
					}
				}
				if chunk.ContextSourceRange != nil {
					match.ContextSourceRange = &SemanticByteRange{
						StartByte: chunk.ContextSourceRange.StartByte,
						EndByte:   chunk.ContextSourceRange.EndByte,
					}
				}
				if chunk.StructuralGroupSourceRange != nil {
					match.StructuralGroupSourceRange = &SemanticByteRange{
						StartByte: chunk.StructuralGroupSourceRange.StartByte,
						EndByte:   chunk.StructuralGroupSourceRange.EndByte,
					}
				}
				detail.ChunkMatches[j] = match
			}
		}
		out[i] = detail
	}
	return out
}

func mapProductionCandidates(ctx context.Context, opened []productionOpenedScope, candidates []retrieve.Candidate) ([]index.Result, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	byScope := make(map[string]retrieve.IndexEntryHydrator, len(opened))
	for _, item := range opened {
		byScope[item.scope] = item.hydrator
	}
	results := make([]index.Result, 0, len(candidates))
	for _, candidate := range candidates {
		hydrator, ok := byScope[candidate.Scope]
		if !ok {
			return nil, fmt.Errorf("semantic recall scope %q is not prepared", candidate.Scope)
		}
		mapped, err := hydrator.MapCandidates(ctx, []retrieve.Candidate{candidate})
		if err != nil {
			return nil, err
		}
		if len(mapped) == 0 {
			continue
		}
		results = append(results, mapped[0])
	}
	return results, nil
}

type multiScopeLegacyLexicalFallback []retrieve.LegacyLexicalAdapter

func (adapters multiScopeLegacyLexicalFallback) Recall(ctx context.Context, query string, limit int) ([]retrieve.Candidate, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("legacy lexical fallback limit must be greater than zero")
	}
	var merged []retrieve.Candidate
	for _, adapter := range adapters {
		candidates, err := adapter.Recall(ctx, query, limit)
		if err != nil {
			return nil, err
		}
		merged = append(merged, candidates...)
	}
	if len(merged) > limit {
		merged = merged[:limit]
	}
	return merged, nil
}
