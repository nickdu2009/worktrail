package triggereval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/store"
)

func TestCollectWorktrailEvidence(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".worktrail")
	candidateDir := filepath.Join(root, "candidates", "project")
	if err := os.MkdirAll(candidateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	candidateData, err := store.RenderMarkdown(map[string]any{
		"schema":         "worktrail.candidate.v1",
		"id":             "handoff-1",
		"candidate_type": "handoff",
		"status":         "pending",
		"target_path":    "handoffs/handoff-1.md",
	}, "# Handoff\n")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(candidateDir, "handoff-1.md"), candidateData, 0o644); err != nil {
		t.Fatal(err)
	}
	logDir := filepath.Join(root, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "events.jsonl"), []byte(`{"event":"candidate.create","id":"handoff-1","actor":"test","data":{"target_path":"handoffs/handoff-1.md"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	evidence := CollectWorktrailEvidence(root)
	if !recordPatternObserved("candidate_type=handoff", evidence.WorktrailArtifacts) {
		t.Fatalf("missing candidate artifact: %#v", evidence.WorktrailArtifacts)
	}
	if !recordPatternObserved("candidate_status=pending", evidence.WorktrailArtifacts) {
		t.Fatalf("missing candidate status alias: %#v", evidence.WorktrailArtifacts)
	}
	if !recordPatternObserved("event_type=candidate.create", evidence.WorktrailLogs) {
		t.Fatalf("missing event log: %#v", evidence.WorktrailLogs)
	}
}

func TestRedactEvidence(t *testing.T) {
	root := filepath.Join(t.TempDir(), "private")
	evidence := RedactEvidence(Evidence{
		RunnerStdout: "wrote " + root,
	}, root)
	if evidence.RunnerStdout == "" || evidence.RunnerStdout == "wrote "+root {
		t.Fatalf("stdout was not redacted: %q", evidence.RunnerStdout)
	}
}

func TestCollectCodexTranscriptEvidence(t *testing.T) {
	home := t.TempDir()
	project := filepath.Join(t.TempDir(), "project")
	session := filepath.Join(home, ".codex", "sessions", "2026", "05", "16", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(session), 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join([]string{
		`{"timestamp":"2026-05-16T00:00:00Z","type":"session_meta","payload":{"id":"s1","cwd":"` + filepath.ToSlash(project) + `"}}`,
		`{"timestamp":"2026-05-16T00:00:01Z","type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"I will run the handoff command."}]}}`,
		`{"timestamp":"2026-05-16T00:00:02Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":"{\"command\":\"worktrail handoff summary\"}"}}`,
		`{"timestamp":"2026-05-16T00:00:03Z","type":"response_item","payload":{"type":"function_call","name":"shell","arguments":{"argv":["worktrail","promote","abc"]}}}`,
	}, "\n")
	if err := os.WriteFile(session, []byte(body+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	evidence := CollectCodexTranscriptEvidence(home, project, nil)
	if !commandObserved("worktrail handoff", evidence.CommandsObserved) {
		t.Fatalf("missing transcript command: %#v", evidence.CommandsObserved)
	}
	if !commandObserved("worktrail promote", evidence.MutatingCommandsObserved) {
		t.Fatalf("missing mutating command: %#v", evidence.MutatingCommandsObserved)
	}
	if len(evidence.AssistantMessages) != 1 {
		t.Fatalf("assistant messages = %#v", evidence.AssistantMessages)
	}
}

func TestScanCodexTranscriptCommandsIgnoresAssistantProse(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"message","role":"assistant","content":"You can run worktrail handoff later."}`,
		`{"type":"response_item","payload":{"type":"function_call","arguments":{"cmd":"worktrail context task"}}}`,
	}, "\n")
	commands, _ := scanCodexTranscriptCommands(strings.NewReader(raw))
	if len(commands) != 1 || !commandObserved("worktrail context", commands) {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestCollectCodexJSONLTextEvidence(t *testing.T) {
	raw := strings.Join([]string{
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"Running context."}]}}`,
		`{"type":"response_item","payload":{"type":"function_call","arguments":{"command":"worktrail context task"}}}`,
	}, "\n")
	evidence := CollectCodexJSONLTextEvidence(raw)
	if len(evidence.AssistantMessages) != 1 {
		t.Fatalf("assistant messages = %#v", evidence.AssistantMessages)
	}
	if !commandObserved("worktrail context", evidence.CommandsObserved) {
		t.Fatalf("commands = %#v", evidence.CommandsObserved)
	}
}
