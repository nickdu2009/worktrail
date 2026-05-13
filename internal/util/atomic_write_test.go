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
}

func TestManagedBlock(t *testing.T) {
	got := ApplyManagedBlock("prefix\n", "one")
	got = ApplyManagedBlock(got, "two")
	if want := "two"; !strings.Contains(got, want) {
		t.Fatalf("missing %q in %q", want, got)
	}
}
