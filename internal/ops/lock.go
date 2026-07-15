package ops

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/nickdu2009/worktrail/internal/util"
)

var ErrLocked = errors.New("worktrail operation lock is held")

const ownerlessLockGrace = 30 * time.Second

type LockOwner struct {
	OperationID string    `json:"operation_id"`
	PID         int       `json:"pid"`
	Host        string    `json:"host"`
	AcquiredAt  time.Time `json:"acquired_at"`
}

type LockStatus struct {
	Owner               LockOwner `json:"owner"`
	OwnerMissing        bool      `json:"owner_missing,omitempty"`
	DirectoryModifiedAt time.Time `json:"directory_modified_at,omitempty"`
	Stale               bool      `json:"stale"`
	Recoverable         bool      `json:"recoverable"`
	Reason              string    `json:"reason,omitempty"`
}

type LockError struct {
	Status LockStatus
}

func (e *LockError) Error() string {
	owner := e.Status.Owner
	reason := e.Status.Reason
	if reason == "" {
		reason = "owner is still running"
	}
	return fmt.Sprintf("%v: operation=%q pid=%d host=%q acquired_at=%s: %s",
		ErrLocked, owner.OperationID, owner.PID, owner.Host, owner.AcquiredAt.Format(time.RFC3339Nano), reason)
}

func (e *LockError) Unwrap() error {
	return ErrLocked
}

type Lock struct {
	root     string
	lockPath string
	owner    LockOwner
	released bool
}

func (l *Lock) Owner() LockOwner {
	return l.owner
}

func (l *Lock) Release() error {
	if l == nil || l.released {
		return nil
	}
	current, err := readLockOwner(l.lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			l.released = true
			return nil
		}
		return err
	}
	if current.OperationID != l.owner.OperationID || current.PID != l.owner.PID || current.Host != l.owner.Host {
		return fmt.Errorf("operation lock ownership changed")
	}
	if err := os.RemoveAll(l.lockPath); err != nil {
		return err
	}
	l.released = true
	return syncDirectory(filepath.Dir(l.lockPath))
}

func AcquireLock(root, operationID string) (*Lock, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("ops root is required")
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = newOperationID()
	}
	if err := validateOperationID(operationID); err != nil {
		return nil, err
	}
	opsPath := filepath.Join(root, "ops")
	if err := secureDir(root, opsPath); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(opsPath, "lock")
	if err := os.Mkdir(lockPath, 0o700); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if err := requireDirectory(lockPath); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, &LockError{Status: LockStatus{Reason: "operation lock changed during acquisition"}}
			}
			return nil, err
		}
		status, inspectErr := InspectLock(root)
		if inspectErr != nil {
			if errors.Is(inspectErr, os.ErrNotExist) {
				return nil, &LockError{Status: LockStatus{Reason: "operation lock changed during acquisition"}}
			}
			return nil, inspectErr
		}
		return nil, &LockError{Status: status}
	}
	host, err := os.Hostname()
	if err != nil {
		_ = os.RemoveAll(lockPath)
		return nil, err
	}
	owner := LockOwner{
		OperationID: operationID,
		PID:         os.Getpid(),
		Host:        host,
		AcquiredAt:  time.Now().UTC(),
	}
	data, err := json.Marshal(owner)
	if err != nil {
		_ = os.RemoveAll(lockPath)
		return nil, err
	}
	if err := util.AtomicWrite(filepath.Join(lockPath, "owner.json"), data, 0o600); err != nil {
		_ = os.RemoveAll(lockPath)
		return nil, err
	}
	return &Lock{root: root, lockPath: lockPath, owner: owner}, nil
}

func InspectLock(root string) (LockStatus, error) {
	lockPath := filepath.Join(root, "ops", "lock")
	info, err := os.Lstat(lockPath)
	if errors.Is(err, os.ErrNotExist) {
		return LockStatus{}, os.ErrNotExist
	}
	if err != nil {
		return LockStatus{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return LockStatus{}, fmt.Errorf("operation lock path %q is not a real directory", lockPath)
	}
	host, err := os.Hostname()
	if err != nil {
		return LockStatus{}, err
	}
	owner, err := readLockOwner(lockPath)
	if err != nil {
		status := LockStatus{
			Owner: LockOwner{
				Host:       host,
				AcquiredAt: info.ModTime().UTC(),
			},
			DirectoryModifiedAt: info.ModTime().UTC(),
			Stale:               true,
		}
		if errors.Is(err, os.ErrNotExist) {
			status.OwnerMissing = true
			age := time.Since(info.ModTime())
			if age >= ownerlessLockGrace {
				status.Recoverable = true
				status.Reason = fmt.Sprintf("owner record was never written on this host and lock directory is %s old", age.Round(time.Second))
			} else {
				status.Stale = false
				status.Reason = "owner record is not written yet; lock may still be initializing"
			}
			return status, nil
		}
		status.Reason = fmt.Sprintf("lock directory has no valid owner record; manual inspection required: %v", err)
		return status, nil
	}
	status := LockStatus{Owner: owner}
	if owner.Host != host {
		status.Reason = "lock belongs to another host; liveness cannot be verified locally"
		return status, nil
	}
	alive, known := processAlive(owner.PID)
	if !known {
		status.Reason = "process liveness could not be determined; manual inspection required"
		return status, nil
	}
	if alive {
		status.Reason = "owner process is still running"
		return status, nil
	}
	status.Stale = true
	status.Recoverable = true
	status.Reason = "owner process has exited on this host"
	return status, nil
}

func RemoveStaleLock(root string) error {
	status, err := InspectLock(root)
	if err != nil {
		return err
	}
	if !status.Stale || !status.Recoverable {
		return &LockError{Status: status}
	}
	lockPath := filepath.Join(root, "ops", "lock")
	if status.OwnerMissing {
		info, err := os.Lstat(lockPath)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return errors.New("operation lock changed while checking ownerless staleness")
		}
		if !info.ModTime().UTC().Equal(status.DirectoryModifiedAt) {
			return errors.New("operation lock changed while checking ownerless staleness")
		}
		if _, err := os.Lstat(filepath.Join(lockPath, "owner.json")); !errors.Is(err, os.ErrNotExist) {
			if err == nil {
				return errors.New("operation lock owner appeared while checking staleness")
			}
			return err
		}
		if time.Since(info.ModTime()) < ownerlessLockGrace {
			return errors.New("ownerless operation lock is too new to remove safely")
		}
		if err := os.RemoveAll(lockPath); err != nil {
			return err
		}
		return syncDirectory(filepath.Dir(lockPath))
	}
	current, err := readLockOwner(lockPath)
	if err != nil {
		return err
	}
	if current != status.Owner {
		return errors.New("operation lock changed while checking staleness")
	}
	if err := os.RemoveAll(lockPath); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(lockPath))
}

func acquireReplayLock(root, operationID string) (*Lock, error) {
	lock, err := AcquireLock(root, operationID)
	if err == nil {
		return lock, nil
	}
	var lockErr *LockError
	if !errors.As(err, &lockErr) ||
		!lockErr.Status.Stale ||
		!lockErr.Status.Recoverable ||
		lockErr.Status.Owner.OperationID != operationID {
		return nil, err
	}
	if err := RemoveStaleLock(root); err != nil {
		return nil, err
	}
	return AcquireLock(root, operationID)
}

func acquireExplicitReplayLock(root, operationID string) (*Lock, error) {
	lock, err := AcquireLock(root, operationID)
	if err == nil {
		return lock, nil
	}
	var lockErr *LockError
	if !errors.As(err, &lockErr) ||
		!lockErr.Status.Stale ||
		!lockErr.Status.Recoverable {
		return nil, err
	}
	if err := RemoveStaleLock(root); err != nil {
		return nil, err
	}
	return AcquireLock(root, operationID)
}

func removeOwnedStaleLock(root, operationID string) error {
	status, err := InspectLock(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if status.Owner.OperationID != operationID {
		return nil
	}
	if !status.Stale || !status.Recoverable {
		return &LockError{Status: status}
	}
	return RemoveStaleLock(root)
}

func readLockOwner(lockPath string) (LockOwner, error) {
	data, err := os.ReadFile(filepath.Join(lockPath, "owner.json"))
	if err != nil {
		return LockOwner{}, err
	}
	var owner LockOwner
	if err := json.Unmarshal(data, &owner); err != nil {
		return LockOwner{}, fmt.Errorf("decode operation lock owner: %w", err)
	}
	if owner.OperationID == "" || owner.PID <= 0 || owner.Host == "" || owner.AcquiredAt.IsZero() {
		return LockOwner{}, errors.New("operation lock owner record is incomplete")
	}
	return owner, nil
}

func newOperationID() string {
	var suffix [6]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return fmt.Sprintf("op_%d_%d", time.Now().UTC().UnixNano(), os.Getpid())
	}
	return fmt.Sprintf("op_%d_%s", time.Now().UTC().UnixNano(), hex.EncodeToString(suffix[:]))
}

func validateOperationID(id string) error {
	if id == "" {
		return errors.New("operation id is required")
	}
	if strings.ContainsAny(id, `/\`) || id == "." || id == ".." {
		return errors.New("operation id must not contain path separators")
	}
	return nil
}

func secureDir(root, path string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("secure directory %q escapes root %q", path, root)
	}
	if err := os.MkdirAll(rootAbs, 0o700); err != nil {
		return err
	}
	if err := requireDirectory(rootAbs); err != nil {
		return err
	}
	current := rootAbs
	if rel != "." {
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if errors.Is(err, os.ErrNotExist) {
				if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
					return err
				}
				info, err = os.Lstat(current)
			}
			if err != nil {
				return err
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("secure directory component %q is a symbolic link", current)
			}
			if !info.IsDir() {
				return fmt.Errorf("secure directory component %q is not a directory", current)
			}
		}
	}
	return os.Chmod(pathAbs, 0o700)
}

func requireDirectory(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("operation control path %q is a symbolic link", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("operation control path %q is not a directory", path)
	}
	return nil
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil &&
		!errors.Is(err, syscall.EINVAL) &&
		!errors.Is(err, syscall.ENOTSUP) &&
		!errors.Is(err, syscall.EROFS) {
		return err
	}
	return nil
}
