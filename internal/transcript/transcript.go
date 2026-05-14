package transcript

import (
	"bufio"
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

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/util"
)

const SchemaTranscriptMeta = "worktrail.transcript_meta.v1"

type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at,omitempty"`
	RawType   string    `json:"raw_type,omitempty"`
}

type Transcript struct {
	Source   string    `json:"source"`
	Path     string    `json:"path,omitempty"`
	Messages []Message `json:"messages"`
}

type SyncOptions struct {
	Source          string
	Scope           string
	RawMetadataOnly bool
	Now             time.Time
}

func ParseCodexJSONL(r io.Reader) (Transcript, error) {
	return parseJSONL("codex", r)
}

func ParseClaudeJSONL(r io.Reader) (Transcript, error) {
	return parseJSONL("claude", r)
}

func ParseMarkdown(source string, r io.Reader) (Transcript, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return Transcript{}, err
	}
	var messages []Message
	var role string
	var chunk []string
	flush := func() {
		content := strings.TrimSpace(strings.Join(chunk, "\n"))
		if role != "" && content != "" {
			messages = append(messages, Message{Role: role, Content: content})
		}
		chunk = nil
	}
	for _, line := range strings.Split(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(strings.Trim(trimmed, "#*: "))
		switch lower {
		case "user", "assistant", "system", "tool":
			flush()
			role = lower
		default:
			chunk = append(chunk, line)
		}
	}
	flush()
	if len(messages) == 0 && strings.TrimSpace(string(b)) != "" {
		messages = append(messages, Message{Role: "unknown", Content: strings.TrimSpace(string(b))})
	}
	return Transcript{Source: source, Messages: messages}, nil
}

func Sync(path, root string, opts SyncOptions) (model.TranscriptMeta, error) {
	if path == "" || root == "" {
		return model.TranscriptMeta{}, errors.New("transcript path and root are required")
	}
	source := opts.Source
	if source == "" {
		source = inferSource(path)
	}
	if source == "" {
		return model.TranscriptMeta{}, errors.New("transcript source is required")
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return model.TranscriptMeta{}, err
	}
	hashBytes := sha256.Sum256(b)
	id := util.Slug(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))) + "-" + hex.EncodeToString(hashBytes[:])[:12]
	rawRel := filepath.ToSlash(filepath.Join("raw", source, filepath.Base(path)))
	meta := model.TranscriptMeta{
		Schema:    SchemaTranscriptMeta,
		ID:        id,
		Source:    source,
		Path:      rawRel,
		Scope:     opts.Scope,
		Hash:      hex.EncodeToString(hashBytes[:]),
		CreatedAt: now,
	}
	rawDir := filepath.Join(root, "raw", source)
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		return model.TranscriptMeta{}, err
	}
	if !opts.RawMetadataOnly {
		if err := util.AtomicWrite(filepath.Join(root, rawRel), b, 0o600); err != nil {
			return model.TranscriptMeta{}, err
		}
	} else {
		meta.Path = filepath.ToSlash(path)
	}
	metaBytes, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return model.TranscriptMeta{}, err
	}
	metaPath := filepath.Join(rawDir, id+".metadata.json")
	if err := util.AtomicWrite(metaPath, append(metaBytes, '\n'), 0o600); err != nil {
		return model.TranscriptMeta{}, err
	}
	return meta, nil
}

func parseJSONL(source string, r io.Reader) (Transcript, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var messages []Message
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			return Transcript{}, fmt.Errorf("parse %s jsonl line %d: %w", source, lineNo, err)
		}
		msg := messageFromRaw(raw)
		if msg.Content != "" || msg.Role != "" {
			messages = append(messages, msg)
		}
	}
	if err := scanner.Err(); err != nil {
		return Transcript{}, err
	}
	return Transcript{Source: source, Messages: dedupeMirroredMessages(messages)}, nil
}

func messageFromRaw(raw map[string]any) Message {
	if payload, ok := raw["payload"].(map[string]any); ok {
		if msg := messageFromPayload(raw, payload); msg.Content != "" || msg.Role != "" {
			return msg
		}
	}
	msg := Message{
		Role:    firstString(raw, "role", "speaker", "author"),
		Content: contentFromRaw(raw),
		RawType: firstString(raw, "type", "event", "kind"),
	}
	if msg.Role == "" {
		msg.Role = roleFromNested(raw["message"])
	}
	if msg.Content == "" {
		if nested, ok := raw["message"].(map[string]any); ok {
			msg.Content = contentFromRaw(nested)
		}
	}
	if ts := firstString(raw, "created_at", "timestamp", "time"); ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			msg.CreatedAt = parsed
		}
	}
	return msg
}

func messageFromPayload(raw, payload map[string]any) Message {
	msg := Message{
		Role:    firstString(payload, "role", "speaker", "author"),
		Content: contentFromRaw(payload),
		RawType: firstString(payload, "type", "event", "kind"),
	}
	if msg.RawType == "" {
		msg.RawType = firstString(raw, "type", "event", "kind")
	}
	switch msg.RawType {
	case "user_message":
		msg.Role = "user"
		msg.Content = firstString(payload, "message", "text", "content")
	case "agent_message":
		msg.Role = "assistant"
		msg.Content = firstString(payload, "message", "text", "content")
	}
	if msg.Role == "" {
		msg.Role = roleFromNested(payload["message"])
	}
	if msg.Content == "" {
		if nested, ok := payload["message"].(map[string]any); ok {
			msg.Content = contentFromRaw(nested)
		}
	}
	if ts := firstString(raw, "timestamp", "created_at", "time"); ts != "" {
		if parsed, err := time.Parse(time.RFC3339, ts); err == nil {
			msg.CreatedAt = parsed
		}
	}
	return msg
}

func dedupeMirroredMessages(messages []Message) []Message {
	if len(messages) < 2 {
		return messages
	}
	deduped := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if len(deduped) > 0 && isMirroredMessage(deduped[len(deduped)-1], msg) {
			if msg.RawType == "message" {
				deduped[len(deduped)-1] = msg
			}
			continue
		}
		deduped = append(deduped, msg)
	}
	return deduped
}

func isMirroredMessage(a, b Message) bool {
	if a.Role != b.Role || a.Content != b.Content {
		return false
	}
	aEvent := isCodexEventMessage(a.RawType)
	bEvent := isCodexEventMessage(b.RawType)
	if !(aEvent && b.RawType == "message") && !(bEvent && a.RawType == "message") {
		return false
	}
	if a.CreatedAt.IsZero() || b.CreatedAt.IsZero() {
		return true
	}
	delta := a.CreatedAt.Sub(b.CreatedAt)
	if delta < 0 {
		delta = -delta
	}
	return delta <= 2*time.Second
}

func isCodexEventMessage(rawType string) bool {
	return rawType == "agent_message" || rawType == "user_message"
}

func firstString(raw map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := raw[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}

func contentFromRaw(raw map[string]any) string {
	for _, key := range []string{"content", "text", "message"} {
		switch v := raw[key].(type) {
		case string:
			return v
		case []any:
			var parts []string
			for _, item := range v {
				switch x := item.(type) {
				case string:
					parts = append(parts, x)
				case map[string]any:
					if text := firstString(x, "text", "content"); text != "" {
						parts = append(parts, text)
					}
				}
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n")
			}
		}
	}
	return ""
}

func roleFromNested(v any) string {
	if nested, ok := v.(map[string]any); ok {
		return firstString(nested, "role", "speaker", "author")
	}
	return ""
}

func inferSource(path string) string {
	name := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(name, "codex"):
		return "codex"
	case strings.Contains(name, "claude"):
		return "claude"
	default:
		return ""
	}
}
