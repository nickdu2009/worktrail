package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	"github.com/nickdu2009/worktrail/internal/textsafety"
	"github.com/nickdu2009/worktrail/internal/transcript"
	"github.com/nickdu2009/worktrail/internal/util"
)

type Result struct {
	Tool       string   `json:"tool"`
	Event      string   `json:"event"`
	OK         bool     `json:"ok"`
	Runtime    string   `json:"runtime,omitempty"`
	Checkpoint string   `json:"checkpoint,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

func Run(ctx context.Context, env paths.Env, tool, event string, in io.Reader, out io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	rawPayload, warnings := readPayload(in)
	result := Result{Tool: tool, Event: event, OK: true, Warnings: warnings}
	root := env.ProjectWT
	actor := "hook:" + tool + "-" + event
	eventKey, err := normalizeEvent(tool, event)
	if err != nil {
		return err
	}
	payload := allowlistedPayload(rawPayload)
	durable := durablePayload(payload)
	if err := runtime.EnsureDirs(root); err != nil {
		return err
	}
	if tool == "cursor" {
		if observed, err := writeCursorObservedTranscript(env, payload); err != nil {
			warnings = append(warnings, "cursor transcript registry: "+err.Error())
			result.Warnings = warnings
		} else if observed != "" {
			result.Warnings = warnings
			result.Warnings = append(result.Warnings, "cursor transcript observed: "+filepath.Base(observed))
		} else {
			result.Warnings = warnings
		}
	}
	if _, err := runtime.EnsurePrivateDir(root, "logs"); err != nil {
		return err
	}
	if err := wlog.Append(root, "hook.run", "", actor, map[string]any{
		"tool":              tool,
		"event":             eventKey,
		"payload_fields":    sortedPayloadKeys(durable),
		"validation_status": validationStatus(payload, ""),
	}); err != nil {
		return err
	}

	switch eventKey {
	case "stop", "pre-compact", "post-compact", "session-end":
		hc := hookContextFromPayload(tool, payload)
		recorder := runtime.NewRecorder(root)
		sessionPath, err := writeRuntimeSession(recorder, tool, eventKey, durable, hc, actor)
		if err != nil {
			return err
		}
		result.Runtime = sessionPath
		if eventKey == "pre-compact" || eventKey == "post-compact" {
			checkpoint, err := writeRuntimeCheckpoint(recorder, tool, eventKey, durable, sessionPath, hc, actor)
			if err != nil {
				return err
			}
			result.Checkpoint = checkpoint
		}
		if (eventKey == "stop" || eventKey == "session-end") && shouldCreateTakeoverNote(hc) {
			takeover, err := writeRuntimeTakeover(recorder, tool, eventKey, durable, sessionPath, hc, actor)
			if err != nil {
				return err
			}
			result.Runtime = takeover
		}
	}
	if out != nil {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(result); err != nil {
			return err
		}
	}
	return nil
}

func readPayload(in io.Reader) (map[string]any, []string) {
	payload := map[string]any{}
	if in == nil {
		return payload, nil
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return payload, []string{"read stdin: " + err.Error()}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return payload, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return map[string]any{}, []string{"invalid json on stdin; payload discarded"}
	}
	return payload, nil
}

func writeRuntimeSession(recorder runtime.Recorder, tool, event string, payload map[string]any, hc hookContext, actor string) (string, error) {
	if !hc.HasSignal {
		return "", nil
	}
	title := titleFromHookContext(hc, event)
	session := sessionFromPayload(tool, payload)
	body := runtimeSessionBody(title, tool, event, payload, hc)
	record, err := recorder.WriteSession(runtime.WriteOptions{
		Scope:      "project",
		Title:      title,
		Body:       body,
		SessionID:  session,
		ProjectID:  firstPayloadString(payload, "project_id"),
		TaskID:     taskIDFromPayload(payload, title, hc.HasSignal),
		SourceTool: sourceTool(tool),
		Event:      event,
		Tags:       []string{tool, event, "hook"},
		Actor:      actor,
		FileSuffix: time.Now().UTC().Format("20060102-150405") + "-" + event,
	})
	if err != nil {
		return "", err
	}
	return record.Path, nil
}

func writeRuntimeCheckpoint(recorder runtime.Recorder, tool, event string, payload map[string]any, sessionPath string, hc hookContext, actor string) (string, error) {
	title := titleFromHookContext(hc, event)
	body := checkpointBody(title, tool, event, payload, sessionPath, hc)
	record, err := recorder.WriteCheckpoint(runtime.WriteOptions{
		Scope:      "project",
		Title:      title,
		Body:       body,
		SessionID:  sessionFromPayload(tool, payload),
		ProjectID:  firstPayloadString(payload, "project_id"),
		TaskID:     taskIDFromPayload(payload, title, hc.HasSignal),
		SourceTool: sourceTool(tool),
		Event:      event,
		Tags:       []string{tool, event, "checkpoint"},
		Actor:      actor,
		FileSuffix: time.Now().UTC().Format("20060102-150405") + "-" + event,
	})
	if err != nil {
		return "", err
	}
	return record.Path, nil
}

func writeRuntimeTakeover(recorder runtime.Recorder, tool, event string, payload map[string]any, sessionPath string, hc hookContext, actor string) (string, error) {
	if strings.TrimSpace(sessionPath) == "" {
		return "", nil
	}
	title := titleFromHookContext(hc, event)
	body := takeoverBody(title, tool, event+"-takeover", payload, sessionPath, hc)
	record, err := recorder.WriteTakeoverNote(runtime.WriteOptions{
		Scope:      "project",
		Title:      title,
		Body:       body,
		SessionID:  sessionFromPayload(tool, payload),
		ProjectID:  firstPayloadString(payload, "project_id"),
		TaskID:     taskIDFromPayload(payload, title, hc.HasSignal),
		SourceTool: sourceTool(tool),
		Event:      event + "-takeover",
		Tags:       []string{tool, event, "takeover_note"},
		Actor:      actor,
		FileSuffix: time.Now().UTC().Format("20060102-150405") + "-" + event + "-takeover",
	})
	if err != nil {
		return "", err
	}
	return record.Path, nil
}

func shouldCreateTakeoverNote(hc hookContext) bool {
	if !hc.HasSignal {
		return false
	}
	if strings.TrimSpace(hc.NextStep) != "" {
		return true
	}
	return strings.TrimSpace(hc.CurrentGoal) != "" && strings.TrimSpace(hc.RecentAssistant) != ""
}

func runtimeSessionBody(title, tool, event string, payload map[string]any, hc hookContext) string {
	return "# Runtime Session: " + title + "\n\n" +
		"## Original Intent\nCaptured from " + tool + " hook event `" + event + "`.\n\n" +
		"## Current Goal\n" + valueOrUnknown(hc.CurrentGoal) + "\n\n" +
		"## Constraints\nHooks write runtime-only artifacts and audit logs. They never promote durable knowledge or overwrite explicit session state.\n\n" +
		"## Relevant Context\n" + valueOrUnknown(hc.RecentAssistant) + "\n\n" +
		"## Evidence\n" + payloadSummary(payload, hc.ValidationStatus) + "\n\n" +
		"## Work Done\n" + valueOrUnknown(hc.WorkDone) + "\n\n" +
		"## Validation\n" + validationSummary(hc) + "\n\n" +
		"## Open Questions\nTreat this as degraded recovery unless a manual handoff or explicit session state exists.\n\n" +
		"## Next Step\n" + valueOrDefault(hc.NextStep, "Run `worktrail resume` or `worktrail handoff` before continuing substantial work.") + "\n"
}

func checkpointBody(title, tool, event string, payload map[string]any, sessionPath string, hc hookContext) string {
	return "# Checkpoint: " + title + "\n\n" +
		"## Source Runtime Session\n" + sourceSessionSummary(sessionPath) + "\n\n" +
		"## Recovery Summary\n" + recoverySummary(hc) + "\n\n" +
		"## Event\n" + event + "\n\n" +
		"## Payload Summary\n" + payloadSummary(payload, hc.ValidationStatus) + "\n"
}

func takeoverBody(title, tool, event string, payload map[string]any, sessionPath string, hc hookContext) string {
	return "# Takeover Note: " + title + "\n\n" +
		"## Source Runtime Session\n" + sourceSessionSummary(sessionPath) + "\n\n" +
		"## Recovery Summary\n" + recoverySummary(hc) + "\n\n" +
		"## Event\n" + event + "\n\n" +
		"## Payload Summary\n" + payloadSummary(payload, hc.ValidationStatus) + "\n"
}

type hookContext struct {
	HasSignal        bool
	CurrentGoal      string
	RecentDecision   string
	RecentAssistant  string
	WorkDone         string
	Validation       string
	ValidationStatus string
	NextStep         string
}

func hookContextFromPayload(tool string, payload map[string]any) hookContext {
	hc := hookContext{
		CurrentGoal:     firstPayloadString(payload, "task", "prompt", "message"),
		RecentAssistant: firstPayloadString(payload, "transcript_summary", "summary"),
		Validation:      payloadStringList(payload, "commands"),
		NextStep:        firstPayloadString(payload, "next_step"),
	}
	if hc.CurrentGoal != "" || hc.RecentAssistant != "" || hc.Validation != "" {
		hc.HasSignal = true
	}
	if path := firstPayloadString(payload, "transcript_path", "transcript_file", "session_path"); path != "" {
		if tail, err := transcriptTailContext(tool, path); err == nil && tail.HasSignal {
			hc.HasSignal = true
			if hc.CurrentGoal == "" {
				hc.CurrentGoal = tail.CurrentGoal
			}
			if hc.RecentAssistant == "" {
				hc.RecentAssistant = tail.RecentAssistant
			}
			if hc.WorkDone == "" {
				hc.WorkDone = tail.WorkDone
			}
			if hc.Validation == "" {
				hc.Validation = tail.Validation
			}
			if hc.NextStep == "" {
				hc.NextStep = tail.NextStep
			}
		}
	}
	hc.CurrentGoal = safeSemanticText(hc.CurrentGoal, 300)
	hc.RecentDecision = safeSemanticText(hc.RecentDecision, 300)
	hc.RecentAssistant = safeSemanticText(hc.RecentAssistant, 300)
	hc.WorkDone = safeSemanticText(hc.WorkDone, 300)
	hc.Validation = safeSemanticText(hc.Validation, 500)
	hc.NextStep = safeSemanticText(hc.NextStep, 300)
	hc.ValidationStatus = validationStatus(payload, hc.Validation)
	return hc
}

func transcriptTailContext(tool, path string) (hookContext, error) {
	f, err := os.Open(path)
	if err != nil {
		return hookContext{}, err
	}
	defer f.Close()
	var tr transcript.Transcript
	switch tool {
	case "cursor":
		tr, err = transcript.ParseCursorJSONL(f)
	case "claude":
		tr, err = transcript.ParseClaudeJSONL(f)
	default:
		tr, err = transcript.ParseCodexJSONL(f)
	}
	if err != nil {
		return hookContext{}, err
	}
	hc := hookContext{}
	for _, msg := range tr.Messages {
		switch msg.Role {
		case "user":
			if hc.CurrentGoal == "" {
				hc.CurrentGoal = safeSemanticText(msg.Content, 300)
			}
			if looksLikeDecision(msg.Content) {
				hc.RecentDecision = safeSemanticText(msg.Content, 300)
			}
		case "assistant":
			hc.RecentAssistant = safeSemanticText(msg.Content, 300)
			if looksLikeValidation(msg.Content) {
				hc.Validation = safeSemanticText(msg.Content, 300)
			}
			hc.WorkDone = safeSemanticText(msg.Content, 300)
		}
	}
	hc.HasSignal = hc.CurrentGoal != "" || hc.RecentAssistant != "" || hc.Validation != ""
	return hc, nil
}

func titleFromHookContext(hc hookContext, fallback string) string {
	if strings.TrimSpace(hc.CurrentGoal) != "" {
		return hc.CurrentGoal
	}
	if strings.TrimSpace(hc.RecentAssistant) != "" {
		return hc.RecentAssistant
	}
	return "Worktrail " + fallback
}

func sourceSessionSummary(sessionPath string) string {
	if strings.TrimSpace(sessionPath) == "" {
		return "No runtime session was written because no meaningful task context was available."
	}
	return filepath.Base(sessionPath)
}

func recoverySummary(hc hookContext) string {
	if !hc.HasSignal {
		return "Recovery context was unavailable. The hook payload did not include a task signal or readable bounded transcript context."
	}
	var b strings.Builder
	b.WriteString("- Active goal: ")
	b.WriteString(valueOrUnknown(hc.CurrentGoal))
	b.WriteString("\n- Recent decisions: ")
	b.WriteString(valueOrUnknown(hc.RecentDecision))
	b.WriteString("\n- Work completed: ")
	b.WriteString(valueOrUnknown(hc.WorkDone))
	b.WriteString("\n- Validation: ")
	b.WriteString(validationSummary(hc))
	b.WriteString("\n- Next safe action: ")
	b.WriteString(valueOrDefault(hc.NextStep, "Review the latest handoff or explicit session state before continuing."))
	return b.String()
}

func valueOrUnknown(value string) string {
	return valueOrDefault(value, "Unknown.")
}

func valueOrDefault(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func compactText(value string, limit int) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

var (
	secretAssignmentPattern       = regexp.MustCompile(`(?i)\b(password|passwd|pwd|secret|api[_-]?key|aws_secret_access_key|aws_session_token|access[_-]?token|authorization)["']?\s*[:=]\s*["']?[^\s"',;]+`)
	legacyShortGitHubTokenPattern = regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9_]{20,}\b`)
)

func safeSemanticText(value string, limit int) string {
	result, err := textsafety.Process(value, textsafety.ProfileLocal)
	if err != nil {
		return compactText("[blocked-sensitive-content]", limit)
	}
	value = result.Text
	value = legacyShortGitHubTokenPattern.ReplaceAllString(value, "[redacted-github-token]")
	value = secretAssignmentPattern.ReplaceAllString(value, "$1=[redacted-secret]")
	return compactText(value, limit)
}

func looksLikeDecision(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "decided") || strings.Contains(lower, "decision") || strings.Contains(lower, "采纳") || strings.Contains(lower, "决定")
}

func looksLikeValidation(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "go test") || strings.Contains(lower, "validation") || strings.Contains(lower, "passed") || strings.Contains(lower, "failed") || strings.Contains(lower, "测试")
}

func payloadStringList(payload map[string]any, key string) string {
	raw, ok := payload[key]
	if !ok {
		return ""
	}
	switch v := raw.(type) {
	case []any:
		var values []string
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				values = append(values, safeSemanticText(s, 200))
			}
		}
		return strings.Join(values, "\n")
	case []string:
		values := make([]string, 0, len(v))
		for _, item := range v {
			values = append(values, safeSemanticText(item, 200))
		}
		return strings.Join(values, "\n")
	case string:
		return safeSemanticText(v, 500)
	default:
		return ""
	}
}

func sessionFromPayload(tool string, payload map[string]any) string {
	for _, key := range []string{"session_id", "conversation_id", "transcript_id"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return sourceTool(tool) + ":" + shortHash(value)
		}
	}
	return ""
}

func sourceTool(tool string) string {
	switch tool {
	case "claude":
		return "claude-code"
	case "cursor":
		return "cursor"
	default:
		return "codex"
	}
}

func payloadSummary(payload map[string]any, validation string) string {
	if len(payload) == 0 {
		return "No hook payload was provided.\n"
	}
	fields := sortedPayloadKeys(payload)
	return "- Allowlisted fields: " + strings.Join(fields, ", ") + "\n" +
		"- Validation status: " + withDefaultValidation(validation) + "\n"
}

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

func normalizeEvent(tool, event string) (string, error) {
	event = strings.TrimSpace(event)
	switch tool {
	case "cursor":
		switch event {
		case "sessionStart":
			return "session-start", nil
		case "preCompact":
			return "pre-compact", nil
		case "postCompact":
			return "post-compact", nil
		case "stop":
			return "stop", nil
		case "sessionEnd":
			return "session-end", nil
		}
		return "", fmt.Errorf("unsupported cursor hook event %q; expected one of sessionStart, preCompact, postCompact, stop, sessionEnd", event)
	case "claude", "codex":
		switch event {
		case "SessionStart":
			return "session-start", nil
		case "PreCompact":
			return "pre-compact", nil
		case "PostCompact":
			return "post-compact", nil
		case "Stop":
			return "stop", nil
		case "SessionEnd":
			if tool == "claude" {
				return "session-end", nil
			}
		}
		if tool == "claude" {
			return "", fmt.Errorf("unsupported claude hook event %q; expected one of SessionStart, PreCompact, PostCompact, Stop, SessionEnd", event)
		}
		return "", fmt.Errorf("unsupported codex hook event %q; expected one of SessionStart, PreCompact, PostCompact, Stop", event)
	default:
		return "", fmt.Errorf("unknown hook tool %q", tool)
	}
}

func durablePayload(payload map[string]any) map[string]any {
	safe := map[string]any{}
	for key, value := range payload {
		switch key {
		case "transcript_path", "transcript_file", "session_path":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				safe[key] = safeSemanticText(filepath.Base(s), 100)
			}
		case "workspace_roots":
			safe[key] = workspaceRootBasenames(value)
		case "conversation_id", "session_id", "generation_id", "transcript_id":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				safe[key] = shortHash(s)
			}
		case "task", "prompt", "message", "summary", "transcript_summary", "next_step":
			if s, ok := value.(string); ok {
				safe[key] = safeSemanticText(s, 300)
			}
		case "commands":
			safe[key] = payloadStringList(payload, key)
		case "project_id":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				safe[key] = "project-" + shortHash(s)
			}
		case "task_id":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				safe[key] = "task-" + shortHash(s)
			}
		case "status", "validation_status", "result":
			if s, ok := value.(string); ok {
				safe[key] = safeSemanticText(s, 100)
			}
		}
	}
	return safe
}

func allowlistedPayload(payload map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"session_id": {}, "conversation_id": {}, "generation_id": {}, "transcript_id": {},
		"task": {}, "prompt": {}, "message": {}, "summary": {}, "transcript_summary": {},
		"commands": {}, "transcript_path": {}, "transcript_file": {}, "session_path": {},
		"workspace_roots": {}, "status": {}, "validation_status": {}, "result": {},
		"next_step": {}, "project_id": {}, "task_id": {},
	}
	safe := make(map[string]any, len(allowed))
	for key, value := range payload {
		if _, ok := allowed[key]; ok {
			safe[key] = value
		}
	}
	return safe
}

func sortedPayloadKeys(payload map[string]any) []string {
	keys := make([]string, 0, len(payload))
	for key := range payload {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func taskIDFromPayload(payload map[string]any, title string, hasSignal bool) string {
	if taskID := firstPayloadString(payload, "task_id"); taskID != "" {
		return taskID
	}
	if !hasSignal {
		return ""
	}
	title = strings.TrimSpace(title)
	if title == "" {
		return ""
	}
	return "task-" + util.Slug(title)
}

func validationStatus(payload map[string]any, evidence string) string {
	for _, key := range []string{"validation_status", "result"} {
		switch strings.ToLower(strings.TrimSpace(firstPayloadString(payload, key))) {
		case "failed", "failure", "error":
			return "failed"
		case "passed", "pass", "success", "succeeded":
			return "passed"
		}
	}
	lower := strings.ToLower(evidence)
	if strings.Contains(lower, "failed") || strings.Contains(lower, "failure") || strings.Contains(lower, "error:") {
		return "failed"
	}
	if strings.Contains(lower, "passed") || strings.Contains(lower, "succeeded") {
		return "passed"
	}
	return "unknown"
}

func validationSummary(hc hookContext) string {
	status := withDefaultValidation(hc.ValidationStatus)
	if strings.TrimSpace(hc.Validation) == "" {
		return "Status: " + status + ".\nEvidence: none recorded."
	}
	return "Status: " + status + ".\nEvidence: " + hc.Validation
}

func withDefaultValidation(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed":
		return "passed"
	case "failed":
		return "failed"
	default:
		return "unknown"
	}
}

func workspaceRootBasenames(value any) any {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	roots := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			roots = append(roots, safeSemanticText(filepath.Base(s), 100))
		}
	}
	return roots
}

func writeCursorObservedTranscript(env paths.Env, payload map[string]any) (string, error) {
	path, _ := payload["transcript_path"].(string)
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil
	}
	now := time.Now().UTC()
	idSource := firstPayloadString(payload, "conversation_id", "session_id", "generation_id", "transcript_id")
	if idSource == "" {
		idSource = path
	}
	id := "observed-" + shortHash(idSource)
	meta := map[string]any{
		"schema":        "worktrail.cursor_observed_transcript.v1",
		"id":            id,
		"source":        "cursor",
		"path":          path,
		"path_basename": filepath.Base(path),
		"created_at":    now,
	}
	if session := firstPayloadString(payload, "conversation_id", "session_id"); session != "" {
		meta["source_session"] = "cursor:" + shortHash(session)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", err
	}
	if _, err := runtime.EnsurePrivateDir(env.ProjectWT, filepath.Join("raw", "cursor")); err != nil {
		return "", err
	}
	out := filepath.Join(env.ProjectWT, "raw", "cursor", id+".metadata.json")
	if err := util.AtomicWrite(out, append(data, '\n'), 0o600); err != nil {
		return "", err
	}
	return out, nil
}

func firstPayloadString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])[:12]
}
