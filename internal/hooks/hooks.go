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
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
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
		statePath, err := writeState(env, tool, eventKey, payload)
		if err != nil {
			return err
		}
		result.State = statePath
		if eventKey == "pre-compact" || eventKey == "post-compact" {
			checkpoint, err := writeCheckpoint(env, tool, eventKey, payload, statePath)
			if err != nil {
				return err
			}
			result.Checkpoint = checkpoint
		}
		if eventKey == "stop" || eventKey == "session-end" {
			candidate, err := writeHandoffCandidate(env, tool, eventKey, payload)
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

func writeState(env paths.Env, tool, event string, payload map[string]any) (string, error) {
	now := time.Now()
	title := titleFromPayload(payload, event)
	session := sessionFromPayload(tool, payload)
	state := model.State{
		Schema:         model.SchemaState,
		ID:             "st_" + now.Format("20060102_150405") + "_" + util.Slug(title),
		Scope:          "project",
		Type:           "implementation",
		Title:          title,
		Status:         "active",
		SourceTool:     sourceTool(tool),
		SourceSessions: optionalList(session),
		CreatedAt:      now,
		UpdatedAt:      now,
		Tags:           []string{tool, event},
	}
	body := stateBody(title, tool, event, durablePayload(tool, payload))
	data, err := store.RenderMarkdown(state, body)
	if err != nil {
		return "", err
	}
	path := filepath.Join(env.ProjectWT, "state", "active", "latest.md")
	if err := util.AtomicWrite(path, data, 0o644); err != nil {
		return "", err
	}
	if err := wlog.Append(env.ProjectWT, "state.update", state.ID, "hook:"+tool+"-"+event, map[string]any{"path": path}); err != nil {
		return "", err
	}
	return path, nil
}

func writeCheckpoint(env paths.Env, tool, event string, payload map[string]any, statePath string) (string, error) {
	now := time.Now()
	title := titleFromPayload(payload, event)
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
		"## Source State\n" + statePath + "\n\n" +
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

func writeHandoffCandidate(env paths.Env, tool, event string, payload map[string]any) (string, error) {
	now := time.Now()
	title := titleFromPayload(payload, event)
	session := sessionFromPayload(tool, payload)
	safePayload := durablePayload(tool, payload)
	candidate := model.Candidate{
		Schema:          model.SchemaCandidate,
		ID:              "cand_" + now.Format("20060102_150405") + "_" + util.Slug(title),
		Scope:           "project",
		CandidateType:   "handoff",
		TargetPath:      ".worktrail/handoffs/" + now.Format("20060102-150405") + "-" + util.Slug(title) + ".md",
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

func stateBody(title, tool, event string, payload map[string]any) string {
	return "# State Capsule: " + title + "\n\n" +
		"## Original Intent\nCaptured from " + tool + " hook event `" + event + "`.\n\n" +
		"## Current Goal\n" + title + "\n\n" +
		"## Constraints\nHooks may create state, checkpoints, candidates, and logs, but never promote.\n\n" +
		"## Evidence\n" + payloadSummary(payload) + "\n\n" +
		"## Work Done\nHook event processed and recorded.\n\n" +
		"## Validation\nNo validation command was inferred from the hook payload.\n\n" +
		"## Open Questions\nReview generated candidates before promotion.\n\n" +
		"## Next Step\nRun `/worktrail-review` when ready to inspect pending candidates.\n"
}

func titleFromPayload(payload map[string]any, fallback string) string {
	for _, key := range []string{"task", "prompt", "message", "summary", "transcript_summary"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return "Worktrail " + fallback
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
