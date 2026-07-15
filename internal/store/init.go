package store

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

type Config struct {
	Schema    string    `json:"schema"`
	Scope     string    `json:"scope"`
	Version   string    `json:"version"`
	ProjectID string    `json:"project_id,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

var projectBootstrapKnowledgeDefaults = map[string]string{
	"project.md":                       "# Project\n\n",
	"rules/coding-rules.md":            "# Coding Rules\n\n",
	"rules/testing-rules.md":           "# Testing Rules\n\n",
	"rules/security-rules.md":          "# Security Rules\n\n",
	"prompts/project-review.md":        "# Project Review\n\n",
	"prompts/generate-config-draft.md": "# Generate Config Draft\n\n",
	"index.md":                         "# Worktrail Project Index\n\n`project.md` is the canonical manual overview for this repository.\n\n",
}

func InitUser(env paths.Env) error {
	dirs := []string{
		"profile", "workflows", "prompts", "lessons",
		"state/active", "state/checkpoints", "state/archived",
		"candidates/user", "raw/codex", "raw/claude", "raw/cursor",
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
		"index.md":                             "# Worktrail User Index\n\nProfile, workflows, prompts, and lessons under this root are the canonical user knowledge surface.\n\n",
	}
	if err := writeDefaults(env.UserRoot, defaults); err != nil {
		return err
	}
	return wlog.Append(env.UserRoot, "init", "", "cli:init-user", nil)
}

func InitProject(env paths.Env) error {
	dirs := []string{
		"requirements", "architecture", "decisions", "handoffs/local", "handoffs/team", "rules", "prompts",
		"integrations", "validation", "glossary",
		"state/active", "state/archived",
		"runtime/sessions", "runtime/checkpoints", "runtime/recovery", "runtime/migrations",
		"ops",
		"candidates/project", "raw/codex", "raw/claude", "raw/cursor",
		"exports", "index", "logs",
	}
	if err := makeTree(env.ProjectWT, dirs); err != nil {
		return err
	}
	if err := ensurePrivateDirs(
		filepath.Join(env.ProjectWT, "handoffs", "local"),
		filepath.Join(env.ProjectWT, "ops"),
	); err != nil {
		return err
	}
	if err := writeConfig(env.ProjectWT, "project"); err != nil {
		return err
	}
	if err := writeDefaults(env.ProjectWT, projectBootstrapKnowledgeDefaults); err != nil {
		return err
	}
	if err := EnsureProjectGitignore(env); err != nil {
		return err
	}
	if err := mergeProjectCodexHooks(filepath.Join(env.ProjectRoot, ".codex", "hooks.json")); err != nil {
		return err
	}
	return wlog.Append(env.ProjectWT, "init", "", "cli:init-project", nil)
}

func IsProjectBootstrapKnowledgePath(path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	_, ok := projectBootstrapKnowledgeDefaults[path]
	return ok
}

const ProjectGitignoreBody = `# Worktrail local integration installs. These are generated per developer.
.agents/
.codex/
.claude/

# Worktrail runtime/local state. Formal project knowledge files under
# .worktrail/ remain trackable by default.
.worktrail/state/
.worktrail/candidates/
.worktrail/raw/
.worktrail/index/
.worktrail/logs/
.worktrail/staging/
.worktrail/runtime/
.worktrail/handoffs/local/
.worktrail/ops/
.worktrail/derived/index/
.worktrail/derived/cache/
.worktrail/derived/exports/
.worktrail/.cache/
.worktrail/exports/
/.worktrail-handoff-v2-backups/`

const legacyProjectGitignoreBody = `# Worktrail local integration installs. These are generated per developer by
# ` + "`" + `worktrail install codex` + "`" + ` / future local integrations.
.agents/
.codex/
.claude/

# Worktrail runtime/local state. Formal project knowledge files under
# ` + "`" + `.worktrail/` + "`" + ` remain trackable by default.
.worktrail/state/
.worktrail/candidates/
.worktrail/raw/
.worktrail/index/
.worktrail/logs/
.worktrail/staging/
.worktrail/runtime/
.worktrail/derived/index/
.worktrail/derived/cache/
.worktrail/derived/exports/
.worktrail/.cache/
.worktrail/exports/`

func EnsureProjectGitignore(env paths.Env) error {
	path := filepath.Join(env.ProjectRoot, ".gitignore")
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !os.IsNotExist(err) {
		return err
	}
	existing = strings.Replace(existing, legacyProjectGitignoreBody, "", 1)
	return util.AtomicWrite(path, []byte(util.ApplyHashManagedBlock(existing, ProjectGitignoreBody)), 0o644)
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

func ensurePrivateDirs(dirs ...string) error {
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return err
		}
	}
	return nil
}

func writeConfig(root, scope string) error {
	path := filepath.Join(root, "config.json")
	if data, err := os.ReadFile(path); err == nil {
		if scope != "project" {
			return nil
		}
		var existing map[string]any
		if err := json.Unmarshal(data, &existing); err != nil {
			return err
		}
		if projectID, _ := existing["project_id"].(string); strings.TrimSpace(projectID) != "" {
			return nil
		}
		projectID, err := newProjectID()
		if err != nil {
			return err
		}
		existing["project_id"] = projectID
		b, err := json.MarshalIndent(existing, "", "  ")
		if err != nil {
			return err
		}
		return util.AtomicWrite(path, append(b, '\n'), 0o644)
	} else if !os.IsNotExist(err) {
		return err
	}
	cfg := Config{Schema: "worktrail.config.v1", Scope: scope, Version: "0.1.0", CreatedAt: time.Now()}
	if scope == "project" {
		projectID, err := newProjectID()
		if err != nil {
			return err
		}
		cfg.ProjectID = projectID
	}
	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(b, '\n'), 0o644)
}

func newProjectID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"project_%x-%x-%x-%x-%x",
		value[0:4],
		value[4:6],
		value[6:8],
		value[8:10],
		value[10:16],
	), nil
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
	existing["hooks"] = map[string]any{
		"SessionStart": "worktrail hook codex SessionStart",
		"PreCompact":   "worktrail hook codex PreCompact",
		"Stop":         "worktrail hook codex Stop",
	}
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), 0o644)
}
