package app

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/retrieve"
)

func TestProductionSemanticSearcherUsesPreparedRuntimeOnce(t *testing.T) {
	controller := &semanticController{}
	runtime := &semanticRuntimeStub{response: SemanticSearchResponse{
		Policy:  "semantic-retrieve-v2",
		Profile: "profile",
		Lanes: []SemanticSearchLane{
			{Scope: "user", Name: "chunk_fts"},
			{Scope: "user", Name: "vector_knn"},
			{Scope: "project", Name: "chunk_fts"},
			{Scope: "project", Name: "vector_knn"},
		},
		Results: []index.Result{
			{Entry: index.Entry{Scope: "project", Path: "rules/project.md", Title: "project", ID: "shared"}, Score: 0.03},
			{Entry: index.Entry{Scope: "user", Path: "rules/user.md", Title: "user", ID: "shared"}, Score: 0.02},
		},
	}}
	var prepared int
	searcher := productionSemanticSearcher{
		env: paths.Env{},
		deps: semanticProductionDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
			build: func(input composition.Input) (composition.Result, error) {
				if input.Versions != composition.DefaultSubsystemVersions() {
					t.Fatalf("subsystem versions = %#v", input.Versions)
				}
				return composition.Result{Controller: controller}, nil
			},
			prepare: func(_ context.Context, _ composition.Result, _ paths.Env, request SemanticSearchRequest) (productionSemanticRuntime, error) {
				prepared++
				if request.Scope != "all" || request.Query != "semantic query" || request.Type != "rule" || request.Topic != "semantic" || request.Tag != "tag" || request.Mode != contracts.ModeRequired || request.Limit != 2 {
					t.Fatalf("request = %#v", request)
				}
				return runtime, nil
			},
		},
	}

	response, err := searcher.Search(context.Background(), SemanticSearchRequest{
		Query: "semantic query", Scope: "all", Type: "rule", Topic: "semantic", Tag: "tag", Mode: contracts.ModeRequired, Limit: 2,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if prepared != 1 {
		t.Fatalf("prepare calls = %d, want 1", prepared)
	}
	if runtime.calls != 1 || runtime.closed != 1 {
		t.Fatalf("runtime calls=%d closed=%d", runtime.calls, runtime.closed)
	}
	if len(response.Results) != 2 || response.Results[0].Entry.Scope != "project" || response.Results[1].Entry.Scope != "user" {
		t.Fatalf("results = %#v", response.Results)
	}
	if response.Results[0].Entry.ID != "shared" || response.Results[1].Entry.ID != "shared" {
		t.Fatalf("scope-qualified same ID collapsed incorrectly: %#v", response.Results)
	}
	if len(response.Lanes) != 4 {
		t.Fatalf("lanes = %#v, want four scope-qualified lanes", response.Lanes)
	}
}

func TestProductionSemanticSearcherPreflightFailureNeverStartsRuntime(t *testing.T) {
	controller := &semanticController{}
	searcher := productionSemanticSearcher{
		deps: semanticProductionDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
			build: func(composition.Input) (composition.Result, error) {
				return composition.Result{Controller: controller}, nil
			},
			prepare: func(context.Context, composition.Result, paths.Env, SemanticSearchRequest) (productionSemanticRuntime, error) {
				return nil, &generation.Error{Code: contracts.ReasonGenerationMissing}
			},
		},
	}

	response, err := searcher.Search(context.Background(), SemanticSearchRequest{Query: "query", Scope: "all", Mode: contracts.ModeAuto, Limit: searchResultLimit})
	if err != nil {
		t.Fatalf("auto search: %v", err)
	}
	if !response.Degraded || !reflect.DeepEqual(response.DegradedReasons, []contracts.ReasonCode{contracts.ReasonGenerationMissing}) || len(response.Results) != 0 {
		t.Fatalf("auto response = %#v", response)
	}
	if controller.startCalls != 0 {
		t.Fatalf("controller start calls = %d, want 0", controller.startCalls)
	}
}

func TestProductionSemanticSearcherRequiredReturnsStableTypedReason(t *testing.T) {
	searcher := productionSemanticSearcher{
		deps: semanticProductionDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
			build: func(composition.Input) (composition.Result, error) {
				return composition.Result{}, &generation.Error{Code: contracts.ReasonProfileStale}
			},
		},
	}

	_, err := searcher.Search(context.Background(), SemanticSearchRequest{Query: "query", Scope: "project", Mode: contracts.ModeRequired, Limit: searchResultLimit})
	var semanticErr *SemanticSearchError
	if !errors.As(err, &semanticErr) || semanticErr.Code != contracts.ReasonProfileStale {
		t.Fatalf("required error = %v, want profile-stale semantic error", err)
	}
}

func TestProductionSemanticSearcherProfileMismatchAcrossScopes(t *testing.T) {
	searcher := productionSemanticSearcher{
		deps: semanticProductionDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
			build: func(composition.Input) (composition.Result, error) {
				return composition.Result{}, nil
			},
			prepare: func(context.Context, composition.Result, paths.Env, SemanticSearchRequest) (productionSemanticRuntime, error) {
				return nil, &retrieve.Error{Code: contracts.ReasonProfileMismatchAcrossScopes, Message: "mismatch"}
			},
		},
	}

	response, err := searcher.Search(context.Background(), SemanticSearchRequest{Query: "query", Scope: "all", Mode: contracts.ModeAuto, Limit: searchResultLimit})
	if err != nil {
		t.Fatalf("auto search: %v", err)
	}
	if !response.Degraded || len(response.Results) != 0 || !reflect.DeepEqual(response.DegradedReasons, []contracts.ReasonCode{contracts.ReasonProfileMismatchAcrossScopes}) {
		t.Fatalf("auto response = %#v", response)
	}

	_, err = searcher.Search(context.Background(), SemanticSearchRequest{Query: "query", Scope: "all", Mode: contracts.ModeRequired, Limit: searchResultLimit})
	var semanticErr *SemanticSearchError
	if !errors.As(err, &semanticErr) || semanticErr.Code != contracts.ReasonProfileMismatchAcrossScopes {
		t.Fatalf("required error = %v", err)
	}
}

func TestSemanticContextRankingsKeepScopeQualifiedSamePath(t *testing.T) {
	rankings := semanticContextRankings([]index.Result{
		{Entry: index.Entry{ID: "shared", Scope: "user", Path: "rules/shared.md"}},
		{Entry: index.Entry{ID: "shared", Scope: "project", Path: "rules/shared.md"}},
	})
	want := []struct {
		scope   string
		entryID string
		path    string
		rank    int
	}{
		{"user", "shared", "rules/shared.md", 1},
		{"project", "shared", "rules/shared.md", 2},
	}
	if len(rankings) != len(want) {
		t.Fatalf("rankings = %#v", rankings)
	}
	for i, ranking := range rankings {
		if ranking.Scope != want[i].scope || ranking.EntryID != want[i].entryID || ranking.Path != want[i].path || ranking.Rank != want[i].rank {
			t.Fatalf("ranking[%d] = %#v, want %#v", i, ranking, want[i])
		}
	}
}

type semanticRuntimeStub struct {
	response SemanticSearchResponse
	err      error
	calls    int
	closed   int
}

func (s *semanticRuntimeStub) Recall(_ context.Context, _ SemanticSearchRequest) (SemanticSearchResponse, error) {
	s.calls++
	return s.response, s.err
}

func (s *semanticRuntimeStub) Close() error {
	s.closed++
	return nil
}
