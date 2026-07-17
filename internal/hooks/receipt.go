package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/runtime"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	receiptRetention = 30 * 24 * time.Hour
	receiptFileMode  = 0o600
	receiptClaimed   = "claimed"
	receiptCompleted = "completed"
)

var (
	// ErrReceiptClaimed means a prior attempt claimed the receipt but did not
	// complete. Callers must not treat this as successful dedupe; clear via
	// doctor ops repair (hooks do not auto-replay).
	ErrReceiptClaimed = errors.New("hook receipt claimed pending repair")
)

type hookReceipt struct {
	Schema    string    `json:"schema"`
	EffectKey string    `json:"effect_key"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ClaimedReceipt summarizes an incomplete receipt for doctor status/repair.
type ClaimedReceipt struct {
	EffectKey string    `json:"effect_key"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func receiptPath(root, effectKey string) (string, error) {
	dir, err := runtime.EnsurePrivateDir(root, filepath.Join("ops", "hook-receipts"))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(effectKey))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(dir, name), nil
}

func loadReceipt(root, effectKey string) (*hookReceipt, error) {
	path, err := receiptPath(root, effectKey)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var receipt hookReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func receiptDecision(existing *hookReceipt, effectKey string) (skip bool, err error) {
	if existing == nil {
		return false, nil
	}
	switch strings.TrimSpace(existing.Status) {
	case receiptCompleted:
		return true, nil
	case receiptClaimed:
		return false, fmt.Errorf("%w: %s", ErrReceiptClaimed, effectKey)
	default:
		return false, fmt.Errorf("hook receipt unknown status %q for %s", existing.Status, effectKey)
	}
}

// applyEffectOnce claims a receipt under ops/hook-receipts, runs apply, then
// completes the receipt.
//
// Lock boundary: a per-receipt lease spans claim, apply, and complete so Doctor
// repair cannot clear an in-flight receipt. Claim and complete also take the
// project ops lock briefly. apply() runs WITHOUT that project lock because
// runtime/audit writes call wlog.Append, which also needs it (non-reentrant).
//
// Atomicity note (ops-control-tree constraint): ops journal refuses writes
// inside the ops tree, so claim/complete cannot share one journal intent with
// the runtime/audit effect. A crash after successful apply but before completed
// status leaves claimed for doctor repair (no auto-replay). Apply failures roll
// back the claim so a later attempt can retry.
func applyEffectOnce(root, effectKey string, apply func() error) (bool, error) {
	absPath, err := receiptPath(root, effectKey)
	if err != nil {
		return false, err
	}
	lease, err := acquireReceiptLease(effectKey, absPath, false)
	if err != nil {
		if errors.Is(err, ops.ErrLocked) {
			return false, fmt.Errorf("%w: %s", ErrReceiptClaimed, effectKey)
		}
		return false, err
	}
	defer func() { _ = lease.Release() }()

	if existing, err := loadReceipt(root, effectKey); err != nil {
		return false, err
	} else if skip, err := receiptDecision(existing, effectKey); err != nil {
		return false, err
	} else if skip {
		return false, nil
	}

	now := time.Now().UTC()
	claimed := hookReceipt{
		Schema:    "worktrail.hook_receipt.v1",
		EffectKey: effectKey,
		Status:    receiptClaimed,
		CreatedAt: now,
		UpdatedAt: now,
	}
	claimedData, err := json.MarshalIndent(claimed, "", "  ")
	if err != nil {
		return false, err
	}
	completed := claimed
	completed.Status = receiptCompleted
	completed.UpdatedAt = now
	completedData, err := json.MarshalIndent(completed, "", "  ")
	if err != nil {
		return false, err
	}

	var claimedOK bool
	err = withOpsLockRetry(func() error {
		var claimErr error
		claimedOK, claimErr = claimReceipt(root, effectKey, absPath, claimedData)
		return claimErr
	})
	if err != nil || !claimedOK {
		return false, err
	}

	if err := apply(); err != nil {
		_ = os.Remove(absPath) // rollback claim so a later attempt can retry
		return false, err
	}

	if err := withOpsLockRetry(func() error {
		return completeReceipt(root, effectKey, absPath, completedData)
	}); err != nil {
		// Effect may have applied; leave claimed for doctor repair (no auto-replay).
		return false, err
	}
	return true, nil
}

// acquireReceiptLease serializes one receipt effect with doctor repair. The
// lease intentionally uses a nested ops lock: the outer project ops lock cannot
// cover apply() because runtime/audit writes need that lock themselves.
func acquireReceiptLease(effectKey, receiptPath string, repairStale bool) (*ops.Lock, error) {
	leaseRoot := strings.TrimSuffix(receiptPath, ".json") + ".lease"
	operationID := "hook-effect-" + shortHash(effectKey)
	lock, err := ops.AcquireLock(leaseRoot, operationID)
	if err == nil || !repairStale {
		return lock, err
	}
	var lockErr *ops.LockError
	if !errors.As(err, &lockErr) || !lockErr.Status.Stale || !lockErr.Status.Recoverable {
		return nil, err
	}
	if err := ops.RemoveStaleLock(leaseRoot); err != nil {
		return nil, err
	}
	return ops.AcquireLock(leaseRoot, operationID)
}

func withOpsLockRetry(fn func() error) error {
	// Ops lock is process-global per project root. Claim and complete each hold
	// it briefly, while apply() runs unlocked. Waiters must poll on a short
	// interval so they can enter during that unlocked window instead of sleeping
	// through it under load.
	var last error
	for attempt := 0; attempt < 250; attempt++ {
		last = fn()
		if last == nil || !errors.Is(last, ops.ErrLocked) {
			return last
		}
		time.Sleep(4 * time.Millisecond)
	}
	return last
}

func claimReceipt(root, effectKey, absPath string, claimedData []byte) (bool, error) {
	opID := "hook-receipt-" + shortHash(effectKey)
	lock, err := ops.New(root).AcquireReadyLock(opID)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Release() }()

	if again, err := loadReceipt(root, effectKey); err != nil {
		return false, err
	} else if skip, err := receiptDecision(again, effectKey); err != nil {
		return false, err
	} else if skip {
		return false, nil
	}

	f, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, receiptFileMode)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			again, loadErr := loadReceipt(root, effectKey)
			if loadErr != nil {
				return false, loadErr
			}
			if skip, decErr := receiptDecision(again, effectKey); decErr != nil {
				return false, decErr
			} else if skip {
				return false, nil
			}
			return false, fmt.Errorf("%w: %s", ErrReceiptClaimed, effectKey)
		}
		return false, err
	}
	if _, err := f.Write(append(claimedData, '\n')); err != nil {
		_ = f.Close()
		_ = os.Remove(absPath)
		return false, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(absPath)
		return false, err
	}
	return true, nil
}

func completeReceipt(root, effectKey, absPath string, completedData []byte) error {
	opID := "hook-receipt-done-" + shortHash(effectKey)
	lock, err := ops.New(root).AcquireReadyLock(opID)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Release() }()
	return util.AtomicWrite(absPath, append(completedData, '\n'), receiptFileMode)
}

// ListClaimedReceipts returns incomplete receipts under ops/hook-receipts.
func ListClaimedReceipts(root string) ([]ClaimedReceipt, error) {
	dir := filepath.Join(root, "ops", "hook-receipts")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []ClaimedReceipt
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var receipt hookReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			continue
		}
		if receipt.Status != receiptClaimed {
			continue
		}
		out = append(out, ClaimedReceipt{
			EffectKey: receipt.EffectKey,
			Path:      path,
			CreatedAt: receipt.CreatedAt,
			UpdatedAt: receipt.UpdatedAt,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

// ClearClaimedReceipts removes incomplete receipts so later hook calls can retry.
// Hooks never auto-replay the original effect. A receipt whose effect lease is
// active is left in place; Doctor reports it as remaining instead of racing the
// in-flight effect and allowing a duplicate execution.
func ClearClaimedReceipts(root string) ([]string, error) {
	claimed, err := ListClaimedReceipts(root)
	if err != nil {
		return nil, err
	}
	cleared := make([]string, 0, len(claimed))
	for _, item := range claimed {
		lease, err := acquireReceiptLease(item.EffectKey, item.Path, true)
		if errors.Is(err, ops.ErrLocked) {
			continue
		}
		if err != nil {
			return cleared, err
		}

		current, err := readReceiptAtPath(item.Path)
		if err == nil && current.Status == receiptClaimed {
			err = os.Remove(item.Path)
		}
		releaseErr := lease.Release()
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return cleared, err
		}
		if releaseErr != nil {
			return cleared, releaseErr
		}
		if err == nil && current.Status == receiptClaimed {
			cleared = append(cleared, item.EffectKey)
		}
	}
	sort.Strings(cleared)
	return cleared, nil
}

func readReceiptAtPath(path string) (*hookReceipt, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var receipt hookReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

// PruneArtifacts removes aged completed receipts and idle session bindings.
// Intended for explicit CLI (runtime prune / doctor ops repair), never auto hooks.
func PruneArtifacts(root string, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := pruneReceipts(root, now); err != nil {
		return err
	}
	return pruneIdleBindings(root, now)
}

func pruneReceipts(root string, now time.Time) error {
	dir := filepath.Join(root, "ops", "hook-receipts")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var receipt hookReceipt
		if err := json.Unmarshal(data, &receipt); err != nil {
			continue
		}
		if receipt.Status == receiptCompleted && now.Sub(receipt.UpdatedAt) > receiptRetention {
			_ = os.Remove(path)
		}
	}
	return nil
}
