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

	// Open and validate every generation/profile before starting the daemon.
	var profiles []string
	for _, scopeName := range scopes {
		semanticDir, err := env.SemanticIndexRoot(scopeName)
		if err != nil {
			return semanticeval.RetrievalRankings{}, err
		}
		pointer, err := generation.ReadActive(semanticDir)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("read active pointer for %s: %w", scopeName, err)
		}
		profiles = append(profiles, pointer.RecallProfileID)
	}
	if len(profiles) > 1 {
		for i := 1; i < len(profiles); i++ {
			if profiles[i] != profiles[0] {
				return semanticeval.RetrievalRankings{}, fmt.Errorf("semantic profile mismatch across scopes")
			}
		}
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

	if _, err := composed.Controller.Start(ctx); err != nil {
		return semanticeval.RetrievalRankings{}, err
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
		entryIDs, err := collectEntryFTS(env, scopes, query.Text, policy.HardCap)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("entry fts for %q: %w", query.ID, err)
		}

		var (
			chunkLanes, denseLanes [][]retrieve.LaneHit
			allMatches             []retrieve.MatchHit
			laneDiag               []retrieve.Lane
		)
		for _, runtime := range runtimes {
			ftsAdapter := retrieve.GenerationChunkFTS{
				Active:    runtime.active,
				Tokenizer: index.NewTokenizer(),
				Scope:     runtime.scope,
			}
			chunkHits, matches, lane, err := retrieve.CollectEntryLane(
				func(limit int) ([]retrieve.RawChunkHit, error) {
					return ftsAdapter.SearchChunks(ctx, query.Text, limit)
				},
				policy,
				runtime.scope,
				retrieve.LaneNameChunkFTS,
				retrieve.ExactFilters{},
				false,
			)
			if err != nil {
				return semanticeval.RetrievalRankings{}, fmt.Errorf("chunk fts for %q: %w", query.ID, err)
			}
			chunkLanes = append(chunkLanes, chunkHits)
			allMatches = append(allMatches, matches...)
			laneDiag = append(laneDiag, lane)
		}

		embedding, err := embedder.EmbedQuery(ctx, query.Text)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("embed query %q: %w", query.ID, err)
		}
		for _, runtime := range runtimes {
			knnAdapter := retrieve.GenerationVectorKNN{Active: runtime.active, Scope: runtime.scope}
			denseHits, matches, lane, err := retrieve.CollectEntryLane(
				func(limit int) ([]retrieve.RawChunkHit, error) {
					return knnAdapter.SearchChunks(ctx, embedding, limit)
				},
				policy,
				runtime.scope,
				retrieve.LaneNameVectorKNN,
				retrieve.ExactFilters{},
				true,
			)
			if err != nil {
				return semanticeval.RetrievalRankings{}, fmt.Errorf("dense knn for %q: %w", query.ID, err)
			}
			denseLanes = append(denseLanes, denseHits)
			allMatches = append(allMatches, matches...)
			laneDiag = append(laneDiag, lane)
		}

		allLanes := append(append([][]retrieve.LaneHit{}, chunkLanes...), denseLanes...)
		fused := retrieve.FuseRanks(policy.RRFK, allLanes...)
		hydrated, err := hydrateAcrossRoots(ctx, runtimes, fused)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("hydrate for %q: %w", query.ID, err)
		}
		governed := retrieve.ApplyGovernance(hydrated, policy)

		loaders := map[string]retrieve.ChunkLoader{}
		for _, runtime := range runtimes {
			loaders[runtime.scope] = retrieve.GenerationChunkLoader{Active: runtime.active}
		}
		named := []retrieve.NamedLaneHits{
			{Name: retrieve.LaneNameChunkFTS, Hits: flattenHits(chunkLanes)},
			{Name: retrieve.LaneNameVectorKNN, Hits: flattenHits(denseLanes)},
		}
		evidence, err := retrieve.SelectEvidence(ctx, governed, named, allMatches, loaders, policy)
		if err != nil {
			return semanticeval.RetrievalRankings{}, fmt.Errorf("evidence for %q: %w", query.ID, err)
		}

		item := semanticeval.RetrievalQueryRankings{
			QueryID: query.ID,
			Lanes: map[string][]semanticeval.ScopedEntryID{
				semanticeval.LaneEntryFTS: entryIDs,
				semanticeval.LaneChunkFTS: scopedIDsFromHits(flattenHits(chunkLanes)),
				semanticeval.LaneDense:    scopedIDsFromHits(flattenHits(denseLanes)),
				semanticeval.LaneRRF:      scopedIDsFromCandidates(fused),
				semanticeval.LaneGoverned: scopedIDsFromCandidates(governed),
			},
			Evidence:    rankedEvidenceFrom(governed, evidence),
			Diagnostics: diagnosticsFromLanes(governed, evidence, laneDiag),
		}
		out.Queries = append(out.Queries, item)
	}
	return out, nil
}

func rankedEvidenceFrom(candidates []retrieve.Candidate, evidence []retrieve.EntryEvidence) []semanticeval.RankedEntryEvidence {
	out := make([]semanticeval.RankedEntryEvidence, 0, len(candidates))
	for i, candidate := range candidates {
		item := semanticeval.RankedEntryEvidence{Scope: candidate.Scope, EntryID: candidate.EntryID}
		if i < len(evidence) {
			for _, chunk := range evidence[i].Chunks {
				mapped := semanticeval.RankedEvidenceChunk{
					ChunkID:           chunk.ChunkID,
					EvidenceRole:      chunk.EvidenceRole,
					StructuralGroupID: chunk.StructuralGroupID,
					Primary: semanticeval.EvalByteRange{
						StartByte: chunk.PrimarySourceRange.StartByte,
						EndByte:   chunk.PrimarySourceRange.EndByte,
					},
				}
				if chunk.ContextSourceRange != nil {
					mapped.Context = &semanticeval.EvalByteRange{
						StartByte: chunk.ContextSourceRange.StartByte,
						EndByte:   chunk.ContextSourceRange.EndByte,
					}
				}
				if chunk.StructuralGroupSourceRange != nil {
					mapped.Group = &semanticeval.EvalByteRange{
						StartByte: chunk.StructuralGroupSourceRange.StartByte,
						EndByte:   chunk.StructuralGroupSourceRange.EndByte,
					}
				}
				item.Chunks = append(item.Chunks, mapped)
			}
		}
		out = append(out, item)
	}
	return out
}

func diagnosticsFromLanes(candidates []retrieve.Candidate, evidence []retrieve.EntryEvidence, lanes []retrieve.Lane) *semanticeval.QueryDiagnostics {
	diag := &semanticeval.QueryDiagnostics{EntryOccupancy: map[string]int{}}
	seenEntries := map[string]bool{}
	for _, candidate := range candidates {
		key := candidate.Scope + "\x00" + candidate.EntryID
		if seenEntries[key] {
			diag.DuplicateEntries++
		}
		seenEntries[key] = true
	}
	seenChunks := map[string]bool{}
	for i, entry := range evidence {
		scope := ""
		if i < len(candidates) {
			scope = candidates[i].Scope
		}
		for _, chunk := range entry.Chunks {
			key := scope + "\x00" + chunk.ChunkID
			if seenChunks[key] {
				diag.DuplicateChunkEvidence++
			}
			seenChunks[key] = true
			if chunk.PrimarySourceRange.EndByte < chunk.PrimarySourceRange.StartByte {
				diag.RangeViolations++
			}
		}
	}
	for _, lane := range lanes {
		if lane.RefillRounds > diag.RefillRounds {
			diag.RefillRounds = lane.RefillRounds
		}
		if lane.WindowSaturated {
			diag.WindowSaturated = true
		}
	}
	return diag
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

func collectEntryFTS(env paths.Env, scopes []string, query string, limit int) ([]semanticeval.ScopedEntryID, error) {
	var ids []semanticeval.ScopedEntryID
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
			ids = append(ids, semanticeval.ScopedEntryID{Scope: scope, EntryID: hit.Entry.ID})
		}
	}
	return semanticeval.RankedScopedEntryIDs(ids), nil
}

func flattenHits(lanes [][]retrieve.LaneHit) []retrieve.LaneHit {
	var out []retrieve.LaneHit
	for _, lane := range lanes {
		out = append(out, lane...)
	}
	return out
}

func scopedIDsFromHits(hits []retrieve.LaneHit) []semanticeval.ScopedEntryID {
	ids := make([]semanticeval.ScopedEntryID, len(hits))
	for i, hit := range hits {
		ids[i] = semanticeval.ScopedEntryID{Scope: hit.Scope, EntryID: hit.EntryID}
	}
	return semanticeval.RankedScopedEntryIDs(ids)
}

func scopedIDsFromCandidates(candidates []retrieve.Candidate) []semanticeval.ScopedEntryID {
	ids := make([]semanticeval.ScopedEntryID, len(candidates))
	for i, candidate := range candidates {
		ids[i] = semanticeval.ScopedEntryID{Scope: candidate.Scope, EntryID: candidate.EntryID}
	}
	return semanticeval.RankedScopedEntryIDs(ids)
}

func hydrateAcrossRoots(ctx context.Context, runtimes []scopeRuntime, candidates []retrieve.Candidate) ([]retrieve.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	hydrators := make(map[string]retrieve.IndexEntryHydrator, len(runtimes))
	for _, runtime := range runtimes {
		hydrators[runtime.scope] = retrieve.IndexEntryHydrator{
			Root:  runtime.root,
			Query: index.Query{Scope: runtime.scope},
		}
	}
	hydrated := make([]retrieve.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		hydrator, ok := hydrators[candidate.Scope]
		if !ok {
			return nil, fmt.Errorf("semantic recall scope %q is missing", candidate.Scope)
		}
		mapped, err := hydrator.Hydrate(ctx, []retrieve.Candidate{candidate})
		if err != nil {
			return nil, err
		}
		if len(mapped) == 0 {
			continue
		}
		hydrated = append(hydrated, mapped[0])
	}
	return hydrated, nil
}
