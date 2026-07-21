package retrieve

import (
	"fmt"
	"strings"
)

type laneDiagnostics struct {
	RawHits          int
	FilterRejections int
	EligibleEntries  int
	RefillRounds     int
	WindowSaturated  bool
}

type prefixProvider struct {
	fetch      func(limit int) ([]RawChunkHit, error)
	hits       []RawChunkHit
	fetched    bool
	queryCalls int
}

func newPrefixProvider(fetch func(limit int) ([]RawChunkHit, error)) *prefixProvider {
	return &prefixProvider{fetch: fetch}
}

func (p *prefixProvider) ensure(hardCap int) error {
	if p.fetched {
		return nil
	}
	if p.fetch == nil {
		return fmt.Errorf("prefix provider backend is not configured")
	}
	hits, err := p.fetch(hardCap)
	p.queryCalls++
	if err != nil {
		return err
	}
	p.hits = normalizeRawHits(hits)
	p.fetched = true
	return nil
}

func (p *prefixProvider) prefix(n int) []RawChunkHit {
	if n <= 0 || len(p.hits) == 0 {
		return nil
	}
	if n > len(p.hits) {
		n = len(p.hits)
	}
	return p.hits[:n]
}

func (p *prefixProvider) QueryCalls() int {
	return p.queryCalls
}

// CollectEntryLane runs request-local hard-cap fetch + prefix widening and
// collapses eligible hits to consecutive lane_entry_rank values.
func CollectEntryLane(
	fetch func(limit int) ([]RawChunkHit, error),
	policy Policy,
	scope string,
	filters ExactFilters,
	applyFilters bool,
) ([]LaneHit, Lane, error) {
	provider := newPrefixProvider(fetch)
	hits, diagnostics, err := collectEntryLane(provider, policy, scope, filters, applyFilters)
	lane := Lane{
		Scope:            scope,
		RawHits:          diagnostics.RawHits,
		FilterRejections: diagnostics.FilterRejections,
		EligibleEntries:  diagnostics.EligibleEntries,
		RefillRounds:     diagnostics.RefillRounds,
		HardCap:          policy.HardCap,
		WindowSaturated:  diagnostics.WindowSaturated,
	}
	return hits, lane, err
}

func collectEntryLane(
	provider *prefixProvider,
	policy Policy,
	scope string,
	filters ExactFilters,
	applyFilters bool,
) ([]LaneHit, laneDiagnostics, error) {
	if err := provider.ensure(policy.HardCap); err != nil {
		return nil, laneDiagnostics{}, err
	}

	window := policy.InitialWindow
	if window > policy.HardCap {
		window = policy.HardCap
	}

	var (
		eligible     []LaneHit
		seenEntry    = map[string]bool{}
		seenChunk    = map[string]bool{}
		rejections   int
		processed    int
		refillRounds int
	)

	for {
		refillRounds++
		prefixLen := window
		if prefixLen > policy.HardCap {
			prefixLen = policy.HardCap
		}
		prefix := provider.prefix(prefixLen)
		targetReached := false
		for _, hit := range prefix[processed:] {
			if hit.ChunkID == "" || hit.EntryID == "" {
				continue
			}
			if seenChunk[hit.ChunkID] {
				continue
			}
			seenChunk[hit.ChunkID] = true
			if seenEntry[hit.EntryID] {
				continue
			}
			if applyFilters && !matchesExactFilters(hit, filters) {
				rejections++
				continue
			}
			seenEntry[hit.EntryID] = true
			eligible = append(eligible, LaneHit{
				Scope:         scope,
				ChunkID:       hit.ChunkID,
				EntryID:       hit.EntryID,
				Rank:          len(eligible) + 1,
				BestChunkRank: hit.Rank,
			})
			if len(eligible) >= policy.EligibleEntryTarget {
				targetReached = true
				break
			}
		}
		// raw_hits is the final cumulative prefix length, not the early-stop walk offset.
		processed = len(prefix)

		backendExhausted := len(provider.hits) < prefixLen
		atHardCap := prefixLen >= policy.HardCap
		if targetReached || backendExhausted || atHardCap {
			break
		}
		next := window * policy.WindowGrowth
		if next <= window {
			break
		}
		if next > policy.HardCap {
			next = policy.HardCap
		}
		window = next
	}

	diagnostics := laneDiagnostics{
		RawHits:          processed,
		FilterRejections: rejections,
		EligibleEntries:  len(eligible),
		RefillRounds:     refillRounds,
		// Saturated only when the hard-cap prefix was fully available yet the
		// unique-entry target was still unmet (backend not exhausted early).
		WindowSaturated: len(eligible) < policy.EligibleEntryTarget && len(provider.hits) >= policy.HardCap,
	}
	return eligible, diagnostics, nil
}

func normalizeRawHits(hits []RawChunkHit) []RawChunkHit {
	out := make([]RawChunkHit, 0, len(hits))
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

func matchesExactFilters(hit RawChunkHit, filters ExactFilters) bool {
	if typeFilter := strings.TrimSpace(filters.Type); typeFilter != "" && hit.DocumentType != typeFilter {
		return false
	}
	if topicFilter := strings.TrimSpace(filters.Topic); topicFilter != "" && hit.DocumentTopic != topicFilter {
		return false
	}
	if tagFilter := strings.TrimSpace(filters.Tag); tagFilter != "" {
		found := false
		for _, tag := range hit.Tags {
			if tag == tagFilter {
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
