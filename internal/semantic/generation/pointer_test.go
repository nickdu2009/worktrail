package generation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

func TestWritePointerAtomicallyPersistsAndRejectsMalformedPointer(t *testing.T) {
	directory := t.TempDir()
	expected := testPointer("current", "")
	if err := writePointer(directory, expected); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	path, err := activePointerPath(directory)
	if err != nil {
		t.Fatalf("active pointer path: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat pointer: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("pointer mode = %o, want 600", got)
	}
	actual, err := ReadActive(directory)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if actual != expected {
		t.Fatalf("pointer = %#v, want %#v", actual, expected)
	}
	if err := os.WriteFile(path, []byte(`{"schema":`), 0o600); err != nil {
		t.Fatalf("write malformed pointer: %v", err)
	}
	if _, err := ReadActive(directory); err == nil {
		t.Fatal("read malformed pointer succeeded")
	}
}

func TestActivateValidationFailureLeavesPointerUnchanged(t *testing.T) {
	directory := t.TempDir()
	expected := testPointer("current", "")
	if err := writePointer(directory, expected); err != nil {
		t.Fatalf("write pointer: %v", err)
	}

	_, err := Activate(context.Background(), directory, func(context.Context) (ActivationCandidate, error) {
		return ActivationCandidate{}, errors.New("candidate is invalid")
	}, ActivateOptions{})
	if err == nil {
		t.Fatal("activate succeeded after validation failure")
	}
	actual, err := ReadActive(directory)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if actual != expected {
		t.Fatalf("pointer = %#v, want %#v", actual, expected)
	}
}

func TestRetirePendingWaitsForSharedLease(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("current", "retired")
	if err := writePointer(directory, pointer); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	database, err := generationPath(directory, "retired", ".sqlite")
	if err != nil {
		t.Fatalf("database path: %v", err)
	}
	if err := os.WriteFile(database, []byte("database"), 0o600); err != nil {
		t.Fatalf("write database: %v", err)
	}
	lease, err := AcquireGenerationLease(context.Background(), directory, "retired", LeaseShared)
	if err != nil {
		t.Fatalf("acquire shared lease: %v", err)
	}
	defer lease.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err = RetirePending(ctx, directory)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("retire error = %v, want deadline exceeded", err)
	}
	if _, err := os.Stat(database); err != nil {
		t.Fatalf("retired database was removed while pinned: %v", err)
	}
	actual, err := ReadActive(directory)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if actual.RetireGenerationID != "retired" {
		t.Fatalf("retire generation ID = %q, want retired", actual.RetireGenerationID)
	}
}

func TestRetirePendingIsIdempotent(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("current", "retired")
	if err := writePointer(directory, pointer); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	for _, suffix := range []string{".sqlite", ".sqlite-wal", ".sqlite-shm"} {
		path, err := generationPath(directory, "retired", suffix)
		if err != nil {
			t.Fatalf("generation path: %v", err)
		}
		if err := os.WriteFile(path, []byte(suffix), 0o600); err != nil {
			t.Fatalf("write retired file: %v", err)
		}
	}
	lease, err := AcquireGenerationLease(context.Background(), directory, "retired", LeaseShared)
	if err != nil {
		t.Fatalf("create lease file: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release setup lease: %v", err)
	}

	if err := RetirePending(context.Background(), directory); err != nil {
		t.Fatalf("retire pending: %v", err)
	}
	if err := RetirePending(context.Background(), directory); err != nil {
		t.Fatalf("repeat retire pending: %v", err)
	}
	actual, err := ReadActive(directory)
	if err != nil {
		t.Fatalf("read pointer: %v", err)
	}
	if actual.RetireGenerationID != "" {
		t.Fatalf("retire generation ID = %q, want empty", actual.RetireGenerationID)
	}
	for _, suffix := range []string{".sqlite", ".sqlite-wal", ".sqlite-shm", ".lease"} {
		path, err := generationPath(directory, "retired", suffix)
		if err != nil {
			t.Fatalf("generation path: %v", err)
		}
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("retired file %s still exists or could not be checked: %v", path, err)
		}
	}
}

func testPointer(generationID, retireGenerationID string) Pointer {
	return Pointer{
		Schema:             PointerSchema,
		Version:            PointerVersion,
		Scope:              "project",
		GenerationID:       generationID,
		RecallProfileID:    "profile",
		BundleID:           "bundle",
		SnapshotHash:       "snapshot",
		ActivatedAt:        time.Date(2026, time.July, 16, 0, 0, 0, 0, time.UTC),
		RetireGenerationID: retireGenerationID,
	}
}
