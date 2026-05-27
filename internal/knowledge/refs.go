package knowledge

import (
	"path"
	"regexp"
	"strings"
)

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func HasMarkdownLink(text, basePath, target string) bool {
	target = normalizeRefPath(target)
	if target == "" {
		return false
	}
	for _, match := range markdownLinkPattern.FindAllStringSubmatch(text, -1) {
		if len(match) < 2 {
			continue
		}
		if normalizeMarkdownTarget(basePath, match[1]) == target {
			return true
		}
	}
	return false
}

func HasPathText(text, target string) bool {
	target = normalizeRefPath(target)
	if target == "" {
		return false
	}
	return strings.Contains(filepathToSlash(text), target)
}

func normalizeMarkdownTarget(basePath, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if idx := strings.Index(raw, "#"); idx >= 0 {
		raw = raw[:idx]
	}
	if idx := strings.Index(raw, "?"); idx >= 0 {
		raw = raw[:idx]
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "mailto:") {
		return ""
	}
	if strings.HasPrefix(raw, "/") {
		return normalizeRefPath(strings.TrimPrefix(raw, "/"))
	}
	if strings.HasPrefix(raw, "./") || strings.HasPrefix(raw, "../") {
		return normalizeRefPath(path.Join(path.Dir(basePath), raw))
	}
	return normalizeRefPath(raw)
}

func normalizeRefPath(raw string) string {
	raw = filepathToSlash(strings.TrimSpace(raw))
	raw = strings.TrimPrefix(raw, "./")
	if raw == "" || raw == "." {
		return ""
	}
	return path.Clean(raw)
}

func filepathToSlash(text string) string {
	return strings.ReplaceAll(text, "\\", "/")
}
