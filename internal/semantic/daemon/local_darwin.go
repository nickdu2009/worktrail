//go:build darwin

package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const startLockDirectory = "start-locks"

// NewStartLocker returns a Darwin flock-backed locker rooted at root. It does
// not create root until a lock is acquired.
func NewStartLocker(root string) (StartLocker, error) {
	if err := validateLockRoot(root); err != nil {
		return nil, err
	}
	return darwinStartLocker{root: root}, nil
}

type darwinStartLocker struct {
	root string
}

func (locker darwinStartLocker) Lock(ctx context.Context, bundleID string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateBundleID(bundleID); err != nil {
		return nil, err
	}
	lockRoot := filepath.Join(locker.root, startLockDirectory)
	if err := os.MkdirAll(lockRoot, 0o700); err != nil {
		return nil, fmt.Errorf("create semantic daemon lock directory: %w", err)
	}
	if err := os.Chmod(lockRoot, 0o700); err != nil {
		return nil, fmt.Errorf("secure semantic daemon lock directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(lockRoot, bundleID+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open semantic daemon start lock: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("secure semantic daemon start lock: %w", err)
	}
	if err := acquireDarwinLock(ctx, file); err != nil {
		_ = file.Close()
		return nil, err
	}

	var once sync.Once
	return func() {
		once.Do(func() {
			_ = unix.Flock(int(file.Fd()), unix.LOCK_UN)
			_ = file.Close()
		})
	}, nil
}

func acquireDarwinLock(ctx context.Context, file *os.File) error {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return fmt.Errorf("acquire semantic daemon start lock: %w", err)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// NewEndpointAllocator returns the Darwin loopback ephemeral-port allocator.
func NewEndpointAllocator() EndpointAllocator {
	return darwinEndpointAllocator{}
}

type darwinEndpointAllocator struct{}

func (darwinEndpointAllocator) Allocate(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("allocate semantic daemon loopback endpoint: %w", err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		return "", fmt.Errorf("release semantic daemon loopback endpoint: %w", err)
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port == "" {
		return "", errors.New("allocated semantic daemon endpoint is not loopback")
	}
	return address, nil
}
