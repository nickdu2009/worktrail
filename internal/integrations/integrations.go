package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
	wtmpl "github.com/nickdu2009/worktrail/templates"
)

type Tool string

const (
	ToolCodex  Tool = "codex"
	ToolClaude Tool = "claude"
)

type Options struct {
	User    bool
	Project bool
}

type Action struct {
	Path   string
	Action string
}

type Check struct {
	Name string
	Path string
	OK   bool
	Note string
}

type Report struct {
	Tool    Tool
	Actions []Action
	Checks  []Check
}

func Install(env paths.Env, tool Tool, opts Options) (Report, error) {
	cfg, err := configFor(tool, env)
	if err != nil {
		return Report{}, err
	}
	opts = normalizeOptions(opts)
	report := Report{Tool: tool}
	if opts.User {
		if err := installScope(cfg, "user", &report); err != nil {
			return report, err
		}
	}
	if opts.Project {
		if err := installScope(cfg, "project", &report); err != nil {
			return report, err
		}
	}
	_ = wlog.Append(env.ProjectWT, "install", string(tool), "integrations:"+string(tool), map[string]any{"user": opts.User, "project": opts.Project})
	return report, nil
}

func Uninstall(env paths.Env, tool Tool, opts Options) (Report, error) {
	cfg, err := configFor(tool, env)
	if err != nil {
		return Report{}, err
	}
	opts = normalizeOptions(opts)
	report := Report{Tool: tool}
	if opts.User {
		if err := uninstallScope(cfg, "user", &report); err != nil {
			return report, err
		}
	}
	if opts.Project {
		if err := uninstallScope(cfg, "project", &report); err != nil {
			return report, err
		}
	}
	_ = wlog.Append(env.ProjectWT, "install", string(tool), "integrations:uninstall-"+string(tool), map[string]any{"user": opts.User, "project": opts.Project})
	return report, nil
}

func Doctor(env paths.Env, tool Tool, opts Options) (Report, error) {
	cfg, err := configFor(tool, env)
	if err != nil {
		return Report{}, err
	}
	opts = normalizeOptions(opts)
	report := Report{Tool: tool}
	if opts.User {
		doctorScope(cfg, "user", &report)
	}
	if opts.Project {
		doctorScope(cfg, "project", &report)
	}
	return report, nil
}

func InstallCodex(env paths.Env, opts Options) (Report, error) {
	return Install(env, ToolCodex, opts)
}

func InstallClaude(env paths.Env, opts Options) (Report, error) {
	return Install(env, ToolClaude, opts)
}

func UninstallCodex(env paths.Env, opts Options) (Report, error) {
	return Uninstall(env, ToolCodex, opts)
}

func UninstallClaude(env paths.Env, opts Options) (Report, error) {
	return Uninstall(env, ToolClaude, opts)
}

type integrationConfig struct {
	tool             Tool
	rootTemplate     string
	userRootFile     string
	projectRootFile  string
	userSkillRoot    string
	projectSkillRoot string
	userSkills       []string
	projectSkills    []string
	projectJSONPath  string
	projectJSONTmpl  string
}

func configFor(tool Tool, env paths.Env) (integrationConfig, error) {
	switch tool {
	case ToolCodex:
		return integrationConfig{
			tool:             tool,
			rootTemplate:     "root/AGENTS.md",
			userRootFile:     filepath.Join(env.Home, ".codex", "AGENTS.md"),
			userSkillRoot:    filepath.Join(env.Home, ".codex", "skills"),
			projectSkillRoot: filepath.Join(env.ProjectRoot, ".agents", "skills"),
			userSkills:       []string{"worktrail-context", "worktrail-handoff", "worktrail-import", "worktrail-review"},
			projectJSONPath:  filepath.Join(env.ProjectRoot, ".codex", "hooks.json"),
			projectJSONTmpl:  "config/codex-hooks.json",
		}, nil
	case ToolClaude:
		return integrationConfig{
			tool:             tool,
			rootTemplate:     "root/CLAUDE.md",
			userRootFile:     filepath.Join(env.Home, ".claude", "CLAUDE.md"),
			projectRootFile:  filepath.Join(env.ProjectRoot, "CLAUDE.md"),
			userSkillRoot:    filepath.Join(env.Home, ".claude", "skills"),
			projectSkillRoot: filepath.Join(env.ProjectRoot, ".claude", "skills"),
			userSkills:       []string{"worktrail-context", "worktrail-handoff", "worktrail-import", "worktrail-review"},
			projectSkills:    []string{"worktrail-context", "worktrail-state", "worktrail-handoff"},
			projectJSONPath:  filepath.Join(env.ProjectRoot, ".claude", "settings.json"),
			projectJSONTmpl:  "config/claude-settings.json",
		}, nil
	default:
		return integrationConfig{}, fmt.Errorf("unknown integration tool %q", tool)
	}
}

func installScope(cfg integrationConfig, scope string, report *Report) error {
	skillRoot := cfg.userSkillRoot
	skills := cfg.userSkills
	rootFile := cfg.userRootFile
	if scope == "project" {
		rootFile = cfg.projectRootFile
		skillRoot = cfg.projectSkillRoot
		skills = cfg.projectSkills
	}
	if rootFile != "" {
		rootBody, err := wtmpl.Read(cfg.rootTemplate)
		if err != nil {
			return err
		}
		if err := applyManaged(rootFile, rootBody); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: rootFile, Action: "managed-block-installed"})
	}
	for _, skill := range skills {
		body, err := wtmpl.Read("skills/" + skill + "/SKILL.md")
		if err != nil {
			return err
		}
		path := filepath.Join(skillRoot, skill, "SKILL.md")
		if err := applyManaged(path, body); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: path, Action: "managed-block-installed"})
	}
	if scope == "project" && cfg.projectJSONPath != "" {
		env := paths.Env{ProjectRoot: filepath.Dir(filepath.Dir(cfg.projectJSONPath))}
		if err := store.EnsureProjectGitignore(env); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: filepath.Join(env.ProjectRoot, ".gitignore"), Action: "gitignore-managed-block-installed"})
		if err := mergeJSONTemplate(cfg.projectJSONPath, cfg.projectJSONTmpl); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: cfg.projectJSONPath, Action: "worktrail-json-merged"})
	}
	return nil
}

func uninstallScope(cfg integrationConfig, scope string, report *Report) error {
	skillRoot := cfg.userSkillRoot
	skills := cfg.userSkills
	rootFile := cfg.userRootFile
	if scope == "project" {
		rootFile = cfg.projectRootFile
		skillRoot = cfg.projectSkillRoot
		skills = cfg.projectSkills
	}
	if rootFile != "" {
		if err := removeManaged(rootFile); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: rootFile, Action: "managed-block-removed"})
	}
	for _, skill := range skills {
		path := filepath.Join(skillRoot, skill, "SKILL.md")
		if err := removeManaged(path); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: path, Action: "managed-block-removed"})
	}
	if scope == "project" && cfg.projectJSONPath != "" {
		if err := removeJSONWorktrail(cfg.projectJSONPath); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: cfg.projectJSONPath, Action: "worktrail-json-removed"})
	}
	return nil
}

func doctorScope(cfg integrationConfig, scope string, report *Report) {
	skillRoot := cfg.userSkillRoot
	skills := cfg.userSkills
	rootFile := cfg.userRootFile
	if scope == "project" {
		rootFile = cfg.projectRootFile
		skillRoot = cfg.projectSkillRoot
		skills = cfg.projectSkills
	}
	if rootFile != "" {
		report.Checks = append(report.Checks, managedCheck(scope+" root instructions", rootFile))
	}
	for _, skill := range skills {
		path := filepath.Join(skillRoot, skill, "SKILL.md")
		report.Checks = append(report.Checks, managedCheck(scope+" skill "+skill, path))
	}
	if scope == "project" && cfg.projectJSONPath != "" {
		projectRoot := filepath.Dir(filepath.Dir(cfg.projectJSONPath))
		report.Checks = append(report.Checks, hashManagedCheck(scope+" gitignore", filepath.Join(projectRoot, ".gitignore")))
		ok, note := jsonHasWorktrail(cfg.projectJSONPath)
		report.Checks = append(report.Checks, Check{Name: scope + " hooks/settings", Path: cfg.projectJSONPath, OK: ok, Note: note})
	}
}

func applyManaged(path, body string) error {
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return util.AtomicWrite(path, []byte(util.ApplyManagedBlock(existing, body)), 0o644)
}

func removeManaged(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, []byte(util.RemoveManagedBlock(string(data))), 0o644)
}

func mergeJSONTemplate(path, templatePath string) error {
	body, err := wtmpl.Read(templatePath)
	if err != nil {
		return err
	}
	var managed map[string]any
	if err := json.Unmarshal([]byte(body), &managed); err != nil {
		return err
	}
	current := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		if err := json.Unmarshal(data, &current); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for key, value := range managed {
		current[key] = value
	}
	return writeJSONObject(path, current)
}

func removeJSONWorktrail(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	current := map[string]any{}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, &current); err != nil {
		return err
	}
	delete(current, "worktrail")
	return writeJSONObject(path, current)
}

func writeJSONObject(path string, value map[string]any) error {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ordered := make(map[string]any, len(value))
	for _, key := range keys {
		ordered[key] = value[key]
	}
	data, err := json.MarshalIndent(ordered, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), 0o644)
}

func managedCheck(name, path string) Check {
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{Name: name, Path: path, OK: false, Note: err.Error()}
	}
	ok := strings.Contains(string(data), util.ManagedBegin) && strings.Contains(string(data), util.ManagedEnd)
	note := "managed block present"
	if !ok {
		note = "missing managed block"
	}
	return Check{Name: name, Path: path, OK: ok, Note: note}
}

func hashManagedCheck(name, path string) Check {
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{Name: name, Path: path, OK: false, Note: err.Error()}
	}
	ok := strings.Contains(string(data), util.HashManagedBegin) && strings.Contains(string(data), util.HashManagedEnd)
	note := "managed block present"
	if !ok {
		note = "missing managed block"
	}
	return Check{Name: name, Path: path, OK: ok, Note: note}
}

func jsonHasWorktrail(path string) (bool, string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err.Error()
	}
	current := map[string]any{}
	if err := json.Unmarshal(data, &current); err != nil {
		return false, err.Error()
	}
	if _, ok := current["worktrail"]; !ok {
		return false, "missing worktrail key"
	}
	return true, "worktrail key present"
}

func normalizeOptions(opts Options) Options {
	if !opts.User && !opts.Project {
		return Options{User: true}
	}
	return opts
}
