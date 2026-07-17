package eval

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompareFixturePasses(t *testing.T) {
	corpus, reference, candidate := loadFixtureSet(t)
	report, err := Compare(corpus, reference, candidate, DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.FailureReasons) != 0 {
		t.Fatalf("unexpected failed report: %#v", report)
	}
	if report.RecallAtK != 1 || report.MRR != 1 || report.NDCGAtK != 1 || report.TopKOverlap != 1 {
		t.Fatalf("unexpected metrics: %#v", report)
	}
}

func TestCompareRejectsConfigMismatch(t *testing.T) {
	corpus, reference, candidate := loadFixtureSet(t)
	candidate.Config.QueryTemplate = "query: {text}"
	if _, err := Compare(corpus, reference, candidate, DefaultThresholds()); err == nil {
		t.Fatal("expected embedding config mismatch")
	}
}

func TestCompareRejectsInvalidArtifactDigest(t *testing.T) {
	corpus, reference, candidate := loadFixtureSet(t)
	candidate.ArtifactSHA256 = "not-a-sha256"
	if _, err := Compare(corpus, reference, candidate, DefaultThresholds()); err == nil {
		t.Fatal("expected artifact digest validation error")
	}
}

func TestCompareReportsQualityFailure(t *testing.T) {
	corpus, reference, candidate := loadFixtureSet(t)
	candidate.Embeddings[2].Single = []float64{0, 0, 1, 0}
	candidate.Embeddings[2].Batch = []float64{0, 0, 1, 0}
	report, err := Compare(corpus, reference, candidate, DefaultThresholds())
	if err != nil {
		t.Fatal(err)
	}
	if report.Passed || len(report.FailureReasons) == 0 {
		t.Fatalf("expected failed report: %#v", report)
	}
}

func loadFixtureSet(t *testing.T) (Corpus, Capture, Capture) {
	t.Helper()
	root := filepath.Join("..", "..", "..", "testdata", "semantic")
	corpusFile, err := os.Open(filepath.Join(root, "parity-corpus-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer corpusFile.Close()
	referenceFile, err := os.Open(filepath.Join(root, "parity-reference-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer referenceFile.Close()
	candidateFile, err := os.Open(filepath.Join(root, "parity-candidate-fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer candidateFile.Close()

	corpus, err := DecodeCorpus(corpusFile)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := DecodeCapture(referenceFile)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := DecodeCapture(candidateFile)
	if err != nil {
		t.Fatal(err)
	}
	return corpus, reference, candidate
}
