//go:build !darwin

package daemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

var _ Factory = unsupportedFactory{}
var _ Process = unsupportedProcess{}

func TestUnsupportedProcessRejectsOperationsWithoutSideEffects(t *testing.T) {
	process := NewFactory().New(Command{
		Path: "/must-not-be-executed",
		Args: []string{"--must-not-be-executed"},
	})

	requireProcessUnsupported(t, process.Start(context.Background()))

	identity, err := process.Identity()
	requireProcessUnsupported(t, err)
	if identity != (Identity{}) {
		t.Errorf("Identity() = %#v, want zero identity", identity)
	}

	requireProcessUnsupported(t, process.Signal(os.Kill))

	if err := process.Release(); err != nil {
		t.Errorf("Release() error = %v, want nil", err)
	}
}

func TestUnsupportedLocalRuntimeDependenciesHaveNoSideEffects(t *testing.T) {
	root := filepath.Join(t.TempDir(), "locks")
	locker, err := NewStartLocker(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := locker.Lock(context.Background(), "bundle-a"); !errors.Is(err, ErrProcessUnsupported) {
		t.Fatalf("Lock() error = %v, want unsupported", err)
	}
	if _, err := NewEndpointAllocator().Allocate(context.Background()); !errors.Is(err, ErrProcessUnsupported) {
		t.Fatalf("Allocate() error = %v, want unsupported", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unsupported local dependencies created %q: %v", root, err)
	}
}

func requireProcessUnsupported(t *testing.T, err error) {
	t.Helper()

	if !errors.Is(err, ErrProcessUnsupported) {
		t.Fatalf("error = %v, want ErrProcessUnsupported", err)
	}

	var daemonErr *Error
	if !errors.As(err, &daemonErr) {
		t.Fatalf("error = %v, want *Error", err)
	}
	if daemonErr.Code != contracts.ReasonPlatformUnsupported {
		t.Errorf("error code = %q, want %q", daemonErr.Code, contracts.ReasonPlatformUnsupported)
	}
}
