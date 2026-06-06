package index

import (
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/knowledge"
)

func SearchEntries(entries []Entry, query Query) []Result {
	needle := strings.ToLower(strings.TrimSpace(query.Content))
	tags := append([]string{}, query.Tags...)
	if query.Tag != "" {
		tags = append(tags, query.Tag)
	}
	var results []Result
	for _, entry := range entries {
		if query.Scope != "" && entry.Scope != query.Scope {
			continue
		}
		if query.Type != "" && entry.Type != query.Type {
			continue
		}
		if query.Topic != "" && entry.Topic != query.Topic {
			continue
		}
		if len(tags) > 0 && !hasAllTags(entry.Tags, tags) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(entry.Title+"\n"+entry.Content), needle) {
			continue
		}
		score := scoreEntry(entry, needle)
		if !query.IncludeContent {
			entry.Content = ""
		}
		results = append(results, Result{Entry: entry, Score: score})
	}
	return RankSearchResults(results, query.Limit)
}

func RankSearchResults(results []Result, limit int) []Result {
	if len(results) == 0 {
		return results
	}
	out := append([]Result{}, results...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].Entry.UpdatedAt.After(out[j].Entry.UpdatedAt)
		}
		return out[i].Score > out[j].Score
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func hasAllTags(have []string, want []string) bool {
	set := map[string]bool{}
	for _, tag := range have {
		set[strings.ToLower(tag)] = true
	}
	for _, tag := range want {
		if !set[strings.ToLower(tag)] {
			return false
		}
	}
	return true
}

func scoreEntry(entry Entry, needle string) float64 {
	score := 1.0
	if needle != "" {
		title := strings.ToLower(entry.Title)
		content := strings.ToLower(entry.Content)
		if strings.Contains(title, needle) {
			score += 8
		}
		score += float64(strings.Count(content, needle))
	}
	if entry.Active {
		score += 5
	}
	if entry.Active {
		score += 3
	}
	if entry.SourceOfTruth {
		score += 5
	}
	if len(entry.SupersededBy) > 0 || knowledge.IsNonCurrentLifecycle(entry.Lifecycle) || entry.Stage == "historical" || entry.Stage == "retired" {
		score -= 5
	}
	age := time.Since(entry.UpdatedAt)
	if age < 0 {
		age = 0
	}
	switch {
	case age <= 24*time.Hour:
		score += 3
	case age <= 7*24*time.Hour:
		score += 2
	case age <= 30*24*time.Hour:
		score += 1
	}
	return score
}
