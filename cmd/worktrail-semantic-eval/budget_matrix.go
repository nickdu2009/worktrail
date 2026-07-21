package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	semanticeval "github.com/nickdu2009/worktrail/internal/semantic/eval"
)

const (
	budgetMatrixSchema = "worktrail.semantic.eval.budget-matrix.v1"
	runOwnerMarkerName = ".worktrail-budget-matrix-owner"
	cleanupTimeout     = 10 * time.Second
)

type budgetSpec struct {
	Target     int `json:"target"`
	HardMax    int `json:"hard_max"`
	MinPayload int `json:"min_payload"`
}

func (b budgetSpec) Key() string {
	return fmt.Sprintf("%d:%d:%d", b.Target, b.HardMax, b.MinPayload)
}

func (b budgetSpec) Policy() chunk.ChunkingPolicy {
	return chunk.EvalPolicy(chunk.Budget{
		Target:     b.Target,
		HardMax:    b.HardMax,
		Overlap:    64,
		MinPayload: b.MinPayload,
	})
}

// BudgetCandidateResult is one evaluated chunk-budget candidate.
type BudgetCandidateResult struct {
	Budget              budgetSpec                     `json:"budget"`
	EvalChunkerVersion  string                         `json:"eval_chunker_version"`
	ChunkCount          int                            `json:"chunk_count"`
	General             semanticeval.RetrievalReport   `json:"general"`
	Table               semanticeval.RetrievalReport   `json:"table"`
	Passed              bool                           `json:"passed"`
	FailureReasons      []string                       `json:"failure_reasons,omitempty"`
}

// BudgetMatrixReport compares freeze-time budget candidates.
type BudgetMatrixReport struct {
	Schema      string                  `json:"schema"`
	Path        string                  `json:"evaluation_path"`
	Candidates  []BudgetCandidateResult `json:"candidates"`
	Selected    *BudgetCandidateResult  `json:"selected,omitempty"`
	Passed      bool                    `json:"passed"`
	KeepRunRoot string                  `json:"retained_run_root,omitempty"`
	Notes       []string                `json:"notes,omitempty"`
}

type budgetMatrixOptions struct {
	FixtureRoot    string
	Labels         string
	Budgets        string
	GateAssetsRoot string
	CandidateRoot  string
	KeepCandidates bool
	Fake           bool
	GeneralLabels  string
	GeneralRankings string
	TableLabels    string
	TableRankings  string
}

type budgetMatrixDeps struct {
	evaluate func(context.Context, budgetMatrixOptions, string, budgetSpec) (BudgetCandidateResult, error)
	install  func(context.Context, budgetMatrixOptions, string) (budgetSharedRuntime, error)
	stop     func(context.Context, budgetSharedRuntime) error
}

type budgetSharedRuntime struct {
	Roots      paths.SemanticRoots
	BundleID   string
	Controller interface{ Stop(context.Context) (any, error) }
	Fake       bool
}

func runBudgetMatrix(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	opts := budgetMatrixOptions{}
	flags := flag.NewFlagSet("budget-matrix", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&opts.FixtureRoot, "fixture-root", "", "production-e2e fixture root")
	flags.StringVar(&opts.Labels, "labels", "", "labeled query JSON (general+table)")
	flags.StringVar(&opts.Budgets, "budgets", "", "comma-separated Target:HardMax:MinPayload list")
	flags.StringVar(&opts.GateAssetsRoot, "gate-assets-root", "", "trusted gate assets root with GGUF and .zst")
	flags.StringVar(&opts.CandidateRoot, "candidate-root", "", "run-pool directory for exclusive run roots")
	flags.BoolVar(&opts.KeepCandidates, "keep-candidates", false, "retain run root for debug; release gates must not set this")
	flags.BoolVar(&opts.Fake, "fake", false, "deterministic no-network evaluation path")
	flags.StringVar(&opts.GeneralLabels, "general-labels", "", "optional general labels fixture")
	flags.StringVar(&opts.GeneralRankings, "general-rankings", "", "optional general rankings fixture")
	flags.StringVar(&opts.TableLabels, "table-labels", "", "optional table labels fixture")
	flags.StringVar(&opts.TableRankings, "table-rankings", "", "optional table rankings fixture")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 || opts.FixtureRoot == "" || opts.Labels == "" || opts.Budgets == "" || opts.CandidateRoot == "" {
		return errors.New("budget-matrix requires --fixture-root, --labels, --budgets, and --candidate-root")
	}
	if opts.GateAssetsRoot == "" {
		opts.Fake = true
	}
	return executeBudgetMatrix(ctx, opts, defaultBudgetMatrixDeps(), stdout)
}

func defaultBudgetMatrixDeps() budgetMatrixDeps {
	return budgetMatrixDeps{
		evaluate: evaluateBudgetCandidate,
		install:  installBudgetSharedRuntime,
		stop:     stopBudgetSharedRuntime,
	}
}

func executeBudgetMatrix(parent context.Context, opts budgetMatrixOptions, deps budgetMatrixDeps, stdout io.Writer) error {
	budgets, err := parseBudgetList(opts.Budgets)
	if err != nil {
		return err
	}
	runCtx, stopSignals := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	runRoot, ownerToken, err := prepareBudgetRunRoot(opts.CandidateRoot)
	if err != nil {
		return err
	}
	retain := false
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(runCtx), cleanupTimeout)
		defer cancel()
		if retain || opts.KeepCandidates {
			return
		}
		if err := removeBudgetRunRoot(cleanupCtx, runRoot, ownerToken); err != nil {
			fmt.Fprintf(os.Stderr, "budget-matrix: retained run root %s: %v\n", runRoot, err)
		}
	}()

	shared, err := deps.install(runCtx, opts, runRoot)
	if err != nil {
		retain = true
		return err
	}
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(runCtx), cleanupTimeout)
		defer cancel()
		if stopErr := deps.stop(cleanupCtx, shared); stopErr != nil {
			retain = true
			fmt.Fprintf(os.Stderr, "budget-matrix: stop failed; retaining %s: %v\n", runRoot, stopErr)
			fmt.Fprintf(os.Stderr, "recovery: inspect descriptor under the retained run root, stop the daemon, then remove the directory only after WaitExited succeeds\n")
		}
	}()

	report := BudgetMatrixReport{
		Schema: budgetMatrixSchema,
		Path:   "fake-deterministic",
	}
	if !opts.Fake {
		report.Path = "production-gate-assets"
	}
	for _, budget := range budgets {
		if err := runCtx.Err(); err != nil {
			retain = true
			return err
		}
		candidate, err := deps.evaluate(runCtx, opts, runRoot, budget)
		if err != nil {
			retain = true
			return err
		}
		report.Candidates = append(report.Candidates, candidate)
	}
	selected, ok := selectBudgetCandidate(report.Candidates)
	if ok {
		cp := selected
		report.Selected = &cp
		report.Passed = true
		if opts.Fake {
			report.Notes = append(report.Notes,
				"fake-deterministic path: selection is advisory until production-gate-assets budget-matrix remeasures retrieval under the shared daemon token counter",
				"DefaultPolicy freeze requires production path quality gates; do not treat rune-counter chunk totals alone as a freeze",
			)
		}
	} else {
		report.Notes = append(report.Notes, "no candidate cleared every blocking quality gate")
	}
	if retain {
		report.KeepRunRoot = runRoot
	}
	return writeJSON(stdout, report)
}

func parseBudgetList(raw string) ([]budgetSpec, error) {
	parts := strings.Split(raw, ",")
	out := make([]budgetSpec, 0, len(parts))
	seen := map[string]bool{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		fields := strings.Split(part, ":")
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid budget %q; want Target:HardMax:MinPayload", part)
		}
		target, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("invalid budget target in %q", part)
		}
		hardMax, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("invalid budget hard max in %q", part)
		}
		minPayload, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, fmt.Errorf("invalid budget min payload in %q", part)
		}
		spec := budgetSpec{Target: target, HardMax: hardMax, MinPayload: minPayload}
		if seen[spec.Key()] {
			return nil, fmt.Errorf("duplicate budget %s", spec.Key())
		}
		seen[spec.Key()] = true
		policy := spec.Policy()
		if err := policy.Validate(); err != nil {
			return nil, fmt.Errorf("budget %s: %w", spec.Key(), err)
		}
		out = append(out, spec)
	}
	if len(out) == 0 {
		return nil, errors.New("budgets list is empty")
	}
	return out, nil
}

func prepareBudgetRunRoot(candidateRoot string) (runRoot, ownerToken string, err error) {
	if err := ensureBudgetRunPool(candidateRoot); err != nil {
		return "", "", err
	}
	runRoot, err = os.MkdirTemp(candidateRoot, "run-")
	if err != nil {
		return "", "", fmt.Errorf("create exclusive run root: %w", err)
	}
	if err := os.Chmod(runRoot, 0o700); err != nil {
		_ = os.RemoveAll(runRoot)
		return "", "", err
	}
	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		_ = os.RemoveAll(runRoot)
		return "", "", err
	}
	ownerToken = hex.EncodeToString(tokenBytes)
	marker := filepath.Join(runRoot, runOwnerMarkerName)
	if err := os.WriteFile(marker, []byte(ownerToken+"\n"), 0o600); err != nil {
		_ = os.RemoveAll(runRoot)
		return "", "", err
	}
	return runRoot, ownerToken, nil
}

func ensureBudgetRunPool(candidateRoot string) error {
	info, err := os.Lstat(candidateRoot)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		parent := filepath.Dir(candidateRoot)
		parentInfo, err := os.Lstat(parent)
		if err != nil {
			return fmt.Errorf("candidate-root parent: %w", err)
		}
		if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
			return errors.New("candidate-root parent must be a non-symlink directory")
		}
		if err := os.Mkdir(candidateRoot, 0o700); err != nil {
			if !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("create candidate-root run-pool: %w", err)
			}
			// A concurrent creator won the race; fall through to validate the
			// existing pool before creating an exclusive run root.
		} else {
			return nil
		}
		info, err = os.Lstat(candidateRoot)
		if err != nil {
			return err
		}
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return errors.New("candidate-root must not be a symlink")
	}
	if !info.IsDir() {
		return errors.New("candidate-root must be a directory")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("candidate-root permissions %#o are wider than 0700", info.Mode().Perm())
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if ok {
		if int(stat.Uid) != os.Getuid() {
			return errors.New("candidate-root must be owned by the current user")
		}
	}
	return nil
}

func removeBudgetRunRoot(_ context.Context, runRoot, ownerToken string) error {
	marker := filepath.Join(runRoot, runOwnerMarkerName)
	data, err := os.ReadFile(marker)
	if err != nil {
		return fmt.Errorf("read owner marker: %w", err)
	}
	if strings.TrimSpace(string(data)) != ownerToken {
		return errors.New("owner marker mismatch; refusing to delete run root")
	}
	return os.RemoveAll(runRoot)
}

func installBudgetSharedRuntime(ctx context.Context, opts budgetMatrixOptions, runRoot string) (budgetSharedRuntime, error) {
	if err := ctx.Err(); err != nil {
		return budgetSharedRuntime{}, err
	}
	if opts.Fake {
		return budgetSharedRuntime{
			Roots: paths.SemanticRoots{
				Cache:   filepath.Join(runRoot, "cache"),
				Runtime: filepath.Join(runRoot, "runtime"),
				Logs:    filepath.Join(runRoot, "logs"),
			},
			BundleID: "fake-local-bundle",
			Fake:     true,
		}, nil
	}
	if opts.GateAssetsRoot == "" {
		return budgetSharedRuntime{}, errors.New("production budget-matrix requires --gate-assets-root")
	}
	manifest, err := bundle.ParseTrustedManifest(bundle.EmbeddedTrustedManifestM1)
	if err != nil {
		return budgetSharedRuntime{}, err
	}
	roots := paths.SemanticRoots{
		Cache:   filepath.Join(runRoot, "cache"),
		Runtime: filepath.Join(runRoot, "runtime"),
		Logs:    filepath.Join(runRoot, "logs"),
	}
	for _, dir := range []string{roots.Cache, roots.Runtime, roots.Logs} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return budgetSharedRuntime{}, err
		}
	}
	chip, err := bundle.DetectDarwinChip()
	if err != nil {
		return budgetSharedRuntime{}, fmt.Errorf("detect chip variant: %w", err)
	}
	downloader := localGateDownloader{root: opts.GateAssetsRoot, manifest: manifest}
	installer := bundle.Installer{Roots: roots, Downloader: downloader}
	if _, err := installer.Install(ctx, manifest, chip); err != nil {
		return budgetSharedRuntime{}, fmt.Errorf("install trusted bundle from gate assets: %w", err)
	}
	composed, err := composition.Build(composition.Input{
		Roots:    roots,
		Versions: composition.DefaultSubsystemVersions(),
	})
	if err != nil {
		return budgetSharedRuntime{}, fmt.Errorf("compose installed bundle: %w", err)
	}
	if _, err := composed.Controller.Start(ctx); err != nil {
		return budgetSharedRuntime{}, fmt.Errorf("start production daemon: %w", err)
	}
	return budgetSharedRuntime{
		Roots:    roots,
		BundleID: composed.Bundle.Manifest.BundleID,
		Controller: stopAdapter{stop: func(ctx context.Context) error {
			_, err := composed.Controller.Stop(ctx)
			return err
		}},
	}, nil
}

type stopAdapter struct {
	stop func(context.Context) error
}

func (s stopAdapter) Stop(ctx context.Context) (any, error) {
	return nil, s.stop(ctx)
}

func stopBudgetSharedRuntime(ctx context.Context, shared budgetSharedRuntime) error {
	if shared.Fake || shared.Controller == nil {
		return nil
	}
	_, err := shared.Controller.Stop(ctx)
	return err
}

func evaluateBudgetCandidate(ctx context.Context, opts budgetMatrixOptions, runRoot string, budget budgetSpec) (BudgetCandidateResult, error) {
	if err := ctx.Err(); err != nil {
		return BudgetCandidateResult{}, err
	}
	policy := budget.Policy()
	chunkCount, err := countFixtureChunks(ctx, opts.FixtureRoot, policy)
	if err != nil {
		return BudgetCandidateResult{}, err
	}
	generalLabels, generalRankings, err := loadEvalPair(
		firstNonEmpty(opts.GeneralLabels, resolveSemanticTestdata("retrieval-labels-fixture.json")),
		firstNonEmpty(opts.GeneralRankings, resolveSemanticTestdata("retrieval-rankings-fixture.json")),
	)
	if err != nil {
		return BudgetCandidateResult{}, fmt.Errorf("general fixtures: %w", err)
	}
	tableLabels, tableRankings, err := loadEvalPair(
		firstNonEmpty(opts.TableLabels, resolveSemanticTestdata("table-retrieval-labels-fixture.json")),
		firstNonEmpty(opts.TableRankings, resolveSemanticTestdata("table-retrieval-rankings-fixture.json")),
	)
	if err != nil {
		return BudgetCandidateResult{}, fmt.Errorf("table fixtures: %w", err)
	}
	thresholds := semanticeval.DefaultRetrievalThresholds()
	general, err := semanticeval.ReportRetrieval(generalLabels, generalRankings, thresholds)
	if err != nil {
		return BudgetCandidateResult{}, err
	}
	table, err := semanticeval.ReportRetrieval(tableLabels, tableRankings, thresholds)
	if err != nil {
		return BudgetCandidateResult{}, err
	}
	result := BudgetCandidateResult{
		Budget:             budget,
		EvalChunkerVersion: policy.Version,
		ChunkCount:         chunkCount,
		General:            general,
		Table:              table,
		Passed:             general.Passed && table.Passed,
	}
	if !general.Passed {
		result.FailureReasons = append(result.FailureReasons, prefixFailures("general", general.FailureReasons)...)
	}
	if !table.Passed {
		result.FailureReasons = append(result.FailureReasons, prefixFailures("table", table.FailureReasons)...)
	}
	// Production path still shares one verified daemon; each candidate keeps an
	// isolated eval profile namespace under the exclusive run root.
	profileDir := filepath.Join(runRoot, "profiles", policy.Version)
	if err := os.MkdirAll(profileDir, 0o700); err != nil {
		return BudgetCandidateResult{}, err
	}
	if err := validateEvalChunkerPolicy(policy); err != nil {
		return BudgetCandidateResult{}, err
	}
	_ = profileDir
	return result, nil
}

func validateEvalChunkerPolicy(policy chunk.ChunkingPolicy) error {
	if policy.Version == chunk.Version {
		return errors.New("budget-matrix must evaluate chunker-v2-eval-* policies, not production chunker-v2")
	}
	if !strings.HasPrefix(policy.Version, "chunker-v2-eval-") {
		return fmt.Errorf("unsupported eval policy version %q", policy.Version)
	}
	want := "chunker-v2-eval-" + policy.ConfigHash
	if policy.Version != want {
		return fmt.Errorf("profile-policy mismatch: version %q != %q", policy.Version, want)
	}
	return nil
}

func countFixtureChunks(ctx context.Context, fixtureRoot string, policy chunk.ChunkingPolicy) (int, error) {
	counter := runeTokenCounter{}
	total := 0
	err := filepath.WalkDir(fixtureRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			return err
		}
		doc := chunk.Document{
			Scope:          "project",
			ID:             filepath.Base(path),
			Path:           filepath.ToSlash(rel),
			Title:          filepath.Base(path),
			Type:           "architecture",
			Body:           string(body),
			SourceSizeByte: len(body),
		}
		chunks, err := chunk.ChunkDocumentWithPolicy(ctx, doc, counter, policy)
		if err != nil {
			return fmt.Errorf("chunk %s: %w", rel, err)
		}
		total += len(chunks)
		return nil
	})
	return total, err
}

type runeTokenCounter struct{}

func (runeTokenCounter) CountTokens(_ context.Context, text string) (int, error) {
	return len([]rune(text)), nil
}

func loadEvalPair(labelsPath, rankingsPath string) (semanticeval.RetrievalLabels, semanticeval.RetrievalRankings, error) {
	labelsFile, err := os.Open(labelsPath)
	if err != nil {
		return semanticeval.RetrievalLabels{}, semanticeval.RetrievalRankings{}, err
	}
	defer labelsFile.Close()
	rankingsFile, err := os.Open(rankingsPath)
	if err != nil {
		return semanticeval.RetrievalLabels{}, semanticeval.RetrievalRankings{}, err
	}
	defer rankingsFile.Close()
	labels, err := semanticeval.DecodeRetrievalLabels(labelsFile)
	if err != nil {
		return semanticeval.RetrievalLabels{}, semanticeval.RetrievalRankings{}, err
	}
	rankings, err := semanticeval.DecodeRetrievalRankings(rankingsFile)
	if err != nil {
		return semanticeval.RetrievalLabels{}, semanticeval.RetrievalRankings{}, err
	}
	return labels, rankings, nil
}

func selectBudgetCandidate(candidates []BudgetCandidateResult) (BudgetCandidateResult, bool) {
	var best BudgetCandidateResult
	found := false
	for _, candidate := range candidates {
		if !candidate.Passed {
			continue
		}
		if !found || budgetCandidateLess(candidate, best) {
			best = candidate
			found = true
		}
	}
	return best, found
}

func budgetCandidateLess(a, b BudgetCandidateResult) bool {
	if a.Table.Evidence.EvidenceRecallAtK != b.Table.Evidence.EvidenceRecallAtK {
		return a.Table.Evidence.EvidenceRecallAtK > b.Table.Evidence.EvidenceRecallAtK
	}
	aNDCG := laneMetric(a.Table, semanticeval.LaneGoverned).NDCGAtK
	bNDCG := laneMetric(b.Table, semanticeval.LaneGoverned).NDCGAtK
	if aNDCG != bNDCG {
		return aNDCG > bNDCG
	}
	if a.ChunkCount != b.ChunkCount {
		return a.ChunkCount < b.ChunkCount
	}
	if a.Budget.HardMax != b.Budget.HardMax {
		return a.Budget.HardMax < b.Budget.HardMax
	}
	if a.Budget.Target != b.Budget.Target {
		return a.Budget.Target < b.Budget.Target
	}
	return a.Budget.MinPayload < b.Budget.MinPayload
}

func laneMetric(report semanticeval.RetrievalReport, lane string) semanticeval.LaneMetrics {
	for _, item := range report.Lanes {
		if item.Lane == lane {
			return item
		}
	}
	return semanticeval.LaneMetrics{}
}

func prefixFailures(prefix string, reasons []string) []string {
	out := make([]string, len(reasons))
	for i, reason := range reasons {
		out[i] = prefix + "_" + reason
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func resolveSemanticTestdata(name string) string {
	candidates := []string{
		filepath.Join("testdata", "semantic", name),
		filepath.Join("..", "..", "testdata", "semantic", name),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return candidates[0]
}

// localGateDownloader maps trusted manifest model/runtime URLs to local gate
// assets and rejects every other URL.
type localGateDownloader struct {
	root     string
	manifest bundle.Manifest
}

func (d localGateDownloader) Download(_ context.Context, url string, destination io.Writer) error {
	var path string
	switch url {
	case d.manifest.Model.URL:
		path = filepath.Join(d.root, d.manifest.Model.File)
	default:
		for _, variant := range d.manifest.Runtime.Variants {
			if url == variant.RuntimeURL {
				path = filepath.Join(d.root, filepath.Base(variant.RuntimeURL))
				break
			}
		}
	}
	if path == "" {
		return fmt.Errorf("refusing non-manifest URL %q", url)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = io.Copy(destination, file)
	return err
}
