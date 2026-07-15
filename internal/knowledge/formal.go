package knowledge

import (
	"path/filepath"
	"strings"
)

func IsFormalKnowledgePath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "project.md" || path == "index.md" {
		return true
	}
	for _, prefix := range []string{
		"architecture/",
		"decisions/",
		"requirements/",
		"workflows/",
		"validation/",
		"integrations/",
		"glossary/",
		"rules/",
		"lessons/",
		"prompts/",
	} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func IsLegacyHandoffPath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if !strings.HasPrefix(path, "handoffs/") {
		return false
	}
	name := strings.TrimPrefix(path, "handoffs/")
	return name != "" && !strings.Contains(name, "/") && strings.EqualFold(filepath.Ext(name), ".md")
}
