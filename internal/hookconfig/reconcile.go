package hookconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrLegacyCodexUserHook = errors.New("legacy_codex_user_hook_requires_manual_migration")

type ReconcileMode int

const (
	ModeInstall ReconcileMode = iota
	ModeUninstall
)

type DiagnoseFinding struct {
	Code    string
	Message string
	Path    string
}

// Reconcile applies or removes managed Worktrail handlers while preserving user
// root fields, matcher groups, handlers, and relative order.
func Reconcile(host string, current []byte, mode ReconcileMode) ([]byte, error) {
	spec, err := DesiredForHost(host)
	if err != nil {
		return nil, err
	}
	doc, err := parseDocument(current)
	if err != nil {
		return nil, err
	}
	switch host {
	case HostCursor:
		if mode == ModeUninstall {
			removeCursorHandlers(doc, spec)
		} else {
			installCursorHandlers(doc, spec)
		}
	case HostCodex:
		if err := prepareCodexLegacy(doc, mode); err != nil {
			return nil, err
		}
		if mode == ModeUninstall {
			removeCodexHandlers(doc, spec)
		} else {
			installCodexHandlers(doc, spec)
		}
	default:
		return nil, fmt.Errorf("unsupported hooks host %q", host)
	}
	return marshalDocument(doc)
}

func Diagnose(host string, current []byte) ([]DiagnoseFinding, error) {
	spec, err := DesiredForHost(host)
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(current))) == 0 {
		return []DiagnoseFinding{{
			Code:    "hooks_missing",
			Message: "hooks configuration is missing",
		}}, nil
	}
	doc, err := parseDocument(current)
	if err != nil {
		return []DiagnoseFinding{{
			Code:    "hooks_malformed_json",
			Message: err.Error(),
		}}, nil
	}
	var findings []DiagnoseFinding
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		findings = append(findings, DiagnoseFinding{
			Code:    "hooks_missing",
			Message: "hooks object is missing",
		})
		return findings, nil
	}
	switch host {
	case HostCursor:
		findings = append(findings, diagnoseCursor(hooks, spec)...)
	case HostCodex:
		findings = append(findings, diagnoseCodex(hooks, spec)...)
	}
	return findings, nil
}

func parseDocument(current []byte) (map[string]any, error) {
	doc := map[string]any{}
	if len(strings.TrimSpace(string(current))) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(current, &doc); err != nil {
		return nil, fmt.Errorf("malformed hooks JSON: %w", err)
	}
	return doc, nil
}

func marshalDocument(doc map[string]any) ([]byte, error) {
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func installCursorHandlers(doc map[string]any, spec DesiredSpec) {
	if _, ok := doc["version"]; !ok {
		doc["version"] = 1
	}
	hooks := ensureMap(doc, "hooks")
	for _, handler := range spec.Handlers {
		list := asObjectSlice(hooks[handler.Event])
		list = upsertCursorHandler(list, handler)
		hooks[handler.Event] = list
	}
}

func removeCursorHandlers(doc map[string]any, spec DesiredSpec) {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for _, handler := range spec.Handlers {
		list := asObjectSlice(hooks[handler.Event])
		next := list[:0]
		for _, item := range list {
			if commandOf(item) == handler.Command {
				continue
			}
			next = append(next, item)
		}
		if len(next) == 0 {
			delete(hooks, handler.Event)
		} else {
			hooks[handler.Event] = next
		}
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
}

func upsertCursorHandler(list []map[string]any, handler DesiredHandler) []map[string]any {
	entry := map[string]any{
		"command": handler.Command,
		"timeout": handler.TimeoutSeconds,
	}
	for i, item := range list {
		if commandOf(item) == handler.Command {
			list[i] = mergePreservingUnknown(item, entry)
			return list
		}
	}
	return append(list, entry)
}

func prepareCodexLegacy(doc map[string]any, mode ReconcileMode) error {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return nil
	}
	upgraded := map[string]any{}
	for event, value := range hooks {
		switch typed := value.(type) {
		case string:
			command := strings.TrimSpace(typed)
			if isLegacyWorktrailCodexScalar(event, command) {
				if mode == ModeInstall {
					upgraded[event] = []any{
						map[string]any{
							"hooks": []any{
								map[string]any{
									"type":    "command",
									"command": ManagedCommand(HostCodex, event),
									"timeout": timeoutForCodexEvent(event),
								},
							},
						},
					}
				}
				// Uninstall drops legacy Worktrail scalars.
				continue
			}
			if mode == ModeUninstall {
				// Preserve non-Worktrail user scalars so uninstall can still
				// remove managed handlers from other events.
				upgraded[event] = typed
				continue
			}
			return fmt.Errorf("%w: event %s", ErrLegacyCodexUserHook, event)
		case []any:
			upgraded[event] = typed
		case nil:
			continue
		default:
			return fmt.Errorf("%w: event %s has unsupported value type", ErrLegacyCodexUserHook, event)
		}
	}
	doc["hooks"] = upgraded
	return nil
}

func installCodexHandlers(doc map[string]any, spec DesiredSpec) {
	hooks := ensureMap(doc, "hooks")
	for _, handler := range spec.Handlers {
		groups := asObjectSlice(hooks[handler.Event])
		groups = upsertCodexHandler(groups, handler)
		hooks[handler.Event] = groups
	}
}

func removeCodexHandlers(doc map[string]any, spec DesiredSpec) {
	hooks, _ := doc["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for _, handler := range spec.Handlers {
		value, ok := hooks[handler.Event]
		if !ok {
			continue
		}
		// Preserve non-Worktrail user scalars; only strip managed array handlers.
		if command, isString := value.(string); isString {
			if isLegacyWorktrailCodexScalar(handler.Event, command) {
				delete(hooks, handler.Event)
			}
			continue
		}
		groups := asObjectSlice(value)
		nextGroups := make([]map[string]any, 0, len(groups))
		for _, group := range groups {
			handlers := asObjectSlice(group["hooks"])
			kept := handlers[:0]
			for _, item := range handlers {
				if commandOf(item) == handler.Command {
					continue
				}
				kept = append(kept, item)
			}
			if len(kept) == 0 {
				continue
			}
			group["hooks"] = kept
			nextGroups = append(nextGroups, group)
		}
		if len(nextGroups) == 0 {
			delete(hooks, handler.Event)
		} else {
			hooks[handler.Event] = nextGroups
		}
	}
	if len(hooks) == 0 {
		delete(doc, "hooks")
	}
}

func upsertCodexHandler(groups []map[string]any, handler DesiredHandler) []map[string]any {
	entry := map[string]any{
		"type":    "command",
		"command": handler.Command,
		"timeout": handler.TimeoutSeconds,
	}
	targetIdx := -1
	for i, group := range groups {
		if matcherEquals(group, handler.Matcher) {
			targetIdx = i
			handlers := asObjectSlice(group["hooks"])
			replaced := false
			for j, item := range handlers {
				if commandOf(item) == handler.Command {
					handlers[j] = mergePreservingUnknown(item, entry)
					replaced = true
					break
				}
			}
			if !replaced {
				// Also replace any other managed command for same event in this group.
				for j, item := range handlers {
					if strings.HasPrefix(commandOf(item), "worktrail hook "+HostCodex+" "+handler.Event) {
						handlers[j] = mergePreservingUnknown(item, entry)
						replaced = true
						break
					}
				}
			}
			if !replaced {
				handlers = append(handlers, entry)
			}
			group["hooks"] = handlers
			groups[i] = group
			return groups
		}
		// Replace managed handler found in any group (dedupe).
		handlers := asObjectSlice(group["hooks"])
		for j, item := range handlers {
			if commandOf(item) == handler.Command {
				handlers[j] = mergePreservingUnknown(item, entry)
				group["hooks"] = handlers
				if handler.Matcher != "" {
					group["matcher"] = handler.Matcher
				} else {
					delete(group, "matcher")
				}
				groups[i] = group
				return groups
			}
		}
	}
	group := map[string]any{
		"hooks": []any{entry},
	}
	if handler.Matcher != "" {
		group["matcher"] = handler.Matcher
	}
	if targetIdx >= 0 {
		groups[targetIdx] = group
		return groups
	}
	return append(groups, group)
}

func diagnoseCursor(hooks map[string]any, spec DesiredSpec) []DiagnoseFinding {
	var findings []DiagnoseFinding
	for _, handler := range spec.Handlers {
		list := asObjectSlice(hooks[handler.Event])
		matches := 0
		for _, item := range list {
			if commandOf(item) != handler.Command {
				continue
			}
			matches++
			if timeoutOf(item) != handler.TimeoutSeconds {
				findings = append(findings, DiagnoseFinding{
					Code:    "hooks_timeout_mismatch",
					Message: fmt.Sprintf("%s timeout=%v want=%d", handler.Event, item["timeout"], handler.TimeoutSeconds),
					Path:    handler.Event,
				})
			}
		}
		switch matches {
		case 0:
			findings = append(findings, DiagnoseFinding{
				Code:    "hooks_managed_handler_missing",
				Message: "missing managed handler for " + handler.Event,
				Path:    handler.Event,
			})
		case 1:
		default:
			findings = append(findings, DiagnoseFinding{
				Code:    "hooks_managed_handler_duplicate",
				Message: "duplicate managed handler for " + handler.Event,
				Path:    handler.Event,
			})
		}
	}
	return findings
}

func diagnoseCodex(hooks map[string]any, spec DesiredSpec) []DiagnoseFinding {
	var findings []DiagnoseFinding
	for event, value := range hooks {
		if _, ok := value.(string); ok {
			command := strings.TrimSpace(fmt.Sprint(value))
			if isLegacyWorktrailCodexScalar(event, command) {
				findings = append(findings, DiagnoseFinding{
					Code:    "hooks_legacy_worktrail_scalar",
					Message: "legacy Worktrail Codex scalar requires reinstall",
					Path:    event,
				})
			} else {
				findings = append(findings, DiagnoseFinding{
					Code:    "legacy_codex_user_hook_requires_manual_migration",
					Message: "non-Worktrail Codex scalar hook requires manual migration",
					Path:    event,
				})
			}
		}
	}
	for _, handler := range spec.Handlers {
		matches := 0
		for _, group := range asObjectSlice(hooks[handler.Event]) {
			for _, item := range asObjectSlice(group["hooks"]) {
				if commandOf(item) != handler.Command {
					continue
				}
				matches++
				if typeOf(item) != "command" {
					findings = append(findings, DiagnoseFinding{
						Code:    "hooks_handler_type_mismatch",
						Message: handler.Event + " managed handler type must be command",
						Path:    handler.Event,
					})
				}
				if timeoutOf(item) != handler.TimeoutSeconds {
					findings = append(findings, DiagnoseFinding{
						Code:    "hooks_timeout_mismatch",
						Message: fmt.Sprintf("%s timeout=%v want=%d", handler.Event, item["timeout"], handler.TimeoutSeconds),
						Path:    handler.Event,
					})
				}
			}
		}
		switch matches {
		case 0:
			findings = append(findings, DiagnoseFinding{
				Code:    "hooks_managed_handler_missing",
				Message: "missing managed handler for " + handler.Event,
				Path:    handler.Event,
			})
		case 1:
		default:
			findings = append(findings, DiagnoseFinding{
				Code:    "hooks_managed_handler_duplicate",
				Message: "duplicate managed handler for " + handler.Event,
				Path:    handler.Event,
			})
		}
	}
	return findings
}

func isLegacyWorktrailCodexScalar(event, command string) bool {
	command = strings.TrimSpace(command)
	if command == ManagedCommand(HostCodex, event) {
		return true
	}
	// Accept historical event casing variants that still point at worktrail hook.
	return strings.HasPrefix(command, "worktrail hook codex ")
}

func timeoutForCodexEvent(event string) int {
	for _, handler := range CodexDesiredSpec().Handlers {
		if handler.Event == event {
			return handler.TimeoutSeconds
		}
	}
	return DefaultTimeoutSeconds
}

func ensureMap(doc map[string]any, key string) map[string]any {
	if existing, ok := doc[key].(map[string]any); ok && existing != nil {
		return existing
	}
	next := map[string]any{}
	doc[key] = next
	return next
}

func asObjectSlice(value any) []map[string]any {
	raw, ok := value.([]any)
	if !ok || len(raw) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if obj, ok := item.(map[string]any); ok {
			out = append(out, obj)
		}
	}
	return out
}

func commandOf(item map[string]any) string {
	command, _ := item["command"].(string)
	return strings.TrimSpace(command)
}

func typeOf(item map[string]any) string {
	value, _ := item["type"].(string)
	return strings.TrimSpace(value)
}

func timeoutOf(item map[string]any) int {
	switch value := item["timeout"].(type) {
	case float64:
		return int(value)
	case int:
		return value
	case json.Number:
		n, _ := value.Int64()
		return int(n)
	default:
		return 0
	}
}

func matcherEquals(group map[string]any, want string) bool {
	have, _ := group["matcher"].(string)
	return strings.TrimSpace(have) == strings.TrimSpace(want)
}

func mergePreservingUnknown(existing, desired map[string]any) map[string]any {
	next := map[string]any{}
	for key, value := range existing {
		next[key] = value
	}
	for key, value := range desired {
		next[key] = value
	}
	return next
}

// StableJSON returns canonical JSON for golden comparisons.
func StableJSON(data []byte) (string, error) {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return "", err
	}
	normalized := normalizeJSON(value)
	out, err := json.MarshalIndent(normalized, "", "  ")
	if err != nil {
		return "", err
	}
	return string(out) + "\n", nil
}

func normalizeJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make(map[string]any, len(typed))
		for _, key := range keys {
			out[key] = normalizeJSON(typed[key])
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = normalizeJSON(item)
		}
		return out
	default:
		return typed
	}
}
