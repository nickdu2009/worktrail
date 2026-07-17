package generation

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

// Error reports a stable reason for an active-generation open failure.
type Error struct {
	Code    contracts.ReasonCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return string(e.Code)
}

// Unwrap exposes the underlying filesystem, lease, or database error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Active is a lease-protected, read-only sealed generation. It remains safe
// from retirement until Close releases its shared lease.
type Active struct {
	sealed *SealedCandidate
	pin    *Pin

	mu sync.RWMutex

	close    sync.Once
	closeErr error
}

// OpenActive pins the active generation, verifies that its immutable identity
// matches expected metadata, then opens its sealed database read-only.
func OpenActive(ctx context.Context, semanticDir string, expected Metadata) (*Active, error) {
	pin, err := PinActive(ctx, semanticDir)
	if err != nil {
		return nil, classifyOpenError(err)
	}
	release := true
	defer func() {
		if release {
			_ = pin.Release()
		}
	}()

	if expected.Generation == "" {
		expected.Generation = pin.Pointer.GenerationID
	}
	if expected.Snapshot == "" {
		expected.Snapshot = pin.Pointer.SnapshotHash
	}
	if err := expected.validate(); err != nil {
		return nil, err
	}
	if err := precheckActiveMetadata(pin.Pointer, expected); err != nil {
		return nil, err
	}
	path, err := generationPath(semanticDir, pin.Pointer.GenerationID, ".sqlite")
	if err != nil {
		return nil, err
	}
	sealed, err := OpenSealed(path, expected)
	if err != nil {
		return nil, classifyOpenError(err)
	}

	release = false
	return &Active{sealed: sealed, pin: pin}, nil
}

func precheckActiveMetadata(pointer Pointer, expected Metadata) error {
	for _, field := range []struct {
		name string
		got  string
		want string
	}{
		{"generation", pointer.GenerationID, expected.Generation},
		{"profile", pointer.RecallProfileID, expected.Profile},
		{"snapshot", pointer.SnapshotHash, expected.Snapshot},
	} {
		if field.got != field.want {
			return &Error{
				Code:    contracts.ReasonProfileStale,
				Message: fmt.Sprintf("active generation %s mismatch: got %q, want %q", field.name, field.got, field.want),
			}
		}
	}
	return nil
}

func classifyOpenError(err error) error {
	switch {
	case errors.Is(err, ErrLeaseUnsupported):
		return &Error{
			Code:    contracts.ReasonPlatformUnsupported,
			Message: "semantic generation leases are unsupported on this platform",
			Err:     err,
		}
	case errors.Is(err, ErrNoActivePointer), errors.Is(err, os.ErrNotExist):
		return &Error{
			Code:    contracts.ReasonGenerationMissing,
			Message: "active semantic generation is missing",
			Err:     err,
		}
	default:
		return err
	}
}

// Close closes the sealed database before releasing its shared lease. It is
// safe to call more than once.
func (a *Active) Close() error {
	if a == nil {
		return nil
	}
	a.close.Do(func() {
		a.mu.Lock()
		defer a.mu.Unlock()

		var sealedErr, releaseErr error
		if a.sealed != nil {
			sealedErr = a.sealed.Close()
		}
		if a.pin != nil {
			releaseErr = a.pin.Release()
		}
		a.closeErr = errors.Join(sealedErr, releaseErr)
	})
	return a.closeErr
}

func (a *Active) withReadOnlyDB(query func(*sql.DB) error) error {
	if a == nil {
		return newQueryError(QueryErrorMetadata, "open", errors.New("active generation is required"))
	}

	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.sealed == nil || a.sealed.db == nil {
		return newQueryError(QueryErrorMetadata, "open", errors.New("active generation is closed"))
	}
	return query(a.sealed.db)
}
