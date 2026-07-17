//go:build linux || darwin || freebsd || openbsd || windows

package eval

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestRunVecSpike(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vectors.sqlite")
	report, err := RunVecSpike(context.Background(), VecOptions{
		Path:      path,
		Count:     64,
		Dimension: 8,
		Queries:   4,
		Limit:     5,
		Seed:      42,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Schema != VecReportSchema || report.VecVersion == "" {
		t.Fatalf("unexpected report identity: %#v", report)
	}
	if report.DatabasePath != path || report.DatabaseSize <= 0 {
		t.Fatalf("unexpected database report: %#v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestSyntheticVectorIsDeterministicAndNormalized(t *testing.T) {
	first := syntheticVector(1024, 7, 3)
	second := syntheticVector(1024, 7, 3)
	var squared float64
	for i := range first {
		if first[i] != second[i] {
			t.Fatalf("vector differs at %d", i)
		}
		squared += float64(first[i] * first[i])
	}
	if math.Abs(math.Sqrt(squared)-1) > 1e-6 {
		t.Fatalf("unexpected norm %.12f", math.Sqrt(squared))
	}
}

func TestRunVecSpikeRejectsUnboundedCount(t *testing.T) {
	_, err := RunVecSpike(context.Background(), VecOptions{
		Count:     100_001,
		Dimension: 8,
		Queries:   1,
		Limit:     1,
	})
	if err == nil {
		t.Fatal("expected count validation error")
	}
}

func TestRunVecSpikeDoesNotOverwriteDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "existing.sqlite")
	if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := RunVecSpike(context.Background(), VecOptions{
		Path:      path,
		Count:     1,
		Dimension: 2,
		Queries:   1,
		Limit:     1,
	})
	if err == nil {
		t.Fatal("expected existing database rejection")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "keep" {
		t.Fatalf("existing database changed: %q", content)
	}
}
