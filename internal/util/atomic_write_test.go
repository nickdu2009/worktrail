package util

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAtomicWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a", "b.txt")
	if err := AtomicWrite(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("content = %q", string(b))
	}
	if err := AtomicWrite(path, []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "replacement" {
		t.Fatalf("replacement content = %q", string(b))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode = %#o, want 0600", got)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("temporary files remain after atomic write: %v", matches)
	}
}

func TestManagedBlock(t *testing.T) {
	got := ApplyManagedBlock("prefix\n", "one")
	got = ApplyManagedBlock(got, "two")
	if want := "two"; !strings.Contains(got, want) {
		t.Fatalf("missing %q in %q", want, got)
	}
}

func TestHashManagedBlock(t *testing.T) {
	got := ApplyHashManagedBlock("*.test\n", ".codex/")
	got = ApplyHashManagedBlock(got, ".agents/")
	if strings.Contains(got, "<!--") {
		t.Fatalf("hash managed block should not use markdown comments: %q", got)
	}
	if !strings.Contains(got, HashManagedBegin) || !strings.Contains(got, ".agents/") || strings.Contains(got, ".codex/") {
		t.Fatalf("unexpected hash managed block: %q", got)
	}
}
