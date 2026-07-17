package hooks

import (
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/textsafety"
)

const maxContextBytes = 6144

func renderHookContext(env paths.Env, projectID string, task *uniqueTask) string {
	if task == nil {
		return ""
	}
	sections := map[string]string{
		"Current Goal":    extractSection(task.Capsule.Body, "Current Goal"),
		"Constraints":     extractSection(task.Capsule.Body, "Constraints"),
		"Decisions Made":  extractSection(task.Capsule.Body, "Decisions Made"),
		"Next Step":       extractSection(task.Capsule.Body, "Next Step"),
	}
	var b strings.Builder
	b.WriteString("Worktrail active task context\n")
	if projectID != "" {
		b.WriteString("project_id: ")
		b.WriteString(projectID)
		b.WriteByte('\n')
	}
	b.WriteString("task_id: ")
	b.WriteString(task.TaskID)
	b.WriteByte('\n')
	if goal := sanitizeContextField(sections["Current Goal"], 400); goal != "" {
		b.WriteString("current_goal: ")
		b.WriteString(goal)
		b.WriteByte('\n')
	}
	if constraints := sanitizeContextField(sections["Constraints"], 400); constraints != "" {
		b.WriteString("constraints: ")
		b.WriteString(constraints)
		b.WriteByte('\n')
	}
	if decision := sanitizeContextField(sections["Decisions Made"], 300); decision != "" {
		b.WriteString("recent_decision: ")
		b.WriteString(decision)
		b.WriteByte('\n')
	}
	if next := sanitizeContextField(sections["Next Step"], 300); next != "" {
		b.WriteString("next_step: ")
		b.WriteString(next)
		b.WriteByte('\n')
	}
	refs := relativeRefs(env, task.Capsule.Body, 3)
	if len(refs) > 0 {
		b.WriteString("refs:\n")
		for _, ref := range refs {
			b.WriteString("- ")
			b.WriteString(ref)
			b.WriteByte('\n')
		}
	}
	text := b.String()
	if len(text) > maxContextBytes {
		text = trimToBytes(text, maxContextBytes)
	}
	return text
}

func extractSection(body, title string) string {
	marker := "## " + title
	idx := strings.Index(body, marker)
	if idx < 0 {
		return ""
	}
	rest := body[idx+len(marker):]
	if next := strings.Index(rest, "\n## "); next >= 0 {
		rest = rest[:next]
	}
	return strings.TrimSpace(rest)
}

func sanitizeContextField(value string, limit int) string {
	value = strings.TrimSpace(strings.Join(strings.Fields(value), " "))
	if value == "" {
		return ""
	}
	if strings.Contains(value, string(filepath.Separator)) && (filepath.IsAbs(value) || strings.Contains(value, ":\\")) {
		value = filepath.Base(value)
	}
	result, err := textsafety.Process(value, textsafety.ProfileLocal)
	if err != nil {
		return "[redacted]"
	}
	value = result.Text
	if len(value) > limit {
		value = trimToBytes(value, limit)
	}
	return value
}

func relativeRefs(env paths.Env, body string, limit int) []string {
	words := strings.Fields(body)
	refs := make([]string, 0, limit)
	seen := map[string]struct{}{}
	for _, word := range words {
		word = strings.Trim(word, "`\"'()[]{},")
		if word == "" || strings.Contains(word, "://") {
			continue
		}
		if filepath.IsAbs(word) {
			continue
		}
		slash := filepath.ToSlash(word)
		if !strings.Contains(slash, "/") || strings.HasPrefix(slash, "../") {
			continue
		}
		if strings.HasPrefix(slash, ".worktrail/") || strings.HasSuffix(slash, ".md") || strings.HasPrefix(slash, "internal/") || strings.HasPrefix(slash, "docs/") {
			if _, ok := seen[slash]; ok {
				continue
			}
			seen[slash] = struct{}{}
			refs = append(refs, slash)
			if len(refs) >= limit {
				break
			}
		}
	}
	_ = env
	return refs
}

func trimToBytes(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	if limit <= 3 {
		return value[:limit]
	}
	for limit > 0 && !utf8.ValidString(value[:limit]) {
		limit--
	}
	return strings.TrimSpace(value[:limit-3]) + "..."
}

func shouldInjectContext(binding *taskBinding, task *uniqueTask) bool {
	if binding == nil || task == nil {
		return false
	}
	if binding.NeedsContextRefresh {
		return true
	}
	if binding.LastInjectedRevision == "" {
		return true
	}
	return binding.LastInjectedRevision != task.Revision
}
