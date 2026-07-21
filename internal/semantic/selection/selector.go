// Package selection selects context-pack candidates from precomputed semantic
// rankings. It does not perform retrieval, loading, or context-pack policy.
package selection

import (
	"sort"

	"github.com/nickdu2009/worktrail/internal/contextpack"
)

// Ranking associates a canonical context item identity with its semantic rank.
// Lower ranks are selected first. Prefer (Scope, EntryID) when EntryID is set.
// Rankings without EntryID match (Scope, Path). Unscoped path rankings never
// match scoped candidates.
type Ranking struct {
	Scope   string
	EntryID string
	Path    string
	Rank    int
}

// Selector implements contextpack.Selector using only precomputed rankings.
type Selector struct {
	ranksByEntry map[itemIdentity]int
	ranksByPath  map[itemIdentity]int
}

var _ contextpack.Selector = (*Selector)(nil)

// New creates a selector from precomputed semantic rankings. If rankings
// contain the same identity more than once, the first ranking wins.
func New(rankings []Ranking) *Selector {
	ranksByEntry := make(map[itemIdentity]int, len(rankings))
	ranksByPath := make(map[itemIdentity]int, len(rankings))
	for _, ranking := range rankings {
		if ranking.EntryID != "" {
			identity := itemIdentity{scope: ranking.Scope, entryID: ranking.EntryID}
			if _, exists := ranksByEntry[identity]; !exists {
				ranksByEntry[identity] = ranking.Rank
			}
			continue
		}
		identity := itemIdentity{scope: ranking.Scope, path: ranking.Path}
		if _, exists := ranksByPath[identity]; !exists {
			ranksByPath[identity] = ranking.Rank
		}
	}
	return &Selector{ranksByEntry: ranksByEntry, ranksByPath: ranksByPath}
}

// Select orders ranked candidates by ascending semantic rank, preserving their
// input order for equal ranks. It then fills remaining capacity with unranked
// candidates in input order. Only request candidates are returned.
func (s *Selector) Select(request contextpack.SelectionRequest) ([]contextpack.Item, error) {
	if request.Limit <= 0 || len(request.Candidates) == 0 {
		return []contextpack.Item{}, nil
	}

	ranked := make([]rankedItem, 0, len(request.Candidates))
	unranked := make([]contextpack.Item, 0, len(request.Candidates))
	seen := make(map[itemIdentity]struct{}, len(request.Candidates))
	for _, item := range request.Candidates {
		identity := identityForItem(item)
		if _, duplicate := seen[identity]; duplicate {
			continue
		}
		seen[identity] = struct{}{}

		if rank, ok := s.rank(item); ok {
			ranked = append(ranked, rankedItem{item: cloneItem(item), rank: rank})
			continue
		}
		unranked = append(unranked, cloneItem(item))
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		return ranked[i].rank < ranked[j].rank
	})

	limit := request.Limit
	if limit > len(ranked)+len(unranked) {
		limit = len(ranked) + len(unranked)
	}
	selected := make([]contextpack.Item, 0, limit)
	for _, candidate := range ranked {
		if len(selected) == limit {
			return selected, nil
		}
		selected = append(selected, candidate.item)
	}
	selected = append(selected, unranked[:limit-len(selected)]...)
	return selected, nil
}

type rankedItem struct {
	item contextpack.Item
	rank int
}

type itemIdentity struct {
	scope   string
	entryID string
	path    string
}

func identityForItem(item contextpack.Item) itemIdentity {
	if item.EntryID != "" {
		return itemIdentity{scope: item.Scope, entryID: item.EntryID}
	}
	return itemIdentity{scope: item.Scope, path: item.Path}
}

func (s *Selector) rank(item contextpack.Item) (int, bool) {
	if s == nil {
		return 0, false
	}
	if item.EntryID != "" {
		if rank, ok := s.ranksByEntry[itemIdentity{scope: item.Scope, entryID: item.EntryID}]; ok {
			return rank, true
		}
	}
	rank, ok := s.ranksByPath[itemIdentity{scope: item.Scope, path: item.Path}]
	return rank, ok
}

func cloneItem(item contextpack.Item) contextpack.Item {
	item.SupersededBy = append([]string(nil), item.SupersededBy...)
	item.Tags = append([]string(nil), item.Tags...)
	return item
}
