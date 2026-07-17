package retrieve

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
)

const (
	queryEmbeddingDimension = 1024
	queryEmbeddingTolerance = 1e-3
)

// DaemonCredentials supplies the active local daemon connection details.
// daemon.Store satisfies this interface.
type DaemonCredentials interface {
	Load() (daemon.Descriptor, error)
	APIKey() (string, error)
}

// DaemonQueryEmbedder adapts the daemon embedding boundary to QueryEmbedder.
// It reads endpoint, API key, and model alias on demand, but never starts or
// stops the daemon.
type DaemonQueryEmbedder struct {
	Embedder    daemon.Embedder
	Credentials DaemonCredentials
}

// EmbedQuery embeds exactly one query using the currently stored daemon
// descriptor. Query text and API keys are intentionally never retained.
func (a DaemonQueryEmbedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	if a.Embedder == nil {
		return nil, errors.New("semantic daemon embedder is not configured")
	}
	if a.Credentials == nil {
		return nil, errors.New("semantic daemon credentials are not configured")
	}

	descriptor, err := a.Credentials.Load()
	if err != nil {
		return nil, fmt.Errorf("load semantic daemon descriptor: %w", err)
	}
	apiKey, err := a.Credentials.APIKey()
	if err != nil {
		return nil, fmt.Errorf("load semantic daemon API key: %w", err)
	}
	embeddings, err := a.Embedder.Embed(ctx, descriptor.Endpoint, apiKey, descriptor.Alias, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed semantic daemon query: %w", err)
	}
	if len(embeddings) != 1 || embeddings[0].Index != 0 {
		return nil, errors.New("semantic daemon query response must contain exactly one index 0 embedding")
	}
	return convertQueryEmbedding(embeddings[0].Values)
}

func convertQueryEmbedding(values []float64) ([]float32, error) {
	if len(values) != queryEmbeddingDimension {
		return nil, fmt.Errorf("semantic daemon query embedding dimension = %d, want %d", len(values), queryEmbeddingDimension)
	}

	out := make([]float32, len(values))
	var squaredLength float64
	for i, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, errors.New("semantic daemon query embedding contains a non-finite value")
		}
		converted := float32(value)
		if math.IsNaN(float64(converted)) || math.IsInf(float64(converted), 0) {
			return nil, errors.New("semantic daemon query embedding conversion contains a non-finite value")
		}
		out[i] = converted
		squaredLength += float64(converted) * float64(converted)
	}
	length := math.Sqrt(squaredLength)
	if math.IsNaN(length) || math.IsInf(length, 0) || math.Abs(length-1) > queryEmbeddingTolerance {
		return nil, errors.New("semantic daemon query embedding is not L2-normalized after conversion")
	}
	return out, nil
}

// IndexEntryHydrator attaches index entry metadata to semantic candidates.
// Query supplies only the index filters; it does not apply semantic governance.
type IndexEntryHydrator struct {
	Root  string
	Query index.Query
}

// Hydrate loads all requested entries in one batch, applies explicit index
// filters, and restores the original candidate order for matching entries.
func (a IndexEntryHydrator) Hydrate(ctx context.Context, candidates []Candidate) ([]Candidate, error) {
	pairs, err := a.load(ctx, candidates)
	if err != nil {
		return nil, err
	}

	hydrated := make([]Candidate, 0, len(pairs))
	for _, pair := range pairs {
		candidate := pair.candidate
		candidate.DocumentID = pair.entry.ID
		candidate.SourceOfTruth = pair.entry.SourceOfTruth
		candidate.Active = pair.entry.Active
		candidate.Lifecycle = pair.entry.Lifecycle
		candidate.Superseded = len(pair.entry.SupersededBy) > 0
		hydrated = append(hydrated, candidate)
	}
	return hydrated, nil
}

// MapCandidates maps candidates to presentation-ready index results. It keeps
// candidate order and scores intact, and does not run semantic governance.
func (a IndexEntryHydrator) MapCandidates(ctx context.Context, candidates []Candidate) ([]index.Result, error) {
	pairs, err := a.load(ctx, candidates)
	if err != nil {
		return nil, err
	}

	results := make([]index.Result, len(pairs))
	for i, pair := range pairs {
		results[i] = index.Result{Entry: pair.entry, Score: pair.candidate.Score}
	}
	return results, nil
}

type candidateEntry struct {
	candidate Candidate
	entry     index.Entry
}

func (a IndexEntryHydrator) load(ctx context.Context, candidates []Candidate) ([]candidateEntry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return []candidateEntry{}, nil
	}

	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.EntryID
	}
	entries, err := index.EntriesByID(a.Root, ids)
	if err != nil {
		return nil, fmt.Errorf("load semantic recall entries: %w", err)
	}
	entriesByID := make(map[string]index.Entry, len(entries))
	for _, entry := range entries {
		entriesByID[entry.ID] = entry
	}

	pairs := make([]candidateEntry, 0, len(candidates))
	for _, candidate := range candidates {
		entry, ok := entriesByID[candidate.EntryID]
		if !ok {
			return nil, fmt.Errorf("semantic recall entry %q is missing", candidate.EntryID)
		}
		if !matchesIndexFilters(entry, a.Query) {
			continue
		}
		pairs = append(pairs, candidateEntry{candidate: candidate, entry: entry})
	}
	return pairs, nil
}

func matchesIndexFilters(entry index.Entry, query index.Query) bool {
	if query.Scope != "" && entry.Scope != query.Scope {
		return false
	}
	if query.Type != "" && entry.Type != query.Type {
		return false
	}
	if query.Topic != "" && entry.Topic != query.Topic {
		return false
	}

	tags := append([]string{}, query.Tags...)
	if query.Tag != "" {
		tags = append(tags, query.Tag)
	}
	for _, tag := range tags {
		found := false
		for _, entryTag := range entry.Tags {
			if entryTag == tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// GenerationChunkFTS adapts one caller-owned active generation to ChunkFTS.
// It never closes Active: the caller that opened the generation owns its lease.
type GenerationChunkFTS struct {
	Active *generation.Active
}

// SearchChunks queries the sealed generation's exact FTS lane.
func (a GenerationChunkFTS) SearchChunks(_ context.Context, query string, limit int) ([]LaneHit, error) {
	hits, err := a.Active.ChunkFTS(query, limit)
	if err != nil {
		return nil, fmt.Errorf("search active generation chunk FTS: %w", err)
	}
	return mapGenerationHits(hits), nil
}

// GenerationVectorKNN adapts one caller-owned active generation to VectorKNN.
// It never closes Active: the caller that opened the generation owns its lease.
type GenerationVectorKNN struct {
	Active *generation.Active
}

// SearchChunks queries the sealed generation's exact vector KNN lane.
func (a GenerationVectorKNN) SearchChunks(_ context.Context, embedding []float32, limit int) ([]LaneHit, error) {
	hits, err := a.Active.VectorKNN(embedding, limit)
	if err != nil {
		return nil, fmt.Errorf("search active generation vector KNN: %w", err)
	}
	return mapGenerationHits(hits), nil
}

func mapGenerationHits(hits []generation.ChunkHit) []LaneHit {
	out := make([]LaneHit, len(hits))
	for i, hit := range hits {
		out[i] = LaneHit{
			ChunkID: hit.ChunkID,
			EntryID: hit.EntryID,
			Rank:    hit.Rank,
		}
	}
	return out
}

// LegacyLexicalAdapter adapts the existing entry-level lexical policy for use
// after semantic generation is unavailable. Root and Query are provided by the
// composition layer; Query supplies the scope, type, topic, and tag filters.
// Recall replaces only Content and Limit with the facade request values.
type LegacyLexicalAdapter struct {
	Root  string
	Query index.Query
}

// Recall runs the existing detailed lexical FTS path. DetailedSearch preserves
// the configured lexical filters and performs no substring fallback. Its raw
// SQLite FTS5 bm25 score ranks lower values first, so Candidate.Score is that
// raw score, not an RRF score.
//
// The legacy path already returns presentation-ready lexical results. This
// adapter intentionally does not apply semantic governance to those results.
func (a LegacyLexicalAdapter) Recall(_ context.Context, queryText string, limit int) ([]Candidate, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("legacy lexical fallback limit must be greater than zero")
	}

	query := a.Query
	query.Content = queryText
	query.Limit = limit
	hits, err := index.DetailedSearch(a.Root, query)
	if err != nil {
		return nil, fmt.Errorf("search legacy lexical index: %w", err)
	}

	candidates := make([]Candidate, len(hits))
	for i, hit := range hits {
		candidates[i] = Candidate{
			ChunkID:       hit.Entry.ID,
			EntryID:       hit.Entry.ID,
			DocumentID:    hit.Entry.ID,
			Score:         hit.RawScore,
			SourceOfTruth: hit.Entry.SourceOfTruth,
			Active:        hit.Entry.Active,
			Lifecycle:     hit.Entry.Lifecycle,
			Superseded:    len(hit.Entry.SupersededBy) > 0,
		}
	}
	return candidates, nil
}
