package hooks

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
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

func TestCodexStopCreatesRuntimeSessionAndTakeoverWithoutPromotion(t *testing.T) {
	env := hookEnv(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", "codex_stop.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "codex", "Stop", bytes.NewReader(fixture), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), `"runtime"`) {
		t.Fatalf("expected runtime record in output: %s", out.String())
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("hook stop must not write primary active state, err=%v", err)
	}
	checkpoints, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "checkpoints", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checkpoints) != 1 {
		t.Fatalf("expected one takeover runtime record, got %d", len(checkpoints))
	}
	data, err := os.ReadFile(checkpoints[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"runtime_type": "takeover_note"`) {
		t.Fatalf("runtime takeover note metadata unexpected:\n%s", data)
	}
	sessions, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "sessions", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one runtime session, got %d", len(sessions))
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(events), "candidate.promote") || strings.Contains(string(events), "candidate.create") {
		t.Fatalf("hook must not create or promote candidates:\n%s", events)
	}
}

func TestMalformedInputStillLogsHookRun(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	if err := Run(context.Background(), env, "claude", "Stop", strings.NewReader("{not json"), &out); err != nil {
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

func TestLegacyEventShapesAreRejected(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	err := Run(context.Background(), env, "claude", "pre-compact", strings.NewReader(`{"task":"legacy"}`), &out)
	if err == nil {
		t.Fatal("expected legacy event shape to be rejected")
	}
	if !strings.Contains(err.Error(), "unsupported claude hook event") {
		t.Fatalf("unexpected error: %v", err)
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
	if strings.Contains(out.String(), `"runtime"`) {
		t.Fatalf("event-only hook should not create a runtime record: %s", out.String())
	}
	candidates, err := filepath.Glob(filepath.Join(env.ProjectWT, "candidates", "project", "cand_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("event-only hook should not write candidates, got %d", len(candidates))
	}
}

func TestStopWithCommandsWritesRuntimeSessionNotPrimaryState(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	payload := `{"task":"Investigate candidate inbox noise","commands":["go test ./internal/hooks"]}`
	if err := Run(context.Background(), env, "codex", "Stop", strings.NewReader(payload), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("hook stop must not write primary active state, err=%v", err)
	}
	sessions, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "sessions", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one runtime session, got %d", len(sessions))
	}
	candidates, err := filepath.Glob(filepath.Join(env.ProjectWT, "candidates", "project", "cand_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("commands-only stop should not write candidates, got %d", len(candidates))
	}
}

func TestClaudePreCompactCreatesRuntimeCheckpoint(t *testing.T) {
	env := hookEnv(t)
	fixture, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", "claude_session_end.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "claude", "PreCompact", bytes.NewReader(fixture), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	checkpoints, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "checkpoints", "*.md"))
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
	if err := Run(context.Background(), env, "claude", "PreCompact", strings.NewReader(`{"event":"pre-compact"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	checkpoints, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "checkpoints", "*.md"))
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

func TestHookRuntimeCanUseBoundedTranscriptContext(t *testing.T) {
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
	if err := Run(context.Background(), env, "codex", "PreCompact", strings.NewReader(`{"transcript_path":"`+filepath.ToSlash(transcriptPath)+`"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	sessions, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "sessions", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one runtime session, got %d", len(sessions))
	}
	state, err := os.ReadFile(sessions[0])
	if err != nil {
		t.Fatal(err)
	}
	text := string(state)
	if !strings.Contains(text, "Implement bounded recovery context.") || !strings.Contains(text, "Ran go test ./internal/hooks") {
		t.Fatalf("runtime session missing transcript context:\n%s", text)
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
	sessions, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "sessions", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one runtime session from transcript context, got %d", len(sessions))
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
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "active", "latest.md")); !os.IsNotExist(err) {
		t.Fatalf("transcript-only stop must not write primary active state, err=%v", err)
	}
	candidates, err := filepath.Glob(filepath.Join(env.ProjectWT, "candidates", "project", "cand_*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 0 {
		t.Fatalf("expected no candidate for transcript-only signal, got %d", len(candidates))
	}
}

func TestHookStopDoesNotOverwriteExplicitActiveState(t *testing.T) {
	env := hookEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:      "project",
		Title:      "Explicit CLI task",
		Body:       "# State Capsule: Explicit CLI task\n\n## Next Step\nKeep explicit state intact.\n",
		SourceTool: "worktrail",
		Actor:      "cli:state-start",
	}); err != nil {
		t.Fatalf("Start explicit state: %v", err)
	}
	before, err := wtstate.LatestExplicit(env, "project")
	if err != nil {
		t.Fatalf("LatestExplicit: %v", err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader(`{"task":"Hook chatter only","status":"completed"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	after, err := wtstate.LatestExplicit(env, "project")
	if err != nil {
		t.Fatalf("LatestExplicit after hook: %v", err)
	}
	if after.State.ID != before.State.ID || !strings.Contains(after.Body, "Keep explicit state intact") {
		t.Fatalf("hook stop overwrote explicit active state:\n%s", after.Body)
	}
}
