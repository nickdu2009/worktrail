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

func TestEventOnlyStopDoesNotOverwriteLatestState(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader(`{"status":"completed","generation_id":"runtime-only"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("event-only hook should not write latest state, err=%v", err)
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), "hook.run") {
		t.Fatalf("event-only hook should still log hook.run:\n%s", events)
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
	body, err := os.ReadFile(checkpoints[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "## Recovery Summary") || !strings.Contains(string(body), "Prepare compact-safe Worktrail state") {
		t.Fatalf("checkpoint missing recovery summary:\n%s", body)
	}
}

func TestPreCompactWithoutContextWritesUnavailableRecoverySummary(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	if err := Run(context.Background(), env, "claude", "pre-compact", strings.NewReader(`{"event":"pre-compact"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	checkpoints, err := filepath.Glob(filepath.Join(env.ProjectWT, "state", "checkpoints", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 {
		t.Fatalf("expected one checkpoint, got %d", len(checkpoints))
	}
	body, err := os.ReadFile(checkpoints[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "Recovery context was unavailable") {
		t.Fatalf("checkpoint should explain unavailable recovery context:\n%s", body)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("context-free compact should not write latest state, err=%v", err)
	}
}

func TestHookStateCanUseBoundedTranscriptContext(t *testing.T) {
	env := hookEnv(t)
	transcriptPath := filepath.Join(t.TempDir(), "codex.jsonl")
	body := strings.Join([]string{
		`{"timestamp":"2026-05-14T00:00:00Z","type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Implement bounded recovery context."}]}}`,
		`{"timestamp":"2026-05-14T00:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Ran go test ./internal/hooks and it passed."}]}}`,
	}, "\n")
	if err := os.WriteFile(transcriptPath, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "codex", "pre-compact", strings.NewReader(`{"transcript_path":"`+filepath.ToSlash(transcriptPath)+`"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	state, err := os.ReadFile(filepath.Join(env.ProjectWT, "state", "active", "latest.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(state)
	if !strings.Contains(text, "Implement bounded recovery context.") || !strings.Contains(text, "Ran go test ./internal/hooks") {
		t.Fatalf("state missing transcript context:\n%s", text)
	}
}

func TestCursorStopSanitizesDurablePayloadAndRecordsObservedTranscript(t *testing.T) {
	env := hookEnv(t)
	transcript := filepath.Join(t.TempDir(), "cursor-transcript.jsonl")
	if err := os.WriteFile(transcript, []byte(`{"role":"user","content":"hello"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	payload := `{
  "conversation_id": "cursor-conversation-secret",
  "generation_id": "cursor-generation-secret",
  "user_email": "person@example.com",
  "workspace_roots": ["` + filepath.ToSlash(env.ProjectRoot) + `"],
  "transcript_path": "` + filepath.ToSlash(transcript) + `",
  "status": "completed"
}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader(payload), &out); err != nil {
		t.Fatalf("Run cursor hook: %v", err)
	}
	if !strings.Contains(out.String(), "cursor transcript observed") {
		t.Fatalf("expected observed transcript warning in output: %s", out.String())
	}
	observed, err := filepath.Glob(filepath.Join(env.ProjectWT, "raw", "cursor", "observed-*.metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(observed) != 1 {
		t.Fatalf("observed registry files = %d, want 1", len(observed))
	}
	registry, err := os.ReadFile(observed[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(registry), filepath.ToSlash(transcript)) {
		t.Fatalf("registry should keep private transcript path for local import:\n%s", registry)
	}
	candidates, err := filepath.Glob(filepath.Join(env.ProjectWT, "candidates", "project", "cand_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected one candidate, got %d", len(candidates))
	}
	candidate, err := os.ReadFile(candidates[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(candidate)
	for _, forbidden := range []string{
		"person@example.com",
		filepath.ToSlash(transcript),
		"cursor-conversation-secret",
		"cursor-generation-secret",
	} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("durable candidate leaked %q:\n%s", forbidden, text)
		}
	}
	if !strings.Contains(text, filepath.Base(transcript)) {
		t.Fatalf("durable candidate should keep transcript basename only:\n%s", text)
	}
}
