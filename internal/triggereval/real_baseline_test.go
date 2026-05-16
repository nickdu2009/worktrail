package triggereval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRealCodexBaseline(t *testing.T) {
	if os.Getenv("WORKTRAIL_TRIGGER_EVAL_REAL") != "1" {
		t.Skip("set WORKTRAIL_TRIGGER_EVAL_REAL=1 to run real Codex baseline")
	}
	cases, err := LoadCases(filepath.Join("..", "..", "testdata", "trigger-eval", "cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	selected, label := selectRealBaselineCases(cases, os.Getenv("WORKTRAIL_TRIGGER_EVAL_CASE_ID"))
	if len(selected) == 0 {
		t.Fatalf("no cases selected for %q", label)
	}
	outDir := os.Getenv(envOutDir)
	if outDir == "" {
		outDir = filepath.Join(".worktrail", "exports", "trigger-eval")
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join("..", "..", outDir)
	}
	outDir, err = filepath.Abs(outDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	harness := Harness{
		Runner:       NewCodexRunnerFromEnv(),
		WorktrailBin: os.Getenv("WORKTRAIL_TRIGGER_EVAL_WORKTRAIL_BIN"),
		OutputDir:    outDir,
		Timeout:      5 * time.Minute,
	}
	evidences, results, err := harness.RunCases(context.Background(), selected)
	if err != nil {
		t.Fatal(err)
	}
	report := BuildReport(selected, results, evidences, ReportOptions{
		Date:         time.Now().UTC(),
		Tool:         ToolCodex,
		RunnerConfig: "real codex baseline: " + label,
	})
	jsonReport, err := RenderJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "codex-baseline-report.json"), jsonReport, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "codex-baseline-report.md"), []byte(RenderMarkdown(report)), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(results) != len(selected) {
		t.Fatalf("results = %d, want %d", len(results), len(selected))
	}
	t.Logf("baseline cases=%d hit_rate=%.4f skip_rate=%.4f report_dir=%s", len(results), report.Metrics.CommandHitRate, report.Metrics.SkipRate, outDir)
	for _, result := range results {
		t.Logf("case=%s behavior=%s evidence=%s skip=%q", result.CaseID, result.Behavior, result.EvidenceStrength, result.SkipReason)
	}
}

func selectRealBaselineCases(cases []Case, requested string) ([]Case, string) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = "codex-handoff-new-conversation"
	}
	if requested == "all" {
		return append([]Case(nil), cases...), "all"
	}
	for _, c := range cases {
		if c.ID == requested {
			return []Case{c}, requested
		}
	}
	return nil, requested
}
