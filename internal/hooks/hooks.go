package hooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/transcript"
	"github.com/nickdu2009/worktrail/internal/util"
)

type Result struct {
	Tool       string   `json:"tool"`
	Event      string   `json:"event"`
	OK         bool     `json:"ok"`
	State      string   `json:"state,omitempty"`
	Checkpoint string   `json:"checkpoint,omitempty"`
	Candidate  string   `json:"candidate,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
}

func Run(ctx context.Context, env paths.Env, tool, event string, in io.Reader, out io.Writer) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	payload, warnings := readPayload(in)
	result := Result{Tool: tool, Event: event, OK: true, Warnings: warnings}
	root := env.ProjectWT
	actor := "hook:" + tool + "-" + event
	if err := ensureWorktrail(root); err != nil {
		return err
	}
	if tool == "cursor" {
		if observed, err := writeCursorObservedTranscript(env, payload); err != nil {
			warnings = append(warnings, "cursor transcript registry: "+err.Error())
			result.Warnings = warnings
		} else if observed != "" {
			result.Warnings = warnings
			result.Warnings = append(result.Warnings, "cursor transcript observed: "+observed)
		} else {
			result.Warnings = warnings
		}
	}
	if err := wlog.Append(root, "hook.run", "", actor, map[string]any{"tool": tool, "event": event, "payload": payload}); err != nil {
		return err
	}

	eventKey := normalizeEvent(event)
	switch eventKey {
	case "stop", "pre-compact", "post-compact", "session-end":
		hc := hookContextFromPayload(tool, payload)
		statePath, err := writeState(env, tool, eventKey, payload, hc)
		if err != nil {
			return err
		}
		result.State = statePath
		if eventKey == "pre-compact" || eventKey == "post-compact" {
			checkpoint, err := writeCheckpoint(env, tool, eventKey, payload, statePath, hc)
			if err != nil {
				return err
			}
			result.Checkpoint = checkpoint
		}
		if (eventKey == "stop" || eventKey == "session-end") && shouldCreateOperationalHandoff(hc) {
			candidate, err := writeHandoffCandidate(env, tool, eventKey, payload, hc)
			if err != nil {
				return err
			}
			result.Candidate = candidate
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
		payload["_raw"] = string(data)
		payload["_parse_error"] = err.Error()
		return payload, []string{"invalid json on stdin; recorded raw payload"}
	}
	return payload, nil
}

func ensureWorktrail(root string) error {
	return paths.EnsureDirs(
		filepath.Join(root, "state", "active"),
		filepath.Join(root, "state", "checkpoints"),
		filepath.Join(root, "candidates", "project"),
		filepath.Join(root, "raw", "cursor"),
		filepath.Join(root, "logs"),
	)
}

func writeState(env paths.Env, tool, event string, payload map[string]any, hc hookContext) (string, error) {
	if !hc.HasSignal {
		return "", nil
	}
	title := titleFromHookContext(hc, event)
	session := sessionFromPayload(tool, payload)
	body := stateBody(title, tool, event, durablePayload(tool, payload), hc)
	active, err := wtstate.LatestActive(env, "project")
	if err == nil {
		updated, err := wtstate.Update(env, wtstate.UpdateOptions{
			Scope:          "project",
			ID:             active.State.ID,
			SourceSessions: optionalList(session),
			ReplaceBody:    &body,
			Actor:          "hook:" + tool + "-" + event,
		})
		if err != nil {
			return "", err
		}
		return updated.Path, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:          "project",
		TaskID:         "task-" + util.Slug(title),
		Type:           "implementation",
		Title:          title,
		SourceTool:     sourceTool(tool),
		SourceSessions: optionalList(session),
		Tags:           []string{tool, event},
		Body:           body,
		Actor:          "hook:" + tool + "-" + event,
	})
	if err != nil {
		return "", err
	}
	return cap.Path, nil
}

func writeCheckpoint(env paths.Env, tool, event string, payload map[string]any, statePath string, hc hookContext) (string, error) {
	now := time.Now()
	title := titleFromHookContext(hc, event)
	safePayload := durablePayload(tool, payload)
	meta := map[string]any{
		"schema":      model.SchemaState,
		"id":          "chk_" + now.Format("20060102_150405") + "_" + util.Slug(title),
		"scope":       "project",
		"type":        "checkpoint",
		"title":       title,
		"status":      "active",
		"source_tool": sourceTool(tool),
		"created_at":  now,
		"updated_at":  now,
		"tags":        []string{tool, event, "checkpoint"},
	}
	body := "# Checkpoint: " + title + "\n\n" +
		"## Source State\n" + sourceStateSummary(statePath) + "\n\n" +
		"## Recovery Summary\n" + recoverySummary(hc) + "\n\n" +
		"## Event\n" + event + "\n\n" +
		"## Payload Summary\n" + payloadSummary(safePayload) + "\n"
	data, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(env.ProjectWT, "state", "checkpoints", now.Format("20060102-150405")+"-"+event+".md")
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	if err := wlog.Append(env.ProjectWT, "state.checkpoint", meta["id"].(string), "hook:"+tool+"-"+event, map[string]any{"path": path}); err != nil {
		return "", err
	}
	return path, nil
}

func writeHandoffCandidate(env paths.Env, tool, event string, payload map[string]any, hc hookContext) (string, error) {
	now := time.Now()
	title := titleFromHookContext(hc, event)
	session := sessionFromPayload(tool, payload)
	safePayload := durablePayload(tool, payload)
	candidate := model.Candidate{
		Schema:          model.SchemaCandidate,
		ID:              "cand_" + now.Format("20060102_150405") + "_" + util.Slug(title),
		Scope:           "project",
		CandidateType:   "handoff",
		TargetPath:      "handoffs/" + now.Format("20060102-150405") + "-" + util.Slug(title) + ".md",
		Title:           title,
		Summary:         "Draft handoff candidate generated by " + tool + " " + event + " hook.",
		Operation:       "create",
		Status:          "pending",
		SourceSessions:  optionalList(session),
		RedactionStatus: "clean",
		CreatedAt:       now,
		UpdatedAt:       now,
		Tags:            []string{tool, event, "hook-generated"},
	}
	body := "# Candidate: " + title + "\n\n" +
		"## Proposed Content\n" +
		"Draft handoff generated from the " + tool + " `" + event + "` hook.\n\n" +
		"## Source Evidence\n" + payloadSummary(safePayload) + "\n\n" +
		"## Review Required\n" +
		"This candidate is pending. Hooks never promote, merge, discard, restore, retire, delete, or replace knowledge.\n"
	data, err := store.RenderMarkdown(candidate, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(env.ProjectWT, "candidates", "project", candidate.ID+".md")
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	if err := wlog.Append(env.ProjectWT, "candidate.create", candidate.ID, "hook:"+tool+"-"+event, map[string]any{"path": path, "status": "pending"}); err != nil {
		return "", err
	}
	return path, nil
}

func shouldCreateOperationalHandoff(hc hookContext) bool {
	if !hc.HasSignal {
		return false
	}
	if strings.TrimSpace(hc.NextStep) != "" {
		return true
	}
	return strings.TrimSpace(hc.CurrentGoal) != "" && strings.TrimSpace(hc.RecentAssistant) != ""
}

func stateBody(title, tool, event string, payload map[string]any, hc hookContext) string {
	return "# State Capsule: " + title + "\n\n" +
		"## Original Intent\nCaptured from " + tool + " hook event `" + event + "`.\n\n" +
		"## Current Goal\n" + valueOrUnknown(hc.CurrentGoal) + "\n\n" +
		"## Constraints\nHooks may create state, checkpoints, candidates, and logs, but never promote.\n\n" +
		"## Relevant Context\n" + valueOrUnknown(hc.RecentAssistant) + "\n\n" +
		"## Evidence\n" + payloadSummary(payload) + "\n\n" +
		"## Work Done\n" + valueOrUnknown(hc.WorkDone) + "\n\n" +
		"## Validation\n" + valueOrUnknown(hc.Validation) + "\n\n" +
		"## Open Questions\nReview generated candidates before promotion.\n\n" +
		"## Next Step\n" + valueOrDefault(hc.NextStep, "Run `/worktrail-review` when ready to inspect pending candidates.") + "\n"
}

func titleFromPayload(payload map[string]any, fallback string) string {
	for _, key := range []string{"task", "prompt", "message", "summary", "transcript_summary"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "Worktrail " + fallback
}

type hookContext struct {
	HasSignal       bool
	CurrentGoal     string
	RecentDecision  string
	RecentAssistant string
	WorkDone        string
	Validation      string
	NextStep        string
}

func hookContextFromPayload(tool string, payload map[string]any) hookContext {
	hc := hookContext{
		CurrentGoal:     firstPayloadString(payload, "task", "prompt", "message"),
		RecentAssistant: firstPayloadString(payload, "transcript_summary", "summary"),
		Validation:      payloadStringList(payload, "commands"),
	}
	if hc.CurrentGoal != "" || hc.RecentAssistant != "" {
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
				hc.CurrentGoal = compactText(msg.Content, 300)
			}
			if looksLikeDecision(msg.Content) {
				hc.RecentDecision = compactText(msg.Content, 300)
			}
		case "assistant":
			hc.RecentAssistant = compactText(msg.Content, 300)
			if looksLikeValidation(msg.Content) {
				hc.Validation = compactText(msg.Content, 300)
			}
			hc.WorkDone = compactText(msg.Content, 300)
		}
	}
	hc.HasSignal = hc.CurrentGoal != "" || hc.RecentAssistant != ""
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

func sourceStateSummary(statePath string) string {
	if strings.TrimSpace(statePath) == "" {
		return "No active state was written because no meaningful task context was available."
	}
	return statePath
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
	b.WriteString(valueOrUnknown(hc.Validation))
	b.WriteString("\n- Next safe action: ")
	b.WriteString(valueOrDefault(hc.NextStep, "Review the latest state, checkpoint, and pending candidates."))
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

func looksLikeDecision(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "decided") || strings.Contains(lower, "decision") || strings.Contains(lower, "采纳") || strings.Contains(lower, "决定")
}

func looksLikeValidation(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "go test") || strings.Contains(lower, "validation") || strings.Contains(lower, "passed") || strings.Contains(lower, "测试")
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
				values = append(values, strings.TrimSpace(s))
			}
		}
		return strings.Join(values, "\n")
	case []string:
		return strings.Join(v, "\n")
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func sessionFromPayload(tool string, payload map[string]any) string {
	for _, key := range []string{"session_id", "conversation_id", "transcript_id"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			if tool == "cursor" {
				return "cursor:" + shortHash(value)
			}
			return strings.TrimSpace(value)
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

func optionalList(value string) []string {
	if value == "" {
		return nil
	}
	return []string{value}
}

func payloadSummary(payload map[string]any) string {
	if len(payload) == 0 {
		return "No hook payload was provided.\n"
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v\n", payload)
	}
	return "```json\n" + string(data) + "\n```\n"
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

func normalizeEvent(event string) string {
	switch event {
	case "preCompact":
		return "pre-compact"
	case "postCompact":
		return "post-compact"
	case "sessionEnd":
		return "session-end"
	default:
		return event
	}
}

func durablePayload(tool string, payload map[string]any) map[string]any {
	if tool != "cursor" {
		return payload
	}
	safe := map[string]any{}
	for key, value := range payload {
		switch key {
		case "user_email":
			continue
		case "transcript_path":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				safe[key] = filepath.Base(s)
			}
		case "workspace_roots":
			safe[key] = workspaceRootBasenames(value)
		case "conversation_id", "session_id", "generation_id", "transcript_id":
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				safe[key] = shortHash(s)
			}
		default:
			safe[key] = value
		}
	}
	return safe
}

func workspaceRootBasenames(value any) any {
	items, ok := value.([]any)
	if !ok {
		return value
	}
	roots := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
			roots = append(roots, filepath.Base(s))
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
