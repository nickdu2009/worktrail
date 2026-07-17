package hooks

import "time"

type DecisionKind string

const (
	DecisionAllow DecisionKind = "allow"
	DecisionDeny  DecisionKind = "deny"
	DecisionDefer DecisionKind = "defer"
	DecisionNoop  DecisionKind = "noop"
)

type NormalizedHookEvent struct {
	Host           string
	NativeEvent    string
	ProjectRoot    string
	ConfigProjectID string
	PayloadProjectID string
	SessionID      string
	TurnID         string
	GenerationID   string
	ToolName       string
	ToolUseID      string
	Command        string
	FilePath       string
	MCPName        string
	Raw            map[string]any
	OccurredAt     time.Time
}

type HookDecision struct {
	Kind          DecisionKind
	ReasonCode    string
	UserMessage   string
	AgentMessage  string
	Context       string
	Effects       []EffectPlan
	ExitCode      int // 0 default; 2 for Cursor/PreToolUse deny
}

type EffectPlan struct {
	Kind string // terminal|checkpoint|validation|context|binding_clear|audit
	Key  string
}

type ExitCodeError struct {
	Code    int
	Message string
}

func (e *ExitCodeError) Error() string {
	if e == nil {
		return "hook exit"
	}
	if e.Message != "" {
		return e.Message
	}
	return ""
}

func (e *ExitCodeError) ExitCode() int {
	if e == nil {
		return 1
	}
	return e.Code
}
