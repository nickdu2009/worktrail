package store

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

type Config struct {
	Schema    string    `json:"schema"`
	Scope     string    `json:"scope"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

func InitUser(env paths.Env) error {
	dirs := []string{
		"profile", "workflows", "prompts", "lessons",
		"state/active", "state/checkpoints", "state/archived",
		"candidates/user", "raw/codex", "raw/claude",
		"exports", "index", "logs",
	}
	if err := makeTree(env.UserRoot, dirs); err != nil {
		return err
	}
	if err := writeConfig(env.UserRoot, "user"); err != nil {
		return err
	}
	defaults := map[string]string{
		"profile/preferences.md":               "# Preferences\n\n",
		"profile/coding-style.md":              "# Coding Style\n\n",
		"profile/architecture-style.md":        "# Architecture Style\n\n",
		"profile/tools.md":                     "# Tools\n\n",
		"workflows/bug-handoff.md":             "# Bug Handoff\n\n",
		"workflows/long-session-management.md": "# Long Session Management\n\n",
		"workflows/ai-coding-review.md":        "# AI Coding Review\n\n",
		"workflows/project-bootstrap.md":       "# Project Bootstrap\n\n",
		"prompts/codex-task.md":                "# Codex Task\n\n",
		"prompts/claude-design-review.md":      "# Claude Design Review\n\n",
		"prompts/agent-handoff.md":             "# Agent Handoff\n\n",
		"lessons/ai-coding-gotchas.md":         "# AI Coding Gotchas\n\n",
		"lessons/context-management.md":        "# Context Management\n\n",
		"index.md":                             "# Worktrail User Index\n\n",
		"log.md":                               "# Worktrail User Log\n\n",
	}
	if err := writeDefaults(env.UserRoot, defaults); err != nil {
		return err
	}
	return wlog.Append(env.UserRoot, "init", "", "cli:init-user", nil)
}

func InitProject(env paths.Env) error {
	dirs := []string{
		"decisions", "handoffs", "rules", "prompts",
		"state/active", "state/checkpoints", "state/archived",
		"candidates/project", "raw/codex", "raw/claude",
		"exports", "index", "logs",
	}
	if err := makeTree(env.ProjectWT, dirs); err != nil {
		return err
	}
	if err := writeConfig(env.ProjectWT, "project"); err != nil {
		return err
	}
	defaults := map[string]string{
		"project.md":                       "# Project\n\n",
		"current-state.md":                 "# Current State\n\n",
		"rules/coding-rules.md":            "# Coding Rules\n\n",
		"rules/testing-rules.md":           "# Testing Rules\n\n",
		"rules/security-rules.md":          "# Security Rules\n\n",
		"prompts/project-review.md":        "# Project Review\n\n",
		"prompts/generate-config-draft.md": "# Generate Config Draft\n\n",
		"index.md":                         "# Worktrail Project Index\n\n",
		"log.md":                           "# Worktrail Project Log\n\n",
	}
	if err := writeDefaults(env.ProjectWT, defaults); err != nil {
		return err
	}
	if err := mergeProjectCodexHooks(filepath.Join(env.ProjectRoot, ".codex", "hooks.json")); err != nil {
		return err
	}
	return wlog.Append(env.ProjectWT, "init", "", "cli:init-project", nil)
}

func makeTree(root string, dirs []string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func writeConfig(root, scope string) error {
	path := filepath.Join(root, "config.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	cfg := Config{Schema: "worktrail.config.v1", Scope: scope, Version: "0.1.0", CreatedAt: time.Now()}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(b, '\n'), 0o644)
}

func writeDefaults(root string, files map[string]string) error {
	for rel, body := range files {
		path := filepath.Join(root, rel)
		if _, err := os.Stat(path); err == nil {
			continue
		}
		if err := util.AtomicWrite(path, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func mergeProjectCodexHooks(path string) error {
	existing := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &existing); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	existing["worktrail"] = map[string]any{
		"mcp": map[string]any{
			"command": "worktrail",
			"args":    []string{"mcp", "serve", "--stdio"},
		},
		"hooks": map[string]any{
			"session-start": "worktrail hook codex session-start",
			"user-prompt":   "worktrail hook codex user-prompt",
			"post-tool-use": "worktrail hook codex post-tool-use",
			"stop":          "worktrail hook codex stop",
		},
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), 0o644)
}
