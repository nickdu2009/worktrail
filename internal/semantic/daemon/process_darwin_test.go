//go:build darwin

package daemon

import (
	"context"
	"net"
	"os"
	"testing"
	"time"
)

func TestDarwinFactoryOpenRequiresKernelStartTimeMatch(t *testing.T) {
	identity, err := darwinIdentity(os.Getpid())
	if err != nil {
		t.Fatalf("darwinIdentity() error = %v", err)
	}
	if identity.PID != os.Getpid() || identity.StartedAt.IsZero() {
		t.Fatalf("darwinIdentity() = %#v", identity)
	}

	process, err := NewFactory().Open(identity)
	if err != nil {
		t.Fatalf("Open(kernel identity) error = %v", err)
	}
	if err := process.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	mismatched := identity
	mismatched.StartedAt = mismatched.StartedAt.Add(-time.Microsecond)
	if process, err := NewFactory().Open(mismatched); err == nil || process != nil {
		t.Fatalf("Open(PID-reused identity) = %v, %v; want rejected identity and nil process", process, err)
	}
}

func TestDarwinStartLockerSerializesOneBundle(t *testing.T) {
	locker, err := NewStartLocker(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	unlock, err := locker.Lock(context.Background(), "bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()

	blocked, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	second, err := locker.Lock(blocked, "bundle-a")
	if second != nil || err == nil {
		t.Fatalf("second Lock() unexpectedly acquired or returned no error: %v", err)
	}
	if err != context.DeadlineExceeded {
		t.Fatalf("second Lock() error = %v, want deadline exceeded", err)
	}

	unlock()
	released, err := locker.Lock(context.Background(), "bundle-a")
	if err != nil {
		t.Fatalf("Lock() after unlock = %v", err)
	}
	released()
	if _, err := locker.Lock(context.Background(), "../bundle-a"); err == nil {
		t.Fatal("Lock() accepted a traversal bundle ID")
	}
}

func TestDarwinEndpointAllocatorReturnsLoopbackAddress(t *testing.T) {
	address, err := NewEndpointAllocator().Allocate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" {
		t.Fatalf("Allocate() = %q, want 127.0.0.1:port", address)
	}
}
