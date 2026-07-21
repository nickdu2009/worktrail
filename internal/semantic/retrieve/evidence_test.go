package retrieve

import (
	"context"
	"testing"
)

type fakeChunkLoader struct {
	meta map[string]ChunkMeta
}

func (f fakeChunkLoader) LoadChunks(_ context.Context, chunkIDs []string) (map[string]ChunkMeta, error) {
	out := make(map[string]ChunkMeta, len(chunkIDs))
	for _, id := range chunkIDs {
		if meta, ok := f.meta[id]; ok {
			out[id] = meta
		}
	}
	return out, nil
}

func TestSelectEvidenceKeepsCandidateOrderAndBoundsMatches(t *testing.T) {
	loader := fakeChunkLoader{meta: map[string]ChunkMeta{
		"c1": {ChunkID: "c1", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 1, Primary: ByteRange{10, 20}, PrevChunkID: "", NextChunkID: "c2"},
		"c2": {ChunkID: "c2", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 2, Primary: ByteRange{20, 30}, PrevChunkID: "c1", NextChunkID: "c3"},
		"c3": {ChunkID: "c3", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 3, Primary: ByteRange{30, 40}, PrevChunkID: "c2", NextChunkID: "c4"},
		"c4": {ChunkID: "c4", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 4, Primary: ByteRange{40, 50}, PrevChunkID: "c3", NextChunkID: ""},
		"c5": {ChunkID: "c5", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 5, Primary: ByteRange{50, 60}, PrevChunkID: "c4", NextChunkID: ""},
	}}
	candidates := []Candidate{
		{Scope: "project", EntryID: "entry", Score: 0.9},
		{Scope: "project", EntryID: "other", Score: 0.1},
	}
	lanes := []NamedLaneHits{
		{Name: LaneNameChunkFTS, Hits: []LaneHit{{Scope: "project", EntryID: "entry", Rank: 1}, {Scope: "project", EntryID: "other", Rank: 2}}},
		{Name: LaneNameVectorKNN, Hits: []LaneHit{{Scope: "project", EntryID: "entry", Rank: 2}}},
	}
	matches := []MatchHit{
		{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "c1", EntryID: "entry", Rank: 1},
		{Scope: "project", Lane: LaneNameVectorKNN, ChunkID: "c1", EntryID: "entry", Rank: 4},
		{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "c2", EntryID: "entry", Rank: 2},
		{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "c3", EntryID: "entry", Rank: 3},
		{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "c4", EntryID: "entry", Rank: 5},
		{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "c5", EntryID: "entry", Rank: 6},
	}
	before := append([]Candidate(nil), candidates...)
	got, err := SelectEvidence(context.Background(), candidates, lanes, matches, map[string]ChunkLoader{"project": loader}, DefaultPolicy())
	if err != nil {
		t.Fatalf("SelectEvidence() error = %v", err)
	}
	if candidates[0].Score != before[0].Score || candidates[1].EntryID != before[1].EntryID {
		t.Fatalf("SelectEvidence mutated candidates: %#v", candidates)
	}
	if len(got) != 2 || got[0].FinalRank != 1 || got[0].LexicalRank != 1 || got[0].SemanticRank != 2 {
		t.Fatalf("entry evidence = %#v", got)
	}
	// Top 3 matches are c1/c2/c3; same-group neighbors skip already-selected
	// matches, so only c3 attaches c4.
	if len(got[0].Chunks) != 4 {
		t.Fatalf("chunk count = %d, want 4: %#v", len(got[0].Chunks), got[0].Chunks)
	}
	seen := map[string]int{}
	for _, chunk := range got[0].Chunks {
		seen[chunk.ChunkID]++
		if chunk.EvidenceRole == EvidenceRoleMatch && (len(chunk.Lanes) == 0 || chunk.BestChunkRanks == nil) {
			t.Fatalf("match missing lanes/ranks: %#v", chunk)
		}
		if chunk.EvidenceRole == EvidenceRoleNeighbor && (len(chunk.Lanes) != 0 || chunk.BestChunkRanks != nil) {
			t.Fatalf("neighbor should omit lanes/ranks: %#v", chunk)
		}
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("duplicate chunk evidence for %q = %d", id, count)
		}
	}
	if got[0].Chunks[0].ChunkID != "c1" || got[0].Chunks[0].EvidenceRole != EvidenceRoleMatch {
		t.Fatalf("first chunk = %#v", got[0].Chunks[0])
	}
	if got[0].Chunks[3].ChunkID != "c4" || got[0].Chunks[3].EvidenceRole != EvidenceRoleNeighbor {
		t.Fatalf("neighbor after third match = %#v", got[0].Chunks[3])
	}
	if got[0].Chunks[0].BestChunkRanks[LaneNameChunkFTS] != 1 || got[0].Chunks[0].BestChunkRanks[LaneNameVectorKNN] != 4 {
		t.Fatalf("merged ranks = %#v", got[0].Chunks[0].BestChunkRanks)
	}
}

func TestSelectEvidenceNeighborDoesNotCrossGroupOrClaimTwice(t *testing.T) {
	loader := fakeChunkLoader{meta: map[string]ChunkMeta{
		"a1": {ChunkID: "a1", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 1, Primary: ByteRange{0, 10}, NextChunkID: "shared"},
		"a2": {ChunkID: "a2", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 2, Primary: ByteRange{10, 20}, PrevChunkID: "shared"},
		"shared": {ChunkID: "shared", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 3, Primary: ByteRange{20, 30}},
		"cross": {ChunkID: "cross", EntryID: "entry", ChunkKind: "text", StructuralGroupID: "prose", ChunkOrder: 4, Primary: ByteRange{30, 40}},
		"b1": {ChunkID: "b1", EntryID: "entry", ChunkKind: "table_row_group", StructuralGroupID: "g1", ChunkOrder: 5, Primary: ByteRange{40, 50}, NextChunkID: "cross"},
	}}
	got, err := SelectEvidence(
		context.Background(),
		[]Candidate{{Scope: "project", EntryID: "entry"}},
		nil,
		[]MatchHit{
			{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "a1", EntryID: "entry", Rank: 1},
			{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "a2", EntryID: "entry", Rank: 2},
			{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "b1", EntryID: "entry", Rank: 3},
		},
		map[string]ChunkLoader{"project": loader},
		DefaultPolicy(),
	)
	if err != nil {
		t.Fatalf("SelectEvidence() error = %v", err)
	}
	roles := map[string]string{}
	for _, chunk := range got[0].Chunks {
		roles[chunk.ChunkID] = chunk.EvidenceRole
	}
	if roles["shared"] != EvidenceRoleNeighbor {
		t.Fatalf("shared neighbor role = %q, chunks=%#v", roles["shared"], got[0].Chunks)
	}
	if _, ok := roles["cross"]; ok {
		t.Fatalf("cross-group neighbor attached: %#v", got[0].Chunks)
	}
	neighborCount := 0
	for _, chunk := range got[0].Chunks {
		if chunk.EvidenceRole == EvidenceRoleNeighbor {
			neighborCount++
		}
	}
	if neighborCount != 1 {
		t.Fatalf("neighbor count = %d, want 1 (shared claimed once)", neighborCount)
	}
}

func TestSelectEvidenceWithoutLoaderKeepsRanksOnly(t *testing.T) {
	got, err := SelectEvidence(
		context.Background(),
		[]Candidate{{Scope: "project", EntryID: "entry"}},
		[]NamedLaneHits{{Name: LaneNameChunkFTS, Hits: []LaneHit{{Scope: "project", EntryID: "entry", Rank: 3}}}},
		[]MatchHit{{Scope: "project", Lane: LaneNameChunkFTS, ChunkID: "c1", EntryID: "entry", Rank: 1}},
		nil,
		DefaultPolicy(),
	)
	if err != nil {
		t.Fatalf("SelectEvidence() error = %v", err)
	}
	if got[0].LexicalRank != 3 || got[0].FinalRank != 1 || len(got[0].Chunks) != 0 {
		t.Fatalf("evidence = %#v", got[0])
	}
}
