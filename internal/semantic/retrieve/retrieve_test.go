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
	hits  []LaneHit
	err   error
	calls []string
}

func (f *fakeFTS) SearchChunks(_ context.Context, _ string, _ int) ([]LaneHit, error) {
	f.calls = append(f.calls, "fts")
	return f.hits, f.err
}

type fakeVector struct {
	hits  []LaneHit
	err   error
	calls []string
}

func (f *fakeVector) SearchChunks(_ context.Context, _ []float32, _ int) ([]LaneHit, error) {
	f.calls = append(f.calls, "vector")
	return f.hits, f.err
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
	return candidates, nil
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

func TestRecallModesAndGenerationGateFallback(t *testing.T) {
	gate := &fakeGate{reason: contracts.ReasonGenerationMissing, err: errors.New("missing")}
	fts := &fakeFTS{hits: []LaneHit{{ChunkID: "lexical", EntryID: "entry", Rank: 1}}}
	hydrator := &fakeHydrator{}
	fallback := &fakeLegacyLexicalFallback{candidates: []Candidate{{ChunkID: "legacy", EntryID: "entry"}}}
	facade := Facade{
		Gate:                  gate,
		ChunkFTS:              fts,
		Hydrator:              hydrator,
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
	if len(auto.Lanes) != 1 || auto.Lanes[0] != (Lane{Name: "legacy_lexical"}) {
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

func TestRecallAutoGateFailureReportsUnavailableLegacyFallback(t *testing.T) {
	tests := []struct {
		name     string
		fallback LegacyLexicalFallback
		calls    *fakeLegacyLexicalFallback
	}{
		{name: "not configured"},
		{
			name: "failed",
			calls: &fakeLegacyLexicalFallback{
				err: errors.New("legacy index unavailable"),
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.calls != nil {
				test.fallback = test.calls.Recall
			}
			facade := Facade{
				Gate:                  &fakeGate{reason: contracts.ReasonProfileStale, err: errors.New("stale")},
				LegacyLexicalFallback: test.fallback,
			}

			response, err := facade.Recall(context.Background(), Request{
				Mode:  contracts.ModeAuto,
				Query: "q",
				Scope: "project",
			})
			if err != nil {
				t.Fatalf("Recall() error = %v, want degraded diagnostic response", err)
			}
			if !response.Degraded || response.Reason != contracts.ReasonProfileStale {
				t.Fatalf("response = %#v, want preserved generation diagnostic", response)
			}
			if response.NextStep != "worktrail semantic rebuild --scope project" {
				t.Fatalf("next step = %q", response.NextStep)
			}
			if len(response.Candidates) != 0 {
				t.Fatalf("candidates = %#v, want no false fallback result", response.Candidates)
			}
			if len(response.Lanes) != 1 || response.Lanes[0] != degradedLane("legacy_lexical", contracts.ReasonRuntimeUnavailable) {
				t.Fatalf("lanes = %#v, want unavailable legacy lexical diagnostic", response.Lanes)
			}
			if test.calls != nil && len(test.calls.calls) != 1 {
				t.Fatalf("legacy fallback calls = %d, want 1", len(test.calls.calls))
			}
		})
	}
}

func TestRecallUsesRRFAndDeduplicatesChunks(t *testing.T) {
	hydrator := &fakeHydrator{}
	facade := Facade{
		Gate:      &fakeGate{},
		ChunkFTS:  &fakeFTS{hits: []LaneHit{{ChunkID: "shared", EntryID: "entry-a", Rank: 10}, {ChunkID: "lexical", EntryID: "entry-b", Rank: 1}}},
		Embedder:  &fakeEmbedder{vector: []float32{1}},
		VectorKNN: &fakeVector{hits: []LaneHit{{ChunkID: "shared", EntryID: "entry-a", Rank: 1}, {ChunkID: "vector", EntryID: "entry-c", Rank: 2}}},
		Hydrator:  hydrator,
	}

	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(response.Candidates) != 3 {
		t.Fatalf("candidate count = %d, want chunk dedupe", len(response.Candidates))
	}
	if response.Candidates[0].ChunkID != "shared" {
		t.Fatalf("top candidate = %#v, want fused shared chunk", response.Candidates[0])
	}
	if math.Abs(response.Candidates[0].Score-(1.0/70.0+1.0/61.0)) > 1e-12 {
		t.Fatalf("shared score = %v, want rank-only RRF score", response.Candidates[0].Score)
	}
}

func TestRecallAppliesGovernanceAndDiversity(t *testing.T) {
	hydrator := &fakeHydrator{mutate: func(candidates []Candidate) []Candidate {
		for index := range candidates {
			switch candidates[index].ChunkID {
			case "truth":
				candidates[index].SourceOfTruth = true
				candidates[index].DocumentID = "document-a"
			case "same-document":
				candidates[index].DocumentID = "document-a"
			case "third-document":
				candidates[index].DocumentID = "document-a"
			case "superseded":
				candidates[index].Superseded = true
			case "historical":
				candidates[index].Lifecycle = "historical"
			}
		}
		return candidates
	}}
	facade := Facade{
		ChunkFTS: &fakeFTS{hits: []LaneHit{
			{ChunkID: "same-document", EntryID: "b", Rank: 1},
			{ChunkID: "truth", EntryID: "a", Rank: 5},
			{ChunkID: "third-document", EntryID: "c", Rank: 2},
			{ChunkID: "superseded", EntryID: "d", Rank: 1},
			{ChunkID: "historical", EntryID: "e", Rank: 1},
		}},
		Hydrator: hydrator,
	}

	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeLexical, Query: "q"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if len(response.Candidates) != 2 {
		t.Fatalf("governed candidate count = %d, want two document-diverse current candidates", len(response.Candidates))
	}
	if response.Candidates[0].ChunkID != "truth" {
		t.Fatalf("source of truth candidate did not outrank other current candidate: %#v", response.Candidates)
	}
	for _, candidate := range response.Candidates {
		if candidate.ChunkID == "superseded" || candidate.ChunkID == "historical" {
			t.Fatalf("non-current candidate survived governance: %#v", candidate)
		}
	}
}

func TestRecallReportsLaneDegradation(t *testing.T) {
	facade := Facade{
		Gate:      &fakeGate{},
		ChunkFTS:  &fakeFTS{err: errors.New("fts unavailable")},
		Embedder:  &fakeEmbedder{vector: []float32{1}},
		VectorKNN: &fakeVector{hits: []LaneHit{{ChunkID: "vector", EntryID: "entry", Rank: 1}}},
		Hydrator:  &fakeHydrator{},
	}

	response, err := facade.Recall(context.Background(), Request{Mode: contracts.ModeAuto, Query: "q"})
	if err != nil {
		t.Fatalf("Recall() error = %v", err)
	}
	if !response.Degraded || response.Reason != contracts.ReasonRuntimeUnavailable {
		t.Fatalf("response = %#v, want degraded lane response", response)
	}
	if len(response.Candidates) != 1 || response.Candidates[0].ChunkID != "vector" {
		t.Fatalf("candidates = %#v, want surviving vector lane", response.Candidates)
	}
}

func TestRecallChecksGateBeforeEmbedding(t *testing.T) {
	gate := &fakeGate{reason: contracts.ReasonProfileStale, err: errors.New("stale")}
	embedder := &fakeEmbedder{vector: []float32{1}}
	facade := Facade{
		Gate:     gate,
		Embedder: embedder,
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
	mutated.LaneTopK = 1
	mutated.MaxPerDocument = 1
	mutated.FinalLimit = 1

	policy, err := (Facade{}).policy()
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
		{
			name: "RRFK",
			policy: Policy{
				Version:        "test",
				LaneTopK:       1,
				MaxPerDocument: 1,
				FinalLimit:     1,
			},
		},
		{
			name: "LaneTopK",
			policy: Policy{
				Version:        "test",
				RRFK:           1,
				MaxPerDocument: 1,
				FinalLimit:     1,
			},
		},
		{
			name: "MaxPerDocument",
			policy: Policy{
				Version:    "test",
				RRFK:       1,
				LaneTopK:   1,
				FinalLimit: 1,
			},
		},
		{
			name: "FinalLimit",
			policy: Policy{
				Version:        "test",
				RRFK:           1,
				LaneTopK:       1,
				MaxPerDocument: 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := (Facade{Policy: test.policy}).Recall(context.Background(), Request{
				Mode: contracts.ModeLexical,
			})
			if err == nil {
				t.Fatal("Recall() error = nil, want invalid policy error")
			}
		})
	}
}
