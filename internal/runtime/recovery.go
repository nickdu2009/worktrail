package runtime

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
)

type RecoveryItem struct {
	RelPath        string `json:"relpath"`
	QuarantinePath string `json:"quarantine_path"`
	ContentHash    string `json:"content_hash"`
	Error          string `json:"error"`
}

type RecoveryProposal struct {
	Root        string         `json:"-"`
	Scope       string         `json:"scope"`
	GeneratedAt time.Time      `json:"generated_at"`
	Items       []RecoveryItem `json:"items,omitempty"`
}

type RecoveryResult struct {
	Quarantined int    `json:"quarantined"`
	OperationID string `json:"operation_id,omitempty"`
}

// RecoveryPlan diagnoses malformed runtime Markdown. Symbolic links and other
// non-regular entries are refused by the runtime scanner and are never
// considered quarantine candidates.
func RecoveryPlan(env paths.Env, scope string, now time.Time) (RecoveryProposal, error) {
	root, resolvedScope, err := scopeRoot(env, scope)
	if err != nil {
		return RecoveryProposal{}, err
	}
	return recoveryPlanRoot(root, resolvedScope, now)
}

func recoveryPlanRoot(root, scope string, now time.Time) (RecoveryProposal, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	proposal := RecoveryProposal{Root: root, Scope: scope, GeneratedAt: now}
	for _, dir := range []string{DirSessions, DirCheckpoints, DirRecovery} {
		_, diagnostics, err := listAllRoot(root, dir)
		if err != nil {
			return RecoveryProposal{}, err
		}
		for _, diagnostic := range diagnostics {
			switch diagnostic.Kind {
			case "symlink", "non_regular":
				return RecoveryProposal{}, fmt.Errorf("runtime recovery refuses unsafe entry %q: %s", diagnostic.Path, diagnostic.Error)
			case "malformed":
				source, err := paths.SafeJoin(root, filepath.FromSlash(diagnostic.Path))
				if err != nil {
					return RecoveryProposal{}, err
				}
				data, _, err := readRegularFile(root, source)
				if err != nil {
					return RecoveryProposal{}, err
				}
				quarantineRel := filepath.ToSlash(filepath.Join("runtime", "quarantine", dir, filepath.Base(source)))
				proposal.Items = append(proposal.Items, RecoveryItem{
					RelPath:        filepath.ToSlash(diagnostic.Path),
					QuarantinePath: quarantineRel,
					ContentHash:    hashBytes(data),
					Error:          diagnostic.Error,
				})
			}
		}
	}
	sort.Slice(proposal.Items, func(i, j int) bool { return proposal.Items[i].RelPath < proposal.Items[j].RelPath })
	return proposal, nil
}

func ApplyRecovery(plan RecoveryProposal) (RecoveryResult, error) {
	root := strings.TrimSpace(plan.Root)
	if root == "" {
		return RecoveryResult{}, errors.New("runtime recovery plan root is required")
	}
	current, err := recoveryPlanRoot(root, plan.Scope, plan.GeneratedAt)
	if err != nil {
		return RecoveryResult{}, err
	}
	if !sameRecoveryItems(plan.Items, current.Items) {
		return RecoveryResult{}, errors.New("runtime recovery plan is stale")
	}
	if len(plan.Items) == 0 {
		return RecoveryResult{}, nil
	}
	spec := ops.Spec{}
	for _, item := range plan.Items {
		source, err := paths.SafeJoin(root, filepath.FromSlash(item.RelPath))
		if err != nil {
			return RecoveryResult{}, err
		}
		data, _, err := readRegularFile(root, source)
		if err != nil {
			return RecoveryResult{}, err
		}
		if hashBytes(data) != item.ContentHash {
			return RecoveryResult{}, fmt.Errorf("runtime recovery plan is stale for %q: content hash changed", item.RelPath)
		}
		destination, err := paths.SafeJoin(root, filepath.FromSlash(item.QuarantinePath))
		if err != nil {
			return RecoveryResult{}, err
		}
		relDir, err := filepath.Rel(root, filepath.Dir(destination))
		if err != nil {
			return RecoveryResult{}, err
		}
		if _, err := EnsurePrivateDir(root, relDir); err != nil {
			return RecoveryResult{}, err
		}
		if info, statErr := os.Lstat(destination); statErr == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return RecoveryResult{}, fmt.Errorf("runtime quarantine destination %q is a symbolic link", item.QuarantinePath)
			}
			return RecoveryResult{}, fmt.Errorf("runtime quarantine destination %q already exists", item.QuarantinePath)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return RecoveryResult{}, statErr
		}
		spec.Writes = append(spec.Writes, ops.Write{Path: item.QuarantinePath, Data: data, Mode: 0o600})
		spec.Deletes = append(spec.Deletes, item.RelPath)
	}
	operation, err := ops.New(root).Begin(spec)
	if err != nil {
		return RecoveryResult{}, err
	}
	result := RecoveryResult{OperationID: operation.Intent().ID}
	if err := operation.Commit(); err != nil {
		return result, err
	}
	result.Quarantined = len(plan.Items)
	if _, err := EnsurePrivateDir(root, "logs"); err == nil {
		_ = wlog.Append(root, "runtime.quarantine", result.OperationID, "doctor:recovery", map[string]any{
			"quarantined": result.Quarantined,
		})
	}
	return result, nil
}

func sameRecoveryItems(left, right []RecoveryItem) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
