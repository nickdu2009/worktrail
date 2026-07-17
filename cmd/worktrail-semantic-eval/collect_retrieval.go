package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	semanticeval "github.com/nickdu2009/worktrail/internal/semantic/eval"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/retrieve"
)

type scopeRuntime struct {
	scope  string
	root   string
	active *generation.Active
}

// collectRetrievalRankings gathers explicit lane rankings for release engineering.
// It never infers ranks from public search JSON output.
func collectRetrievalRankings(ctx context.Context, labelsPath, scope string) (semanticeval.RetrievalRankings, error) {
	labelsFile, err := os.Open(labelsPath)
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}
	defer labelsFile.Close()
	labels, err := semanticeval.DecodeRetrievalLabels(labelsFile)
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}

	env, err := paths.Discover()
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}
	roots, err := paths.DiscoverSemanticRoots()
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}
	composed, err := composition.Build(composition.Input{
		Roots:    roots,
		Versions: composition.DefaultSubsystemVersions(),
	})
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}
	if _, err := composed.Controller.Start(ctx); err != nil {
		return semanticeval.RetrievalRankings{}, err
	}

	scopes, err := collectScopes(scope)
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}
	runtimes := make([]scopeRuntime, 0, len(scopes))
	defer func() {
		for i := len(runtimes) - 1; i >= 0; i-- {
			_ = runtimes[i].active.Close()
		}
	}()

	metadata, err := generation.NewRebuildMetadata(
		composed.Identity.RecallProfileID,
		composed.Identity.ModelSpaceID,
		composed.Identity.RecallProfile.SQLiteVecVersion,
		composed.Identity.ModelSpace.Dimension,
	)
	if err != nil {
		return semanticeval.RetrievalRankings{}, err
	}
	for _, scopeName := range scopes {
		root, err := env.ScopeRoot(scopeName)
		if err != nil {
			return semanticeval.RetrievalRankings{}, err
		}
		semanticDir, err := env.SemanticIndexRoot(scopeName)
		if err != nil {
			return semanticeval.RetrievalRankings{}, err
		}
		active, err := generation.OpenActive(ctx, semanticDir, metadata)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("open active generation for %s: %w", scopeName, err)
		}
		runtimes = append(runtimes, scopeRuntime{
			scope:  scopeName,
			root:   root,
			active: active,
		})
	}

	policy := retrieve.DefaultPolicy()
	embedder := retrieve.DaemonQueryEmbedder{
		Embedder:    composed.Client,
		Credentials: composed.Store,
	}
	out := semanticeval.RetrievalRankings{
		Schema: semanticeval.RetrievalRankingsSchema,
	}
	for _, query := range labels.Queries {
		entryIDs, err := collectEntryFTS(env, scopes, query.Text, policy.LaneTopK)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("entry fts for %q: %w", query.ID, err)
		}
		var chunkHits, denseHits []retrieve.LaneHit
		for _, runtime := range runtimes {
			fts, err := (retrieve.GenerationChunkFTS{Active: runtime.active}).SearchChunks(ctx, query.Text, policy.LaneTopK)
			if err != nil {
				return semanticeval.RetrievalRankings{}, fmt.Errorf("chunk fts for %q: %w", query.ID, err)
			}
			chunkHits = append(chunkHits, fts...)
		}
		embedding, err := embedder.EmbedQuery(ctx, query.Text)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("embed query %q: %w", query.ID, err)
		}
		for _, runtime := range runtimes {
			knn, err := (retrieve.GenerationVectorKNN{Active: runtime.active}).SearchChunks(ctx, embedding, policy.LaneTopK)
			if err != nil {
				return semanticeval.RetrievalRankings{}, fmt.Errorf("dense knn for %q: %w", query.ID, err)
			}
			denseHits = append(denseHits, knn...)
		}
		chunkHits = renumberHits(chunkHits)
		denseHits = renumberHits(denseHits)
		fused := retrieve.FuseRanks(policy.RRFK, chunkHits, denseHits)
		hydrated, err := hydrateAcrossRoots(ctx, runtimes, fused)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("hydrate for %q: %w", query.ID, err)
		}
		governed := retrieve.ApplyGovernance(hydrated, policy)

		out.Queries = append(out.Queries, semanticeval.RetrievalQueryRankings{
			QueryID: query.ID,
			Lanes: map[string][]string{
				semanticeval.LaneEntryFTS: entryIDs,
				semanticeval.LaneChunkFTS: entryIDsFromHits(chunkHits),
				semanticeval.LaneDense:    entryIDsFromHits(denseHits),
				semanticeval.LaneRRF:      entryIDsFromCandidates(fused),
				semanticeval.LaneGoverned: entryIDsFromCandidates(governed),
			},
		})
	}
	return out, nil
}

func collectScopes(scope string) ([]string, error) {
	switch strings.TrimSpace(scope) {
	case "project", "user":
		return []string{scope}, nil
	case "all", "":
		return []string{"user", "project"}, nil
	default:
		return nil, fmt.Errorf("invalid collect scope %q", scope)
	}
}

func collectEntryFTS(env paths.Env, scopes []string, query string, limit int) ([]string, error) {
	var ids []string
	for _, scope := range scopes {
		root, err := env.ScopeRoot(scope)
		if err != nil {
			return nil, err
		}
		hits, err := index.DetailedSearch(root, index.Query{
			Scope:   scope,
			Content: query,
			Limit:   limit,
		})
		if err != nil {
			return nil, err
		}
		for _, hit := range hits {
			ids = append(ids, hit.Entry.ID)
		}
	}
	return semanticeval.RankedEntryIDs(ids), nil
}

func renumberHits(hits []retrieve.LaneHit) []retrieve.LaneHit {
	out := make([]retrieve.LaneHit, 0, len(hits))
	seenChunk := map[string]bool{}
	for _, hit := range hits {
		if hit.ChunkID == "" || hit.EntryID == "" || seenChunk[hit.ChunkID] {
			continue
		}
		seenChunk[hit.ChunkID] = true
		hit.Rank = len(out) + 1
		out = append(out, hit)
	}
	return out
}

func entryIDsFromHits(hits []retrieve.LaneHit) []string {
	ids := make([]string, len(hits))
	for i, hit := range hits {
		ids[i] = hit.EntryID
	}
	return semanticeval.RankedEntryIDs(ids)
}

func entryIDsFromCandidates(candidates []retrieve.Candidate) []string {
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.EntryID
	}
	return semanticeval.RankedEntryIDs(ids)
}

func hydrateAcrossRoots(ctx context.Context, runtimes []scopeRuntime, candidates []retrieve.Candidate) ([]retrieve.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.EntryID
	}
	entriesByID := map[string]index.Entry{}
	for _, runtime := range runtimes {
		entries, err := index.EntriesByID(runtime.root, ids)
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			entriesByID[entry.ID] = entry
		}
	}
	hydrated := make([]retrieve.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		entry, ok := entriesByID[candidate.EntryID]
		if !ok {
			return nil, fmt.Errorf("semantic recall entry %q is missing", candidate.EntryID)
		}
		candidate.DocumentID = entry.ID
		candidate.SourceOfTruth = entry.SourceOfTruth
		candidate.Active = entry.Active
		candidate.Lifecycle = entry.Lifecycle
		candidate.Superseded = len(entry.SupersededBy) > 0
		hydrated = append(hydrated, candidate)
	}
	return hydrated, nil
}
