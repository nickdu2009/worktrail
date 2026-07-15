package ops

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

	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	IntentVersion = 1
	AbsentHash    = "absent"

	actionWrite  = "write"
	actionDelete = "delete"
)

var ErrPendingOperation = errors.New("pending worktrail operation requires explicit repair")

type PendingOperationError struct {
	IDs []string
}

func (e *PendingOperationError) Error() string {
	return fmt.Sprintf("%v: ids=%s; run worktrail doctor ops --repair --confirm",
		ErrPendingOperation, strings.Join(e.IDs, ","))
}

func (e *PendingOperationError) Unwrap() error {
	return ErrPendingOperation
}

type Write struct {
	Path string
	Data []byte
	Mode os.FileMode
}

type Spec struct {
	ID      string
	Writes  []Write
	Deletes []string
}

type Action struct {
	Kind       string `json:"kind"`
	Path       string `json:"path"`
	BeforeHash string `json:"before_hash"`
	AfterHash  string `json:"after_hash"`
	BeforeMode uint32 `json:"before_mode,omitempty"`
	Mode       uint32 `json:"mode,omitempty"`
}

type Intent struct {
	Version   int       `json:"version"`
	ID        string    `json:"id"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	Actions   []Action  `json:"actions"`
}

type Failpoint func(phase string, index int, action Action) error

type Coordinator struct {
	Root      string
	Failpoint Failpoint
}

type Operation struct {
	coordinator *Coordinator
	intent      Intent
	lock        *Lock
	finished    bool
}

func New(root string) *Coordinator {
	return &Coordinator{Root: root}
}

func (c *Coordinator) Begin(spec Spec) (*Operation, error) {
	return c.BeginBuild(spec.ID, func() (Spec, error) {
		return spec, nil
	})
}

// BeginBuild acquires the cross-process lock before invoking build. Callers
// should read and validate mutable state inside build so the staged before
// hashes and the derived write set share the same serialization boundary.
func (c *Coordinator) BeginBuild(operationID string, build func() (Spec, error)) (*Operation, error) {
	if c == nil || strings.TrimSpace(c.Root) == "" {
		return nil, errors.New("ops coordinator root is required")
	}
	if build == nil {
		return nil, errors.New("operation build callback is required")
	}
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		operationID = newOperationID()
	}
	if err := validateOperationID(operationID); err != nil {
		return nil, err
	}
	lock, err := c.AcquireReadyLock(operationID)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Operation, error) {
		_ = os.RemoveAll(c.stagingPath(operationID))
		_ = lock.Release()
		return nil, err
	}
	if err := c.ensureLayout(); err != nil {
		return fail(err)
	}
	if _, err := os.Stat(c.intentPath(operationID)); err == nil {
		return nil, errors.Join(&PendingOperationError{IDs: []string{operationID}}, lock.Release())
	} else if !errors.Is(err, os.ErrNotExist) {
		return fail(err)
	}
	spec, err := build()
	if err != nil {
		return fail(err)
	}
	if id := strings.TrimSpace(spec.ID); id != "" && id != operationID {
		return fail(fmt.Errorf("built operation id %q does not match locked id %q", id, operationID))
	}
	spec.ID = operationID
	if len(spec.Writes) == 0 && len(spec.Deletes) == 0 {
		return fail(errors.New("operation must declare at least one write or delete"))
	}
	actions, err := c.stage(spec)
	if err != nil {
		return fail(err)
	}
	intent := Intent{
		Version:   IntentVersion,
		ID:        spec.ID,
		State:     "prepared",
		CreatedAt: time.Now().UTC(),
		Actions:   actions,
	}
	if err := c.writeIntent(intent); err != nil {
		return fail(err)
	}
	return &Operation{coordinator: c, intent: intent, lock: lock}, nil
}

// AcquireReadyLock acquires the operation lock and checks for other pending
// journal operations while still holding it. Callers must keep the returned
// lock until their mutation is complete so the preflight cannot race a writer.
func (c *Coordinator) AcquireReadyLock(operationID string) (*Lock, error) {
	if c == nil || strings.TrimSpace(c.Root) == "" {
		return nil, errors.New("ops coordinator root is required")
	}
	lock, err := AcquireLock(c.Root, operationID)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*Lock, error) {
		return nil, errors.Join(err, lock.Release())
	}
	if err := c.ensureLayout(); err != nil {
		return fail(err)
	}
	pending, err := c.pendingOperationIDs("")
	if err != nil {
		return fail(err)
	}
	ownerID := lock.Owner().OperationID
	other := pending[:0]
	for _, id := range pending {
		if id != ownerID {
			other = append(other, id)
		}
	}
	if len(other) > 0 {
		return fail(&PendingOperationError{IDs: append([]string(nil), other...)})
	}
	return lock, nil
}

func (o *Operation) Intent() Intent {
	if o == nil {
		return Intent{}
	}
	return o.intent
}

func (o *Operation) Commit() error {
	if o == nil || o.coordinator == nil || o.lock == nil {
		return errors.New("operation is not initialized")
	}
	if o.finished {
		return errors.New("operation is already finished")
	}
	commitErr := o.coordinator.commitIntent(&o.intent)
	releaseErr := o.lock.Release()
	o.finished = true
	return errors.Join(commitErr, releaseErr)
}

func (o *Operation) Abort() error {
	if o == nil || o.coordinator == nil || o.lock == nil {
		return errors.New("operation is not initialized")
	}
	if o.finished {
		return errors.New("operation is already finished")
	}
	abortErr := o.coordinator.abortIntent(o.intent)
	releaseErr := o.lock.Release()
	o.finished = true
	return errors.Join(abortErr, releaseErr)
}

// Replay reports an unfinished operation without applying it. Pending journal
// recovery is intentionally centralized in ReplayPending, which is invoked by
// the confirmed doctor ops repair path.
func (c *Coordinator) Replay(operationID string) error {
	if c == nil || strings.TrimSpace(c.Root) == "" {
		return errors.New("ops coordinator root is required")
	}
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if _, err := os.Stat(c.commitPath(operationID)); err == nil {
		_ = os.RemoveAll(c.stagingPath(operationID))
		return removeOwnedStaleLock(c.Root, operationID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(c.abortPath(operationID)); err == nil {
		return removeOwnedStaleLock(c.Root, operationID)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := c.readIntent(operationID); err != nil {
		return err
	}
	return &PendingOperationError{IDs: []string{operationID}}
}

func (c *Coordinator) ReplayPending() ([]string, error) {
	return c.ReplayPendingPrefix("")
}

// ReplayPendingPrefix explicitly replays pending operations whose IDs share
// prefix. It holds the operation lock across journal discovery and all replay,
// so a normal writer cannot enter between the preflight and replay.
func (c *Coordinator) ReplayPendingPrefix(prefix string) ([]string, error) {
	if c == nil || strings.TrimSpace(c.Root) == "" {
		return nil, errors.New("ops coordinator root is required")
	}
	if err := c.ensureLayout(); err != nil {
		return nil, err
	}
	replayID := "op_replay_pending_" + strings.TrimPrefix(newOperationID(), "op_")
	lock, err := acquireExplicitReplayLock(c.Root, replayID)
	if err != nil {
		return nil, err
	}
	release := func(replayed []string, replayErr error) ([]string, error) {
		return replayed, errors.Join(replayErr, lock.Release())
	}
	ids, err := c.pendingOperationIDs(prefix)
	if err != nil {
		return release(nil, err)
	}
	var replayed []string
	for _, id := range ids {
		intent, err := c.readIntent(id)
		if err != nil {
			return release(replayed, fmt.Errorf("read operation %q for replay: %w", id, err))
		}
		if err := c.commitIntent(&intent); err != nil {
			return release(replayed, fmt.Errorf("replay operation %q: %w", id, err))
		}
		replayed = append(replayed, id)
	}
	return release(replayed, nil)
}

func (c *Coordinator) pendingOperationIDs(prefix string) ([]string, error) {
	entries, err := os.ReadDir(c.journalPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".intent.json") {
			continue
		}
		id := strings.TrimSuffix(entry.Name(), ".intent.json")
		if prefix != "" && !strings.HasPrefix(id, prefix) {
			continue
		}
		if _, err := os.Stat(c.commitPath(id)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		if _, err := os.Stat(c.abortPath(id)); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func (c *Coordinator) Abort(operationID string) error {
	if c == nil || strings.TrimSpace(c.Root) == "" {
		return errors.New("ops coordinator root is required")
	}
	if err := validateOperationID(operationID); err != nil {
		return err
	}
	if _, err := os.Stat(c.commitPath(operationID)); err == nil {
		return fmt.Errorf("operation %q is already committed", operationID)
	}
	if _, err := os.Stat(c.abortPath(operationID)); err == nil {
		return nil
	}
	intent, err := c.readIntent(operationID)
	if err != nil {
		return err
	}
	lock, err := acquireReplayLock(c.Root, operationID)
	if err != nil {
		return err
	}
	abortErr := c.abortIntent(intent)
	return errors.Join(abortErr, lock.Release())
}

func (c *Coordinator) stage(spec Spec) ([]Action, error) {
	staging := c.stagingPath(spec.ID)
	if err := secureDir(c.Root, filepath.Join(staging, "before")); err != nil {
		return nil, err
	}
	if err := secureDir(c.Root, filepath.Join(staging, "after")); err != nil {
		return nil, err
	}
	seen := make(map[string]struct{}, len(spec.Writes)+len(spec.Deletes))
	actions := make([]Action, 0, len(spec.Writes)+len(spec.Deletes))
	for _, write := range spec.Writes {
		rel, target, err := c.target(write.Path)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[rel]; exists {
			return nil, fmt.Errorf("operation declares target %q more than once", rel)
		}
		seen[rel] = struct{}{}
		beforeHash, beforeData, beforeMode, err := readHashAndData(target)
		if err != nil {
			return nil, err
		}
		if beforeHash != AbsentHash {
			if err := c.writeStaged(spec.ID, "before", rel, beforeData); err != nil {
				return nil, err
			}
		}
		if err := c.writeStaged(spec.ID, "after", rel, write.Data); err != nil {
			return nil, err
		}
		mode := write.Mode
		if mode == 0 {
			mode = 0o600
		}
		actions = append(actions, Action{
			Kind:       actionWrite,
			Path:       rel,
			BeforeHash: beforeHash,
			AfterHash:  hashBytes(write.Data),
			BeforeMode: uint32(beforeMode.Perm()),
			Mode:       uint32(mode.Perm()),
		})
	}
	for _, deletePath := range spec.Deletes {
		rel, target, err := c.target(deletePath)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[rel]; exists {
			return nil, fmt.Errorf("operation declares target %q more than once", rel)
		}
		seen[rel] = struct{}{}
		beforeHash, beforeData, beforeMode, err := readHashAndData(target)
		if err != nil {
			return nil, err
		}
		if beforeHash != AbsentHash {
			if err := c.writeStaged(spec.ID, "before", rel, beforeData); err != nil {
				return nil, err
			}
		}
		actions = append(actions, Action{
			Kind:       actionDelete,
			Path:       rel,
			BeforeHash: beforeHash,
			AfterHash:  AbsentHash,
			BeforeMode: uint32(beforeMode.Perm()),
		})
	}
	return actions, nil
}

func (c *Coordinator) commitIntent(intent *Intent) error {
	if _, err := os.Stat(c.commitPath(intent.ID)); err == nil {
		_ = os.RemoveAll(c.stagingPath(intent.ID))
		return nil
	}
	if intent.Version != IntentVersion {
		return fmt.Errorf("unsupported operation intent version %d", intent.Version)
	}
	if intent.State != "prepared" && intent.State != "committing" {
		return fmt.Errorf("operation %q cannot commit from state %q", intent.ID, intent.State)
	}
	intent.State = "committing"
	if err := c.writeIntent(*intent); err != nil {
		return err
	}
	for index, action := range intent.Actions {
		if c.Failpoint != nil {
			if err := c.Failpoint("commit", index, action); err != nil {
				return err
			}
		}
		if err := c.applyAction(intent.ID, action); err != nil {
			return fmt.Errorf("apply %s %q: %w", action.Kind, action.Path, err)
		}
	}
	if c.Failpoint != nil {
		if err := c.Failpoint("commit-marker", len(intent.Actions), Action{}); err != nil {
			return err
		}
	}
	if err := c.writeMarker(c.commitPath(intent.ID), intent.ID, "committed"); err != nil {
		return err
	}
	return os.RemoveAll(c.stagingPath(intent.ID))
}

func (c *Coordinator) abortIntent(intent Intent) error {
	if _, err := os.Stat(c.abortPath(intent.ID)); err == nil {
		return nil
	}
	for index := len(intent.Actions) - 1; index >= 0; index-- {
		action := intent.Actions[index]
		if c.Failpoint != nil {
			if err := c.Failpoint("abort", index, action); err != nil {
				return err
			}
		}
		if err := c.restoreAction(intent.ID, action); err != nil {
			return fmt.Errorf("abort %s %q: %w", action.Kind, action.Path, err)
		}
	}
	if err := c.writeMarker(c.abortPath(intent.ID), intent.ID, "aborted"); err != nil {
		return err
	}
	return os.RemoveAll(c.stagingPath(intent.ID))
}

func (c *Coordinator) applyAction(operationID string, action Action) error {
	_, target, err := c.target(action.Path)
	if err != nil {
		return err
	}
	currentHash, _, _, err := readHashAndData(target)
	if err != nil {
		return err
	}
	if currentHash == action.AfterHash {
		return nil
	}
	if currentHash != action.BeforeHash {
		return fmt.Errorf("target hash changed: got %s, want before %s or after %s", currentHash, action.BeforeHash, action.AfterHash)
	}
	switch action.Kind {
	case actionWrite:
		data, err := os.ReadFile(c.stagedFile(operationID, "after", action.Path))
		if err != nil {
			return err
		}
		if hashBytes(data) != action.AfterHash {
			return errors.New("staged write hash does not match intent")
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		return util.AtomicWrite(target, data, os.FileMode(action.Mode))
	case actionDelete:
		if action.AfterHash != AbsentHash {
			return errors.New("delete action must have absent after hash")
		}
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	default:
		return fmt.Errorf("unknown action kind %q", action.Kind)
	}
}

func (c *Coordinator) restoreAction(operationID string, action Action) error {
	_, target, err := c.target(action.Path)
	if err != nil {
		return err
	}
	currentHash, _, _, err := readHashAndData(target)
	if err != nil {
		return err
	}
	if currentHash == action.BeforeHash {
		return nil
	}
	if currentHash != action.AfterHash {
		return fmt.Errorf("target hash changed: got %s, want before %s or after %s", currentHash, action.BeforeHash, action.AfterHash)
	}
	if action.BeforeHash == AbsentHash {
		if err := os.Remove(target); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return syncDirectory(filepath.Dir(target))
	}
	data, err := os.ReadFile(c.stagedFile(operationID, "before", action.Path))
	if err != nil {
		return err
	}
	if hashBytes(data) != action.BeforeHash {
		return errors.New("staged preimage hash does not match intent")
	}
	mode := os.FileMode(action.BeforeMode)
	if mode == 0 {
		mode = 0o600
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return err
	}
	return util.AtomicWrite(target, data, mode)
}

func (c *Coordinator) ensureLayout() error {
	for _, path := range []string{filepath.Join(c.Root, "ops"), c.journalPath(), filepath.Join(c.Root, "ops", "staging")} {
		if err := secureDir(c.Root, path); err != nil {
			return err
		}
	}
	return nil
}

func (c *Coordinator) writeStaged(operationID, side, rel string, data []byte) error {
	path := c.stagedFile(operationID, side, rel)
	if err := secureDir(c.Root, filepath.Dir(path)); err != nil {
		return err
	}
	return util.AtomicWrite(path, data, 0o600)
}

func (c *Coordinator) writeIntent(intent Intent) error {
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(c.intentPath(intent.ID), append(data, '\n'), 0o600)
}

func (c *Coordinator) readIntent(operationID string) (Intent, error) {
	data, err := os.ReadFile(c.intentPath(operationID))
	if err != nil {
		return Intent{}, err
	}
	var intent Intent
	if err := json.Unmarshal(data, &intent); err != nil {
		return Intent{}, fmt.Errorf("decode operation intent: %w", err)
	}
	if intent.ID != operationID {
		return Intent{}, fmt.Errorf("operation intent id %q does not match %q", intent.ID, operationID)
	}
	return intent, nil
}

func (c *Coordinator) writeMarker(path, operationID, state string) error {
	data, err := json.Marshal(struct {
		ID        string    `json:"id"`
		State     string    `json:"state"`
		CreatedAt time.Time `json:"created_at"`
	}{
		ID:        operationID,
		State:     state,
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), 0o600)
}

func (c *Coordinator) target(path string) (string, string, error) {
	path = filepath.Clean(filepath.FromSlash(strings.TrimSpace(path)))
	if path == "." || filepath.IsAbs(path) {
		return "", "", errors.New("operation target must be a non-empty relative path")
	}
	if path == ".." || strings.HasPrefix(path, ".."+string(filepath.Separator)) {
		return "", "", errors.New("operation target escapes the scope root")
	}
	rel := filepath.ToSlash(path)
	if rel == "ops" || strings.HasPrefix(rel, "ops/") {
		return "", "", errors.New("operation target must not modify the ops control directory")
	}
	rootAbs, err := filepath.Abs(c.Root)
	if err != nil {
		return "", "", err
	}
	target := filepath.Join(rootAbs, path)
	check, err := filepath.Rel(rootAbs, target)
	if err != nil || check == ".." || strings.HasPrefix(check, ".."+string(filepath.Separator)) {
		return "", "", errors.New("operation target escapes the scope root")
	}
	current := rootAbs
	parts := strings.Split(path, string(filepath.Separator))
	for _, part := range parts[:len(parts)-1] {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return "", "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", "", fmt.Errorf("operation target parent %q is a symbolic link", current)
		}
		if !info.IsDir() {
			return "", "", fmt.Errorf("operation target parent %q is not a directory", current)
		}
	}
	return rel, target, nil
}

func (c *Coordinator) journalPath() string {
	return filepath.Join(c.Root, "ops", "journal")
}

func (c *Coordinator) stagingPath(operationID string) string {
	return filepath.Join(c.Root, "ops", "staging", operationID)
}

func (c *Coordinator) stagedFile(operationID, side, rel string) string {
	return filepath.Join(c.stagingPath(operationID), side, filepath.FromSlash(rel))
}

func (c *Coordinator) intentPath(operationID string) string {
	return filepath.Join(c.journalPath(), operationID+".intent.json")
}

func (c *Coordinator) commitPath(operationID string) string {
	return filepath.Join(c.journalPath(), operationID+".commit.json")
}

func (c *Coordinator) abortPath(operationID string) string {
	return filepath.Join(c.journalPath(), operationID+".abort.json")
}

func readHashAndData(path string) (string, []byte, os.FileMode, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return AbsentHash, nil, 0, nil
	}
	if err != nil {
		return "", nil, 0, err
	}
	if !info.Mode().IsRegular() {
		return "", nil, 0, fmt.Errorf("operation target %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, 0, err
	}
	return hashBytes(data), data, info.Mode(), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
