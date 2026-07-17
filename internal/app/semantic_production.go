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
	prepareScope  func(context.Context, composition.Result, paths.Env, string, SemanticSearchRequest) (semanticScopeRecall, error)
}

type semanticScopeRecall interface {
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
		prepareScope:  prepareProductionSemanticScope,
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

	scopes, err := semanticRequestScopes(request.Scope)
	if err != nil {
		return s.unavailable(request, contracts.ReasonRuntimeUnavailable)
	}
	runtimes := make([]semanticScopeRecall, 0, len(scopes))
	for _, scope := range scopes {
		runtime, err := s.deps.prepareScope(ctx, composed, s.env, scope, request)
		if err != nil {
			closeSemanticScopeRuntimes(runtimes)
			return s.unavailable(request, semanticProductionReason(err))
		}
		runtimes = append(runtimes, runtime)
	}
	defer closeSemanticScopeRuntimes(runtimes)

	// Starting after all active generations have been opened prevents an all
	// scope request from returning a partial semantic result.
	if _, err := composed.Controller.Start(ctx); err != nil {
		return s.unavailable(request, semanticProductionReason(err))
	}

	response := SemanticSearchResponse{Profile: composed.Identity.RecallProfileID}
	for _, runtime := range runtimes {
		scopeResponse, err := runtime.Recall(ctx, request)
		if err != nil {
			return s.unavailable(request, semanticProductionReason(err))
		}
		if semanticSearchDegraded(scopeResponse) {
			reason := firstSemanticReason(scopeResponse)
			return s.unavailable(request, reason)
		}
		if response.Policy == "" {
			response.Policy = scopeResponse.Policy
		}
		response.Lanes = append(response.Lanes, scopeResponse.Lanes...)
		response.Results = append(response.Results, scopeResponse.Results...)
	}
	response.Results = index.RankSearchResults(response.Results, request.Limit)
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
		NextSteps:       []string{semanticSearchRebuildStep(request.Scope)},
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

func closeSemanticScopeRuntimes(runtimes []semanticScopeRecall) {
	for i := len(runtimes) - 1; i >= 0; i-- {
		_ = runtimes[i].Close()
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

func prepareProductionSemanticScope(
	ctx context.Context,
	composed composition.Result,
	env paths.Env,
	scope string,
	request SemanticSearchRequest,
) (semanticScopeRecall, error) {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return nil, err
	}
	semanticDir, err := env.SemanticIndexRoot(scope)
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
	active, err := generation.OpenActive(ctx, semanticDir, metadata)
	if err != nil {
		return nil, err
	}

	query := index.Query{
		Scope: scope,
		Type:  request.Type,
		Topic: request.Topic,
		Tag:   request.Tag,
	}
	hydrator := retrieve.IndexEntryHydrator{Root: root, Query: query}
	facade := retrieve.Facade{
		Policy:    retrieve.DefaultPolicy(),
		ChunkFTS:  retrieve.GenerationChunkFTS{Active: active},
		VectorKNN: retrieve.GenerationVectorKNN{Active: active},
		Embedder: retrieve.DaemonQueryEmbedder{
			Embedder:    composed.Client,
			Credentials: composed.Store,
		},
		Hydrator: hydrator,
		Gate:     activeGenerationGate{},
		LegacyLexicalFallback: retrieve.LegacyLexicalAdapter{
			Root:  root,
			Query: query,
		}.Recall,
	}
	return productionSemanticScopeRecall{
		active:    active,
		facade:    facade,
		hydrator:  hydrator,
		profileID: composed.Identity.RecallProfileID,
	}, nil
}

type activeGenerationGate struct{}

func (activeGenerationGate) Check(context.Context) (contracts.ReasonCode, error) {
	return "", nil
}

type productionSemanticScopeRecall struct {
	active    *generation.Active
	facade    retrieve.Facade
	hydrator  retrieve.IndexEntryHydrator
	profileID string
}

func (s productionSemanticScopeRecall) Recall(ctx context.Context, request SemanticSearchRequest) (SemanticSearchResponse, error) {
	response, err := s.facade.Recall(ctx, retrieve.Request{
		Query: request.Query,
		Mode:  request.Mode,
		Scope: request.Scope,
	})
	if err != nil {
		return SemanticSearchResponse{}, err
	}
	results, err := s.hydrator.MapCandidates(ctx, response.Candidates)
	if err != nil {
		return SemanticSearchResponse{}, err
	}
	lanes := make([]SemanticSearchLane, len(response.Lanes))
	for i, lane := range response.Lanes {
		lanes[i] = SemanticSearchLane{
			Name:     lane.Name,
			Degraded: lane.Degraded,
			Reason:   lane.Reason,
		}
	}
	searchResponse := SemanticSearchResponse{
		Results:  results,
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
	}
	return searchResponse, nil
}

func (s productionSemanticScopeRecall) Close() error {
	return s.active.Close()
}
