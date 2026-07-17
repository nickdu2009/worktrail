package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/runtime"
)

func TestApplyEffectOnceCompletedDedupe(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	calls := 0
	applied, err := applyEffectOnce(root, "effect-a", func() error {
		calls++
		return nil
	})
	if err != nil || !applied || calls != 1 {
		t.Fatalf("first apply applied=%v err=%v calls=%d", applied, err, calls)
	}
	applied, err = applyEffectOnce(root, "effect-a", func() error {
		calls++
		return nil
	})
	if err != nil || applied || calls != 1 {
		t.Fatalf("dedupe applied=%v err=%v calls=%d", applied, err, calls)
	}
}

func TestApplyEffectOnceFailedApplyRollsBackClaim(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	boom := errors.New("apply failed")
	applied, err := applyEffectOnce(root, "effect-fail", func() error { return boom })
	if applied || !errors.Is(err, boom) {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
	claimed, err := ListClaimedReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("failed apply must rollback claim, got %+v", claimed)
	}
	calls := 0
	applied, err = applyEffectOnce(root, "effect-fail", func() error {
		calls++
		return nil
	})
	if err != nil || !applied || calls != 1 {
		t.Fatalf("retry after rollback applied=%v err=%v calls=%d", applied, err, calls)
	}
}

func TestApplyEffectOnceClaimedNotTreatedAsSuccess(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	path, err := receiptPath(root, "effect-claimed")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	receipt := hookReceipt{
		Schema:    "worktrail.hook_receipt.v1",
		EffectKey: "effect-claimed",
		Status:    receiptClaimed,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	calls := 0
	applied, err := applyEffectOnce(root, "effect-claimed", func() error {
		calls++
		return nil
	})
	if applied || !errors.Is(err, ErrReceiptClaimed) || calls != 0 {
		t.Fatalf("claimed must not dedupe as success: applied=%v err=%v calls=%d", applied, err, calls)
	}
	cleared, err := ClearClaimedReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 1 || cleared[0] != "effect-claimed" {
		t.Fatalf("cleared=%v", cleared)
	}
	applied, err = applyEffectOnce(root, "effect-claimed", func() error {
		calls++
		return nil
	})
	if err != nil || !applied || calls != 1 {
		t.Fatalf("after repair applied=%v err=%v calls=%d", applied, err, calls)
	}
}

func TestApplyEffectOnceConcurrency(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	var mu sync.Mutex
	calls := 0
	var wg sync.WaitGroup
	results := make(chan bool, 8)
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			applied, err := applyEffectOnce(root, "effect-race", func() error {
				mu.Lock()
				calls++
				mu.Unlock()
				time.Sleep(5 * time.Millisecond)
				return nil
			})
			results <- applied
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	appliedCount := 0
	for applied := range results {
		if applied {
			appliedCount++
		}
	}
	for err := range errs {
		// Losers should usually see ErrReceiptClaimed. Under heavy package-parallel
		// load, ops lock acquisition can still time out as ErrLocked after the
		// winner already applied; that is acceptable as long as exactly-once holds.
		if err == nil || errors.Is(err, ErrReceiptClaimed) || errors.Is(err, ops.ErrLocked) {
			continue
		}
		t.Fatalf("unexpected err: %v", err)
	}
	if calls != 1 || appliedCount != 1 {
		t.Fatalf("calls=%d appliedCount=%d want 1/1", calls, appliedCount)
	}
}

func TestClearClaimedReceiptsSkipsInFlightEffect(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)

	go func() {
		_, err := applyEffectOnce(root, "effect-in-flight", func() error {
			close(started)
			<-release
			return nil
		})
		done <- err
	}()

	<-started
	cleared, err := ClearClaimedReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared) != 0 {
		t.Fatalf("in-flight receipt must not be cleared: %v", cleared)
	}
	claimed, err := ListClaimedReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 1 || claimed[0].EffectKey != "effect-in-flight" {
		t.Fatalf("in-flight receipt = %+v", claimed)
	}

	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	claimed, err = ListClaimedReceipts(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Fatalf("completed effect must not remain claimed: %+v", claimed)
	}
}

func TestApplyEffectOnceCompleteFailpointLeavesClaimed(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	path, err := receiptPath(root, "effect-complete-fail")
	if err != nil {
		t.Fatal(err)
	}
	// Force AtomicWrite failure by replacing the receipt path with a directory
	// after claim+apply would succeed. We simulate by writing claimed, then
	// replacing the file with a directory before a second complete attempt via
	// Clear/repair path. Direct failpoint: make parent of receipt a file.
	dir := filepath.Dir(path)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not-a-dir"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = applyEffectOnce(root, "effect-complete-fail", func() error { return nil })
	if err == nil {
		t.Fatal("expected receipt path failure when hook-receipts is a file")
	}
	_ = os.Remove(dir)
	if _, err := runtime.EnsurePrivateDir(root, filepath.Join("ops", "hook-receipts")); err != nil {
		t.Fatal(err)
	}

	// Simulate crash after apply: write claimed receipt manually and ensure
	// later calls refuse success-dedupe until repair.
	now := time.Now().UTC()
	receipt := hookReceipt{
		Schema:    "worktrail.hook_receipt.v1",
		EffectKey: "effect-complete-fail",
		Status:    receiptClaimed,
		CreatedAt: now,
		UpdatedAt: now,
	}
	data, _ := json.MarshalIndent(receipt, "", "  ")
	path, err = receiptPath(root, "effect-complete-fail")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
	applied, err := applyEffectOnce(root, "effect-complete-fail", func() error { return nil })
	if applied || !errors.Is(err, ErrReceiptClaimed) {
		t.Fatalf("applied=%v err=%v", applied, err)
	}
}

func TestSymlinkEscapeFormalPathDenied(t *testing.T) {
	env := hookEnv(t)
	arch := filepath.Join(env.ProjectWT, "architecture")
	if err := os.MkdirAll(arch, 0o755); err != nil {
		t.Fatal(err)
	}
	tmp := filepath.Join(env.ProjectRoot, "tmp-link-target")
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(env.ProjectRoot, "escape-link")
	if err := os.Symlink(arch, link); err != nil {
		t.Fatal(err)
	}
	decision := evaluateShellGuard(env, "echo x > escape-link/via-symlink.md")
	if !decision.Deny {
		t.Fatalf("symlink escape into formal path must deny: %+v", decision)
	}
}

func TestPruneArtifactsRetention(t *testing.T) {
	env := hookEnv(t)
	root := env.ProjectWT
	now := time.Now().UTC()

	oldPath, err := receiptPath(root, "old-completed")
	if err != nil {
		t.Fatal(err)
	}
	old := hookReceipt{
		Schema:    "worktrail.hook_receipt.v1",
		EffectKey: "old-completed",
		Status:    receiptCompleted,
		CreatedAt: now.Add(-40 * 24 * time.Hour),
		UpdatedAt: now.Add(-40 * 24 * time.Hour),
	}
	data, _ := json.MarshalIndent(old, "", "  ")
	if err := os.WriteFile(oldPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	freshPath, err := receiptPath(root, "fresh-completed")
	if err != nil {
		t.Fatal(err)
	}
	fresh := old
	fresh.EffectKey = "fresh-completed"
	fresh.UpdatedAt = now.Add(-2 * 24 * time.Hour)
	fresh.CreatedAt = fresh.UpdatedAt
	data, _ = json.MarshalIndent(fresh, "", "  ")
	if err := os.WriteFile(freshPath, append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}

	binding := taskBinding{
		Host:        "cursor",
		SessionHash: "deadbeefcafe",
		StateID:     "s",
		TaskID:      "t",
		LastSeenAt:  now.Add(-48 * time.Hour),
	}
	if err := saveBinding(root, binding); err != nil {
		t.Fatal(err)
	}
	if err := PruneArtifacts(root, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(oldPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("old completed receipt should be pruned: %v", err)
	}
	if _, err := os.Stat(freshPath); err != nil {
		t.Fatalf("fresh completed receipt should remain: %v", err)
	}
	bindPath, err := bindingPathFromHash(root, "cursor", "deadbeefcafe")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bindPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("idle binding should be pruned: %v", err)
	}
}

func TestEncodeJSONWriterFailure(t *testing.T) {
	err := encodeJSON(failWriter{}, map[string]any{"permission": "deny"})
	if err == nil {
		t.Fatal("expected writer failure")
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, errors.New("stdout write failed")
}
