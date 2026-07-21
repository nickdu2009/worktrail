package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	semanticeval "github.com/nickdu2009/worktrail/internal/semantic/eval"
)

func TestRunParityFixture(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "semantic")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"parity",
		"--cases", filepath.Join(root, "parity-corpus-fixture.json"),
		"--reference", filepath.Join(root, "parity-reference-fixture.json"),
		"--candidate", filepath.Join(root, "parity-candidate-fixture.json"),
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run parity: %v; stderr=%s", err, stderr.String())
	}
	var report semanticeval.Report
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Schema != semanticeval.ReportSchema {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRunVecFixture(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"vec",
		"--count", "32",
		"--dimension", "8",
		"--queries", "2",
		"--limit", "4",
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run vec: %v; stderr=%s", err, stderr.String())
	}
	var report semanticeval.VecReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report.Schema != semanticeval.VecReportSchema || report.Count != 32 {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRunRetrievalReportFixture(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "semantic")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"retrieval-report",
		"--labels", filepath.Join(root, "retrieval-labels-fixture.json"),
		"--rankings", filepath.Join(root, "retrieval-rankings-fixture.json"),
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run retrieval-report: %v; stderr=%s", err, stderr.String())
	}
	var report semanticeval.RetrievalReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Schema != semanticeval.RetrievalReportSchema {
		t.Fatalf("unexpected report: %#v", report)
	}
}

func TestRetrievalReportHelpDocumentsLaneSpecificGates(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"retrieval-report", "--help"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("expected help sentinel")
	}
	help := stderr.String()
	for _, expected := range []string{
		"-min-rrf-mrr",
		"-min-governed-recall-at-k",
		"-min-governed-mrr",
		"-min-evidence-recall-at-k",
		"require governed recall, MRR, and nDCG@k to meet entry FTS",
	} {
		if !strings.Contains(help, expected) {
			t.Fatalf("help missing %q:\n%s", expected, help)
		}
	}
}

func TestRunTableRetrievalReportFixture(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "semantic")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{
		"retrieval-report",
		"--labels", filepath.Join(root, "table-retrieval-labels-fixture.json"),
		"--rankings", filepath.Join(root, "table-retrieval-rankings-fixture.json"),
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run table retrieval-report: %v; stderr=%s", err, stderr.String())
	}
	var report semanticeval.RetrievalReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Evidence.EvidenceRecallAtK < 0.9 {
		t.Fatalf("unexpected table report: %#v", report)
	}
}

func TestRunRejectsUnknownCommand(t *testing.T) {
	if err := run(context.Background(), []string{"download"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected unknown command error")
	}
}

func TestRunDecompressZstdRejectsMissingPaths(t *testing.T) {
	if err := run(context.Background(), []string{"decompress-zstd"}, &bytes.Buffer{}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected missing path error")
	}
}
