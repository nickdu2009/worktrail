// Package selection selects context-pack candidates from precomputed semantic
// rankings. It does not perform retrieval, loading, or context-pack policy.
package selection

import (
	"sort"

	"github.com/nickdu2009/worktrail/internal/contextpack"
)

// Ranking associates a canonical context item identity with its semantic rank.
// Lower ranks are selected first. Scope and Path together identify a scoped
// context item. A ranking with an empty Scope is an unscoped fallback for a
// matching path.
type Ranking struct {
	Scope string
	Path  string
	Rank  int
}

// Selector implements contextpack.Selector using only precomputed rankings.
type Selector struct {
	ranksByIdentity map[itemIdentity]int
}

var _ contextpack.Selector = (*Selector)(nil)

// New creates a selector from precomputed semantic rankings. If rankings
// contain the same scope and path more than once, the first ranking wins.
func New(rankings []Ranking) *Selector {
	ranksByIdentity := make(map[itemIdentity]int, len(rankings))
	for _, ranking := range rankings {
		identity := itemIdentity{scope: ranking.Scope, path: ranking.Path}
		if _, exists := ranksByIdentity[identity]; !exists {
			ranksByIdentity[identity] = ranking.Rank
		}
	}
	return &Selector{ranksByIdentity: ranksByIdentity}
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
	scope string
	path  string
}

func identityForItem(item contextpack.Item) itemIdentity {
	return itemIdentity{scope: item.Scope, path: item.Path}
}

func (s *Selector) rank(item contextpack.Item) (int, bool) {
	if s == nil {
		return 0, false
	}
	identity := identityForItem(item)
	if rank, ok := s.ranksByIdentity[identity]; ok {
		return rank, true
	}
	if identity.scope == "" {
		return 0, false
	}
	rank, ok := s.ranksByIdentity[itemIdentity{path: identity.path}]
	return rank, ok
}

func cloneItem(item contextpack.Item) contextpack.Item {
	item.SupersededBy = append([]string(nil), item.SupersededBy...)
	item.Tags = append([]string(nil), item.Tags...)
	return item
}
