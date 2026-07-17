package daemon

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestDaemonTokenCounterLoadsCurrentCredentials(t *testing.T) {
	counter := &tokenCounterStub{count: 3}
	credentials := &adapterCredentialsStub{
		descriptor: Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
		apiKey:     "secret-key",
	}
	adapter := DaemonTokenCounter{Counter: counter, Credentials: credentials}

	count, err := adapter.CountTokens(context.Background(), "authentication gate")
	if err != nil {
		t.Fatalf("CountTokens() error = %v", err)
	}
	if count != 3 {
		t.Fatalf("CountTokens() = %d, want 3", count)
	}
	if counter.endpoint != credentials.descriptor.Endpoint || counter.apiKey != credentials.apiKey || counter.content != "authentication gate" {
		t.Fatalf("counter call = endpoint %q key %q content %q", counter.endpoint, counter.apiKey, counter.content)
	}
	if credentials.loadCalls != 1 || credentials.keyCalls != 1 {
		t.Fatalf("credential calls = load %d key %d, want 1 each", credentials.loadCalls, credentials.keyCalls)
	}
}

func TestDaemonTokenCounterPreservesCredentialAndCounterErrors(t *testing.T) {
	loadErr := errors.New("descriptor unavailable")
	adapter := DaemonTokenCounter{
		Counter:     &tokenCounterStub{},
		Credentials: &adapterCredentialsStub{loadErr: loadErr},
	}
	if _, err := adapter.CountTokens(context.Background(), "text"); !errors.Is(err, loadErr) {
		t.Fatalf("CountTokens() error = %v, want descriptor error", err)
	}

	keyErr := errors.New("key unavailable")
	adapter = DaemonTokenCounter{
		Counter: &tokenCounterStub{},
		Credentials: &adapterCredentialsStub{
			descriptor: Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
			keyErr:     keyErr,
		},
	}
	if _, err := adapter.CountTokens(context.Background(), "text"); !errors.Is(err, keyErr) {
		t.Fatalf("CountTokens() error = %v, want API key error", err)
	}

	counterErr := errors.New("tokenize rejected")
	adapter = DaemonTokenCounter{
		Counter: &tokenCounterStub{err: counterErr},
		Credentials: &adapterCredentialsStub{
			descriptor: Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
			apiKey:     "secret-key",
		},
	}
	if _, err := adapter.CountTokens(context.Background(), "text"); !errors.Is(err, counterErr) {
		t.Fatalf("CountTokens() error = %v, want counter error", err)
	}
}

func TestDaemonGenerationEmbedderBatchesAndPreservesOrder(t *testing.T) {
	inputs := make([]string, documentEmbeddingBatchSize+1)
	for index := range inputs {
		inputs[index] = string(rune('a' + index))
	}
	credentials := &adapterCredentialsStub{
		descriptor: Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
		apiKey:     "secret-key",
	}
	embedder := &adapterEmbedderStub{
		response: func(call int, inputs []string) []Embedding {
			embeddings := make([]Embedding, len(inputs))
			for index := range inputs {
				embeddings[index] = Embedding{Index: index, Values: unitDocumentEmbedding(call*documentEmbeddingBatchSize + index)}
			}
			return embeddings
		},
	}
	adapter := DaemonGenerationEmbedder{Embedder: embedder, Credentials: credentials}

	vectors, err := adapter.EmbedBatch(context.Background(), inputs)
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}
	if len(embedder.calls) != 2 || len(embedder.calls[0].inputs) != documentEmbeddingBatchSize || len(embedder.calls[1].inputs) != 1 {
		t.Fatalf("embedding batches = %#v", embedder.calls)
	}
	for _, call := range embedder.calls {
		if call.endpoint != credentials.descriptor.Endpoint || call.apiKey != credentials.apiKey || call.alias != credentials.descriptor.Alias {
			t.Fatalf("embedding call = %#v", call)
		}
	}
	if credentials.loadCalls != 1 || credentials.keyCalls != 1 {
		t.Fatalf("credential calls = load %d key %d, want 1 each", credentials.loadCalls, credentials.keyCalls)
	}
	for index, vector := range vectors {
		if len(vector) != expectedEmbeddingDimension || vector[index] != 1 {
			t.Fatalf("vectors[%d] = len %d value %v, want unit vector at %d", index, len(vector), vector[index], index)
		}
	}
}

func TestDaemonGenerationEmbedderReturnsEmptyBatchWithoutReadingCredentials(t *testing.T) {
	credentials := &adapterCredentialsStub{}
	embedder := &adapterEmbedderStub{}
	adapter := DaemonGenerationEmbedder{Embedder: embedder, Credentials: credentials}

	vectors, err := adapter.EmbedBatch(context.Background(), nil)
	if err != nil {
		t.Fatalf("EmbedBatch() error = %v", err)
	}
	if len(vectors) != 0 {
		t.Fatalf("EmbedBatch() = %#v, want empty result", vectors)
	}
	if credentials.loadCalls != 0 || credentials.keyCalls != 0 || len(embedder.calls) != 0 {
		t.Fatalf("empty batch accessed daemon dependencies")
	}
}

func TestDaemonGenerationEmbedderRejectsMalformedEmbeddings(t *testing.T) {
	wrongDimension := unitDocumentEmbedding(0)[:expectedEmbeddingDimension-1]
	nonFinite := unitDocumentEmbedding(0)
	nonFinite[0] = math.NaN()
	tests := []struct {
		name       string
		embeddings []Embedding
	}{
		{name: "missing", embeddings: nil},
		{name: "wrong order", embeddings: []Embedding{{Index: 1, Values: unitDocumentEmbedding(0)}}},
		{name: "wrong dimension", embeddings: []Embedding{{Index: 0, Values: wrongDimension}}},
		{name: "non-finite", embeddings: []Embedding{{Index: 0, Values: nonFinite}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := DaemonGenerationEmbedder{
				Embedder: &adapterEmbedderStub{response: func(int, []string) []Embedding {
					return test.embeddings
				}},
				Credentials: &adapterCredentialsStub{
					descriptor: Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
					apiKey:     "secret-key",
				},
			}
			if _, err := adapter.EmbedBatch(context.Background(), []string{"document"}); err == nil {
				t.Fatal("EmbedBatch() accepted malformed embeddings")
			}
		})
	}
}

func TestConvertDocumentEmbeddingRejectsInvalidFloat32Result(t *testing.T) {
	values := unitDocumentEmbedding(0)
	values[0] = math.MaxFloat64
	if _, err := convertDocumentEmbedding(values); err == nil {
		t.Fatal("convertDocumentEmbedding() accepted a non-finite normalized value")
	}
}

type adapterCredentialsStub struct {
	descriptor Descriptor
	apiKey     string
	loadErr    error
	keyErr     error
	loadCalls  int
	keyCalls   int
}

func (s *adapterCredentialsStub) Load() (Descriptor, error) {
	s.loadCalls++
	return s.descriptor, s.loadErr
}

func (s *adapterCredentialsStub) APIKey() (string, error) {
	s.keyCalls++
	return s.apiKey, s.keyErr
}

type tokenCounterStub struct {
	count    int
	endpoint string
	apiKey   string
	content  string
	err      error
}

func (s *tokenCounterStub) CountTokens(_ context.Context, endpoint, apiKey, content string) (int, error) {
	s.endpoint = endpoint
	s.apiKey = apiKey
	s.content = content
	return s.count, s.err
}

type adapterEmbeddingCall struct {
	endpoint string
	apiKey   string
	alias    string
	inputs   []string
}

type adapterEmbedderStub struct {
	calls    []adapterEmbeddingCall
	response func(call int, inputs []string) []Embedding
	err      error
}

func (s *adapterEmbedderStub) Embed(_ context.Context, endpoint, apiKey, alias string, inputs []string) ([]Embedding, error) {
	s.calls = append(s.calls, adapterEmbeddingCall{
		endpoint: endpoint,
		apiKey:   apiKey,
		alias:    alias,
		inputs:   append([]string(nil), inputs...),
	})
	if s.err != nil {
		return nil, s.err
	}
	if s.response == nil {
		return nil, nil
	}
	return s.response(len(s.calls)-1, inputs), nil
}

func unitDocumentEmbedding(index int) []float64 {
	values := make([]float64, expectedEmbeddingDimension)
	values[index] = 1
	return values
}
