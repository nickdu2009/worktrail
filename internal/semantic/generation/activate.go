package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"
)

// Pin is a stable active generation reference. Its lease remains held until
// Release, preventing RetirePending from deleting that generation.
type Pin struct {
	Pointer Pointer
	lease   *Lease
}

// Release releases the generation lease held by the pin.
func (p *Pin) Release() error {
	if p == nil {
		return nil
	}
	return p.lease.Release()
}

// PinActive reads the active pointer, acquires its shared lease, then rereads
// the pointer. A changed pointer is never returned with the previous lease.
func PinActive(ctx context.Context, semanticDir string) (*Pin, error) {
	first, err := ReadActive(semanticDir)
	if err != nil {
		return nil, err
	}
	lease, err := AcquireGenerationLease(ctx, semanticDir, first.GenerationID, LeaseShared)
	if err != nil {
		return nil, fmt.Errorf("lease active semantic generation: %w", err)
	}
	second, err := ReadActive(semanticDir)
	if err != nil {
		_ = lease.Release()
		return nil, err
	}
	if second != first {
		_ = lease.Release()
		return nil, errors.New("semantic generation active pointer changed while pinning")
	}
	return &Pin{Pointer: second, lease: lease}, nil
}

// ActivateOptions configures the lock-in final source check for Activate.
type ActivateOptions struct {
	// FinalSnapshot returns the live source snapshot hash that must match the
	// sealed candidate and ActivationCandidate. When nil, Activate skips the
	// lock-in source check.
	FinalSnapshot func(context.Context) (string, error)
}

// Activate validates a candidate before taking the coordination lock, then
// optionally rechecks the live source snapshot under the lock before
// atomically publishing the pointer and marking the previous generation for
// retirement. It never selects or restores an older generation itself.
func Activate(ctx context.Context, semanticDir string, validate CandidateValidator, opts ActivateOptions) (Pointer, error) {
	if validate == nil {
		return Pointer{}, errors.New("semantic generation candidate validator is required")
	}
	candidate, err := validate(ctx)
	if err != nil {
		return Pointer{}, fmt.Errorf("validate semantic generation candidate: %w", err)
	}
	next, err := candidate.pointer(time.Now().UTC())
	if err != nil {
		return Pointer{}, err
	}

	coordination, err := acquireScopeLease(ctx, semanticDir)
	if err != nil {
		return Pointer{}, fmt.Errorf("lock semantic generation scope: %w", err)
	}
	defer coordination.Release()

	if opts.FinalSnapshot != nil {
		live, err := opts.FinalSnapshot(ctx)
		if err != nil {
			return Pointer{}, fmt.Errorf("final source check: %w", err)
		}
		if live != candidate.SnapshotHash || live != next.SnapshotHash {
			return Pointer{}, fmt.Errorf(
				"%w: sealed %q, pointer %q, live %q",
				ErrSourcesChanged,
				candidate.SnapshotHash,
				next.SnapshotHash,
				live,
			)
		}
	}

	current, err := ReadActive(semanticDir)
	switch {
	case err == nil:
		if current.Scope != next.Scope {
			return Pointer{}, errors.New("semantic generation candidate scope differs from active pointer")
		}
		if current.RetireGenerationID != "" {
			return Pointer{}, errors.New("semantic generation retirement is pending")
		}
		if current.GenerationID == next.GenerationID {
			return Pointer{}, errors.New("semantic generation candidate is already active")
		}
		next.RetireGenerationID = current.GenerationID
	case errors.Is(err, ErrNoActivePointer):
	default:
		return Pointer{}, fmt.Errorf("read active semantic generation pointer: %w", err)
	}
	if err := writePointer(semanticDir, next); err != nil {
		return Pointer{}, fmt.Errorf("publish active semantic generation pointer: %w", err)
	}
	return next, nil
}

// RetirePending removes the generation explicitly marked by the active
// pointer, but only while holding that old generation's exclusive lease.
func RetirePending(ctx context.Context, semanticDir string) error {
	coordination, err := acquireScopeLease(ctx, semanticDir)
	if err != nil {
		return fmt.Errorf("lock semantic generation scope: %w", err)
	}
	defer coordination.Release()

	pointer, err := ReadActive(semanticDir)
	if errors.Is(err, ErrNoActivePointer) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read active semantic generation pointer: %w", err)
	}
	if pointer.RetireGenerationID == "" {
		return nil
	}
	if pointer.RetireGenerationID == pointer.GenerationID {
		return errors.New("semantic generation cannot retire the active generation")
	}

	lease, err := AcquireGenerationLease(ctx, semanticDir, pointer.RetireGenerationID, LeaseExclusive)
	if err != nil {
		return fmt.Errorf("lock retired semantic generation: %w", err)
	}
	defer lease.Release()

	for _, suffix := range []string{".sqlite", ".sqlite-wal", ".sqlite-shm", ".lease"} {
		path, err := generationPath(semanticDir, pointer.RetireGenerationID, suffix)
		if err != nil {
			return err
		}
		if err := removeIfPresent(path); err != nil {
			return fmt.Errorf("remove retired semantic generation %q: %w", path, err)
		}
	}
	pointer.RetireGenerationID = ""
	if err := writePointer(semanticDir, pointer); err != nil {
		return fmt.Errorf("clear semantic generation retirement: %w", err)
	}
	return nil
}

func removeIfPresent(path string) error {
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
