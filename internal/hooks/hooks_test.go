package hooks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func hookEnv(t *testing.T) paths.Env {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	return paths.Env{
		Home:        filepath.Join(t.TempDir(), "home"),
		UserRoot:    filepath.Join(t.TempDir(), "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func TestCodexStopCreatesStateCandidateAndLogsWithoutPromotion(t *testing.T) {
	env := hookEnv(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", "codex_stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "codex", "stop", bytes.NewReader(fixture), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), `"candidate"`) {
		t.Fatalf("expected candidate in output: %s", out.String())
	}
	for _, path := range []string{
		filepath.Join(env.ProjectWT, "state", "active", "latest.md"),
		filepath.Join(env.ProjectWT, "logs", "events.jsonl"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("expected %s: %v", path, err)
		}
	}
	candidates, err := filepath.Glob(filepath.Join(env.ProjectWT, "candidates", "project", "cand_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(candidates))
	}
	data, err := os.ReadFile(candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"status": "pending"`) {
		t.Fatalf("candidate was not left pending:\n%s", data)
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(events), "candidate.promote") {
		t.Fatalf("hook must not promote:\n%s", events)
	}
}

func TestMalformedInputStillLogsHookRun(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	if err := Run(context.Background(), env, "claude", "post-tool-use", strings.NewReader("{not json"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "invalid json") {
		t.Fatalf("expected parse warning: %s", out.String())
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), "hook.run") {
		t.Fatalf("expected hook.run log:\n%s", events)
	}
}

func TestClaudePreCompactCreatesCheckpoint(t *testing.T) {
	env := hookEnv(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", "claude_session_end.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "claude", "pre-compact", bytes.NewReader(fixture), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	checkpoints, err := filepath.Glob(filepath.Join(env.ProjectWT, "state", "checkpoints", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 {
		t.Fatalf("expected one checkpoint, got %d", len(checkpoints))
	}
}
