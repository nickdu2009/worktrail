package triggereval

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	envCodexCmd  = "WORKTRAIL_TRIGGER_EVAL_CODEX_CMD"
	envCodexHome = "WORKTRAIL_TRIGGER_EVAL_CODEX_HOME"
	envOutDir    = "WORKTRAIL_TRIGGER_EVAL_OUT"
	envTimeout   = "WORKTRAIL_TRIGGER_EVAL_TIMEOUT"
)

type Runner interface {
	Run(context.Context, RunRequest) (RunResult, error)
}

type RunRequest struct {
	Case                 Case
	ProjectRoot          string
	Home                 string
	WorktrailHome        string
	WorktrailProjectRoot string
	OutputDir            string
	PromptFile           string
	Timeout              time.Duration
}

type RunResult struct {
	Evidence Evidence
	ExitCode int
	TimedOut bool
}

type FakeRunner struct {
	Results map[string]RunResult
}

func (r FakeRunner) Run(_ context.Context, req RunRequest) (RunResult, error) {
	if r.Results == nil {
		return RunResult{Evidence: Evidence{CaseID: req.Case.ID, Tool: req.Case.Tool}}, nil
	}
	result, ok := r.Results[req.Case.ID]
	if !ok {
		return RunResult{Evidence: Evidence{CaseID: req.Case.ID, Tool: req.Case.Tool}}, nil
	}
	result.Evidence.CaseID = req.Case.ID
	result.Evidence.Tool = req.Case.Tool
	return result, nil
}

type CodexRunner struct {
	Argv       []string
	CodexHome  string
	Timeout    time.Duration
	OutDir     string
	SkipReason string
}

func NewCodexRunnerFromEnv() CodexRunner {
	cmdText := strings.TrimSpace(os.Getenv(envCodexCmd))
	if cmdText == "" {
		return CodexRunner{SkipReason: envCodexCmd + " is not configured"}
	}
	argv, err := SplitCommandLine(cmdText)
	if err != nil {
		return CodexRunner{SkipReason: fmt.Sprintf("parse %s: %v", envCodexCmd, err)}
	}
	timeout, err := parseTimeout(os.Getenv(envTimeout))
	if err != nil {
		return CodexRunner{SkipReason: fmt.Sprintf("parse %s: %v", envTimeout, err)}
	}
	return CodexRunner{
		Argv:      argv,
		CodexHome: strings.TrimSpace(os.Getenv(envCodexHome)),
		Timeout:   timeout,
		OutDir:    strings.TrimSpace(os.Getenv(envOutDir)),
	}
}

func (r CodexRunner) Run(ctx context.Context, req RunRequest) (RunResult, error) {
	evidence := Evidence{CaseID: req.Case.ID, Tool: req.Case.Tool}
	if r.SkipReason != "" {
		evidence.SkipReason = r.SkipReason
		return RunResult{Evidence: evidence}, nil
	}
	if len(r.Argv) == 0 {
		evidence.SkipReason = "codex argv is empty"
		return RunResult{Evidence: evidence}, nil
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = req.Timeout
	}
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	argv := expandArgv(r.Argv, req)
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = req.ProjectRoot
	cmd.Env = runnerEnv(req)
	if strings.TrimSpace(r.CodexHome) != "" {
		cmd.Env = append(cmd.Env, "CODEX_HOME="+r.CodexHome)
	}
	if prompt, err := os.Open(req.PromptFile); err == nil {
		defer prompt.Close()
		cmd.Stdin = prompt
	}
	stdout, stderr := &strings.Builder{}, &strings.Builder{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	err := cmd.Run()
	evidence.RunnerStdout = stdout.String()
	evidence.RunnerStderr = stderr.String()
	evidence = MergeEvidence(evidence, CollectCodexJSONLTextEvidence(evidence.RunnerStdout))
	result := RunResult{Evidence: evidence}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.Evidence.SkipReason = "codex runner timed out"
		return result, nil
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			result.Evidence.SkipReason = fmt.Sprintf("codex runner exited with code %d: %s", result.ExitCode, summarizeRunnerFailure(result.Evidence.RunnerStdout, result.Evidence.RunnerStderr))
			return result, nil
		}
		result.Evidence.SkipReason = fmt.Sprintf("codex runner failed: %v", err)
		return result, nil
	}
	return result, nil
}

func SplitCommandLine(input string) ([]string, error) {
	var args []string
	var b strings.Builder
	var quote rune
	escaped := false
	for _, r := range input {
		switch {
		case escaped:
			b.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				b.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			if b.Len() > 0 {
				args = append(args, b.String())
				b.Reset()
			}
		default:
			b.WriteRune(r)
		}
	}
	if escaped {
		return nil, errors.New("trailing escape")
	}
	if quote != 0 {
		return nil, errors.New("unterminated quote")
	}
	if b.Len() > 0 {
		args = append(args, b.String())
	}
	if len(args) == 0 {
		return nil, errors.New("empty command")
	}
	return args, nil
}

func parseTimeout(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(value); err == nil {
		return d, nil
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return 0, err
	}
	return time.Duration(seconds) * time.Second, nil
}

func expandArgv(argv []string, req RunRequest) []string {
	values := map[string]string{
		"PROJECT_ROOT":           req.ProjectRoot,
		"PROMPT_FILE":            req.PromptFile,
		"HOME":                   req.Home,
		"WORKTRAIL_HOME":         req.WorktrailHome,
		"WORKTRAIL_PROJECT_ROOT": req.WorktrailProjectRoot,
		"OUTPUT_DIR":             req.OutputDir,
	}
	out := make([]string, 0, len(argv))
	for _, arg := range argv {
		out = append(out, os.Expand(arg, func(key string) string {
			if value, ok := values[key]; ok {
				return value
			}
			return ""
		}))
	}
	return out
}

func runnerEnv(req RunRequest) []string {
	env := []string{
		"HOME=" + req.Home,
		"WORKTRAIL_HOME=" + req.WorktrailHome,
		"WORKTRAIL_PROJECT_ROOT=" + req.WorktrailProjectRoot,
		"PROJECT_ROOT=" + req.ProjectRoot,
		"PROMPT_FILE=" + req.PromptFile,
		"OUTPUT_DIR=" + req.OutputDir,
	}
	if path := os.Getenv("PATH"); path != "" {
		env = append(env, "PATH="+path)
	}
	return env
}

func summarizeText(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return "no stderr"
	}
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}

func summarizeRunnerFailure(stdout, stderr string) string {
	if strings.TrimSpace(stderr) != "" {
		return summarizeText(stderr, 240)
	}
	if strings.TrimSpace(stdout) != "" {
		return summarizeText(stdout, 240)
	}
	return "no stdout or stderr"
}
