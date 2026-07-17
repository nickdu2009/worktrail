package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/store"
)

func hookEnv(t *testing.T) paths.Env {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	env := paths.Env{
		Home:        filepath.Join(t.TempDir(), "home"),
		UserRoot:    filepath.Join(t.TempDir(), "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	if err := store.InitProject(env); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestCodexStopCreatesRuntimeWithoutTakeover(t *testing.T) {
	env := hookEnv(t)
	fixture := `{"session_id":"codex-session-1","turn_id":"turn-1"}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "codex", "Stop", strings.NewReader(fixture), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("expected noop JSON, got %s", out.String())
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
	checkpoints, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "checkpoints", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range checkpoints {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), `"runtime_type": "takeover_note"`) {
			t.Fatalf("must not create takeover notes: %s", data)
		}
	}
	if strings.Contains(string(events), "candidate.promote") || strings.Contains(string(events), "candidate.create") {
		t.Fatalf("hook must not create or promote candidates:\n%s", events)
	}
}

func TestMalformedInputReturnsNoopExit0(t *testing.T) {
	env := hookEnv(t)
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader("{not json"), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("expected noop JSON, got %s", out.String())
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

func TestCursorShellDenyExit2(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","command":"echo x > .worktrail/architecture/blocked.md"}`
	var out bytes.Buffer
	err := Run(context.Background(), env, "cursor", "beforeShellExecution", strings.NewReader(payload), &out)
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err=%v want ExitCodeError 2", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["permission"] != "deny" {
		t.Fatalf("wire=%s", out.String())
	}
}

func TestCodexPreToolUseDenyExit2(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","tool_name":"Bash","tool_use_id":"tu1","tool_input":{"command":"echo x > .worktrail/decisions/blocked.md"}}`
	var out bytes.Buffer
	err := Run(context.Background(), env, "codex", "PreToolUse", strings.NewReader(payload), &out)
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err=%v want ExitCodeError 2", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["permissionDecision"] != "deny" || wire["block"] != true {
		t.Fatalf("wire=%s", out.String())
	}
}

func TestCodexPermissionRequestDenyExit0(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","tool_name":"Bash","tool_input":{"command":"rm .worktrail/rules/coding-rules.md"}}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "codex", "PermissionRequest", strings.NewReader(payload), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	specific, _ := wire["hookSpecificOutput"].(map[string]any)
	if specific == nil {
		t.Fatalf("missing hookSpecificOutput wire=%s", out.String())
	}
	decision, _ := specific["decision"].(map[string]any)
	if specific["hookEventName"] != "PermissionRequest" || decision["behavior"] != "deny" {
		t.Fatalf("wire=%s", out.String())
	}
}

func TestCursorAfterFileEditAuditOnly(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","file_path":".worktrail/architecture/x.md"}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "afterFileEdit", strings.NewReader(payload), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), `"permission":"deny"`) {
		t.Fatalf("afterFileEdit must not deny: %s", out.String())
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), "file_edit_audit") {
		t.Fatalf("expected audit log:\n%s", events)
	}
}

func TestControlledWorktrailCLIAllowed(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","command":"worktrail draft create --type architecture --target architecture/x.md"}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "beforeShellExecution", strings.NewReader(payload), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["permission"] != "allow" {
		t.Fatalf("wire=%s", out.String())
	}
}

func TestControlledWorktrailCLIRedirectToFormalDenied(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","command":"worktrail draft create --type architecture --target architecture/x.md > .worktrail/architecture/evil.md"}`
	var out bytes.Buffer
	err := Run(context.Background(), env, "cursor", "beforeShellExecution", strings.NewReader(payload), &out)
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err=%v want ExitCodeError 2", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["permission"] != "deny" {
		t.Fatalf("controlled CLI redirect to formal path must deny, wire=%s", out.String())
	}
}

func TestControlledWorktrailCLITeeToFormalDenied(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","command":"worktrail draft create --type architecture --target architecture/x.md | tee .worktrail/architecture/evil.md"}`
	var out bytes.Buffer
	err := Run(context.Background(), env, "cursor", "beforeShellExecution", strings.NewReader(payload), &out)
	var exitErr *ExitCodeError
	if !errors.As(err, &exitErr) || exitErr.Code != 2 {
		t.Fatalf("err=%v want ExitCodeError 2", err)
	}
	if !strings.Contains(out.String(), `"permission":"deny"`) {
		t.Fatalf("controlled CLI tee to formal path must deny, wire=%s", out.String())
	}
}

func TestControlledWorktrailCLITeeAppendToFormalDenied(t *testing.T) {
	env := hookEnv(t)
	for _, command := range []string{
		"worktrail draft create --type architecture --target architecture/x.md | tee -a .worktrail/architecture/evil.md",
		"worktrail draft create --type architecture --target architecture/x.md | tee --append .worktrail/architecture/evil.md",
		"worktrail draft create --type architecture --target architecture/x.md | tee --append -- .worktrail/architecture/evil.md",
	} {
		t.Run(command, func(t *testing.T) {
			payload := `{"session_id":"s1","command":` + strconv.Quote(command) + `}`
			var out bytes.Buffer
			err := Run(context.Background(), env, "cursor", "beforeShellExecution", strings.NewReader(payload), &out)
			var exitErr *ExitCodeError
			if !errors.As(err, &exitErr) || exitErr.Code != 2 {
				t.Fatalf("err=%v want ExitCodeError 2", err)
			}
			if !strings.Contains(out.String(), `"permission":"deny"`) {
				t.Fatalf("controlled CLI tee append to formal path must deny, wire=%s", out.String())
			}
		})
	}
}

func TestUniqueTaskContextInjection(t *testing.T) {
	env := hookEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-hooks",
		Title:  "Hook context",
		Body:   "# State Capsule: Hook context\n\n## Current Goal\nInject me\n\n## Next Step\nContinue\n",
	}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "sessionStart", strings.NewReader(`{"session_id":"sess-a"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	ctx, _ := wire["additional_context"].(string)
	if !strings.Contains(ctx, "task-hooks") || !strings.Contains(ctx, "Inject me") {
		t.Fatalf("context missing fields: %s", ctx)
	}
	if strings.Contains(ctx, env.ProjectRoot) {
		t.Fatalf("context leaked absolute path: %s", ctx)
	}
	if len(ctx) > maxContextBytes {
		t.Fatalf("context too large: %d", len(ctx))
	}
}

func TestAmbiguousTasksSkipContext(t *testing.T) {
	env := hookEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "t1", Title: "One"}); err != nil {
		t.Fatal(err)
	}
	if _, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "t2", Title: "Two"}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "sessionStart", strings.NewReader(`{"session_id":"sess-b"}`), &out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.TrimSpace(out.String()) != "{}" {
		t.Fatalf("expected empty context wire, got %s", out.String())
	}
}

func TestHookStopDoesNotOverwriteExplicitActiveState(t *testing.T) {
	env := hookEnv(t)
	started, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-keep",
		Title:  "Keep me",
		Body:   "# State Capsule: Keep me\n\n## Current Goal\nStay\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := wtstate.LatestExplicit(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader(`{"session_id":"s","generation_id":"g1"}`), &out); err != nil {
		t.Fatal(err)
	}
	after, err := wtstate.LatestExplicit(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if before.State.ID != after.State.ID || after.State.ID != started.State.ID {
		t.Fatalf("explicit state changed")
	}
}

func TestTerminalEffectIdempotent(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"same","generation_id":"gen-1"}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), env, "cursor", "stop", strings.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}
	sessions, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "sessions", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one terminal runtime after duplicate stop, got %d", len(sessions))
	}
}

func TestComplexShellAuditOnly(t *testing.T) {
	env := hookEnv(t)
	payload := `{"session_id":"s1","command":"echo hi | tee .worktrail/architecture/x.md"}`
	var out bytes.Buffer
	if err := Run(context.Background(), env, "cursor", "beforeShellExecution", strings.NewReader(payload), &out); err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(out.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire["permission"] != "allow" {
		t.Fatalf("complex shell should audit-only allow, got %s", out.String())
	}
}

func TestFixtureGoldenMatrix(t *testing.T) {
	env := hookEnv(t)
	cases := []struct {
		host       string
		event      string
		file       string
		wantExit2  bool
		wantSubstr string
		allowEmpty bool
	}{
		{"cursor", "sessionStart", "cursor-sessionstart.json", false, "", true},
		{"cursor", "beforeShellExecution", "cursor-before-shell-deny.json", true, `"permission":"deny"`, false},
		{"cursor", "beforeMCPExecution", "cursor-before-mcp-deny.json", true, `"permission":"deny"`, false},
		{"cursor", "afterShellExecution", "cursor-after-shell.json", false, "{}", false},
		{"cursor", "afterMCPExecution", "cursor-after-mcp.json", false, "{}", false},
		{"cursor", "afterFileEdit", "cursor-after-file-edit.json", false, "{}", false},
		{"cursor", "preCompact", "cursor-precompact.json", false, "{}", false},
		{"cursor", "stop", "cursor-stop.json", false, "{}", false},
		{"cursor", "sessionEnd", "cursor-sessionend.json", false, "{}", false},
		{"codex", "SessionStart", "codex-sessionstart.json", false, "", true},
		{"codex", "UserPromptSubmit", "codex-userpromptsubmit.json", false, "", true},
		{"codex", "PreToolUse", "codex-pretooluse-deny.json", true, `"permissionDecision":"deny"`, false},
		{"codex", "PermissionRequest", "codex-permissionrequest-deny.json", false, `"behavior":"deny"`, false},
		{"codex", "PostToolUse", "codex-posttooluse.json", false, "{}", false},
		{"codex", "PreCompact", "codex-precompact.json", false, "{}", false},
		{"codex", "PostCompact", "codex-postcompact.json", false, "{}", false},
		{"codex", "SubagentStop", "codex-subagentstop.json", false, "{}", false},
		{"codex", "Stop", "codex_stop.json", false, "{}", false},
	}
	for _, tc := range cases {
		t.Run(tc.host+"/"+tc.event, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			err = Run(context.Background(), env, tc.host, tc.event, bytes.NewReader(raw), &out)
			if tc.wantExit2 {
				var exitErr *ExitCodeError
				if !errors.As(err, &exitErr) || exitErr.Code != 2 {
					t.Fatalf("err=%v want exit 2", err)
				}
			} else if err != nil {
				t.Fatalf("err=%v", err)
			}
			got := strings.TrimSpace(out.String())
			if tc.allowEmpty {
				if got != "{}" && got != "" {
					// Without unique task, context injection must stay empty.
					if !strings.Contains(got, "additional_context") && !strings.Contains(got, "additionalContext") {
						return
					}
					t.Fatalf("expected empty context wire without unique task, got %s", got)
				}
				return
			}
			if tc.wantSubstr != "" && !strings.Contains(out.String(), tc.wantSubstr) {
				t.Fatalf("output=%s want substr %s", out.String(), tc.wantSubstr)
			}
		})
	}
}

func TestSessionStartUniqueTaskGolden(t *testing.T) {
	env := hookEnv(t)
	if _, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "golden-task",
		Title:  "Golden",
		Body:   "# State Capsule: Golden\n\n## Current Goal\nUnique task context\n\n## Next Step\nContinue\n",
	}); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		host  string
		event string
		file  string
		key   string
	}{
		{"cursor", "sessionStart", "cursor-sessionstart.json", "additional_context"},
		{"codex", "SessionStart", "codex-sessionstart.json", "additionalContext"},
	} {
		t.Run(tc.host, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "fixtures", "hooks", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			var out bytes.Buffer
			if err := Run(context.Background(), env, tc.host, tc.event, bytes.NewReader(raw), &out); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.key) || !strings.Contains(out.String(), "golden-task") {
				t.Fatalf("unique-task golden missing context: %s", out.String())
			}
			if !strings.Contains(out.String(), "Unique task context") {
				t.Fatalf("unique-task golden missing goal: %s", out.String())
			}
		})
	}
}
