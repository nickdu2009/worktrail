package retrieve

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

type fakeGate struct {
	reason contracts.ReasonCode
	err    error
	calls  []string
}

func (f *fakeGate) Check(context.Context) (contracts.ReasonCode, error) {
	f.calls = append(f.calls, "gate")
	return f.reason, f.err
}

type fakeFTS struct {
	hits      []RawChunkHit
	err       error
	calls     int
	lastLimit int
	limits    []int
}

func (f *fakeFTS) SearchChunks(_ context.Context, _ string, limit int) ([]RawChunkHit, error) {
	f.calls++
	f.lastLimit = limit
	f.limits = append(f.limits, limit)
	return append([]RawChunkHit(nil), f.hits...), f.err
}

type fakeVector struct {
	hits      []RawChunkHit
	err       error
	calls     int
	lastLimit int
}

func (f *fakeVector) SearchChunks(_ context.Context, _ []float32, limit int) ([]RawChunkHit, error) {
	f.calls++
	f.lastLimit = limit
	return append([]RawChunkHit(nil), f.hits...), f.err
}

type fakeEmbedder struct {
	vector []float32
	err    error
	calls  []string
}

func (f *fakeEmbedder) EmbedQuery(_ context.Context, _ string) ([]float32, error) {
	f.calls = append(f.calls, "embed")
	return f.vector, f.err
}

type fakeHydrator struct {
	calls  []string
	mutate func([]Candidate) []Candidate
}

func (f *fakeHydrator) Hydrate(_ context.Context, candidates []Candidate) ([]Candidate, error) {
	f.calls = append(f.calls, "hydrate")
	if f.mutate != nil {
		return f.mutate(candidates), nil
	}
	out := make([]Candidate, len(candidates))
	copy(out, candidates)
	for i := range out {
		if out[i].DocumentID == "" {
			out[i].DocumentID = out[i].EntryID
		}
	}
	return out, nil
}

type fakeLegacyLexicalFallback struct {
	candidates []Candidate
	err        error
	calls      []string
	limits     []int
}

func (f *fakeLegacyLexicalFallback) Recall(_ context.Context, _ string, limit int) ([]Candidate, error) {
	f.calls = append(f.calls, "legacy_lexical")
	f.limits = append(f.limits, limit)
	return f.candidates, f.err
}

func testBackend(scope string, fts ChunkFTS, knn VectorKNN, hydrator EntryHydrator, filtersPushed bool) ScopeBackend {
	return ScopeBackend{
		Scope:         scope,
		ChunkFTS:      fts,
		VectorKNN:     knn,
		Hydrator:      hydrator,
		FiltersPushed: filtersPushed,
	}
}

func TestRecallModesAndGenerationGateFallback(t *testing.T) {
	gate := &fakeGate{reason: contracts.ReasonGenerationMissing, err: errors.New("missing")}
	fts := &fakeFTS{hits: []RawChunkHit{{ChunkID: "lexical", EntryID: "entry", Rank: 1}}}
	hydrator := &fakeHydrator{}
	fallback := &fakeLegacyLexicalFallback{candidates: []Candidate{{Scope: "project", ChunkID: "legacy", EntryID: "entry"}}}
	facade := Facade{
		Gate:                  gate,
		Backends:              []ScopeBackend{testBackend("project", fts, nil, hydrator, true)},
		LegacyLexicalFallback: fallback.Recall,
	}

	lexical, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeLexical, Query: "q"})
	if err != nil {
		t.Fatalf("lexical Recall() error = %v", err)
	}
	if len(gate.calls) != 0 {
		t.Fatalf("lexical mode called gate %d times", len(gate.calls))
	}
	if len(lexical.Candidates) != 1 || lexical.Candidates[0].ChunkID != "lexical" {
		t.Fatalf("lexical candidates = %#v", lexical.Candidates)
	}

	auto, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
	if err != nil {
		t.Fatalf("auto Recall() error = %v", err)
	}
	if !auto.Degraded || auto.Reason != contracts.ReasonGenerationMissing {
		t.Fatalf("auto response = %#v, want missing degraded response", auto)
	}
	if auto.NextStep != "worktrail semantic rebuild --scope all" {
		t.Fatalf("auto next step = %q", auto.NextStep)
	}
	if len(auto.Lanes) != 1 || auto.Lanes[0] != (Lane{Name: LaneNameLegacyLex}) {
		t.Fatalf("auto lanes = %#v", auto.Lanes)
	}
	if len(auto.Candidates) != 1 || auto.Candidates[0].ChunkID != "legacy" {
		t.Fatalf("auto candidates = %#v", auto.Candidates)
	}
	if len(fallback.calls) != 1 || fallback.limits[0] != DefaultPolicy().FinalLimit {
		t.Fatalf("fallback calls = %#v with limits %#v", fallback.calls, fallback.limits)
	}

	fallbackCalls := len(fallback.calls)
	_, err = facade.Recall(context.Background(), Request{Mode: contracts.ModeRequired, Query: "q"})
	var recallErr *Error
	if !errors.As(err, &recallErr) {
		t.Fatalf("required Recall() error = %T %[1]v, want *Error", err)
	}
	if recallErr.Code != contracts.ReasonGenerationMissing {
		t.Fatalf("required error code = %q", recallErr.Code)
	}
	if len(fallback.calls) != fallbackCalls {
		t.Fatalf("required mode called legacy fallback %d times", len(fallback.calls)-fallbackCalls)
	}
}

func TestRecallUsesEntryRRFAcrossScopes(t *testing.T) {
	hydrator := &fakeHydrator{}
	userFTS := &fakeFTS{hits: []RawChunkHit{
		{ChunkID: "u-c1", EntryID: "shared", Rank: 1},
	}}
	userKNN := &fakeVector{hits: []RawChunkHit{
		{ChunkID: "u-c2", EntryID: "shared", Rank: 1},
	}}
	projectFTS := &fakeFTS{hits: []RawChunkHit{
		{ChunkID: "p-c1", EntryID: "shared", Rank: 1},
	}}
	projectKNN := &fakeVector{hits: []RawChunkHit{
		{ChunkID: "p-c3", EntryID: "other", Rank: 1},
		{ChunkID: "p-c2", EntryID: "shared", Rank: 2},
	}}
	facade := Facade{
		Gate:     &fakeGate{},
		Embedder: &fakeEmbedder{vector: []float32{1}},
		Backends: []ScopeBackend{
			testBackend("user", userFTS, userKNN, hydrator, true),
			testBackend("project", projectFTS, projectKNN, hydrator, true),
		},
	}

	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q", Scope: "all"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(response.Candidates) != 3 {
		t.Fatalf("candidates = %#v, want three scope-qualified entries", response.Candidates)
	}
	if response.Candidates[0].Scope != "user" || response.Candidates[0].EntryID != "shared" {
		t.Fatalf("top candidate = %#v, want user/shared fused across lanes", response.Candidates[0])
	}
	wantScore := 1.0/61.0 + 1.0/61.0
	if math.Abs(response.Candidates[0].Score-wantScore) > 1e-12 {
		t.Fatalf("user/shared score = %v, want %v", response.Candidates[0].Score, wantScore)
	}
	if userFTS.calls != 1 || userKNN.calls != 1 || projectFTS.calls != 1 || projectKNN.calls != 1 {
		t.Fatalf("backend calls fts/knn user=%d/%d project=%d/%d", userFTS.calls, userKNN.calls, projectFTS.calls, projectKNN.calls)
	}
	if userFTS.lastLimit != DefaultPolicy().HardCap {
		t.Fatalf("FTS hard-cap limit = %d, want %d", userFTS.lastLimit, DefaultPolicy().HardCap)
	}
}

func TestRecallCollapsesChunksToConsecutiveEntryRanks(t *testing.T) {
	fts := &fakeFTS{hits: []RawChunkHit{
		{ChunkID: "c1", EntryID: "table", Rank: 1},
		{ChunkID: "c2", EntryID: "table", Rank: 2},
		{ChunkID: "c3", EntryID: "other", Rank: 3},
	}}
	facade := Facade{
		Backends: []ScopeBackend{testBackend("project", fts, nil, &fakeHydrator{}, true)},
	}
	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeLexical, Query: "q"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(response.Candidates) != 2 {
		t.Fatalf("candidates = %#v", response.Candidates)
	}
	lane := response.Lanes[0]
	if lane.EligibleEntries != 2 || lane.RawHits != 3 {
		t.Fatalf("lane diagnostics = %#v", lane)
	}
}

func TestPrefixProviderFetchesHardCapOnceAndRefills(t *testing.T) {
	policy := DefaultPolicy()
	policy.InitialWindow = 50
	policy.WindowGrowth = 2
	policy.HardCap = 200
	policy.EligibleEntryTarget = 20

	hits := make([]RawChunkHit, 0, 200)
	for i := 0; i < 100; i++ {
		hits = append(hits, RawChunkHit{ChunkID: "t-" + itoa(i), EntryID: "table", Rank: i + 1, DocumentType: "rule"})
	}
	for i := 0; i < 100; i++ {
		hits = append(hits, RawChunkHit{ChunkID: "e-" + itoa(i), EntryID: "entry-" + itoa(i), Rank: 101 + i, DocumentType: "rule"})
	}
	calls := 0
	fetch := func(limit int) ([]RawChunkHit, error) {
		calls++
		if limit != policy.HardCap {
			t.Fatalf("limit = %d, want hard cap %d", limit, policy.HardCap)
		}
		return hits, nil
	}
	provider := newPrefixProvider(fetch)
	got, diagnostics, err := collectEntryLane(provider, policy, "project", ExactFilters{}, true)
	if err != nil {
		t.Fatalf("collectEntryLane() error = %v", err)
	}
	if calls != 1 || provider.QueryCalls() != 1 {
		t.Fatalf("backend calls = %d, want 1", calls)
	}
	if diagnostics.RefillRounds != 3 {
		t.Fatalf("refill rounds = %d, want 3", diagnostics.RefillRounds)
	}
	if diagnostics.RawHits != 200 || diagnostics.EligibleEntries != 20 || diagnostics.WindowSaturated {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if len(got) != 20 || got[0].EntryID != "table" || got[0].Rank != 1 || got[1].EntryID != "entry-0" || got[1].Rank != 2 {
		t.Fatalf("collapsed hits = %#v", got[:min(5, len(got))])
	}
}

func TestFilterStarvationRefillsAndCountsRejections(t *testing.T) {
	policy := DefaultPolicy()
	policy.InitialWindow = 50
	policy.HardCap = 200
	policy.EligibleEntryTarget = 5

	hits := make([]RawChunkHit, 0, 120)
	for i := 0; i < 100; i++ {
		hits = append(hits, RawChunkHit{ChunkID: "bad-" + itoa(i), EntryID: "bad-" + itoa(i), Rank: i + 1, DocumentType: "note"})
	}
	for i := 0; i < 20; i++ {
		hits = append(hits, RawChunkHit{ChunkID: "good-" + itoa(i), EntryID: "good-" + itoa(i), Rank: 101 + i, DocumentType: "rule"})
	}
	provider := newPrefixProvider(func(limit int) ([]RawChunkHit, error) {
		if limit > len(hits) {
			limit = len(hits)
		}
		return hits[:limit], nil
	})
	got, diagnostics, err := collectEntryLane(provider, policy, "project", ExactFilters{Type: "rule"}, true)
	if err != nil {
		t.Fatalf("collectEntryLane() error = %v", err)
	}
	if diagnostics.FilterRejections != 100 || diagnostics.EligibleEntries != 5 || diagnostics.RefillRounds != 3 {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if len(got) != 5 || got[0].EntryID != "good-0" {
		t.Fatalf("hits = %#v", got)
	}
}

func TestBackendExhaustionDoesNotMarkSaturation(t *testing.T) {
	policy := DefaultPolicy()
	policy.InitialWindow = 50
	policy.HardCap = 200
	policy.EligibleEntryTarget = 20
	hits := []RawChunkHit{
		{ChunkID: "c1", EntryID: "e1", Rank: 1},
		{ChunkID: "c2", EntryID: "e2", Rank: 2},
	}
	provider := newPrefixProvider(func(limit int) ([]RawChunkHit, error) { return hits, nil })
	got, diagnostics, err := collectEntryLane(provider, policy, "project", ExactFilters{}, true)
	if err != nil {
		t.Fatalf("collectEntryLane() error = %v", err)
	}
	if diagnostics.WindowSaturated || diagnostics.RawHits != 2 || len(got) != 2 {
		t.Fatalf("diagnostics = %#v hits=%#v", diagnostics, got)
	}
}

func TestHardCapSaturationWhenTargetUnmet(t *testing.T) {
	policy := DefaultPolicy()
	policy.InitialWindow = 50
	policy.HardCap = 200
	policy.EligibleEntryTarget = 20
	hits := make([]RawChunkHit, 0, 200)
	for i := 0; i < 200; i++ {
		hits = append(hits, RawChunkHit{ChunkID: "c-" + itoa(i), EntryID: "same", Rank: i + 1})
	}
	provider := newPrefixProvider(func(limit int) ([]RawChunkHit, error) { return hits, nil })
	got, diagnostics, err := collectEntryLane(provider, policy, "project", ExactFilters{}, true)
	if err != nil {
		t.Fatalf("collectEntryLane() error = %v", err)
	}
	if !diagnostics.WindowSaturated || diagnostics.EligibleEntries != 1 || len(got) != 1 {
		t.Fatalf("diagnostics = %#v hits=%#v", diagnostics, got)
	}
}

func TestRecallLaneFailureMatrixAutoAndRequired(t *testing.T) {
	t.Run("auto keeps surviving lane", func(t *testing.T) {
		fts := &fakeFTS{err: &Error{Code: contracts.ReasonFTSQueryFailed, Message: "fts boom"}}
		knn := &fakeVector{hits: []RawChunkHit{{ChunkID: "v1", EntryID: "entry", Rank: 1}}}
		facade := Facade{
			Gate:     &fakeGate{},
			Embedder: &fakeEmbedder{vector: []float32{1}},
			Backends: []ScopeBackend{testBackend("project", fts, knn, &fakeHydrator{}, true)},
		}
		response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
		if err != nil {
			t.Fatalf("Recall() error = %v", err)
		}
		if !response.Degraded || response.Reason != contracts.ReasonFTSQueryFailed {
			t.Fatalf("response = %#v", response)
		}
		if len(response.Candidates) != 1 || response.Candidates[0].EntryID != "entry" {
			t.Fatalf("candidates = %#v", response.Candidates)
		}
	})

	t.Run("auto falls back when all lanes fail", func(t *testing.T) {
		fts := &fakeFTS{err: errors.New("fts boom")}
		knn := &fakeVector{err: errors.New("knn boom")}
		fallback := &fakeLegacyLexicalFallback{candidates: []Candidate{{Scope: "project", EntryID: "legacy", ChunkID: "legacy"}}}
		facade := Facade{
			Gate:                  &fakeGate{},
			Embedder:              &fakeEmbedder{vector: []float32{1}},
			Backends:              []ScopeBackend{testBackend("project", fts, knn, &fakeHydrator{}, true)},
			LegacyLexicalFallback: fallback.Recall,
		}
		response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
		if err != nil {
			t.Fatalf("Recall() error = %v", err)
		}
		if len(fallback.calls) != 1 || len(response.Candidates) != 1 || response.Candidates[0].EntryID != "legacy" {
			t.Fatalf("response = %#v fallback=%#v", response, fallback.calls)
		}
	})

	t.Run("required fails on any lane", func(t *testing.T) {
		fts := &fakeFTS{err: &Error{Code: contracts.ReasonFTSQueryFailed, Message: "fts boom"}}
		knn := &fakeVector{hits: []RawChunkHit{{ChunkID: "v1", EntryID: "entry", Rank: 1}}}
		facade := Facade{
			Gate:     &fakeGate{},
			Embedder: &fakeEmbedder{vector: []float32{1}},
			Backends: []ScopeBackend{testBackend("project", fts, knn, &fakeHydrator{}, true)},
		}
		_, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeRequired, Query: "q"})
		var recallErr *Error
		if !errors.As(err, &recallErr) || recallErr.Code != contracts.ReasonFTSQueryFailed {
			t.Fatalf("required error = %v", err)
		}
	})

	t.Run("vector-only and fts-only modes", func(t *testing.T) {
		ftsOnly := Facade{
			Backends: []ScopeBackend{testBackend("project", &fakeFTS{hits: []RawChunkHit{{ChunkID: "c1", EntryID: "e1", Rank: 1}}}, nil, &fakeHydrator{}, true)},
		}
		response, err := ftsOnly.Recall(context.Background(), Request{Mode: contracts.ModeLexical, Query: "q"})
		if err != nil || len(response.Candidates) != 1 {
			t.Fatalf("fts-only = %#v err=%v", response, err)
		}

		knn := &fakeVector{hits: []RawChunkHit{{ChunkID: "v1", EntryID: "e1", Rank: 1}}}
		vectorOnly := Facade{
			Gate:     &fakeGate{},
			Embedder: &fakeEmbedder{vector: []float32{1}},
			Backends: []ScopeBackend{testBackend("project", &fakeFTS{err: errors.New("disabled")}, knn, &fakeHydrator{}, true)},
		}
		response, err = vectorOnly.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
		if err != nil || len(response.Candidates) != 1 || response.Candidates[0].EntryID != "e1" {
			t.Fatalf("vector-only = %#v err=%v", response, err)
		}
	})
}

func TestRecallAppliesGovernanceWithoutMaxPerDocument(t *testing.T) {
	hydrator := &fakeHydrator{mutate: func(candidates []Candidate) []Candidate {
		for index := range candidates {
			switch candidates[index].EntryID {
			case "a":
				candidates[index].SourceOfTruth = true
			case "d":
				candidates[index].Superseded = true
			case "e":
				candidates[index].Lifecycle = "historical"
			}
		}
		return candidates
	}}
	facade := Facade{
		Backends: []ScopeBackend{testBackend("project", &fakeFTS{hits: []RawChunkHit{
			{ChunkID: "b", EntryID: "b", Rank: 1},
			{ChunkID: "a", EntryID: "a", Rank: 5},
			{ChunkID: "c", EntryID: "c", Rank: 2},
			{ChunkID: "d", EntryID: "d", Rank: 1},
			{ChunkID: "e", EntryID: "e", Rank: 1},
		}}, nil, hydrator, true)},
	}

	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeLexical, Query: "q"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(response.Candidates) != 3 {
		t.Fatalf("governed candidates = %#v", response.Candidates)
	}
	if response.Candidates[0].EntryID != "a" {
		t.Fatalf("source of truth did not win: %#v", response.Candidates)
	}
}

func TestRecallChecksGateBeforeEmbedding(t *testing.T) {
	gate := &fakeGate{reason: contracts.ReasonProfileStale, err: errors.New("stale")}
	embedder := &fakeEmbedder{vector: []float32{1}}
	facade := Facade{
		Gate:     gate,
		Embedder: embedder,
		Backends: []ScopeBackend{testBackend("project", &fakeFTS{}, &fakeVector{}, &fakeHydrator{}, true)},
	}

	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if !response.Degraded || response.Reason != contracts.ReasonProfileStale {
		t.Fatalf("response = %#v", response)
	}
	if len(embedder.calls) != 0 {
		t.Fatalf("embedder called despite stale gate")
	}
}

func TestDefaultPolicyIsNotMutableByCallers(t *testing.T) {
	mutated := DefaultPolicy()
	mutated.RRFK = 1
	mutated.InitialWindow = 1
	mutated.HardCap = 1
	mutated.FinalLimit = 1

	policy, err := (Facade{Backends: []ScopeBackend{{}}}).policy()
	if err != nil {
		t.Fatalf("default policy error = %v", err)
	}
	if policy != DefaultPolicy() {
		t.Fatalf("effective policy = %#v, want fresh default %#v", policy, DefaultPolicy())
	}
}

func TestRecallRejectsInvalidPolicy(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
	}{
		{name: "RRFK", policy: Policy{Version: "test", InitialWindow: 1, WindowGrowth: 2, HardCap: 2, EligibleEntryTarget: 1, FinalLimit: 1}},
		{name: "InitialWindow", policy: Policy{Version: "test", RRFK: 1, WindowGrowth: 2, HardCap: 2, EligibleEntryTarget: 1, FinalLimit: 1}},
		{name: "WindowGrowth", policy: Policy{Version: "test", RRFK: 1, InitialWindow: 1, WindowGrowth: 1, HardCap: 2, EligibleEntryTarget: 1, FinalLimit: 1}},
		{name: "HardCap", policy: Policy{Version: "test", RRFK: 1, InitialWindow: 2, WindowGrowth: 2, HardCap: 1, EligibleEntryTarget: 1, FinalLimit: 1}},
		{name: "FinalLimit", policy: Policy{Version: "test", RRFK: 1, InitialWindow: 1, WindowGrowth: 2, HardCap: 2, EligibleEntryTarget: 1}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (Facade{
				Policy:   test.policy,
				Backends: []ScopeBackend{testBackend("project", &fakeFTS{}, nil, &fakeHydrator{}, true)},
			}).Recall(context.Background(), Request{Mode: contracts.ModeLexical})
			if err == nil {
				t.Fatal("Recall() error = nil, want invalid policy error")
			}
		})
	}
}

func TestCrossScopeTieBreakIsStable(t *testing.T) {
	candidates := FuseRanks(60,
		[]LaneHit{{Scope: "project", EntryID: "a", ChunkID: "p", Rank: 1}},
		[]LaneHit{{Scope: "user", EntryID: "a", ChunkID: "u", Rank: 1}},
	)
	if len(candidates) != 2 {
		t.Fatalf("candidates = %#v", candidates)
	}
	if candidates[0].Scope != "project" || candidates[1].Scope != "user" {
		t.Fatalf("tie order = %#v, want project then user", candidates)
	}
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	var digits []byte
	for v > 0 {
		digits = append([]byte{byte('0' + v%10)}, digits...)
		v /= 10
	}
	return string(digits)
}
