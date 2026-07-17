package policy

import "github.com/nickdu2009/worktrail/internal/index"

// FreshSources returns the current semantic-policy source records for one
// scope together with their deterministic snapshot.
func FreshSources(root, scope string) (index.SourceSnapshot, error) {
	return index.FreshSourceSnapshot(root, scope, Version, Select)
}
