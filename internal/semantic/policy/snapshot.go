package policy

import "github.com/nickdu2009/worktrail/internal/index"

// FreshSnapshot returns the current semantic-policy snapshot for one scope.
func FreshSnapshot(root, scope string) (index.Snapshot, error) {
	return index.FreshSnapshot(root, scope, Version, Select)
}
