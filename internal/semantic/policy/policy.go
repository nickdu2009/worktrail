// Package policy selects the default corpus for semantic indexing.
package policy

import (
	"path"
	"sort"
	"strings"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
)

// Version identifies the semantic default corpus policy.
const Version = "semantic-policy-v1"

// Select returns the current formal knowledge, user profile, active state, and
// newest current handoff for each scope. It neither reads files nor modifies
// entries.
func Select(entries []index.Entry) []index.Entry {
	selected := make([]index.Entry, 0, len(entries))
	latestHandoffs := make(map[string]index.Entry)

	for _, entry := range entries {
		rel := normalizedPath(entry.Path)
		switch {
		case isActiveState(entry, rel):
			selected = append(selected, entry)
		case isCurrentHandoff(entry, rel):
			if current, ok := latestHandoffs[entry.Scope]; !ok || newerHandoff(entry, current) {
				latestHandoffs[entry.Scope] = entry
			}
		case isCurrentFormalKnowledge(entry, rel):
			selected = append(selected, entry)
		}
	}

	for _, handoff := range latestHandoffs {
		selected = append(selected, handoff)
	}
	sort.Slice(selected, func(i, j int) bool {
		left, right := normalizedPath(selected[i].Path), normalizedPath(selected[j].Path)
		if left != right {
			return left < right
		}
		if selected[i].Scope != selected[j].Scope {
			return selected[i].Scope < selected[j].Scope
		}
		return selected[i].ID < selected[j].ID
	})
	return selected
}

func isCurrentFormalKnowledge(entry index.Entry, rel string) bool {
	if !isCurrentEntry(entry, rel) {
		return false
	}
	return knowledge.IsFormalKnowledgePath(rel) ||
		(entry.Scope == "user" && strings.HasPrefix(rel, "profile/"))
}

func isCurrentHandoff(entry index.Entry, rel string) bool {
	return isHandoff(rel) && isCurrentEntry(entry, rel)
}

func isCurrentEntry(entry index.Entry, rel string) bool {
	return !isExcludedPath(rel) &&
		knowledge.NormalizeLifecycle(entry.Lifecycle, entry.Stage, entry.Status) == knowledge.LifecycleCurrent
}

func isActiveState(entry index.Entry, rel string) bool {
	return entry.Active &&
		strings.HasPrefix(rel, "state/active/") &&
		rel != "state/active/latest.md"
}

func isHandoff(rel string) bool {
	return strings.HasPrefix(rel, "handoffs/")
}

func newerHandoff(candidate, current index.Entry) bool {
	if !candidate.UpdatedAt.Equal(current.UpdatedAt) {
		return candidate.UpdatedAt.After(current.UpdatedAt)
	}
	candidatePath, currentPath := normalizedPath(candidate.Path), normalizedPath(current.Path)
	if candidatePath != currentPath {
		return candidatePath < currentPath
	}
	return candidate.ID < current.ID
}

func isExcludedPath(rel string) bool {
	for _, root := range []string{
		"candidate",
		"candidates",
		"evidence",
		"runtime",
		"history",
		"historical",
		"raw",
		"import",
		"imports",
		"log",
		"logs",
		"export",
		"exports",
	} {
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return true
		}
	}
	return false
}

func normalizedPath(value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), "\\", "/")
	value = strings.TrimPrefix(value, "./")
	if value == "" || value == "." {
		return ""
	}
	return path.Clean(value)
}
