package triggereval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/transcript"
)

func CollectWorktrailEvidence(projectWT string) Evidence {
	var e Evidence
	e.WorktrailArtifacts = collectCandidates(filepath.Join(projectWT, "candidates"), projectWT)
	e.WorktrailLogs = collectLogs(filepath.Join(projectWT, "logs", "events.jsonl"), projectWT)
	return e
}

func CollectCodexTranscriptEvidence(home, projectRoot string, explicitPaths []string) Evidence {
	var e Evidence
	seen := map[string]bool{}
	for _, path := range explicitPaths {
		path = strings.TrimSpace(path)
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		e = MergeEvidence(e, collectCodexTranscriptPath(path))
	}
	sessions, err := transcript.DiscoverCodexSessions(home, projectRoot)
	if err != nil {
		return e
	}
	sort.SliceStable(sessions, func(i, j int) bool {
		return sessions[i].UpdatedAt.Before(sessions[j].UpdatedAt)
	})
	for _, session := range sessions {
		if seen[session.Path] {
			continue
		}
		seen[session.Path] = true
		e = MergeEvidence(e, collectCodexTranscriptPath(session.Path))
	}
	return e
}

func CollectCodexJSONLTextEvidence(text string) Evidence {
	if strings.TrimSpace(text) == "" {
		return Evidence{}
	}
	e := Evidence{}
	tr, err := transcript.ParseCodexJSONL(strings.NewReader(text))
	if err == nil {
		for _, msg := range tr.Messages {
			if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
				e.AssistantMessages = append(e.AssistantMessages, msg.Content)
			}
		}
	}
	commands, mutating := scanCodexTranscriptCommands(strings.NewReader(text))
	e.CommandsObserved = append(e.CommandsObserved, commands...)
	e.MutatingCommandsObserved = append(e.MutatingCommandsObserved, mutating...)
	return e
}

func MergeEvidence(base Evidence, extra Evidence) Evidence {
	base.TranscriptPaths = append(base.TranscriptPaths, extra.TranscriptPaths...)
	base.AssistantMessages = append(base.AssistantMessages, extra.AssistantMessages...)
	base.CommandsObserved = append(base.CommandsObserved, extra.CommandsObserved...)
	base.WorktrailArtifacts = append(base.WorktrailArtifacts, extra.WorktrailArtifacts...)
	base.WorktrailLogs = append(base.WorktrailLogs, extra.WorktrailLogs...)
	base.MutatingCommandsObserved = append(base.MutatingCommandsObserved, extra.MutatingCommandsObserved...)
	if base.RunnerStdout == "" {
		base.RunnerStdout = extra.RunnerStdout
	}
	if base.RunnerStderr == "" {
		base.RunnerStderr = extra.RunnerStderr
	}
	if base.SkipReason == "" {
		base.SkipReason = extra.SkipReason
	}
	return base
}

func collectCodexTranscriptPath(path string) Evidence {
	e := Evidence{TranscriptPaths: []string{path}}
	f, err := os.Open(path)
	if err != nil {
		e.SkipReason = fmt.Sprintf("read codex transcript: %v", err)
		return e
	}
	tr, err := transcript.ParseCodexJSONL(f)
	_ = f.Close()
	if err == nil {
		for _, msg := range tr.Messages {
			if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
				e.AssistantMessages = append(e.AssistantMessages, msg.Content)
			}
		}
	}
	f, err = os.Open(path)
	if err != nil {
		return e
	}
	defer f.Close()
	commands, mutating := scanCodexTranscriptCommands(f)
	e.CommandsObserved = append(e.CommandsObserved, commands...)
	e.MutatingCommandsObserved = append(e.MutatingCommandsObserved, mutating...)
	return e
}

func scanCodexTranscriptCommands(r io.Reader) ([]string, []string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	var commands []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue
		}
		commands = append(commands, commandsFromRaw(raw)...)
	}
	commands = uniqueStrings(commands)
	return commands, mutatingFromCommands(commands)
}

func commandsFromRaw(raw map[string]any) []string {
	var commands []string
	var walk func(any, string)
	walk = func(value any, parentKey string) {
		switch v := value.(type) {
		case map[string]any:
			for key, child := range v {
				lower := strings.ToLower(key)
				switch lower {
				case "command", "cmd":
					commands = append(commands, commandValue(child)...)
				case "argv":
					if cmd := argvCommand(child); cmd != "" {
						commands = append(commands, cmd)
					}
				case "arguments", "args", "input":
					walkArgumentValue(child, lower, walk)
				default:
					walk(child, lower)
				}
			}
		case []any:
			if parentKey == "argv" {
				if cmd := argvCommand(v); cmd != "" {
					commands = append(commands, cmd)
				}
				return
			}
			for _, child := range v {
				walk(child, parentKey)
			}
		}
	}
	walk(raw, "")
	return uniqueStrings(commands)
}

func walkArgumentValue(value any, key string, walk func(any, string)) {
	switch v := value.(type) {
	case string:
		var nested any
		if err := json.Unmarshal([]byte(v), &nested); err == nil {
			walk(nested, key)
		}
	case map[string]any, []any:
		walk(v, key)
	}
}

func commandValue(value any) []string {
	switch v := value.(type) {
	case string:
		v = strings.TrimSpace(v)
		if v != "" {
			return []string{v}
		}
	case []any:
		if cmd := argvCommand(v); cmd != "" {
			return []string{cmd}
		}
	}
	return nil
}

func argvCommand(value any) string {
	var parts []string
	switch v := value.(type) {
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, s)
			}
		}
	case []string:
		for _, item := range v {
			if strings.TrimSpace(item) != "" {
				parts = append(parts, item)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}

func mutatingFromCommands(commands []string) []string {
	var out []string
	for _, cmd := range commands {
		if isMutatingCommand(cmd) {
			out = append(out, cmd)
		}
	}
	return uniqueStrings(out)
}

func RedactEvidence(e Evidence, roots ...string) Evidence {
	e.TranscriptPaths = redactStrings(e.TranscriptPaths, roots...)
	e.AssistantMessages = redactStrings(e.AssistantMessages, roots...)
	e.CommandsObserved = redactStrings(e.CommandsObserved, roots...)
	e.MutatingCommandsObserved = redactStrings(e.MutatingCommandsObserved, roots...)
	e.RunnerStdout = redactText(e.RunnerStdout, roots...)
	e.RunnerStderr = redactText(e.RunnerStderr, roots...)
	e.SkipReason = redactText(e.SkipReason, roots...)
	e.WorktrailArtifacts = redactRecords(e.WorktrailArtifacts, roots...)
	e.WorktrailLogs = redactRecords(e.WorktrailLogs, roots...)
	return e
}

func collectCandidates(dir string, root string) []EvidenceRecord {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var records []EvidenceRecord
	for _, entry := range entries {
		if entry.IsDir() {
			records = append(records, collectCandidates(filepath.Join(dir, entry.Name()), root)...)
			continue
		}
		if filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		doc, err := store.ParseMarkdown(data)
		if err != nil {
			continue
		}
		rel, _ := filepath.Rel(root, path)
		fields := map[string]string{
			"source_type": "candidate",
		}
		copyMeta(fields, doc.Meta, "schema")
		copyMeta(fields, doc.Meta, "id")
		copyMeta(fields, doc.Meta, "candidate_type")
		copyMeta(fields, doc.Meta, "status")
		copyMeta(fields, doc.Meta, "target_path")
		if status := fields["status"]; status != "" {
			fields["candidate_status"] = status
		}
		records = append(records, EvidenceRecord{Source: "candidate", Path: rel, Fields: fields})
	}
	sort.SliceStable(records, func(i, j int) bool { return records[i].Path < records[j].Path })
	return records
}

func collectLogs(path string, root string) []EvidenceRecord {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	rel, _ := filepath.Rel(root, path)
	var records []EvidenceRecord
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}
		fields := map[string]string{"source_type": "event"}
		if value, ok := event["event"]; ok {
			fields["event_type"] = fmt.Sprint(value)
		}
		if value, ok := event["id"]; ok {
			fields["id"] = fmt.Sprint(value)
		}
		if value, ok := event["actor"]; ok {
			fields["actor"] = fmt.Sprint(value)
		}
		if data, ok := event["data"].(map[string]any); ok {
			for key, value := range data {
				if key == "" {
					continue
				}
				fields[key] = fmt.Sprint(value)
			}
		}
		records = append(records, EvidenceRecord{Source: "event", Path: rel, Fields: fields})
	}
	return records
}

func copyMeta(fields map[string]string, meta map[string]any, key string) {
	if value, ok := meta[key]; ok {
		fields[key] = fmt.Sprint(value)
	}
}

func redactRecords(records []EvidenceRecord, roots ...string) []EvidenceRecord {
	out := make([]EvidenceRecord, 0, len(records))
	for _, rec := range records {
		next := rec
		next.Path = redactText(next.Path, roots...)
		next.Summary = redactText(next.Summary, roots...)
		if rec.Fields != nil {
			next.Fields = map[string]string{}
			for key, value := range rec.Fields {
				next.Fields[key] = redactText(value, roots...)
			}
		}
		out = append(out, next)
	}
	return out
}

func redactStrings(values []string, roots ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, redactText(value, roots...))
	}
	return out
}

func redactText(text string, roots ...string) string {
	out := text
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		out = strings.ReplaceAll(out, root, "[REDACTED:path]")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		out = strings.ReplaceAll(out, home, "[REDACTED:home]")
	}
	if tmp := os.TempDir(); tmp != "" {
		out = strings.ReplaceAll(out, tmp, "[REDACTED:tmp]")
		out = strings.ReplaceAll(out, filepath.Clean(tmp), "[REDACTED:tmp]")
	}
	out = regexp.MustCompile(`/var/folders/[^\s"')]+`).ReplaceAllString(out, "[REDACTED:tmp]")
	out = regexp.MustCompile(`/tmp/[^\s"')]+`).ReplaceAllString(out, "[REDACTED:tmp]")
	return redact.Scan(out).Text
}
