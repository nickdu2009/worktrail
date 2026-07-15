package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/ops"
)

func TestDoctorOpsRepairsSameHostStaleLockAndReplaysPending(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, ".worktrail")
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
	target, operationID := createPendingDelete(t, root)
	writeOpsLockOwner(t, root, ops.LockOwner{
		OperationID: "stale-lock",
		PID:         1 << 30,
		Host:        mustHostname(t),
		AcquiredAt:  time.Now().UTC().Add(-time.Hour),
	})

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"doctor", "ops", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v stderr=%s", err, errOut.String())
	}
	var status opsDoctorReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Repair || status.Lock == nil || !status.Lock.Recoverable || len(status.Pending) != 1 || status.Pending[0] != operationID {
		t.Fatalf("status report = %+v", status)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("read-only status changed target: %v", err)
	}

	out.Reset()
	errOut.Reset()
	err := Run(context.Background(), []string{"doctor", "ops", "repair"}, nil, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "requires --confirm") {
		t.Fatalf("repair without confirm error = %v", err)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"doctor", "ops", "repair", "--confirm", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("repair: %v stderr=%s", err, errOut.String())
	}
	var repaired opsDoctorReport
	if err := json.Unmarshal(out.Bytes(), &repaired); err != nil {
		t.Fatal(err)
	}
	if !repaired.OK || !repaired.LockRemoved || len(repaired.Replayed) != 1 || repaired.Replayed[0] != operationID || len(repaired.Remaining) != 0 {
		t.Fatalf("repair report = %+v", repaired)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending delete was not replayed: %v", err)
	}
}

func TestDoctorOpsConfirmedRepairRemovesAgedOwnerlessLock(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, ".worktrail")
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
	lockPath := filepath.Join(root, "ops", "lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(lockPath, old, old); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"doctor", "ops", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("status: %v stderr=%s", err, errOut.String())
	}
	var status opsDoctorReport
	if err := json.Unmarshal(out.Bytes(), &status); err != nil {
		t.Fatal(err)
	}
	if status.Lock == nil || !status.Lock.OwnerMissing || !status.Lock.Recoverable || status.LockRemoved {
		t.Fatalf("ownerless status = %+v", status)
	}
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("status removed ownerless lock: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"doctor", "ops", "repair", "--confirm", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("repair: %v stderr=%s", err, errOut.String())
	}
	var repaired opsDoctorReport
	if err := json.Unmarshal(out.Bytes(), &repaired); err != nil {
		t.Fatal(err)
	}
	if !repaired.LockRemoved || !repaired.OK {
		t.Fatalf("ownerless repair = %+v", repaired)
	}
	if _, err := os.Stat(lockPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ownerless lock remains: %v", err)
	}
}

func TestDoctorOpsDoesNotTouchForeignOrUnknownLocks(t *testing.T) {
	tests := []struct {
		name      string
		writeLock func(*testing.T, string)
	}{
		{
			name: "foreign",
			writeLock: func(t *testing.T, root string) {
				writeOpsLockOwner(t, root, ops.LockOwner{
					OperationID: "foreign-lock",
					PID:         123,
					Host:        "another-host.invalid",
					AcquiredAt:  time.Now().UTC().Add(-24 * time.Hour),
				})
			},
		},
		{
			name: "unknown",
			writeLock: func(t *testing.T, root string) {
				lockPath := filepath.Join(root, "ops", "lock")
				if err := os.MkdirAll(lockPath, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(lockPath, "owner.json"), []byte(`{"invalid":true}`), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			project := filepath.Join(t.TempDir(), "project")
			root := filepath.Join(project, ".worktrail")
			t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
			t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
			target, _ := createPendingDelete(t, root)
			tc.writeLock(t, root)

			var out, errOut bytes.Buffer
			if err := Run(context.Background(), []string{"doctor", "ops", "--repair", "--confirm", "--format", "json"}, nil, &out, &errOut); err != nil {
				t.Fatalf("repair: %v stderr=%s", err, errOut.String())
			}
			var report opsDoctorReport
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.OK || !report.Blocked || report.LockRemoved || len(report.Replayed) != 0 {
				t.Fatalf("blocked report = %+v", report)
			}
			if _, err := os.Stat(filepath.Join(root, "ops", "lock")); err != nil {
				t.Fatalf("unsafe lock was touched: %v", err)
			}
			if _, err := os.Stat(target); err != nil {
				t.Fatalf("journal replayed behind unsafe lock: %v", err)
			}
		})
	}
}

func createPendingDelete(t *testing.T, root string) (string, string) {
	t.Helper()
	target := filepath.Join(root, "runtime", "sessions", "pending.md")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("pending"), 0o600); err != nil {
		t.Fatal(err)
	}
	coordinator := ops.New(root)
	operation, err := coordinator.Begin(ops.Spec{Deletes: []string{"runtime/sessions/pending.md"}})
	if err != nil {
		t.Fatal(err)
	}
	operationID := operation.Intent().ID
	coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
		if phase == "commit" && index == 0 {
			return errors.New("injected pending operation")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("operation unexpectedly committed")
	}
	return target, operationID
}

func writeOpsLockOwner(t *testing.T, root string, owner ops.LockOwner) {
	t.Helper()
	lockPath := filepath.Join(root, "ops", "lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockPath, "owner.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustHostname(t *testing.T) string {
	t.Helper()
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	return host
}
