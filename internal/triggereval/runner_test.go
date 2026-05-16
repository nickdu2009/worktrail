package triggereval

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type captureRunner struct {
	req RunRequest
}

func (r *captureRunner) Run(_ context.Context, req RunRequest) (RunResult, error) {
	r.req = req
	return RunResult{}, nil
}

func TestSplitCommandLine(t *testing.T) {
	got, err := SplitCommandLine(`codex run --input "$PROMPT_FILE" --flag 'two words'`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"codex", "run", "--input", "$PROMPT_FILE", "--flag", "two words"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("arg[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestSplitCommandLineRejectsUnterminatedQuote(t *testing.T) {
	if _, err := SplitCommandLine(`codex run "unterminated`); err == nil {
		t.Fatal("expected error")
	}
}

func TestCodexRunnerSkipsWithoutConfiguredCommand(t *testing.T) {
	t.Setenv(envCodexCmd, "")
	runner := NewCodexRunnerFromEnv()
	result, err := runner.Run(context.Background(), RunRequest{Case: Case{ID: "c1", Tool: ToolCodex}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Evidence.SkipReason == "" {
		t.Fatalf("expected skip reason: %#v", result)
	}
}

func TestCodexRunnerReadsExplicitCodexHome(t *testing.T) {
	t.Setenv(envCodexCmd, "codex exec -")
	t.Setenv(envCodexHome, "/tmp/codex-home")
	runner := NewCodexRunnerFromEnv()
	if runner.CodexHome != "/tmp/codex-home" {
		t.Fatalf("CodexHome = %q", runner.CodexHome)
	}
}

func TestHarnessRunCasesWithFakeRunner(t *testing.T) {
	h := Harness{
		Runner: FakeRunner{Results: map[string]RunResult{
			"c1": {Evidence: Evidence{CommandsObserved: []string{"worktrail context task"}}},
		}},
		SkipSetup: true,
	}
	cases := []Case{{ID: "c1", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}}
	evidences, results, err := h.RunCases(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidences) != 1 || len(results) != 1 {
		t.Fatalf("counts = %d/%d", len(evidences), len(results))
	}
	if results[0].Behavior != BehaviorHit {
		t.Fatalf("result = %#v", results[0])
	}
}

func TestHarnessUsesRunnerOutputDir(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	h := Harness{
		Runner:    CodexRunner{SkipReason: "skip real runner", OutDir: outDir},
		SkipSetup: true,
	}
	cases := []Case{{ID: "c1", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}}
	evidences, _, err := h.RunCases(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidences) != 1 {
		t.Fatalf("len(evidences) = %d", len(evidences))
	}
	if _, err := os.Stat(filepath.Join(outDir, "c1.prompt.txt")); err != nil {
		t.Fatalf("prompt file was not written to runner output dir: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "runs", "c1-*", "home"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 {
		t.Fatalf("home matches = %v", matches)
	}
	if !strings.HasPrefix(matches[0], filepath.Join(outDir, "runs")) {
		t.Fatalf("home was not created under output runs dir: %s", matches[0])
	}
}

func TestHarnessConvertsRelativeOutputDirToAbsoluteRunPaths(t *testing.T) {
	base := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(oldwd); err != nil {
			t.Fatal(err)
		}
	})
	h := Harness{
		Runner:    CodexRunner{SkipReason: "skip real runner", OutDir: filepath.Join("relative", "out")},
		SkipSetup: true,
	}
	cases := []Case{{ID: "rel", Tool: ToolCodex, Skill: SkillContext, ExpectedCommands: []string{"worktrail context"}}}
	evidences, _, err := h.RunCases(context.Background(), cases)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidences) != 1 {
		t.Fatalf("len(evidences) = %d", len(evidences))
	}
	matches, err := filepath.Glob(filepath.Join(base, "relative", "out", "runs", "rel-*", "home"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || !filepath.IsAbs(matches[0]) {
		t.Fatalf("home should be absolute under relative output dir, got %v", matches)
	}
}

func TestHarnessPlacesWorktrailHomeInsideProjectRoot(t *testing.T) {
	outDir := filepath.Join(t.TempDir(), "out")
	runner := &captureRunner{}
	h := Harness{
		Runner:    runner,
		OutputDir: outDir,
		SkipSetup: true,
	}
	_, err := h.RunCase(context.Background(), Case{ID: "sandbox", Tool: ToolCodex, Skill: SkillContext})
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(runner.req.ProjectRoot, runner.req.WorktrailHome)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		t.Fatalf("WorktrailHome = %q should be inside ProjectRoot = %q", runner.req.WorktrailHome, runner.req.ProjectRoot)
	}
	if filepath.Base(runner.req.WorktrailHome) != ".worktrail-home" {
		t.Fatalf("WorktrailHome = %q, want project-local .worktrail-home", runner.req.WorktrailHome)
	}
}
