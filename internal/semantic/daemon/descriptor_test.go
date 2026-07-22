package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestStoreSaveLoadRoundTripAndPermissions(t *testing.T) {
	store := testStore(t)
	want := Descriptor{
		PID:             4123,
		StartTime:       time.Date(2026, time.July, 16, 2, 0, 0, 0, time.UTC),
		Endpoint:        "http://127.0.0.1:8080",
		LlamaAppVersion: "b123",
		RuntimeSHA256:   "runtime-sha",
		ChipVariant:     "apple-silicon",
		ModelSHA256:     "model-sha",
		Alias:           "default",
		LastSuccess:     time.Date(2026, time.July, 16, 2, 1, 0, 0, time.UTC),
		Readiness:       "ready",
	}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != DescriptorSchema || got.Version != DescriptorVersion {
		t.Fatalf("schema/version = %q/%d", got.Schema, got.Version)
	}
	if got.BundleID != "bundle-a" {
		t.Errorf("BundleID = %q", got.BundleID)
	}
	if got.APIKeyPath != store.APIKeyPath() {
		t.Errorf("APIKeyPath = %q, want %q", got.APIKeyPath, store.APIKeyPath())
	}
	if filepath.Base(got.LogPath) != "bundle-a.log" {
		t.Errorf("LogPath = %q", got.LogPath)
	}
	if got.PID != want.PID || got.Endpoint != want.Endpoint ||
		got.LlamaAppVersion != want.LlamaAppVersion ||
		got.RuntimeSHA256 != want.RuntimeSHA256 ||
		got.ChipVariant != want.ChipVariant ||
		got.ModelSHA256 != want.ModelSHA256 ||
		got.Alias != want.Alias ||
		got.Readiness != want.Readiness ||
		!got.StartTime.Equal(want.StartTime) ||
		!got.LastSuccess.Equal(want.LastSuccess) {
		t.Errorf("round-trip descriptor = %#v", got)
	}

	assertMode(t, filepath.Dir(store.StatePath()), 0o700)
	assertMode(t, store.StatePath(), 0o600)
}

func TestStoreLoadMissingDoesNotCreateRuntimeDirectory(t *testing.T) {
	root := t.TempDir()
	roots := paths.SemanticRoots{
		Runtime: filepath.Join(root, "runtime"),
		Logs:    filepath.Join(root, "logs"),
	}
	store, err := NewStore(roots, "bundle-a")
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Load()
	if !errors.Is(err, ErrDescriptorNotFound) {
		t.Fatalf("Load() error = %v, want ErrDescriptorNotFound", err)
	}
	if _, err := os.Stat(filepath.Dir(store.StatePath())); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load() created runtime directory: %v", err)
	}
}

func TestStoreRejectsEscapedOrMismatchedPaths(t *testing.T) {
	roots := paths.SemanticRoots{
		Runtime: filepath.Join(t.TempDir(), "runtime"),
		Logs:    filepath.Join(t.TempDir(), "logs"),
	}
	if _, err := NewStore(roots, "../escape"); err == nil {
		t.Fatal("NewStore() accepted escaped bundle ID")
	}

	store, err := NewStore(roots, "bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	for _, descriptor := range []Descriptor{
		{BundleID: "other"},
		{APIKeyPath: filepath.Join(filepath.Dir(store.StatePath()), "..", "escape")},
		{LogPath: filepath.Join(filepath.Dir(store.StatePath()), "outside.log")},
	} {
		if err := store.Save(descriptor); err == nil {
			t.Fatalf("Save(%#v) accepted an unsafe descriptor", descriptor)
		}
	}
}

func TestStoreGenerateAPIKeyDoesNotOverwrite(t *testing.T) {
	store := testStore(t)
	first, err := store.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if first == "" {
		t.Fatal("GenerateAPIKey() returned an empty key")
	}
	assertMode(t, store.APIKeyPath(), 0o600)

	if _, err := store.GenerateAPIKey(); !errors.Is(err, ErrAPIKeyExists) {
		t.Fatalf("second GenerateAPIKey() error = %v, want ErrAPIKeyExists", err)
	}
	data, err := os.ReadFile(store.APIKeyPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != first {
		t.Fatalf("API key was overwritten: got %q, want %q", data, first)
	}
}

func TestStoreRemoveDeletesOnlyOwnedFiles(t *testing.T) {
	store := testStore(t)
	if err := store.Save(Descriptor{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(filepath.Dir(store.StatePath()), "keep")
	if err := os.WriteFile(foreign, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.Remove(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{store.StatePath(), store.APIKeyPath()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s still exists: %v", path, err)
		}
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Fatalf("Remove() removed foreign runtime file: %v", err)
	}
}

func TestStoreRemoveRestoresDescriptorWhenSecondDeleteFails(t *testing.T) {
	store := testStore(t)
	want := Descriptor{PID: 4123, Endpoint: "http://127.0.0.1:8080", LlamaAppVersion: "b123", RuntimeSHA256: "runtime-sha", ChipVariant: "m4", ModelSHA256: "model-sha", Alias: "bundle-a", Readiness: StateReady}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	key, err := store.GenerateAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	store.fileOps = &storeFileOperations{remove: func(path string) error {
		if path == store.APIKeyPath() {
			return errors.New("second delete failed")
		}
		return os.Remove(path)
	}}

	if err := store.Remove(); err == nil {
		t.Fatal("Remove() error = nil, want second delete failure")
	}
	got, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after recovered Remove = %v", err)
	}
	if got.PID != want.PID || got.Endpoint != want.Endpoint || got.Alias != want.Alias {
		t.Fatalf("restored descriptor = %#v, want %#v", got, want)
	}
	if gotKey, err := store.APIKey(); err != nil || gotKey != key {
		t.Fatalf("APIKey() after recovered Remove = %q, %v; want %q", gotKey, err, key)
	}
}

func TestStoreQuarantineMovesDescriptorAndKeyOutOfActiveRuntime(t *testing.T) {
	store := testStore(t)
	if err := store.Save(Descriptor{}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}

	if err := store.Quarantine(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{store.StatePath(), store.APIKeyPath()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("active runtime file %s still exists: %v", path, err)
		}
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(store.StatePath()), "quarantine", "identity-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("quarantine directories = %v, want one", matches)
	}
	assertMode(t, matches[0], 0o700)
	for _, name := range []string{stateFileName, apiKeyFileName} {
		if _, err := os.Stat(filepath.Join(matches[0], name)); err != nil {
			t.Errorf("quarantined file %s: %v", name, err)
		}
	}
}

func testStore(t *testing.T) Store {
	t.Helper()
	root := t.TempDir()
	store, err := NewStore(paths.SemanticRoots{
		Runtime: filepath.Join(root, "runtime"),
		Logs:    filepath.Join(root, "logs"),
	}, "bundle-a")
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s permissions = %o, want %o", path, got, want)
	}
}
