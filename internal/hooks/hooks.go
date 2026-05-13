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
	if err := wlog.Append(root, "hook.run", "", actor, map[string]any{"tool": tool, "event": event, "payload": payload}); err != nil {
		return err
	}

	switch event {
	case "stop", "pre-compact", "post-compact", "session-end":
		statePath, err := writeState(env, tool, event, payload)
		if err != nil {
			return err
		}
		result.State = statePath
		if event == "pre-compact" || event == "post-compact" {
			checkpoint, err := writeCheckpoint(env, tool, event, payload, statePath)
			if err != nil {
				return err
			}
			result.Checkpoint = checkpoint
		}
		if event == "stop" || event == "session-end" {
			candidate, err := writeHandoffCandidate(env, tool, event, payload)
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
		filepath.Join(root, "logs"),
	)
}

func writeState(env paths.Env, tool, event string, payload map[string]any) (string, error) {
	now := time.Now()
	title := titleFromPayload(payload, event)
	session := sessionFromPayload(payload)
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
	body := stateBody(title, tool, event, payload)
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
		"## Payload Summary\n" + payloadSummary(payload) + "\n"
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
	session := sessionFromPayload(payload)
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
		"## Source Evidence\n" + payloadSummary(payload) + "\n\n" +
		"## Review Required\n" +
		"This candidate is pending. Hooks never promote, merge, discard, delete, or replace knowledge.\n"
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

func sessionFromPayload(payload map[string]any) string {
	for _, key := range []string{"session_id", "conversation_id", "transcript_id"} {
		if value, ok := payload[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func sourceTool(tool string) string {
	if tool == "claude" {
		return "claude-code"
	}
	return "codex"
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
		return fmt.Errorf("usage: hook <codex|claude> <event>")
	}
	if args[0] != "codex" && args[0] != "claude" {
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
