package hooks

import (
	"encoding/json"
	"io"
	"strings"
)

func encodeJSON(out io.Writer, value any) error {
	if out == nil {
		return nil
	}
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

func cursorSessionStartResponse(context string) map[string]any {
	if strings.TrimSpace(context) == "" {
		return map[string]any{}
	}
	return map[string]any{"additional_context": context}
}

func cursorPermissionResponse(decision HookDecision) map[string]any {
	permission := "allow"
	switch decision.Kind {
	case DecisionDeny:
		permission = "deny"
	case DecisionDefer:
		permission = "ask"
	}
	out := map[string]any{"permission": permission}
	if decision.UserMessage != "" {
		out["user_message"] = decision.UserMessage
	}
	if decision.AgentMessage != "" {
		out["agent_message"] = decision.AgentMessage
	}
	return out
}

func cursorNoopResponse() map[string]any {
	return map[string]any{}
}

func codexSessionContextResponse(event, context string) map[string]any {
	if strings.TrimSpace(context) == "" {
		return map[string]any{}
	}
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     event,
			"additionalContext": context,
		},
	}
}

func codexPreToolUseDenyResponse(message string) map[string]any {
	return map[string]any{
		"permissionDecision": "deny",
		"block":              true,
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": message,
		},
	}
}

func codexPermissionRequestDenyResponse(message string) map[string]any {
	return map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName": "PermissionRequest",
			"decision": map[string]any{
				"behavior": "deny",
				"message":  message,
			},
		},
	}
}

func codexNoopResponse() map[string]any {
	return map[string]any{}
}
