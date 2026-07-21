package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	semanticeval "github.com/nickdu2009/worktrail/internal/semantic/eval"
)

func TestEnsureBudgetRunPoolCreatesAndRejectsUnsafeRoots(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	pool := filepath.Join(parent, "worktrail-budget-matrix")
	if err := ensureBudgetRunPool(pool); err != nil {
		t.Fatalf("first create: %v", err)
	}
	info, err := os.Stat(pool)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("pool mode = %v err=%v", info, err)
	}
	sibling := filepath.Join(parent, "sibling")
	if err := os.WriteFile(sibling, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	wide := filepath.Join(parent, "wide")
	if err := os.Mkdir(wide, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureBudgetRunPool(wide); err == nil {
		t.Fatal("expected wide permission rejection")
	}

	link := filepath.Join(parent, "link")
	if err := os.Symlink(pool, link); err != nil {
		t.Fatal(err)
	}
	if err := ensureBudgetRunPool(link); err == nil {
		t.Fatal("expected symlink rejection")
	}

	if _, err := os.ReadFile(sibling); err != nil {
		t.Fatalf("sibling was disturbed: %v", err)
	}
}

func TestPrepareAndRemoveBudgetRunRootPreservesPool(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	pool := filepath.Join(parent, "pool")
	runRoot, token, err := prepareBudgetRunRoot(pool)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(pool, "keep-me")
	if err := os.WriteFile(marker, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := removeBudgetRunRoot(context.Background(), runRoot, token); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(runRoot); !os.IsNotExist(err) {
		t.Fatalf("run root retained: %v", err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("pool sibling/marker removed: %v", err)
	}
	if err := removeBudgetRunRoot(context.Background(), pool, token); err == nil {
		t.Fatal("refused to delete pool without matching owner marker")
	}
}

func TestSelectBudgetCandidateOrdering(t *testing.T) {
	mk := func(target, hardMax, minPayload, chunks int, evidence, ndcg float64, passed bool) BudgetCandidateResult {
		return BudgetCandidateResult{
			Budget:     budgetSpec{Target: target, HardMax: hardMax, MinPayload: minPayload},
			ChunkCount: chunks,
			Passed:     passed,
			Table: semanticeval.RetrievalReport{
				Evidence: semanticeval.EvidenceMetrics{EvidenceRecallAtK: evidence},
				Lanes: []semanticeval.LaneMetrics{{
					Lane:    semanticeval.LaneGoverned,
					NDCGAtK: ndcg,
				}},
			},
		}
	}
	selected, ok := selectBudgetCandidate([]BudgetCandidateResult{
		mk(384, 640, 80, 10, 0.95, 0.9, true),
		mk(512, 768, 80, 12, 0.99, 0.9, true),
		mk(512, 768, 128, 8, 0.99, 0.95, true),
		mk(640, 768, 80, 7, 0.99, 0.95, false),
	})
	if !ok {
		t.Fatal("expected a selected candidate")
	}
	if selected.Budget.Key() != "512:768:128" {
		t.Fatalf("selected = %s, want evidence/ndcg/chunk winner", selected.Budget.Key())
	}
}

func TestBudgetMatrixFakePathSelectsPassingCandidate(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	pool := filepath.Join(parent, "pool")
	fixtureRoot := filepath.Join("..", "..", "scripts", "semantic", "fixtures", "production-e2e")
	labels := filepath.Join(fixtureRoot, "labeled-queries.json")
	generalLabels := filepath.Join("..", "..", "testdata", "semantic", "retrieval-labels-fixture.json")
	generalRankings := filepath.Join("..", "..", "testdata", "semantic", "retrieval-rankings-fixture.json")
	tableLabels := filepath.Join("..", "..", "testdata", "semantic", "table-retrieval-labels-fixture.json")
	tableRankings := filepath.Join("..", "..", "testdata", "semantic", "table-retrieval-rankings-fixture.json")

	var stdout, stderr bytes.Buffer
	err := runBudgetMatrix(context.Background(), []string{
		"--fixture-root", fixtureRoot,
		"--labels", labels,
		"--budgets", "384:640:80,512:768:80,512:768:128,640:768:80",
		"--candidate-root", pool,
		"--fake",
		"--general-labels", generalLabels,
		"--general-rankings", generalRankings,
		"--table-labels", tableLabels,
		"--table-rankings", tableRankings,
	}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("budget-matrix: %v stderr=%s", err, stderr.String())
	}
	var report BudgetMatrixReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if !report.Passed || report.Selected == nil {
		t.Fatalf("report = %#v", report)
	}
	if report.Path != "fake-deterministic" {
		t.Fatalf("path = %q", report.Path)
	}
	if report.Selected.Budget.Key() == "" {
		t.Fatal("missing selected budget")
	}
	if _, err := os.Stat(pool); err != nil {
		t.Fatalf("run-pool removed: %v", err)
	}
}

func TestBudgetMatrixRejectsProfilePolicyMismatch(t *testing.T) {
	policy := chunk.DefaultPolicy()
	if err := validateEvalChunkerPolicy(policy); err == nil {
		t.Fatal("expected production chunker-v2 rejection")
	}
	eval := chunk.EvalPolicy(chunk.Budget{Target: 512, HardMax: 768, Overlap: 64, MinPayload: 80})
	eval.Version = "chunker-v2-eval-deadbeef"
	if err := validateEvalChunkerPolicy(eval); err == nil {
		t.Fatal("expected config-hash mismatch rejection")
	}
}

func TestBudgetMatrixStopFailureRetainsRunRoot(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	pool := filepath.Join(parent, "pool")
	deps := budgetMatrixDeps{
		install: func(context.Context, budgetMatrixOptions, string) (budgetSharedRuntime, error) {
			return budgetSharedRuntime{Fake: false, Controller: stopAdapter{stop: func(context.Context) error {
				return context.DeadlineExceeded
			}}}, nil
		},
		stop: func(ctx context.Context, shared budgetSharedRuntime) error {
			_, err := shared.Controller.Stop(ctx)
			return err
		},
	}
	opts := budgetMatrixOptions{
		FixtureRoot:   t.TempDir(),
		Labels:        "unused",
		Budgets:       "512:768:80",
		CandidateRoot: pool,
		Fake:          true,
	}
	runRoot, token, err := prepareBudgetRunRoot(pool)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := deps.install(context.Background(), opts, runRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.stop(context.Background(), shared); err == nil {
		t.Fatal("expected stop failure")
	}
	if _, err := os.Stat(runRoot); err != nil {
		t.Fatalf("run root should be retained for recovery: %v", err)
	}
	if err := removeBudgetRunRoot(context.Background(), runRoot, token); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetMatrixConcurrentExclusiveRoots(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	pool := filepath.Join(parent, "pool")
	var wg sync.WaitGroup
	roots := make(chan string, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			root, token, err := prepareBudgetRunRoot(pool)
			if err != nil {
				t.Errorf("prepare: %v", err)
				return
			}
			roots <- root
			time.Sleep(20 * time.Millisecond)
			if err := removeBudgetRunRoot(context.Background(), root, token); err != nil {
				t.Errorf("remove: %v", err)
			}
		}()
	}
	wg.Wait()
	close(roots)
	seen := map[string]bool{}
	for root := range roots {
		if seen[root] {
			t.Fatalf("duplicate exclusive root %s", root)
		}
		seen[root] = true
	}
	if len(seen) != 2 {
		t.Fatalf("roots = %d, want 2", len(seen))
	}
}

func TestLocalGateDownloaderRejectsUnknownURL(t *testing.T) {
	downloader := localGateDownloader{root: t.TempDir()}
	err := downloader.Download(context.Background(), "https://example.invalid/not-in-manifest", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "refusing") {
		t.Fatalf("error = %v", err)
	}
}

func TestRefillBenchmarkForcesThreeWindows(t *testing.T) {
	report, err := runRefillBenchmarkReport(context.Background(), refillBenchmarkOptions{
		CorpusSize: 1000,
		Queries:    8,
		Warmup:     2,
		Fake:       true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Passed || len(report.Lanes) != 2 {
		t.Fatalf("report = %#v", report)
	}
	for _, lane := range report.Lanes {
		if lane.ForcedRefillRounds < 3 {
			t.Fatalf("lane %#v missing forced refill", lane)
		}
	}
}
