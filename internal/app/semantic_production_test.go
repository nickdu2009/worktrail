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
)

func TestProductionSemanticSearcherRanksAllScopeResultsGlobally(t *testing.T) {
	controller := &semanticController{}
	var prepared []string
	user := &semanticScopeRecallStub{response: SemanticSearchResponse{
		Policy:  "semantic-retrieve-v1",
		Profile: "profile",
		Lanes:   []SemanticSearchLane{{Name: "chunk_fts"}, {Name: "vector_knn"}},
		Results: []index.Result{{Entry: index.Entry{Scope: "user", Path: "rules/user.md", Title: "user"}, Score: 1}},
	}}
	project := &semanticScopeRecallStub{response: SemanticSearchResponse{
		Policy:  "semantic-retrieve-v1",
		Profile: "profile",
		Lanes:   []SemanticSearchLane{{Name: "chunk_fts"}, {Name: "vector_knn"}},
		Results: []index.Result{
			{Entry: index.Entry{Scope: "project", Path: "rules/project.md", Title: "project"}, Score: 3},
			{Entry: index.Entry{Scope: "project", Path: "rules/extra.md", Title: "extra"}, Score: 0},
		},
	}}
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
			prepareScope: func(_ context.Context, _ composition.Result, _ paths.Env, scope string, request SemanticSearchRequest) (semanticScopeRecall, error) {
				prepared = append(prepared, scope)
				if request.Scope != "all" || request.Query != "semantic query" || request.Type != "rule" || request.Topic != "semantic" || request.Tag != "tag" || request.Mode != contracts.ModeRequired || request.Limit != 2 {
					t.Fatalf("request = %#v", request)
				}
				if scope == "user" {
					return user, nil
				}
				return project, nil
			},
		},
	}

	response, err := searcher.Search(context.Background(), SemanticSearchRequest{
		Query: "semantic query", Scope: "all", Type: "rule", Topic: "semantic", Tag: "tag", Mode: contracts.ModeRequired, Limit: 2,
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !reflect.DeepEqual(prepared, []string{"user", "project"}) {
		t.Fatalf("prepared scopes = %#v", prepared)
	}
	if controller.startCalls != 1 {
		t.Fatalf("controller start calls = %d, want 1", controller.startCalls)
	}
	if user.calls != 1 || project.calls != 1 {
		t.Fatalf("scope calls user=%d project=%d, want 1 each", user.calls, project.calls)
	}
	if len(response.Results) != 2 || response.Results[0].Entry.Scope != "project" || response.Results[1].Entry.Scope != "user" {
		t.Fatalf("results = %#v", response.Results)
	}
	if user.closed != 1 || project.closed != 1 {
		t.Fatalf("scope closes user=%d project=%d, want 1 each", user.closed, project.closed)
	}
}

func TestProductionSemanticSearcherAllScopeFailureNeverReturnsPartialResults(t *testing.T) {
	controller := &semanticController{}
	user := &semanticScopeRecallStub{response: SemanticSearchResponse{
		Results: []index.Result{{Entry: index.Entry{Scope: "user", Title: "partial"}}},
	}}
	searcher := productionSemanticSearcher{
		deps: semanticProductionDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
			build: func(composition.Input) (composition.Result, error) {
				return composition.Result{Controller: controller}, nil
			},
			prepareScope: func(_ context.Context, _ composition.Result, _ paths.Env, scope string, _ SemanticSearchRequest) (semanticScopeRecall, error) {
				if scope == "user" {
					return user, nil
				}
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
	if user.closed != 1 {
		t.Fatalf("opened user scope was not closed: %d", user.closed)
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

func TestSemanticContextRankingsKeepScopeQualifiedSamePath(t *testing.T) {
	rankings := semanticContextRankings([]index.Result{
		{Entry: index.Entry{Scope: "user", Path: "rules/shared.md"}},
		{Entry: index.Entry{Scope: "project", Path: "rules/shared.md"}},
	})
	want := []struct {
		scope string
		path  string
		rank  int
	}{
		{"user", "rules/shared.md", 1},
		{"project", "rules/shared.md", 2},
	}
	if len(rankings) != len(want) {
		t.Fatalf("rankings = %#v", rankings)
	}
	for i, ranking := range rankings {
		if ranking.Scope != want[i].scope || ranking.Path != want[i].path || ranking.Rank != want[i].rank {
			t.Fatalf("ranking[%d] = %#v, want %#v", i, ranking, want[i])
		}
	}
}

type semanticScopeRecallStub struct {
	response SemanticSearchResponse
	err      error
	calls    int
	closed   int
}

func (s *semanticScopeRecallStub) Recall(_ context.Context, _ SemanticSearchRequest) (SemanticSearchResponse, error) {
	s.calls++
	return s.response, s.err
}

func (s *semanticScopeRecallStub) Close() error {
	s.closed++
	return nil
}
