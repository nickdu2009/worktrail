package eval

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/zstd"
)

func TestDecompressZstdFile(t *testing.T) {
	root := t.TempDir()
	inputPath := filepath.Join(root, "input.zst")
	outputPath := filepath.Join(root, "output")

	input, err := os.Create(inputPath)
	if err != nil {
		t.Fatal(err)
	}
	encoder, err := zstd.NewWriter(input)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("worktrail semantic zstd fixture")
	if _, err := encoder.Write(want); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatal(err)
	}
	if err := input.Close(); err != nil {
		t.Fatal(err)
	}

	if err := DecompressZstdFile(inputPath, outputPath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("output = %q, want %q", got, want)
	}
	if err := DecompressZstdFile(inputPath, outputPath); err == nil {
		t.Fatal("expected existing output rejection")
	}
}
