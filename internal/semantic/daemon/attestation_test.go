package daemon

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

func TestNewSupervisorRejectsNilRuntimeVerifierWithoutSideEffects(t *testing.T) {
	store := testStore(t)
	factory := &fakeFactory{}
	allocator := &fakeAllocator{}
	locker := &fakeLocker{}
	client := &scriptedClient{}
	config := testConfig(store, client, nil, locker, factory, allocator)

	supervisor, err := NewSupervisor(config)
	if supervisor != nil {
		t.Fatal("NewSupervisor() returned a supervisor without a runtime verifier")
	}
	if err == nil || !strings.Contains(err.Error(), "runtime verifier is required") {
		t.Fatalf("NewSupervisor() error = %v, want missing verifier", err)
	}
	if locker.calls != 0 || allocator.calls != 0 || factory.newCalls != 0 || factory.openCalls != 0 || len(client.calls) != 0 {
		t.Fatalf("NewSupervisor() side effects: locks=%d allocations=%d new=%d open=%d readiness=%d",
			locker.calls, allocator.calls, factory.newCalls, factory.openCalls, len(client.calls))
	}
}

func TestSupervisorNilRuntimeVerifierPreventsLifecycleSideEffects(t *testing.T) {
	t.Run("start", func(t *testing.T) {
		store := testStore(t)
		factory := &fakeFactory{}
		allocator := &fakeAllocator{}
		locker := &fakeLocker{}
		client := &scriptedClient{}
		supervisor := &Supervisor{config: testConfig(store, client, nil, locker, factory, allocator)}

		report, err := supervisor.Start(context.Background())
		assertVerifiedBundleRequiredError(t, err)
		if report.Reason != contracts.ReasonRuntimeUnavailable {
			t.Fatalf("Start().Reason = %q, want %q", report.Reason, contracts.ReasonRuntimeUnavailable)
		}
		if locker.calls != 0 || allocator.calls != 0 || factory.newCalls != 0 || factory.openCalls != 0 || len(client.calls) != 0 {
			t.Fatalf("Start() side effects: locks=%d allocations=%d new=%d open=%d readiness=%d",
				locker.calls, allocator.calls, factory.newCalls, factory.openCalls, len(client.calls))
		}
		if _, err := os.Stat(store.StatePath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Start() wrote descriptor: %v", err)
		}
		if _, err := os.Stat(store.APIKeyPath()); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("Start() wrote API key: %v", err)
		}
	})

	t.Run("stop", func(t *testing.T) {
		store := testStore(t)
		identity := testIdentity(89)
		saveDescriptor(t, store, identity, "http://127.0.0.1:41289")
		beforeDescriptor, err := os.ReadFile(store.StatePath())
		if err != nil {
			t.Fatal(err)
		}
		beforeKey, err := os.ReadFile(store.APIKeyPath())
		if err != nil {
			t.Fatal(err)
		}
		factory := &fakeFactory{}
		allocator := &fakeAllocator{}
		locker := &fakeLocker{}
		client := &scriptedClient{}
		supervisor := &Supervisor{config: testConfig(store, client, nil, locker, factory, allocator)}

		report, err := supervisor.Stop(context.Background())
		assertVerifiedBundleRequiredError(t, err)
		if report.Reason != contracts.ReasonRuntimeUnavailable {
			t.Fatalf("Stop().Reason = %q, want %q", report.Reason, contracts.ReasonRuntimeUnavailable)
		}
		if locker.calls != 0 || allocator.calls != 0 || factory.newCalls != 0 || factory.openCalls != 0 || len(client.calls) != 0 {
			t.Fatalf("Stop() side effects: locks=%d allocations=%d new=%d open=%d readiness=%d",
				locker.calls, allocator.calls, factory.newCalls, factory.openCalls, len(client.calls))
		}
		afterDescriptor, err := os.ReadFile(store.StatePath())
		if err != nil {
			t.Fatalf("Stop() removed descriptor: %v", err)
		}
		afterKey, err := os.ReadFile(store.APIKeyPath())
		if err != nil {
			t.Fatalf("Stop() removed API key: %v", err)
		}
		if string(afterDescriptor) != string(beforeDescriptor) || string(afterKey) != string(beforeKey) {
			t.Fatal("Stop() modified descriptor or API key")
		}
	})
}

func assertVerifiedBundleRequiredError(t *testing.T, err error) {
	t.Helper()
	var daemonErr *Error
	if !errors.As(err, &daemonErr) {
		t.Fatalf("error = %T %[1]v, want *Error", err)
	}
	if daemonErr.Code != contracts.ReasonRuntimeUnavailable {
		t.Fatalf("reason = %q, want %q", daemonErr.Code, contracts.ReasonRuntimeUnavailable)
	}
	if daemonErr.Error() != trustedBundleRequiredMessage {
		t.Fatalf("message = %q, want %q", daemonErr.Error(), trustedBundleRequiredMessage)
	}
}

type fakeRuntimeVerifier struct {
	calls   int
	runtime RuntimeSpec
	err     error
}

func (f *fakeRuntimeVerifier) VerifyRuntime(_ context.Context, runtime RuntimeSpec) error {
	f.calls++
	f.runtime = runtime
	return f.err
}
