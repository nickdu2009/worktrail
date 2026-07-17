package generation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

func TestOpenActiveNoPointerDoesNotCreateLease(t *testing.T) {
	directory := t.TempDir()

	_, err := OpenActive(context.Background(), directory, testMetadata())
	requireOpenErrorCode(t, err, contracts.ReasonGenerationMissing)

	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatalf("read semantic directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("semantic directory entries = %v, want no lease or database files", entries)
	}
}

func TestOpenActiveMissingDatabaseReleasesLease(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("missing", "")
	if err := writePointer(directory, pointer); err != nil {
		t.Fatalf("write pointer: %v", err)
	}

	_, err := OpenActive(context.Background(), directory, metadataForPointer(pointer))
	requireOpenErrorCode(t, err, contracts.ReasonGenerationMissing)
	assertExclusiveLeaseAvailable(t, directory, pointer.GenerationID)
}

func TestOpenActiveMetadataMismatchReleasesLease(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Metadata)
	}{
		{"generation", func(m *Metadata) { m.Generation = "other-generation" }},
		{"profile", func(m *Metadata) { m.Profile = "other-profile" }},
		{"snapshot", func(m *Metadata) { m.Snapshot = "other-snapshot" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			directory := t.TempDir()
			pointer := testPointer("metadata-mismatch", "")
			if err := writePointer(directory, pointer); err != nil {
				t.Fatalf("write pointer: %v", err)
			}
			expected := metadataForPointer(pointer)
			tc.mutate(&expected)

			_, err := OpenActive(context.Background(), directory, expected)
			requireOpenErrorCode(t, err, contracts.ReasonProfileStale)
			assertExclusiveLeaseAvailable(t, directory, pointer.GenerationID)
		})
	}
}

func TestOpenActiveUsesGenerationAndSnapshotFromPointer(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("active-from-pointer", "")
	stored := metadataForPointer(pointer)
	createSealedGeneration(t, directory, pointer, stored)
	expected := stored
	expected.Generation = ""
	expected.Snapshot = ""

	active, err := OpenActive(context.Background(), directory, expected)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	if err := active.Close(); err != nil {
		t.Fatalf("close active generation: %v", err)
	}
}

func TestClassifyOpenErrorMapsPlatformUnsupported(t *testing.T) {
	err := classifyOpenError(ErrLeaseUnsupported)
	requireOpenErrorCode(t, err, contracts.ReasonPlatformUnsupported)
	if !errors.Is(err, ErrLeaseUnsupported) {
		t.Fatalf("platform error does not wrap ErrLeaseUnsupported: %v", err)
	}
}

func TestOpenActiveReadOnlyAndCloseReleasesLease(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("active", "")
	expected := metadataForPointer(pointer)
	path := createSealedGeneration(t, directory, pointer, expected)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sealed database before open: %v", err)
	}

	active, err := OpenActive(context.Background(), directory, expected)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	if _, err := active.sealed.db.Exec(`INSERT INTO meta (
		schema, generation, profile, model_space, snapshot, sqlite_vec, dimension, build_state
	) VALUES ('write', 'write', 'write', 'write', 'write', 'write', 8, 'sealed')`); err == nil {
		t.Fatal("active sealed database accepted a write")
	}
	if err := active.Close(); err != nil {
		t.Fatalf("close active generation: %v", err)
	}
	if err := active.Close(); err != nil {
		t.Fatalf("repeat close active generation: %v", err)
	}
	assertExclusiveLeaseAvailable(t, directory, pointer.GenerationID)

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sealed database after close: %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("read-only active generation changed database bytes")
	}
}

func TestOpenActiveBlocksRetirementUntilClose(t *testing.T) {
	directory := t.TempDir()
	retired := testPointer("retired", "")
	expected := metadataForPointer(retired)
	retiredPath := createSealedGeneration(t, directory, retired, expected)

	active, err := OpenActive(context.Background(), directory, expected)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	current := testPointer("current", retired.GenerationID)
	if err := writePointer(directory, current); err != nil {
		t.Fatalf("publish replacement pointer: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err = RetirePending(ctx, directory)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("RetirePending() error = %v, want deadline exceeded", err)
	}
	if _, err := os.Stat(retiredPath); err != nil {
		t.Fatalf("retired database removed while active: %v", err)
	}

	if err := active.Close(); err != nil {
		t.Fatalf("close active generation: %v", err)
	}
	if err := RetirePending(context.Background(), directory); err != nil {
		t.Fatalf("retire after active close: %v", err)
	}
	if _, err := os.Stat(retiredPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("retired database exists after close or could not be checked: %v", err)
	}
}

func createSealedGeneration(t *testing.T, directory string, pointer Pointer, metadata Metadata) string {
	t.Helper()
	path, err := generationPath(directory, pointer.GenerationID, ".sqlite")
	if err != nil {
		t.Fatalf("generation path: %v", err)
	}
	candidate, err := CreateCandidate(path, metadata)
	if err != nil {
		t.Fatalf("create candidate: %v", err)
	}
	if err := candidate.SealCandidate(); err != nil {
		t.Fatalf("seal candidate: %v", err)
	}
	if err := writePointer(directory, pointer); err != nil {
		t.Fatalf("write pointer: %v", err)
	}
	return path
}

func metadataForPointer(pointer Pointer) Metadata {
	metadata := testMetadata()
	metadata.Generation = pointer.GenerationID
	metadata.Profile = pointer.RecallProfileID
	metadata.Snapshot = pointer.SnapshotHash
	return metadata
}

func assertExclusiveLeaseAvailable(t *testing.T, directory, generationID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	lease, err := AcquireGenerationLease(ctx, directory, generationID, LeaseExclusive)
	if err != nil {
		t.Fatalf("exclusive lease after OpenActive failure/close: %v", err)
	}
	if err := lease.Release(); err != nil {
		t.Fatalf("release exclusive lease: %v", err)
	}
}

func requireOpenErrorCode(t *testing.T, err error, want contracts.ReasonCode) {
	t.Helper()
	if err == nil {
		t.Fatal("OpenActive() error = nil")
	}
	var generationErr *Error
	if !errors.As(err, &generationErr) {
		t.Fatalf("OpenActive() error = %T %v, want *Error", err, err)
	}
	if generationErr.Code != want {
		t.Fatalf("OpenActive() error code = %q, want %q", generationErr.Code, want)
	}
}
