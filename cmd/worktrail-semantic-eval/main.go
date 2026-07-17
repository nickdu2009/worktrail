package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	semanticeval "github.com/nickdu2009/worktrail/internal/semantic/eval"
)

var (
	errParityFailed    = errors.New("semantic parity thresholds failed")
	errRetrievalFailed = errors.New("semantic retrieval thresholds failed")
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return errors.New("usage: worktrail-semantic-eval <parity|vec|retrieval-report|collect-retrieval|decompress-zstd> [flags]")
	}
	switch args[0] {
	case "parity":
		return runParity(args[1:], stdout, stderr)
	case "vec":
		return runVec(ctx, args[1:], stdout, stderr)
	case "retrieval-report":
		return runRetrievalReport(args[1:], stdout, stderr)
	case "collect-retrieval":
		return runCollectRetrieval(ctx, args[1:], stdout, stderr)
	case "decompress-zstd":
		return runDecompressZstd(args[1:], stderr)
	default:
		return fmt.Errorf("unknown command %q; use parity, vec, retrieval-report, collect-retrieval, or decompress-zstd", args[0])
	}
}

func runParity(args []string, stdout, stderr io.Writer) error {
	thresholds := semanticeval.DefaultThresholds()
	flags := flag.NewFlagSet("parity", flag.ContinueOnError)
	flags.SetOutput(stderr)
	casesPath := flags.String("cases", "", "path to corpus JSON")
	referencePath := flags.String("reference", "", "path to reference capture JSON")
	candidatePath := flags.String("candidate", "", "path to candidate capture JSON")
	flags.IntVar(&thresholds.K, "k", thresholds.K, "retrieval cutoff")
	flags.Float64Var(&thresholds.NormTolerance, "norm-tolerance", thresholds.NormTolerance, "L2 norm tolerance")
	flags.Float64Var(&thresholds.MinCosine, "min-cosine", thresholds.MinCosine, "minimum corresponding-vector cosine")
	flags.Float64Var(&thresholds.MaxBatchDelta, "max-batch-delta", thresholds.MaxBatchDelta, "maximum single/batch element delta")
	flags.Float64Var(&thresholds.MinTopKOverlap, "min-top-k-overlap", thresholds.MinTopKOverlap, "minimum reference/candidate top-k overlap")
	flags.Float64Var(&thresholds.MinRecallAtK, "min-recall-at-k", thresholds.MinRecallAtK, "minimum labeled recall at k")
	flags.Float64Var(&thresholds.MinMRR, "min-mrr", thresholds.MinMRR, "minimum mean reciprocal rank")
	flags.Float64Var(&thresholds.MinNDCGAtK, "min-ndcg-at-k", thresholds.MinNDCGAtK, "minimum nDCG at k")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *casesPath == "" || *referencePath == "" || *candidatePath == "" {
		return errors.New("parity requires --cases, --reference, and --candidate")
	}

	corpus, err := decodeCorpusFile(*casesPath)
	if err != nil {
		return fmt.Errorf("cases: %w", err)
	}
	reference, err := decodeCaptureFile(*referencePath)
	if err != nil {
		return fmt.Errorf("reference: %w", err)
	}
	candidate, err := decodeCaptureFile(*candidatePath)
	if err != nil {
		return fmt.Errorf("candidate: %w", err)
	}
	report, err := semanticeval.Compare(corpus, reference, candidate, thresholds)
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, report); err != nil {
		return err
	}
	if !report.Passed {
		return errParityFailed
	}
	return nil
}

func runVec(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("vec", flag.ContinueOnError)
	flags.SetOutput(stderr)
	opts := semanticeval.VecOptions{Count: 1_000, Dimension: 1024, Queries: 10, Limit: 10, Seed: 1}
	flags.StringVar(&opts.Path, "db", "", "optional database path; temporary when omitted")
	flags.IntVar(&opts.Count, "count", opts.Count, "synthetic vector count")
	flags.IntVar(&opts.Dimension, "dimension", opts.Dimension, "vector dimension")
	flags.IntVar(&opts.Queries, "queries", opts.Queries, "query count")
	flags.IntVar(&opts.Limit, "limit", opts.Limit, "neighbors per query")
	flags.Uint64Var(&opts.Seed, "seed", opts.Seed, "deterministic generator seed")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("vec accepts flags only")
	}
	report, err := semanticeval.RunVecSpike(ctx, opts)
	if err != nil {
		return err
	}
	return writeJSON(stdout, report)
}

func runRetrievalReport(args []string, stdout, stderr io.Writer) error {
	thresholds := semanticeval.DefaultRetrievalThresholds()
	flags := flag.NewFlagSet("retrieval-report", flag.ContinueOnError)
	flags.SetOutput(stderr)
	labelsPath := flags.String("labels", "", "path to labeled query JSON")
	rankingsPath := flags.String("rankings", "", "path to explicit lane ranking JSON")
	flags.IntVar(&thresholds.K, "k", thresholds.K, "metric cutoff")
	flags.BoolVar(&thresholds.RRF.RequireVsEntryFTS, "require-rrf-vs-entry-fts", thresholds.RRF.RequireVsEntryFTS, "require RRF recall, MRR, and nDCG@k to meet entry FTS")
	flags.BoolVar(&thresholds.Governed.RequireRecallVsEntryFTS, "require-governed-recall-vs-entry-fts", thresholds.Governed.RequireRecallVsEntryFTS, "require governed recall@k to meet entry FTS; governed MRR/nDCG@k are reported, not gated")
	flags.Float64Var(&thresholds.RRF.MinRecallAtK, "min-rrf-recall-at-k", thresholds.RRF.MinRecallAtK, "minimum RRF recall at k")
	flags.Float64Var(&thresholds.RRF.MinMRR, "min-rrf-mrr", thresholds.RRF.MinMRR, "minimum RRF mean reciprocal rank")
	flags.Float64Var(&thresholds.RRF.MinNDCGAtK, "min-rrf-ndcg-at-k", thresholds.RRF.MinNDCGAtK, "minimum RRF nDCG at k")
	flags.Float64Var(&thresholds.Governed.MinRecallAtK, "min-governed-recall-at-k", thresholds.Governed.MinRecallAtK, "minimum governed recall at k; governed MRR/nDCG@k remain informational")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *labelsPath == "" || *rankingsPath == "" {
		return errors.New("retrieval-report requires --labels and --rankings")
	}

	labelsFile, err := os.Open(*labelsPath)
	if err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	defer labelsFile.Close()
	rankingsFile, err := os.Open(*rankingsPath)
	if err != nil {
		return fmt.Errorf("rankings: %w", err)
	}
	defer rankingsFile.Close()

	labels, err := semanticeval.DecodeRetrievalLabels(labelsFile)
	if err != nil {
		return fmt.Errorf("labels: %w", err)
	}
	rankings, err := semanticeval.DecodeRetrievalRankings(rankingsFile)
	if err != nil {
		return fmt.Errorf("rankings: %w", err)
	}
	report, err := semanticeval.ReportRetrieval(labels, rankings, thresholds)
	if err != nil {
		return err
	}
	if err := writeJSON(stdout, report); err != nil {
		return err
	}
	if !report.Passed {
		return errRetrievalFailed
	}
	return nil
}

func runCollectRetrieval(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("collect-retrieval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	labelsPath := flags.String("labels", "", "path to labeled query JSON")
	scope := flags.String("scope", "all", "project|user|all")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || *labelsPath == "" {
		return errors.New("collect-retrieval requires --labels")
	}
	rankings, err := collectRetrievalRankings(ctx, *labelsPath, *scope)
	if err != nil {
		return err
	}
	return writeJSON(stdout, rankings)
}

func runDecompressZstd(args []string, stderr io.Writer) error {
	flags := flag.NewFlagSet("decompress-zstd", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("input", "", "path to a zstd-compressed runtime")
	output := flags.String("output", "", "new executable output path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("decompress-zstd accepts flags only")
	}
	return semanticeval.DecompressZstdFile(*input, *output)
}

func decodeCorpusFile(path string) (semanticeval.Corpus, error) {
	file, err := os.Open(path)
	if err != nil {
		return semanticeval.Corpus{}, err
	}
	defer file.Close()
	return semanticeval.DecodeCorpus(file)
}

func decodeCaptureFile(path string) (semanticeval.Capture, error) {
	file, err := os.Open(path)
	if err != nil {
		return semanticeval.Capture{}, err
	}
	defer file.Close()
	return semanticeval.DecodeCapture(file)
}

func writeJSON(w io.Writer, value any) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}
