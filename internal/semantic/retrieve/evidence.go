package retrieve

import (
	"context"
	"fmt"
	"sort"
)

const (
	EvidenceRoleMatch    = "match"
	EvidenceRoleNeighbor = "neighbor"
)

// MatchHit is one filter-eligible raw chunk observed by a retrieval lane.
// Rank is the lane's raw chunk rank before entry collapse.
type MatchHit struct {
	Scope   string
	Lane    string
	ChunkID string
	EntryID string
	Rank    int
}

// ByteRange is a zero-based, half-open UTF-8 byte range.
type ByteRange struct {
	StartByte int
	EndByte   int
}

// ChunkMeta is sealed chunk metadata needed to render evidence.
type ChunkMeta struct {
	ChunkID           string
	EntryID           string
	ChunkKind         string
	StructuralGroupID string
	HeadingBreadcrumb []string
	ChunkOrder        int
	Primary           ByteRange
	Context           *ByteRange
	Group             *ByteRange
	PrevChunkID       string
	NextChunkID       string
}

// ChunkLoader loads sealed chunk metadata for evidence expansion.
type ChunkLoader interface {
	LoadChunks(ctx context.Context, chunkIDs []string) (map[string]ChunkMeta, error)
}

// ChunkEvidence is one cited chunk attached after entry ranking.
type ChunkEvidence struct {
	ChunkID                    string
	ChunkKind                  string
	StructuralGroupID          string
	HeadingBreadcrumb          []string
	EvidenceRole               string
	Lanes                      []string
	BestChunkRanks             map[string]int
	PrimarySourceRange         ByteRange
	ContextSourceRange         *ByteRange
	StructuralGroupSourceRange *ByteRange
}

// EntryEvidence is the post-ranking evidence for one final candidate.
// LexicalRank and SemanticRank are 0 when that lane did not establish the entry.
type EntryEvidence struct {
	LexicalRank  int
	SemanticRank int
	FinalRank    int
	Chunks       []ChunkEvidence
}

// NamedLaneHits pairs a lane name with its collapsed entry ranks.
type NamedLaneHits struct {
	Name string
	Hits []LaneHit
}

type mergedMatch struct {
	scope     string
	chunkID   string
	entryID   string
	lanes     []string
	bestRanks map[string]int
	bestRank  int
}

// SelectEvidence attaches bounded matching chunks and same-group neighbors
// after final entry ranking. It never changes candidate order or scores.
func SelectEvidence(
	ctx context.Context,
	candidates []Candidate,
	lanes []NamedLaneHits,
	matches []MatchHit,
	loaders map[string]ChunkLoader,
	policy Policy,
) ([]EntryEvidence, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	maxMatches := policy.MaxMatchingChunks
	if maxMatches <= 0 {
		maxMatches = DefaultPolicy().MaxMatchingChunks
	}
	maxNeighbors := policy.MaxNeighborsPerAnchor
	if maxNeighbors <= 0 {
		maxNeighbors = DefaultPolicy().MaxNeighborsPerAnchor
	}

	out := make([]EntryEvidence, len(candidates))
	for i, candidate := range candidates {
		out[i] = EntryEvidence{
			LexicalRank:  findNamedLaneRank(lanes, LaneNameChunkFTS, candidate.Scope, candidate.EntryID),
			SemanticRank: findNamedLaneRank(lanes, LaneNameVectorKNN, candidate.Scope, candidate.EntryID),
			FinalRank:    i + 1,
		}
	}

	mergedByEntry := mergeMatches(matches)
	needIDsByScope := make(map[string][]string)
	selectedMatches := make([][]mergedMatch, len(candidates))
	for i, candidate := range candidates {
		key := candidateKey(candidate.Scope, candidate.EntryID)
		entryMatches := append([]mergedMatch(nil), mergedByEntry[key]...)
		if len(entryMatches) == 0 {
			continue
		}
		sort.SliceStable(entryMatches, func(a, b int) bool {
			if entryMatches[a].bestRank != entryMatches[b].bestRank {
				return entryMatches[a].bestRank < entryMatches[b].bestRank
			}
			return entryMatches[a].chunkID < entryMatches[b].chunkID
		})
		if len(entryMatches) > maxMatches {
			entryMatches = entryMatches[:maxMatches]
		}
		selectedMatches[i] = entryMatches
		for _, match := range entryMatches {
			needIDsByScope[candidate.Scope] = append(needIDsByScope[candidate.Scope], match.chunkID)
		}
	}

	metaByScope, err := loadChunkMeta(ctx, loaders, needIDsByScope)
	if err != nil {
		return nil, err
	}

	neighborNeed := make(map[string][]string)
	for i, candidate := range candidates {
		metas := metaByScope[candidate.Scope]
		for _, match := range selectedMatches[i] {
			meta, ok := metas[match.chunkID]
			if !ok {
				continue
			}
			for _, neighborID := range []string{meta.PrevChunkID, meta.NextChunkID} {
				if neighborID == "" || neighborID == match.chunkID {
					continue
				}
				neighborNeed[candidate.Scope] = append(neighborNeed[candidate.Scope], neighborID)
			}
		}
	}
	neighborMeta, err := loadChunkMeta(ctx, loaders, neighborNeed)
	if err != nil {
		return nil, err
	}
	for scope, metas := range neighborMeta {
		if metaByScope[scope] == nil {
			metaByScope[scope] = metas
			continue
		}
		for id, meta := range metas {
			metaByScope[scope][id] = meta
		}
	}

	claimedNeighbors := make(map[string]bool)
	for i, candidate := range candidates {
		metas := metaByScope[candidate.Scope]
		type rankedMatch struct {
			match mergedMatch
			meta  ChunkMeta
		}
		ranked := make([]rankedMatch, 0, len(selectedMatches[i]))
		for _, match := range selectedMatches[i] {
			meta, ok := metas[match.chunkID]
			if !ok {
				continue
			}
			ranked = append(ranked, rankedMatch{match: match, meta: meta})
		}
		sort.SliceStable(ranked, func(a, b int) bool {
			if ranked[a].match.bestRank != ranked[b].match.bestRank {
				return ranked[a].match.bestRank < ranked[b].match.bestRank
			}
			if ranked[a].meta.ChunkOrder != ranked[b].meta.ChunkOrder {
				return ranked[a].meta.ChunkOrder < ranked[b].meta.ChunkOrder
			}
			return ranked[a].match.chunkID < ranked[b].match.chunkID
		})
		if len(ranked) > maxMatches {
			ranked = ranked[:maxMatches]
		}

		matchIDs := make(map[string]bool, len(ranked))
		for _, item := range ranked {
			matchIDs[item.match.chunkID] = true
		}

		chunks := make([]ChunkEvidence, 0, len(ranked)*2)
		for _, item := range ranked {
			chunks = append(chunks, chunkEvidenceFrom(item.meta, EvidenceRoleMatch, item.match.lanes, item.match.bestRanks))
			if maxNeighbors <= 0 {
				continue
			}
			neighbor, ok := chooseNeighbor(item.meta, metas, matchIDs, claimedNeighbors, candidate.Scope)
			if !ok {
				continue
			}
			claimedNeighbors[candidateKey(candidate.Scope, neighbor.ChunkID)] = true
			chunks = append(chunks, chunkEvidenceFrom(neighbor, EvidenceRoleNeighbor, nil, nil))
		}
		out[i].Chunks = chunks
	}
	return out, nil
}

func mergeMatches(matches []MatchHit) map[string][]mergedMatch {
	byKey := make(map[string]*mergedMatch)
	order := make([]string, 0)
	for _, hit := range matches {
		if hit.Scope == "" || hit.ChunkID == "" || hit.EntryID == "" || hit.Rank <= 0 || hit.Lane == "" {
			continue
		}
		key := candidateKey(hit.Scope, hit.ChunkID)
		existing, ok := byKey[key]
		if !ok {
			existing = &mergedMatch{
				scope:     hit.Scope,
				chunkID:   hit.ChunkID,
				entryID:   hit.EntryID,
				bestRanks: map[string]int{},
				bestRank:  hit.Rank,
			}
			byKey[key] = existing
			order = append(order, key)
		}
		if current, seen := existing.bestRanks[hit.Lane]; !seen || hit.Rank < current {
			existing.bestRanks[hit.Lane] = hit.Rank
		}
		if hit.Rank < existing.bestRank {
			existing.bestRank = hit.Rank
		}
		if !containsString(existing.lanes, hit.Lane) {
			existing.lanes = append(existing.lanes, hit.Lane)
		}
	}
	for _, key := range order {
		sort.Strings(byKey[key].lanes)
	}

	byEntry := make(map[string][]mergedMatch)
	for _, key := range order {
		item := byKey[key]
		entryKey := candidateKey(item.scope, item.entryID)
		byEntry[entryKey] = append(byEntry[entryKey], *item)
	}
	return byEntry
}

func chooseNeighbor(
	anchor ChunkMeta,
	metas map[string]ChunkMeta,
	matchIDs map[string]bool,
	claimed map[string]bool,
	scope string,
) (ChunkMeta, bool) {
	for _, neighborID := range []string{anchor.PrevChunkID, anchor.NextChunkID} {
		if neighborID == "" || matchIDs[neighborID] {
			continue
		}
		if claimed[candidateKey(scope, neighborID)] {
			continue
		}
		neighbor, ok := metas[neighborID]
		if !ok {
			continue
		}
		if neighbor.StructuralGroupID == "" || neighbor.StructuralGroupID != anchor.StructuralGroupID {
			continue
		}
		return neighbor, true
	}
	return ChunkMeta{}, false
}

func chunkEvidenceFrom(meta ChunkMeta, role string, lanes []string, ranks map[string]int) ChunkEvidence {
	evidence := ChunkEvidence{
		ChunkID:            meta.ChunkID,
		ChunkKind:          meta.ChunkKind,
		StructuralGroupID:  meta.StructuralGroupID,
		HeadingBreadcrumb:  append([]string(nil), meta.HeadingBreadcrumb...),
		EvidenceRole:       role,
		PrimarySourceRange: meta.Primary,
	}
	if role == EvidenceRoleMatch {
		evidence.Lanes = append([]string(nil), lanes...)
		if ranks != nil {
			evidence.BestChunkRanks = copyRanks(ranks)
		}
	}
	if meta.Context != nil {
		cloned := *meta.Context
		evidence.ContextSourceRange = &cloned
	}
	if meta.Group != nil {
		cloned := *meta.Group
		evidence.StructuralGroupSourceRange = &cloned
	}
	return evidence
}

func copyRanks(ranks map[string]int) map[string]int {
	out := make(map[string]int, len(ranks))
	for key, value := range ranks {
		out[key] = value
	}
	return out
}

func findNamedLaneRank(lanes []NamedLaneHits, laneName, scope, entryID string) int {
	for _, lane := range lanes {
		if lane.Name != laneName {
			continue
		}
		for _, hit := range lane.Hits {
			if hit.Scope == scope && hit.EntryID == entryID {
				return hit.Rank
			}
		}
	}
	return 0
}

func loadChunkMeta(ctx context.Context, loaders map[string]ChunkLoader, idsByScope map[string][]string) (map[string]map[string]ChunkMeta, error) {
	out := make(map[string]map[string]ChunkMeta, len(idsByScope))
	for scope, ids := range idsByScope {
		unique := uniqueStrings(ids)
		if len(unique) == 0 {
			continue
		}
		loader := loaders[scope]
		if loader == nil {
			continue
		}
		loaded, err := loader.LoadChunks(ctx, unique)
		if err != nil {
			return nil, fmt.Errorf("load chunk evidence for scope %q: %w", scope, err)
		}
		out[scope] = loaded
	}
	return out, nil
}

func uniqueStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
