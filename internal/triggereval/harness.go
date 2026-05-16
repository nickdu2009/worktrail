package triggereval

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const defaultCaseTimeout = 2 * time.Minute

type Harness struct {
	Runner       Runner
	WorktrailBin string
	OutputDir    string
	Timeout      time.Duration
	SkipSetup    bool
}

func (h Harness) RunCases(ctx context.Context, cases []Case) ([]Evidence, []Result, error) {
	if h.Runner == nil {
		h.Runner = NewCodexRunnerFromEnv()
	}
	var evidences []Evidence
	var results []Result
	for _, c := range cases {
		evidence, err := h.RunCase(ctx, c)
		if err != nil {
			return nil, nil, err
		}
		evidences = append(evidences, evidence)
		results = append(results, Score(c, evidence))
	}
	return evidences, results, nil
}

func (h Harness) RunCase(ctx context.Context, c Case) (Evidence, error) {
	if h.Runner == nil {
		h.Runner = NewCodexRunnerFromEnv()
	}
	outputDir := h.OutputDir
	if outputDir == "" {
		outputDir = runnerOutputDir(h.Runner)
	}
	var err error
	if outputDir != "" {
		outputDir, err = filepath.Abs(outputDir)
		if err != nil {
			return Evidence{}, err
		}
	}
	work, err := h.createWorkDir(c, outputDir)
	if err != nil {
		return Evidence{}, err
	}
	home := filepath.Join(work, "home")
	project := filepath.Join(work, "project")
	userWT := filepath.Join(project, ".worktrail-home")
	projectWT := filepath.Join(project, ".worktrail")
	if outputDir == "" {
		outputDir = filepath.Join(projectWT, "exports", "trigger-eval")
	}
	outputDir, err = filepath.Abs(outputDir)
	if err != nil {
		return Evidence{}, err
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return Evidence{}, err
	}
	if err := os.MkdirAll(home, 0o755); err != nil {
		return Evidence{}, err
	}
	if err := os.MkdirAll(userWT, 0o755); err != nil {
		return Evidence{}, err
	}
	if err := os.MkdirAll(project, 0o755); err != nil {
		return Evidence{}, err
	}
	promptFile := filepath.Join(outputDir, c.ID+".prompt.txt")
	if err := os.WriteFile(promptFile, []byte(c.Prompt+"\n"), 0o644); err != nil {
		return Evidence{}, err
	}
	req := RunRequest{
		Case:                 c,
		ProjectRoot:          project,
		Home:                 home,
		WorktrailHome:        userWT,
		WorktrailProjectRoot: project,
		OutputDir:            outputDir,
		PromptFile:           promptFile,
		Timeout:              h.timeout(),
	}
	if !h.SkipSetup {
		if reason := h.setupWorktrail(ctx, req); reason != "" {
			return RedactEvidence(Evidence{CaseID: c.ID, Tool: c.Tool, SkipReason: reason}, work, home, userWT, project, outputDir), nil
		}
	}
	run, err := h.Runner.Run(ctx, req)
	if err != nil {
		return Evidence{}, err
	}
	evidence := run.Evidence
	evidence.CaseID = c.ID
	evidence.Tool = c.Tool
	evidence = MergeEvidence(evidence, CollectWorktrailEvidence(projectWT))
	evidence = MergeEvidence(evidence, CollectCodexTranscriptEvidence(home, project, evidence.TranscriptPaths))
	evidence = RedactEvidence(evidence, work, home, userWT, project, outputDir)
	return evidence, nil
}

func (h Harness) setupWorktrail(ctx context.Context, req RunRequest) string {
	bin := strings.TrimSpace(h.WorktrailBin)
	if bin == "" {
		bin = "worktrail"
	}
	for _, args := range [][]string{
		{"init"},
		{"install", "codex"},
		{"doctor", "codex"},
	} {
		cmd := exec.CommandContext(ctx, bin, args...)
		cmd.Dir = req.ProjectRoot
		cmd.Env = runnerEnv(req)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Sprintf("worktrail %s failed: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return ""
}

func (h Harness) timeout() time.Duration {
	if h.Timeout > 0 {
		return h.Timeout
	}
	return defaultCaseTimeout
}

func (h Harness) createWorkDir(c Case, outputDir string) (string, error) {
	if strings.TrimSpace(outputDir) == "" {
		return os.MkdirTemp("", "worktrail-trigger-eval-*")
	}
	runsDir := filepath.Join(outputDir, "runs")
	if err := os.MkdirAll(runsDir, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(runsDir, safeRunPrefix(c.ID)+"-*")
}

func safeRunPrefix(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "case"
	}
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "case"
	}
	return b.String()
}

func runnerOutputDir(runner Runner) string {
	switch r := runner.(type) {
	case CodexRunner:
		return r.OutDir
	case *CodexRunner:
		return r.OutDir
	default:
		return ""
	}
}
