package hookconfig

import "fmt"

const (
	ContractVersion = "worktrail.hooks.v1"

	HostCursor = "cursor"
	HostCodex  = "codex"

	GuardTimeoutSeconds = 1
	DefaultTimeoutSeconds = 2
)

type DesiredHandler struct {
	Host            string
	Event           string
	Command         string
	TimeoutSeconds  int
	ContractVersion string
	Matcher         string // Codex only; empty means match-all group without matcher key
	Type            string // Codex handler type; always "command"
}

type DesiredSpec struct {
	Host     string
	Handlers []DesiredHandler
}

func ManagedCommand(host, event string) string {
	return fmt.Sprintf("worktrail hook %s %s", host, event)
}

func CursorDesiredSpec() DesiredSpec {
	events := []struct {
		event   string
		timeout int
	}{
		{"sessionStart", DefaultTimeoutSeconds},
		{"beforeShellExecution", GuardTimeoutSeconds},
		{"beforeMCPExecution", GuardTimeoutSeconds},
		{"afterShellExecution", DefaultTimeoutSeconds},
		{"afterMCPExecution", DefaultTimeoutSeconds},
		{"afterFileEdit", DefaultTimeoutSeconds},
		{"preCompact", DefaultTimeoutSeconds},
		{"stop", DefaultTimeoutSeconds},
		{"sessionEnd", DefaultTimeoutSeconds},
	}
	handlers := make([]DesiredHandler, 0, len(events))
	for _, item := range events {
		handlers = append(handlers, DesiredHandler{
			Host:            HostCursor,
			Event:           item.event,
			Command:         ManagedCommand(HostCursor, item.event),
			TimeoutSeconds:  item.timeout,
			ContractVersion: ContractVersion,
		})
	}
	return DesiredSpec{Host: HostCursor, Handlers: handlers}
}

func CodexDesiredSpec() DesiredSpec {
	events := []struct {
		event   string
		timeout int
		matcher string
	}{
		{"SessionStart", DefaultTimeoutSeconds, ""},
		{"UserPromptSubmit", DefaultTimeoutSeconds, ""},
		{"PreToolUse", GuardTimeoutSeconds, ".*"},
		{"PermissionRequest", GuardTimeoutSeconds, ".*"},
		{"PostToolUse", DefaultTimeoutSeconds, ".*"},
		{"PreCompact", DefaultTimeoutSeconds, ""},
		{"PostCompact", DefaultTimeoutSeconds, ""},
		{"SubagentStop", DefaultTimeoutSeconds, ""},
		{"Stop", DefaultTimeoutSeconds, ""},
	}
	handlers := make([]DesiredHandler, 0, len(events))
	for _, item := range events {
		handlers = append(handlers, DesiredHandler{
			Host:            HostCodex,
			Event:           item.event,
			Command:         ManagedCommand(HostCodex, item.event),
			TimeoutSeconds:  item.timeout,
			ContractVersion: ContractVersion,
			Matcher:         item.matcher,
			Type:            "command",
		})
	}
	return DesiredSpec{Host: HostCodex, Handlers: handlers}
}

func DesiredForHost(host string) (DesiredSpec, error) {
	switch host {
	case HostCursor:
		return CursorDesiredSpec(), nil
	case HostCodex:
		return CodexDesiredSpec(), nil
	default:
		return DesiredSpec{}, fmt.Errorf("unsupported hooks host %q", host)
	}
}

func IsManagedCommand(command, host, event string) bool {
	return command == ManagedCommand(host, event)
}
