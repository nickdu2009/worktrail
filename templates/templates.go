package templates

import (
	"embed"
	"fmt"
)

// FS contains Worktrail integration assets installed into Codex and Claude Code.
//
//go:embed skills/*/SKILL.md root/*.md root/*.mdc config/*.json
var FS embed.FS

func Read(path string) (string, error) {
	data, err := FS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read template %s: %w", path, err)
	}
	return string(data), nil
}
