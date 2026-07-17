package hooks

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	"github.com/nickdu2009/worktrail/internal/store"
)

func Main(ctx context.Context, env paths.Env, args []string, in io.Reader, out io.Writer) error {
	if len(args) != 2 {
		return fmt.Errorf("usage: hook <codex|claude|cursor> <event>")
	}
	if args[0] != "codex" && args[0] != "claude" && args[0] != "cursor" {
		return fmt.Errorf("unknown hook tool %q", args[0])
	}
	if strings.TrimSpace(args[1]) == "" {
		return fmt.Errorf("hook event is required")
	}
	if out == nil {
		out = io.Discard
	}
	if in == nil {
		in = strings.NewReader("")
	}
	if _, ok := os.LookupEnv("WORKTRAIL_HOOK_NOOP"); ok {
		return nil
	}
	return Run(ctx, env, args[0], args[1], in, out)
}

func Run(ctx context.Context, env paths.Env, tool, event string, in io.Reader, out io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if err := validateEvent(tool, event); err != nil {
		// Unknown events: fail closed for CLI misuse (non-hook invocation).
		return err
	}
	raw, parseErr := readJSONObject(in)
	decision, encode := processEvent(env, tool, event, raw, parseErr)
	if err := encodeJSON(out, encode); err != nil {
		return err
	}
	if decision.ExitCode == 2 {
		return &ExitCodeError{Code: 2}
	}
	return nil
}

func processEvent(env paths.Env, tool, event string, raw map[string]any, parseErr error) (HookDecision, any) {
	if parseErr != nil {
		return HookDecision{Kind: DecisionNoop, ReasonCode: "payload_parse_error"}, noopWire(tool)
	}
	projectID := readProjectID(env)
	normalized := normalizeEvent(env, tool, event, raw, projectID)
	_ = auditHook(env, normalized, "received", "", nil)

	decision := HookDecision{Kind: DecisionNoop, ReasonCode: "ok"}
	switch tool {
	case "cursor":
		decision = handleCursor(env, normalized)
		return decision, encodeCursor(normalized.NativeEvent, decision)
	case "codex":
		decision = handleCodex(env, normalized)
		return decision, encodeCodex(normalized.NativeEvent, decision)
	default: // claude: privacy-safe no-op lifecycle only
		decision = handleClaude(env, normalized)
		return decision, noopWire(tool)
	}
}

func handleCursor(env paths.Env, ev NormalizedHookEvent) HookDecision {
	switch ev.NativeEvent {
	case "sessionStart":
		return injectSessionContext(env, ev, true)
	case "beforeShellExecution":
		return guardDecision(env, ev, evaluateShellGuard(env, ev.Command), true)
	case "beforeMCPExecution":
		input := mapFromAny(ev.Raw["tool_input"])
		if len(input) == 0 {
			input = mapFromAny(ev.Raw["input"])
		}
		return guardDecision(env, ev, evaluateMCPGuard(env, ev.ToolName, input), true)
	case "afterShellExecution", "afterMCPExecution":
		_ = auditHook(env, ev, "validation_audit", "after_tool_audit", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "after_tool_audit"}
	case "afterFileEdit":
		result := evaluateFileEditAudit(env, ev.FilePath)
		_ = auditHook(env, ev, "file_edit_audit", result.ReasonCode, map[string]any{"path": result.Path})
		return HookDecision{Kind: DecisionNoop, ReasonCode: result.ReasonCode}
	case "preCompact":
		return runCheckpointEffect(env, ev)
	case "stop":
		return runTerminalEffect(env, ev, ev.GenerationID)
	case "sessionEnd":
		_ = deleteBinding(env.ProjectWT, "cursor", ev.SessionID)
		_ = auditHook(env, ev, "binding_clear", "session_end", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "session_end"}
	default:
		return HookDecision{Kind: DecisionNoop, ReasonCode: "unhandled_event"}
	}
}

func handleCodex(env paths.Env, ev NormalizedHookEvent) HookDecision {
	switch ev.NativeEvent {
	case "SessionStart":
		clear := strings.EqualFold(firstPayloadString(ev.Raw, "reason", "source"), "clear")
		if clear {
			_ = deleteBinding(env.ProjectWT, "codex", ev.SessionID)
		}
		return injectSessionContext(env, ev, true)
	case "UserPromptSubmit":
		return injectSessionContext(env, ev, false)
	case "PreToolUse":
		input := mapFromAny(ev.Raw["tool_input"])
		return guardDecision(env, ev, evaluateCodexToolGuard(env, ev.ToolName, input), true)
	case "PermissionRequest":
		input := mapFromAny(ev.Raw["tool_input"])
		result := evaluateCodexToolGuard(env, ev.ToolName, input)
		if result.Deny {
			_ = auditHook(env, ev, "permission_deny", result.ReasonCode, map[string]any{"path": result.Path})
			return HookDecision{
				Kind:         DecisionDeny,
				ReasonCode:   result.ReasonCode,
				UserMessage:  result.Message,
				AgentMessage: result.Message,
				ExitCode:     0, // PermissionRequest deny uses decision wire
			}
		}
		_ = auditHook(env, ev, "permission_defer", result.ReasonCode, nil)
		return HookDecision{Kind: DecisionDefer, ReasonCode: result.ReasonCode}
	case "PostToolUse":
		_ = auditHook(env, ev, "validation_audit", "post_tool_audit", nil)
		key := strings.Join([]string{"codex", ev.ToolUseID, "validation"}, "+")
		_, _ = applyEffectOnce(env.ProjectWT, key, func() error { return nil })
		return HookDecision{Kind: DecisionNoop, ReasonCode: "post_tool_audit"}
	case "PreCompact":
		return runCheckpointEffect(env, ev)
	case "PostCompact":
		if binding, err := loadBinding(env.ProjectWT, "codex", ev.SessionID); err == nil && binding != nil {
			binding.NeedsContextRefresh = true
			binding.LastInjectedRevision = ""
			_ = saveBinding(env.ProjectWT, *binding)
		}
		_ = auditHook(env, ev, "post_compact", "context_refresh", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "post_compact"}
	case "SubagentStop":
		_ = auditHook(env, ev, "subagent_stop", "redacted_summary", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "subagent_stop"}
	case "Stop":
		return runTerminalEffect(env, ev, ev.TurnID)
	default:
		return HookDecision{Kind: DecisionNoop, ReasonCode: "unhandled_event"}
	}
}

func handleClaude(env paths.Env, ev NormalizedHookEvent) HookDecision {
	switch ev.NativeEvent {
	case "PreCompact", "PostCompact":
		return runCheckpointEffect(env, ev)
	case "Stop", "SessionEnd":
		return runTerminalEffect(env, ev, ev.SessionID)
	default:
		_ = auditHook(env, ev, "claude_noop", "ok", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "ok"}
	}
}

func injectSessionContext(env paths.Env, ev NormalizedHookEvent, force bool) HookDecision {
	task, err := resolveUniqueExplicitTask(env)
	if err != nil {
		_ = auditHook(env, ev, "context_error", "task_resolve_failed", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "task_resolve_failed"}
	}
	binding, err := refreshBinding(env, ev.Host, ev.SessionID, task, false)
	if err != nil {
		_ = auditHook(env, ev, "context_error", "binding_failed", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "binding_failed"}
	}
	if task == nil {
		_ = auditHook(env, ev, "context_skip", "no_unique_task", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "no_unique_task"}
	}
	if !force && !shouldInjectContext(binding, task) {
		_ = auditHook(env, ev, "context_skip", "already_injected", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "already_injected"}
	}
	contextText := renderHookContext(env, ev.ConfigProjectID, task)
	key := strings.Join([]string{ev.SessionID, task.TaskID, task.Revision, "context"}, "+")
	_, _ = applyEffectOnce(env.ProjectWT, shortHash(key), func() error { return nil })
	_ = markBindingInjected(env.ProjectWT, binding, task.Revision)
	_ = auditHook(env, ev, "context_inject", "ok", map[string]any{"bytes": len(contextText)})
	return HookDecision{Kind: DecisionAllow, ReasonCode: "context_injected", Context: contextText}
}

func guardDecision(env paths.Env, ev NormalizedHookEvent, result guardResult, denyUsesExit2 bool) HookDecision {
	if result.Deny {
		_ = auditHook(env, ev, "guard_deny", result.ReasonCode, map[string]any{"path": result.Path})
		decision := HookDecision{
			Kind:         DecisionDeny,
			ReasonCode:   result.ReasonCode,
			UserMessage:  result.Message,
			AgentMessage: result.Message,
		}
		if denyUsesExit2 {
			decision.ExitCode = 2
		}
		return decision
	}
	if result.AuditOnly {
		_ = auditHook(env, ev, "guard_audit", result.ReasonCode, map[string]any{"path": result.Path})
		return HookDecision{Kind: DecisionAllow, ReasonCode: result.ReasonCode}
	}
	_ = auditHook(env, ev, "guard_allow", result.ReasonCode, nil)
	return HookDecision{Kind: DecisionAllow, ReasonCode: result.ReasonCode}
}

func runCheckpointEffect(env paths.Env, ev NormalizedHookEvent) HookDecision {
	key := strings.Join([]string{ev.Host, ev.SessionID, ev.GenerationID, ev.TurnID, "checkpoint"}, "+")
	title := "Worktrail checkpoint"
	body := "# Checkpoint\n\n## Event\n" + ev.NativeEvent + "\n"
	_, err := applyEffectOnce(env.ProjectWT, shortHash(key), func() error {
		recorder := runtime.NewRecorder(env.ProjectWT)
		_, err := recorder.WriteCheckpoint(runtime.WriteOptions{
			Scope:      "project",
			Title:      title,
			Body:       body,
			SessionID:  sourceSession(ev),
			ProjectID:  ev.ConfigProjectID,
			SourceTool: sourceTool(ev.Host),
			Event:      ev.NativeEvent,
			Tags:       []string{ev.Host, "checkpoint"},
			Actor:      "hook:" + ev.Host,
			FileSuffix: time.Now().UTC().Format("20060102-150405") + "-checkpoint",
		})
		return err
	})
	if err != nil {
		_ = auditHook(env, ev, "checkpoint_error", "effect_failed", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "checkpoint_error"}
	}
	_ = auditHook(env, ev, "checkpoint", "ok", nil)
	return HookDecision{Kind: DecisionNoop, ReasonCode: "checkpoint"}
}

func runTerminalEffect(env paths.Env, ev NormalizedHookEvent, generationOrTurn string) HookDecision {
	key := strings.Join([]string{ev.ConfigProjectID, ev.Host, ev.SessionID, generationOrTurn, "terminal"}, "+")
	title := "Worktrail terminal"
	body := "# Runtime Session\n\n## Event\n" + ev.NativeEvent + "\n"
	applied, err := applyEffectOnce(env.ProjectWT, shortHash(key), func() error {
		recorder := runtime.NewRecorder(env.ProjectWT)
		_, err := recorder.WriteSession(runtime.WriteOptions{
			Scope:      "project",
			Title:      title,
			Body:       body,
			SessionID:  sourceSession(ev),
			ProjectID:  ev.ConfigProjectID,
			SourceTool: sourceTool(ev.Host),
			Event:      ev.NativeEvent,
			Tags:       []string{ev.Host, "terminal"},
			Actor:      "hook:" + ev.Host,
			FileSuffix: time.Now().UTC().Format("20060102-150405") + "-terminal",
		})
		return err
	})
	if err != nil {
		_ = auditHook(env, ev, "terminal_error", "effect_failed", map[string]any{"error": err.Error()})
		return HookDecision{Kind: DecisionNoop, ReasonCode: "terminal_error"}
	}
	if !applied {
		_ = auditHook(env, ev, "terminal", "deduped", nil)
		return HookDecision{Kind: DecisionNoop, ReasonCode: "terminal_deduped"}
	}
	_ = auditHook(env, ev, "terminal", "ok", nil)
	return HookDecision{Kind: DecisionNoop, ReasonCode: "terminal"}
}

func encodeCursor(event string, decision HookDecision) any {
	switch event {
	case "sessionStart":
		return cursorSessionStartResponse(decision.Context)
	case "beforeShellExecution", "beforeMCPExecution":
		return cursorPermissionResponse(decision)
	default:
		return cursorNoopResponse()
	}
}

func encodeCodex(event string, decision HookDecision) any {
	switch event {
	case "SessionStart", "UserPromptSubmit":
		return codexSessionContextResponse(event, decision.Context)
	case "PreToolUse":
		if decision.Kind == DecisionDeny {
			return codexPreToolUseDenyResponse(decision.AgentMessage)
		}
		return codexNoopResponse()
	case "PermissionRequest":
		if decision.Kind == DecisionDeny {
			return codexPermissionRequestDenyResponse(decision.UserMessage)
		}
		return codexNoopResponse()
	default:
		return codexNoopResponse()
	}
}

func noopWire(tool string) any {
	switch tool {
	case "cursor":
		return cursorNoopResponse()
	case "codex":
		return codexNoopResponse()
	default:
		return map[string]any{}
	}
}

func normalizeEvent(env paths.Env, tool, event string, raw map[string]any, projectID string) NormalizedHookEvent {
	ev := NormalizedHookEvent{
		Host:             tool,
		NativeEvent:      event,
		ProjectRoot:      env.ProjectRoot,
		ConfigProjectID:  projectID,
		PayloadProjectID: firstPayloadString(raw, "project_id"),
		SessionID:        firstPayloadString(raw, "session_id", "conversation_id", "transcript_id"),
		TurnID:           firstPayloadString(raw, "turn_id"),
		GenerationID:     firstPayloadString(raw, "generation_id"),
		ToolName:         firstPayloadString(raw, "tool_name", "toolName", "name"),
		ToolUseID:        firstPayloadString(raw, "tool_use_id", "toolUseId"),
		Command:          firstPayloadString(raw, "command"),
		FilePath:         firstPayloadString(raw, "file_path", "filePath", "path"),
		MCPName:          firstPayloadString(raw, "mcp_server", "server"),
		Raw:              raw,
		OccurredAt:       time.Now().UTC(),
	}
	if ev.Command == "" {
		if input := mapFromAny(raw["tool_input"]); input != nil {
			ev.Command = firstString(input, "command")
			if ev.FilePath == "" {
				ev.FilePath = firstString(input, "path", "file_path")
			}
		}
	}
	if ev.SessionID == "" {
		ev.SessionID = "anonymous"
	}
	return ev
}

func readJSONObject(in io.Reader) (map[string]any, error) {
	payload := map[string]any{}
	if in == nil {
		return payload, nil
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return payload, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return payload, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return map[string]any{}, err
	}
	return payload, nil
}

func readProjectID(env paths.Env) string {
	path := filepath.Join(env.ProjectWT, "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var cfg store.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return ""
	}
	return strings.TrimSpace(cfg.ProjectID)
}

func auditHook(env paths.Env, ev NormalizedHookEvent, action, reason string, extra map[string]any) error {
	if err := runtime.EnsureDirs(env.ProjectWT); err != nil {
		return err
	}
	if _, err := runtime.EnsurePrivateDir(env.ProjectWT, "logs"); err != nil {
		return err
	}
	data := map[string]any{
		"host":        ev.Host,
		"event":       ev.NativeEvent,
		"action":      action,
		"reason_code": reason,
		"session":     shortHash(ev.SessionID),
	}
	if ev.ToolUseID != "" {
		data["tool_use"] = shortHash(ev.ToolUseID)
	}
	if ev.PayloadProjectID != "" && ev.ConfigProjectID != "" && ev.PayloadProjectID != ev.ConfigProjectID {
		data["project_id_mismatch"] = true
	}
	for key, value := range extra {
		if value == nil || value == "" {
			continue
		}
		data[key] = value
	}
	return wlog.Append(env.ProjectWT, "hook.run", "", "hook:"+ev.Host+"-"+ev.NativeEvent, data)
}

func validateEvent(tool, event string) error {
	event = strings.TrimSpace(event)
	switch tool {
	case "cursor":
		switch event {
		case "sessionStart", "beforeShellExecution", "beforeMCPExecution",
			"afterShellExecution", "afterMCPExecution", "afterFileEdit",
			"preCompact", "stop", "sessionEnd":
			return nil
		}
		return fmt.Errorf("unsupported cursor hook event %q", event)
	case "codex":
		switch event {
		case "SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest",
			"PostToolUse", "PreCompact", "PostCompact", "SubagentStop", "Stop":
			return nil
		}
		return fmt.Errorf("unsupported codex hook event %q", event)
	case "claude":
		switch event {
		case "SessionStart", "PreCompact", "PostCompact", "Stop", "SessionEnd":
			return nil
		}
		return fmt.Errorf("unsupported claude hook event %q", event)
	default:
		return fmt.Errorf("unknown hook tool %q", tool)
	}
}

func sourceTool(host string) string {
	switch host {
	case "claude":
		return "claude-code"
	case "cursor":
		return "cursor"
	default:
		return "codex"
	}
}

func sourceSession(ev NormalizedHookEvent) string {
	if ev.SessionID == "" || ev.SessionID == "anonymous" {
		return ""
	}
	return sourceTool(ev.Host) + ":" + shortHash(ev.SessionID)
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return nil
}

func firstPayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
