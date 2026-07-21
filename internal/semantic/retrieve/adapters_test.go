package retrieve

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestDaemonQueryEmbedderConvertsOneEmbeddingAndPassesCredentials(t *testing.T) {
	embedder := &daemonEmbedderStub{embeddings: []daemon.Embedding{{
		Index:  0,
		Values: unitEmbedding64(),
	}}}
	adapter := DaemonQueryEmbedder{
		Embedder: embedder,
		Credentials: daemonCredentialsStub{
			descriptor: daemon.Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
			apiKey:     "secret-key",
		},
	}

	embedding, err := adapter.EmbedQuery(context.Background(), "query text")
	if err != nil {
		t.Fatalf("EmbedQuery() error = %v", err)
	}
	if len(embedding) != queryEmbeddingDimension || embedding[0] != 1 {
		t.Fatalf("EmbedQuery() embedding = len %d first %v", len(embedding), embedding[0])
	}
	if embedder.endpoint != "http://127.0.0.1:43210" || embedder.apiKey != "secret-key" || embedder.alias != "bundle-a" {
		t.Fatalf("daemon credentials = endpoint %q key %q alias %q", embedder.endpoint, embedder.apiKey, embedder.alias)
	}
	if !reflect.DeepEqual(embedder.inputs, []string{"query text"}) {
		t.Fatalf("daemon inputs = %#v, want exactly one query", embedder.inputs)
	}
}

func TestDaemonQueryEmbedderRejectsInvalidResponses(t *testing.T) {
	invalidDimension := unitEmbedding64()[:queryEmbeddingDimension-1]
	nonFinite := unitEmbedding64()
	nonFinite[0] = math.NaN()
	overflow := unitEmbedding64()
	overflow[0] = math.MaxFloat64
	notNormalized := unitEmbedding64()
	notNormalized[0] = 2

	tests := []struct {
		name       string
		embeddings []daemon.Embedding
	}{
		{name: "no embedding"},
		{name: "wrong index", embeddings: []daemon.Embedding{{Index: 1, Values: unitEmbedding64()}}},
		{name: "multiple embeddings", embeddings: []daemon.Embedding{{Index: 0, Values: unitEmbedding64()}, {Index: 1, Values: unitEmbedding64()}}},
		{name: "wrong dimension", embeddings: []daemon.Embedding{{Index: 0, Values: invalidDimension}}},
		{name: "non-finite source", embeddings: []daemon.Embedding{{Index: 0, Values: nonFinite}}},
		{name: "non-finite conversion", embeddings: []daemon.Embedding{{Index: 0, Values: overflow}}},
		{name: "not normalized", embeddings: []daemon.Embedding{{Index: 0, Values: notNormalized}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter := testDaemonQueryEmbedder(&daemonEmbedderStub{embeddings: test.embeddings})
			if _, err := adapter.EmbedQuery(context.Background(), "query"); err == nil {
				t.Fatal("EmbedQuery() error = nil, want invalid daemon response rejection")
			}
		})
	}
}

func TestDaemonQueryEmbedderPreservesEmbedderAndCredentialErrors(t *testing.T) {
	embedErr := errors.New("daemon rejected key")
	adapter := testDaemonQueryEmbedder(&daemonEmbedderStub{err: embedErr})
	if _, err := adapter.EmbedQuery(context.Background(), "query"); !errors.Is(err, embedErr) {
		t.Fatalf("EmbedQuery() error = %v, want wrapped embedder error", err)
	}

	keyErr := errors.New("key unavailable")
	adapter = DaemonQueryEmbedder{
		Embedder: &daemonEmbedderStub{},
		Credentials: daemonCredentialsStub{
			descriptor: daemon.Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
			keyErr:     keyErr,
		},
	}
	if _, err := adapter.EmbedQuery(context.Background(), "query"); !errors.Is(err, keyErr) {
		t.Fatalf("EmbedQuery() error = %v, want wrapped credential error", err)
	}
}

func TestIndexEntryHydratorPreservesOrderAndGovernanceMetadata(t *testing.T) {
	root := legacyLexicalFixture(t)
	candidates := []Candidate{
		{ChunkID: "chunk-second", EntryID: "second", Score: 0.9},
		{ChunkID: "chunk-active", EntryID: "active-one", Score: 0.8},
	}
	entries, err := index.EntriesByID(root, []string{"second", "active-one"})
	if err != nil {
		t.Fatalf("load expected entries: %v", err)
	}
	entriesByID := map[string]index.Entry{}
	for _, entry := range entries {
		entriesByID[entry.ID] = entry
	}

	hydrated, err := (IndexEntryHydrator{Root: root}).Hydrate(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}
	if got, want := candidateEntryIDs(hydrated), []string{"second", "active-one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("hydrated order = %v, want %v", got, want)
	}
	for _, candidate := range hydrated {
		entry := entriesByID[candidate.EntryID]
		if candidate.DocumentID != entry.ID ||
			candidate.SourceOfTruth != entry.SourceOfTruth ||
			candidate.Active != entry.Active ||
			candidate.Lifecycle != entry.Lifecycle ||
			candidate.Superseded != (len(entry.SupersededBy) > 0) {
			t.Fatalf("hydrated candidate = %#v, entry = %#v", candidate, entry)
		}
	}
}

func TestIndexEntryHydratorAppliesIndexFilters(t *testing.T) {
	root := legacyLexicalFixture(t)
	candidates := []Candidate{
		{EntryID: "active-one"},
		{EntryID: "second"},
		{EntryID: "wrong-tag"},
	}
	adapter := IndexEntryHydrator{
		Root: root,
		Query: index.Query{
			Scope: "project",
			Topic: "recall",
			Tags:  []string{"alpha", "common"},
		},
	}
	hydrated, err := adapter.Hydrate(context.Background(), candidates)
	if err != nil {
		t.Fatalf("Hydrate() error = %v", err)
	}
	if got, want := candidateEntryIDs(hydrated), []string{"active-one", "second"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered candidates = %v, want %v", got, want)
	}

	for name, query := range map[string]index.Query{
		"scope": {Scope: "other"},
		"type":  {Type: "other"},
		"topic": {Topic: "other"},
		"tag":   {Tag: "missing"},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := (IndexEntryHydrator{Root: root, Query: query}).Hydrate(context.Background(), candidates)
			if err != nil {
				t.Fatalf("Hydrate() error = %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("Hydrate() candidates = %#v, want filter exclusion", got)
			}
		})
	}
}

func TestIndexEntryHydratorRejectsMissingEntry(t *testing.T) {
	_, err := (IndexEntryHydrator{Root: legacyLexicalFixture(t)}).Hydrate(context.Background(), []Candidate{{EntryID: "missing"}})
	if err == nil {
		t.Fatal("Hydrate() error = nil, want missing entry rejection")
	}
}

func TestIndexEntryHydratorMapsCandidatesWithoutReordering(t *testing.T) {
	root := legacyLexicalFixture(t)
	candidates := []Candidate{
		{EntryID: "second", Score: 0.25},
		{EntryID: "active-one", Score: 0.75},
	}
	results, err := (IndexEntryHydrator{Root: root}).MapCandidates(context.Background(), candidates)
	if err != nil {
		t.Fatalf("MapCandidates() error = %v", err)
	}
	if got, want := []string{results[0].Entry.ID, results[1].Entry.ID}, []string{"second", "active-one"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("result order = %v, want %v", got, want)
	}
	if results[0].Score != 0.25 || results[1].Score != 0.75 {
		t.Fatalf("result scores = %#v, want candidate scores unchanged", results)
	}
}

func TestGenerationAdaptersMapHitsAndKeepActiveOpen(t *testing.T) {
	active := queryableActiveGeneration(t)

	ftsHits, err := (GenerationChunkFTS{Active: active}).SearchChunks(context.Background(), "needle", 1)
	if err != nil {
		t.Fatalf("FTS adapter SearchChunks() error = %v", err)
	}
	if len(ftsHits) != 1 || ftsHits[0].ChunkID != "chunk-1" || ftsHits[0].EntryID != "entry-1" || ftsHits[0].Rank != 1 || ftsHits[0].DocumentType != "rule" {
		t.Fatalf("FTS adapter hits = %#v", ftsHits)
	}

	vectorHits, err := (GenerationVectorKNN{Active: active}).SearchChunks(context.Background(), []float32{1, 0}, 1)
	if err != nil {
		t.Fatalf("vector adapter SearchChunks() error = %v", err)
	}
	if len(vectorHits) != 1 || vectorHits[0].ChunkID != "chunk-1" || vectorHits[0].EntryID != "entry-1" || vectorHits[0].Rank != 1 || vectorHits[0].DocumentType != "rule" {
		t.Fatalf("vector adapter hits = %#v", vectorHits)
	}

	if _, err := active.ChunkFTS("needle", 1); err != nil {
		t.Fatalf("Active was closed by adapter: %v", err)
	}
}

func TestGenerationAdaptersPreserveActiveQueryErrors(t *testing.T) {
	active := queryableActiveGeneration(t)
	if err := active.Close(); err != nil {
		t.Fatalf("close active generation: %v", err)
	}

	for _, test := range []struct {
		name string
		call func() error
	}{
		{
			name: "FTS",
			call: func() error {
				_, err := (GenerationChunkFTS{Active: active}).SearchChunks(context.Background(), "needle", 1)
				return err
			},
		},
		{
			name: "vector",
			call: func() error {
				_, err := (GenerationVectorKNN{Active: active}).SearchChunks(context.Background(), []float32{1, 0}, 1)
				return err
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			var queryErr *generation.QueryError
			if !errors.As(err, &queryErr) {
				t.Fatalf("adapter error = %T %[1]v, want wrapped *generation.QueryError", err)
			}
			if queryErr.Kind != generation.QueryErrorMetadata {
				t.Fatalf("query error kind = %q, want %q", queryErr.Kind, generation.QueryErrorMetadata)
			}
		})
	}
}

func TestLegacyLexicalAdapterPreservesFiltersMetadataAndLimit(t *testing.T) {
	root := legacyLexicalFixture(t)
	adapter := LegacyLexicalAdapter{
		Root: root,
		Query: index.Query{
			Scope: "project",
			Topic: "recall",
			Tags:  []string{"alpha", "common"},
		},
	}

	candidates, err := adapter.Recall(context.Background(), "needle", 1)
	if err != nil {
		t.Fatalf("legacy adapter Recall() error = %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("legacy adapter candidates = %#v, want facade limit of one", candidates)
	}
	got := candidates[0]
	if got.ChunkID != "active-one" || got.EntryID != "active-one" || got.DocumentID != "active-one" {
		t.Fatalf("candidate identity = %#v", got)
	}
	if !got.SourceOfTruth || !got.Active || got.Lifecycle != "retired" || !got.Superseded {
		t.Fatalf("candidate governance metadata = %#v", got)
	}

	hits, err := index.DetailedSearch(root, index.Query{
		Scope:   "project",
		Topic:   "recall",
		Tags:    []string{"alpha", "common"},
		Content: "needle",
		Limit:   1,
	})
	if err != nil {
		t.Fatalf("DetailedSearch() error = %v", err)
	}
	if got.Score != hits[0].RawScore {
		t.Fatalf("candidate score = %v, want raw FTS bm25 score %v", got.Score, hits[0].RawScore)
	}
}

func TestLegacyLexicalAdapterPreservesDetailedSearchErrors(t *testing.T) {
	_, err := (LegacyLexicalAdapter{Root: t.TempDir()}).Recall(context.Background(), " ", 1)
	var detailedErr *index.DetailedQueryError
	if !errors.As(err, &detailedErr) {
		t.Fatalf("adapter error = %T %[1]v, want wrapped *index.DetailedQueryError", err)
	}
}

type staticGenerationEmbedder struct{}

func (staticGenerationEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0}, nil
}

func queryableActiveGeneration(t *testing.T) *generation.Active {
	t.Helper()

	directory := t.TempDir()
	metadata := generation.Metadata{
		Schema:     "worktrail.semantic.generation.sqlite.v2",
		Generation: "test-generation",
		Profile:    "test-profile",
		ModelSpace: "test-space",
		Snapshot:   "test-snapshot",
		SQLiteVec:  "test-sqlite-vec",
		Dimension:  2,
	}
	candidate, err := generation.CreateCandidate(filepath.Join(directory, metadata.Generation+".sqlite"), metadata)
	if err != nil {
		t.Fatalf("CreateCandidate() error = %v", err)
	}
	if err := generation.BuildCandidate(context.Background(), candidate, []chunk.Chunk{{
		ChunkID:           "chunk-1",
		Scope:             "project",
		DocumentID:        "entry-1",
		Path:              "rules/entry-1.md",
		Type:              "rule",
		Kind:              chunk.KindText,
		StructuralGroupID: "group-1",
		Body:              "needle",
		MetadataTerms:     "path: rules/entry-1.md",
		ContextTerms:      "section",
		EmbeddingInput:    "needle",
		EmbeddingHash:     "hash-chunk-1",
		ChunkerVersion:    chunk.Version,
		SourceStart:       0,
		SourceEnd:         6,
		SourceSizeByte:    6,
	}}, staticGenerationEmbedder{}, index.NewTokenizer()); err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	if err := candidate.SealCandidate(); err != nil {
		t.Fatalf("SealCandidate() error = %v", err)
	}
	if _, err := generation.Activate(context.Background(), directory, func(context.Context) (generation.ActivationCandidate, error) {
		return generation.ActivationCandidate{
			Scope:           "project",
			GenerationID:    metadata.Generation,
			RecallProfileID: metadata.Profile,
			BundleID:        "test-bundle",
			SnapshotHash:    metadata.Snapshot,
		}, nil
	}, generation.ActivateOptions{}); err != nil {
		t.Fatalf("Activate() error = %v", err)
	}
	active, err := generation.OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })
	return active
}

func legacyLexicalFixture(t *testing.T) string {
	t.Helper()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"scope":"project"}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	writeLegacyDoc(t, root, "state/active/active-one.md", map[string]any{
		"id":              "active-one",
		"scope":           "project",
		"topic":           "recall",
		"title":           "Needle active",
		"tags":            []string{"alpha", "common"},
		"source_of_truth": true,
		"lifecycle":       "retired",
		"superseded_by":   []string{"current-one"},
	}, "Needle active\n\nneedle")
	writeLegacyDoc(t, root, "rules/second.md", map[string]any{
		"id":    "second",
		"scope": "project",
		"topic": "recall",
		"title": "Second",
		"tags":  []string{"alpha", "common"},
	}, "Needle second\n\nneedle")
	writeLegacyDoc(t, root, "rules/wrong-tag.md", map[string]any{
		"id":    "wrong-tag",
		"scope": "project",
		"topic": "recall",
		"tags":  []string{"other"},
	}, "Needle wrong tag\n\nneedle")
	if _, err := index.Rebuild(root, index.RebuildOptions{Scope: "project"}); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	return root
}

func writeLegacyDoc(t *testing.T, root, relativePath string, metadata map[string]any, body string) {
	t.Helper()
	path := filepath.Join(root, relativePath)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("create document directory: %v", err)
	}
	content, err := store.RenderMarkdown(metadata, body)
	if err != nil {
		t.Fatalf("render document: %v", err)
	}
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write document: %v", err)
	}
}

type daemonEmbedderStub struct {
	embeddings []daemon.Embedding
	err        error
	endpoint   string
	apiKey     string
	alias      string
	inputs     []string
}

func (s *daemonEmbedderStub) Embed(_ context.Context, endpoint, apiKey, alias string, inputs []string) ([]daemon.Embedding, error) {
	s.endpoint = endpoint
	s.apiKey = apiKey
	s.alias = alias
	s.inputs = append([]string(nil), inputs...)
	return s.embeddings, s.err
}

type daemonCredentialsStub struct {
	descriptor daemon.Descriptor
	loadErr    error
	apiKey     string
	keyErr     error
}

func (s daemonCredentialsStub) Load() (daemon.Descriptor, error) {
	return s.descriptor, s.loadErr
}

func (s daemonCredentialsStub) APIKey() (string, error) {
	return s.apiKey, s.keyErr
}

func testDaemonQueryEmbedder(embedder daemon.Embedder) DaemonQueryEmbedder {
	return DaemonQueryEmbedder{
		Embedder: embedder,
		Credentials: daemonCredentialsStub{
			descriptor: daemon.Descriptor{Endpoint: "http://127.0.0.1:43210", Alias: "bundle-a"},
			apiKey:     "secret-key",
		},
	}
}

func unitEmbedding64() []float64 {
	values := make([]float64, queryEmbeddingDimension)
	values[0] = 1
	return values
}

func candidateEntryIDs(candidates []Candidate) []string {
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.EntryID
	}
	return ids
}
