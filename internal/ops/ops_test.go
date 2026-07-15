package ops

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommitDeclaresHashesWritesAndDeletes(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "state", "old.md"), []byte("old"))
	mustWrite(t, filepath.Join(root, "state", "delete.md"), []byte("delete"))

	coordinator := New(root)
	operation, err := coordinator.Begin(Spec{
		ID: "op-commit",
		Writes: []Write{
			{Path: "state/old.md", Data: []byte("new"), Mode: 0o640},
			{Path: "handoffs/local/new.md", Data: []byte("handoff")},
		},
		Deletes: []string{"state/delete.md"},
	})
	if err != nil {
		t.Fatal(err)
	}
	intent := operation.Intent()
	if len(intent.Actions) != 3 {
		t.Fatalf("actions = %d, want 3", len(intent.Actions))
	}
	for _, action := range intent.Actions {
		if action.BeforeHash == "" || action.AfterHash == "" {
			t.Fatalf("action does not declare hashes: %+v", action)
		}
	}
	if err := operation.Commit(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "state", "old.md"), "new")
	assertFile(t, filepath.Join(root, "handoffs", "local", "new.md"), "handoff")
	if _, err := os.Stat(filepath.Join(root, "state", "delete.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted file stat error = %v, want not exist", err)
	}
	assertMode(t, filepath.Join(root, "handoffs", "local", "new.md"), 0o600)
	assertMode(t, filepath.Join(root, "ops"), 0o700)
	assertMode(t, coordinator.intentPath("op-commit"), 0o600)

	replayed, err := coordinator.ReplayPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 0 {
		t.Fatalf("replayed committed operations: %v", replayed)
	}
}

func TestCommitFailureCanReplayIdempotently(t *testing.T) {
	root := t.TempDir()
	coordinator := New(root)
	operation, err := coordinator.Begin(Spec{
		ID: "op-replay",
		Writes: []Write{
			{Path: "a.txt", Data: []byte("a")},
			{Path: "b.txt", Data: []byte("b")},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Failpoint = func(phase string, index int, _ Action) error {
		if phase == "commit" && index == 1 {
			return errors.New("injected crash")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("Commit succeeded, want injected failure")
	}
	assertFile(t, filepath.Join(root, "a.txt"), "a")
	if _, err := os.Stat(filepath.Join(root, "b.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("b.txt stat error = %v, want not exist", err)
	}

	replayed, err := New(root).ReplayPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 || replayed[0] != "op-replay" {
		t.Fatalf("replayed = %v, want op-replay", replayed)
	}
	assertFile(t, filepath.Join(root, "a.txt"), "a")
	assertFile(t, filepath.Join(root, "b.txt"), "b")
}

func TestAbortRestoresPartiallyAppliedOperation(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.txt"), []byte("before"))
	if err := os.Chmod(filepath.Join(root, "a.txt"), 0o640); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "remove.txt"), []byte("keep"))
	coordinator := New(root)
	operation, err := coordinator.Begin(Spec{
		ID:      "op-abort",
		Writes:  []Write{{Path: "a.txt", Data: []byte("after")}, {Path: "new.txt", Data: []byte("new")}},
		Deletes: []string{"remove.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Failpoint = func(phase string, index int, _ Action) error {
		if phase == "commit" && index == 2 {
			return errors.New("stop before delete")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("Commit succeeded, want injected failure")
	}
	coordinator.Failpoint = nil
	if err := coordinator.Abort("op-abort"); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "a.txt"), "before")
	assertMode(t, filepath.Join(root, "a.txt"), 0o640)
	assertFile(t, filepath.Join(root, "remove.txt"), "keep")
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("new.txt stat error = %v, want not exist", err)
	}
}

func TestLockSerializesConcurrentOperations(t *testing.T) {
	root := t.TempDir()
	first, err := New(root).Begin(Spec{ID: "first", Writes: []Write{{Path: "a", Data: []byte("a")}}})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Abort()

	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, err := New(root).Begin(Spec{
				ID:     fmt.Sprintf("second-%d", index),
				Writes: []Write{{Path: fmt.Sprintf("b-%d", index), Data: []byte("b")}},
			})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, ErrLocked) {
			t.Fatalf("concurrent Begin error = %v, want ErrLocked", err)
		}
	}
}

func TestInspectLockDetectsExitedSameHostProcess(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "ops", "lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	owner := LockOwner{
		OperationID: "dead",
		PID:         1 << 30,
		Host:        host,
		AcquiredAt:  time.Now().UTC().Add(-time.Hour),
	}
	data, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(lockPath, "owner.json"), data)
	status, err := InspectLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Stale || !status.Recoverable {
		t.Fatalf("status = %+v, want recoverable stale lock", status)
	}
	if err := RemoveStaleLock(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lock stat error = %v, want not exist", err)
	}
}

func TestInspectLockOnlyReportsForeignHost(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "ops", "lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	owner := LockOwner{
		OperationID: "remote",
		PID:         123,
		Host:        "another-host.invalid",
		AcquiredAt:  time.Now().UTC().Add(-24 * time.Hour),
	}
	data, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(lockPath, "owner.json"), data)
	status, err := InspectLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if status.Recoverable {
		t.Fatalf("foreign lock is recoverable: %+v", status)
	}
	if err := RemoveStaleLock(root); !errors.Is(err, ErrLocked) {
		t.Fatalf("RemoveStaleLock error = %v, want ErrLocked", err)
	}
}

func TestBeginRejectsEscapingAndDuplicateTargets(t *testing.T) {
	root := t.TempDir()
	if _, err := New(root).Begin(Spec{ID: "escape", Writes: []Write{{Path: "../outside", Data: []byte("bad")}}}); err == nil {
		t.Fatal("Begin accepted escaping target")
	}
	operation, err := New(root).Begin(Spec{
		ID:      "duplicate",
		Writes:  []Write{{Path: "same", Data: []byte("value")}},
		Deletes: []string{"same"},
	})
	if err == nil {
		_ = operation.Abort()
		t.Fatal("Begin accepted duplicate target")
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := New(root).Begin(Spec{ID: "symlink", Writes: []Write{{Path: "linked/outside", Data: []byte("bad")}}}); err == nil {
		t.Fatal("Begin accepted symbolic-link parent")
	}
}

func TestBeginBuildConstructsWriteSetAfterLock(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "value.txt"), []byte("latest"))
	builtWithLock := false
	operation, err := New(root).BeginBuild("build-after-lock", func() (Spec, error) {
		if _, err := os.Stat(filepath.Join(root, "ops", "lock", "owner.json")); err != nil {
			return Spec{}, fmt.Errorf("builder ran without lock: %w", err)
		}
		data, err := os.ReadFile(filepath.Join(root, "value.txt"))
		if err != nil {
			return Spec{}, err
		}
		builtWithLock = true
		return Spec{Writes: []Write{{Path: "derived.txt", Data: append(data, []byte("-derived")...)}}}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !builtWithLock {
		t.Fatal("write-set builder was not invoked")
	}
	if err := operation.Commit(); err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "derived.txt"), "latest-derived")
}

func TestBeginBuildRejectsOtherPendingWithTypedError(t *testing.T) {
	root := t.TempDir()
	coordinator := New(root)
	operation, err := coordinator.Begin(Spec{
		ID: "op_migration_pending",
		Writes: []Write{{
			Path: "migration.txt", Data: []byte("migrated"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Failpoint = func(phase string, index int, _ Action) error {
		if phase == "commit" && index == 0 {
			return errors.New("leave migration pending")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("migration operation unexpectedly committed")
	}

	built := false
	_, err = New(root).BeginBuild("ordinary-write", func() (Spec, error) {
		built = true
		return Spec{Writes: []Write{{Path: "ordinary.txt", Data: []byte("ordinary")}}}, nil
	})
	if !errors.Is(err, ErrPendingOperation) {
		t.Fatalf("BeginBuild error = %v, want ErrPendingOperation", err)
	}
	var pendingErr *PendingOperationError
	if !errors.As(err, &pendingErr) || len(pendingErr.IDs) != 1 || pendingErr.IDs[0] != "op_migration_pending" {
		t.Fatalf("pending error = %#v, err=%v", pendingErr, err)
	}
	if built {
		t.Fatal("builder ran despite pending operation")
	}
	if _, err := os.Stat(filepath.Join(root, "ordinary.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ordinary write changed storage: %v", err)
	}
	if err := New(root).Replay("op_migration_pending"); !errors.Is(err, ErrPendingOperation) {
		t.Fatalf("single replay error = %v, want ErrPendingOperation", err)
	}
	if _, err := os.Stat(filepath.Join(root, "migration.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("single replay applied pending migration: %v", err)
	}

	if replayed, err := New(root).ReplayPending(); err != nil || len(replayed) != 1 {
		t.Fatalf("explicit replay = %v, err=%v", replayed, err)
	}
	ordinary, err := New(root).Begin(Spec{
		ID: "ordinary-after-repair", Writes: []Write{{Path: "ordinary.txt", Data: []byte("ordinary")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := ordinary.Commit(); err != nil {
		t.Fatal(err)
	}
}

func TestReplayPendingHoldsLockAcrossDiscoveryAndCommit(t *testing.T) {
	root := t.TempDir()
	coordinator := New(root)
	operation, err := coordinator.Begin(Spec{
		ID: "op_prune_pending",
		Writes: []Write{{
			Path: "pruned.txt", Data: []byte("pruned"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Failpoint = func(phase string, index int, _ Action) error {
		if phase == "commit" && index == 0 {
			return errors.New("leave prune pending")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("prune operation unexpectedly committed")
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	replayer := New(root)
	replayer.Failpoint = func(phase string, index int, _ Action) error {
		if phase == "commit" && index == 0 {
			close(entered)
			<-release
		}
		return nil
	}
	done := make(chan error, 1)
	go func() {
		_, err := replayer.ReplayPending()
		done <- err
	}()
	<-entered
	if _, err := New(root).AcquireReadyLock("racing-writer"); !errors.Is(err, ErrLocked) {
		t.Fatalf("racing preflight error = %v, want ErrLocked", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	assertFile(t, filepath.Join(root, "pruned.txt"), "pruned")
}

func TestCrossProcessLockExcludesSecondWriter(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	release := filepath.Join(t.TempDir(), "release")
	command := exec.Command(os.Args[0], "-test.run=^TestOpsLockHolderHelper$")
	command.Env = append(os.Environ(),
		"WORKTRAIL_OPS_HELPER_ROOT="+root,
		"WORKTRAIL_OPS_HELPER_READY="+ready,
		"WORKTRAIL_OPS_HELPER_RELEASE="+release,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = os.WriteFile(release, []byte("release"), 0o600)
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	waitForFile(t, ready)
	if _, err := New(root).Begin(Spec{ID: "second-process", Writes: []Write{{Path: "second", Data: []byte("bad")}}}); !errors.Is(err, ErrLocked) {
		t.Fatalf("second process Begin error = %v, want ErrLocked", err)
	}
	if err := os.WriteFile(release, []byte("release"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
}

func TestReplayPendingRemovesStaleKilledProcessLock(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(os.Args[0], "-test.run=^TestOpsCrashHolderHelper$")
	command.Env = append(os.Environ(),
		"WORKTRAIL_OPS_HELPER_ROOT="+root,
		"WORKTRAIL_OPS_HELPER_READY="+ready,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ready)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	replayed, err := New(root).ReplayPendingPrefix("op_handoff_")
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 {
		t.Fatalf("replayed = %v, want killed helper operation", replayed)
	}
	assertFile(t, filepath.Join(root, "replayed.txt"), "replayed")
	if _, err := os.Stat(filepath.Join(root, "ops", "lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale lock remains: %v", err)
	}
}

func TestOwnerlessLockCrashRequiresAgeAndExplicitRemoval(t *testing.T) {
	root := t.TempDir()
	ready := filepath.Join(t.TempDir(), "ready")
	command := exec.Command(os.Args[0], "-test.run=^TestOwnerlessLockCrashHelper$")
	command.Env = append(os.Environ(),
		"WORKTRAIL_OWNERLESS_HELPER_ROOT="+root,
		"WORKTRAIL_OWNERLESS_HELPER_READY="+ready,
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, ready)
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()

	lockPath := filepath.Join(root, "ops", "lock")
	status, err := InspectLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status.OwnerMissing || status.Stale || status.Recoverable {
		t.Fatalf("fresh ownerless lock status = %+v, want initializing and non-recoverable", status)
	}
	if replayed, err := New(root).ReplayPending(); !errors.Is(err, ErrLocked) || len(replayed) != 0 {
		t.Fatalf("explicit replay while lock initializes = %v, err=%v", replayed, err)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("explicit replay removed initializing lock: %v", err)
	}

	old := time.Now().Add(-ownerlessLockGrace - time.Second)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}
	status, err = InspectLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if !status.OwnerMissing || !status.Stale || !status.Recoverable {
		t.Fatalf("aged ownerless lock status = %+v, want recoverable orphan", status)
	}
	if err := RemoveStaleLock(root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownerless lock remains after explicit removal: %v", err)
	}
}

func TestControlDirectorySymlinksAreRejectedAtEveryLevel(t *testing.T) {
	for _, component := range []string{"ops", "ops/journal", "ops/staging"} {
		t.Run(component, func(t *testing.T) {
			root := t.TempDir()
			link := filepath.Join(root, filepath.FromSlash(component))
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(t.TempDir(), link); err != nil {
				t.Fatal(err)
			}
			if _, err := New(root).Begin(Spec{ID: "symlink-level", Writes: []Write{{Path: "value", Data: []byte("bad")}}}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
				t.Fatalf("Begin with %s symlink error = %v", component, err)
			}
		})
	}
}

func TestOpsLockHolderHelper(t *testing.T) {
	root := os.Getenv("WORKTRAIL_OPS_HELPER_ROOT")
	if root == "" {
		return
	}
	operation, err := New(root).Begin(Spec{ID: "helper-lock", Writes: []Write{{Path: "helper", Data: []byte("held")}}})
	if err != nil {
		t.Fatal(err)
	}
	defer operation.Abort()
	if err := os.WriteFile(os.Getenv("WORKTRAIL_OPS_HELPER_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(os.Getenv("WORKTRAIL_OPS_HELPER_RELEASE")); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestOwnerlessLockCrashHelper(t *testing.T) {
	root := os.Getenv("WORKTRAIL_OWNERLESS_HELPER_ROOT")
	if root == "" {
		return
	}
	if err := os.MkdirAll(filepath.Join(root, "ops"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "ops", "lock"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("WORKTRAIL_OWNERLESS_HELPER_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func TestOpsCrashHolderHelper(t *testing.T) {
	root := os.Getenv("WORKTRAIL_OPS_HELPER_ROOT")
	if root == "" {
		return
	}
	if _, err := New(root).Begin(Spec{
		ID:     "op_handoff_killed",
		Writes: []Write{{Path: "replayed.txt", Data: []byte("replayed")}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("WORKTRAIL_OPS_HELPER_READY"), []byte("ready"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		time.Sleep(time.Second)
	}
}

func waitForFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertFile(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != want {
		t.Fatalf("%s = %q, want %q", path, data, want)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %#o, want %#o", path, got, want)
	}
}
