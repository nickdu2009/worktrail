package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

var ErrLeaseUnsupported = errors.New("semantic_platform_unsupported")

// LeaseMode determines whether a lease permits concurrent readers.
type LeaseMode uint8

const (
	LeaseShared LeaseMode = iota + 1
	LeaseExclusive
)

// Lease keeps an advisory file lock until Release is called.
type Lease struct {
	file       *os.File
	release    sync.Once
	releaseErr error
}

// AcquireGenerationLease obtains a shared or exclusive lease for generationID.
func AcquireGenerationLease(ctx context.Context, semanticDir, generationID string, mode LeaseMode) (*Lease, error) {
	path, err := generationPath(semanticDir, generationID, ".lease")
	if err != nil {
		return nil, err
	}
	return acquireLease(ctx, path, mode)
}

func acquireScopeLease(ctx context.Context, semanticDir string) (*Lease, error) {
	directory, err := resolveSemanticDir(semanticDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create semantic generation directory: %w", err)
	}
	return acquireLease(ctx, filepath.Join(directory, scopeLeaseName), LeaseExclusive)
}

func acquireLease(ctx context.Context, path string, mode LeaseMode) (*Lease, error) {
	if mode != LeaseShared && mode != LeaseExclusive {
		return nil, errors.New("invalid semantic generation lease mode")
	}
	file, err := acquireLockedFile(ctx, path, mode)
	if err != nil {
		return nil, err
	}
	return &Lease{file: file}, nil
}

// Release unlocks and closes the lease. It is safe to call more than once.
func (l *Lease) Release() error {
	if l == nil {
		return nil
	}
	l.release.Do(func() {
		if l.file == nil {
			return
		}
		l.releaseErr = releaseLockedFile(l.file)
	})
	return l.releaseErr
}
