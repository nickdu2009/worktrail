package integrations

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
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
	ToolCursor Tool = "cursor"
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
	report.Checks = append(report.Checks, worktrailCommandCheck())
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
	report.Checks = append(report.Checks, worktrailCommandCheck())
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
	tool              Tool
	projectRoot       string
	rootTemplate      string
	userRootFile      string
	projectRootFile   string
	ruleTemplate      string
	userRuleFile      string
	projectRuleFile   string
	userSkillRoot     string
	projectSkillRoot  string
	visibleSkillRoots []string
	userSkills        []string
	projectSkills     []string
	projectJSONPath   string
	projectJSONTmpl   string
	userJSONs         []jsonTemplate
	projectJSONs      []jsonTemplate
}

var worktrailUserSkills = []string{
	"worktrail-context",
	"worktrail-doc-preview",
	"worktrail-init",
	"worktrail-state",
	"worktrail-handoff",
	"worktrail-import",
	"worktrail-distill",
	"worktrail-review",
	"worktrail-maintain",
}

var legacyProjectSkills = []string{
	"worktrail-context",
	"worktrail-state",
	"worktrail-handoff",
}

type jsonTemplate struct {
	path     string
	template string
}

func configFor(tool Tool, env paths.Env) (integrationConfig, error) {
	switch tool {
	case ToolCodex:
		return integrationConfig{
			tool:             tool,
			projectRoot:      env.ProjectRoot,
			rootTemplate:     "root/AGENTS.md",
			userRootFile:     filepath.Join(env.Home, ".codex", "AGENTS.md"),
			userSkillRoot:    filepath.Join(env.Home, ".codex", "skills"),
			projectSkillRoot: filepath.Join(env.ProjectRoot, ".agents", "skills"),
			userSkills:       worktrailUserSkills,
			projectJSONPath:  filepath.Join(env.ProjectRoot, ".codex", "hooks.json"),
			projectJSONTmpl:  "config/codex-hooks.json",
		}, nil
	case ToolClaude:
		return integrationConfig{
			tool:             tool,
			projectRoot:      env.ProjectRoot,
			rootTemplate:     "root/CLAUDE.md",
			userRootFile:     filepath.Join(env.Home, ".claude", "CLAUDE.md"),
			userSkillRoot:    filepath.Join(env.Home, ".claude", "skills"),
			projectSkillRoot: filepath.Join(env.ProjectRoot, ".claude", "skills"),
			userSkills:       worktrailUserSkills,
			projectJSONPath:  filepath.Join(env.ProjectRoot, ".claude", "settings.json"),
			projectJSONTmpl:  "config/claude-settings.json",
		}, nil
	case ToolCursor:
		return integrationConfig{
			tool:             tool,
			projectRoot:      env.ProjectRoot,
			ruleTemplate:     "root/cursor-worktrail.mdc",
			userRuleFile:     filepath.Join(env.Home, ".cursor", "rules", "worktrail.mdc"),
			userSkillRoot:    filepath.Join(env.Home, ".cursor", "skills"),
			projectSkillRoot: filepath.Join(env.ProjectRoot, ".cursor", "skills"),
			visibleSkillRoots: []string{
				filepath.Join(env.Home, ".cursor", "skills"),
				filepath.Join(env.Home, ".agents", "skills"),
				filepath.Join(env.Home, ".codex", "skills"),
				filepath.Join(env.Home, ".claude", "skills"),
			},
			userSkills: worktrailUserSkills,
			projectJSONs: []jsonTemplate{
				{path: filepath.Join(env.ProjectRoot, ".cursor", "mcp.json"), template: "config/cursor-mcp.json"},
				{path: filepath.Join(env.ProjectRoot, ".cursor", "hooks.json"), template: "config/cursor-hooks.json"},
			},
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
		if err := cleanupLegacyProjectAgentFiles(cfg, report); err != nil {
			return err
		}
		rootFile = cfg.projectRootFile
		skillRoot = cfg.projectSkillRoot
		skills = cfg.projectSkills
	}
	if rootFile != "" {
		rootBody, err := wtmpl.Read(cfg.rootTemplate)
		if err != nil {
			return err
		}
		rootBody = wtmpl.RenderRootTemplate(rootBody)
		if err := applyManaged(rootFile, rootBody); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: rootFile, Action: "managed-block-installed"})
	}
	ruleFile := cfg.userRuleFile
	if scope == "project" {
		ruleFile = cfg.projectRuleFile
	}
	if ruleFile != "" {
		ruleBody, err := wtmpl.Read(cfg.ruleTemplate)
		if err != nil {
			return err
		}
		ruleBody = wtmpl.RenderRootTemplate(ruleBody)
		if err := applySkillManaged(ruleFile, ruleBody); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: ruleFile, Action: "managed-block-installed"})
	}
	for _, skill := range skills {
		if cfg.tool == ToolCursor && cursorSkillVisible(cfg, skill, skillRoot) {
			report.Actions = append(report.Actions, Action{Path: cursorVisibleSkillPath(cfg, skill, skillRoot), Action: "skill-visible-via-compatible-root"})
			continue
		}
		body, err := wtmpl.Read("skills/" + skill + "/SKILL.md")
		if err != nil {
			return err
		}
		path := filepath.Join(skillRoot, skill, "SKILL.md")
		if err := applySkillManaged(path, body); err != nil {
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
	jsons := cfg.userJSONs
	if scope == "project" {
		jsons = cfg.projectJSONs
		if len(jsons) > 0 {
			if err := store.EnsureProjectGitignore(paths.Env{ProjectRoot: filepath.Dir(filepath.Dir(jsons[0].path))}); err != nil {
				return err
			}
			report.Actions = append(report.Actions, Action{Path: filepath.Join(filepath.Dir(filepath.Dir(jsons[0].path)), ".gitignore"), Action: "gitignore-managed-block-installed"})
		}
	}
	for _, jt := range jsons {
		if err := mergeJSONTemplate(jt.path, jt.template); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: jt.path, Action: "worktrail-json-merged"})
	}
	return nil
}

func uninstallScope(cfg integrationConfig, scope string, report *Report) error {
	skillRoot := cfg.userSkillRoot
	skills := cfg.userSkills
	rootFile := cfg.userRootFile
	if scope == "project" {
		if err := cleanupLegacyProjectAgentFiles(cfg, report); err != nil {
			return err
		}
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
	ruleFile := cfg.userRuleFile
	if scope == "project" {
		ruleFile = cfg.projectRuleFile
	}
	if ruleFile != "" {
		if err := removeSkillManaged(ruleFile); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: ruleFile, Action: "managed-block-removed"})
	}
	for _, skill := range skills {
		path := filepath.Join(skillRoot, skill, "SKILL.md")
		if err := removeSkillManaged(path); err != nil {
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
	jsons := cfg.userJSONs
	if scope == "project" {
		jsons = cfg.projectJSONs
	}
	for _, jt := range jsons {
		if err := removeJSONTemplate(jt.path, jt.template); err != nil {
			return err
		}
		report.Actions = append(report.Actions, Action{Path: jt.path, Action: "worktrail-json-removed"})
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
	ruleFile := cfg.userRuleFile
	if scope == "project" {
		ruleFile = cfg.projectRuleFile
	}
	if ruleFile != "" {
		report.Checks = append(report.Checks, skillCheck(scope+" rule worktrail", ruleFile))
	}
	for _, skill := range skills {
		if cfg.tool == ToolCursor {
			report.Checks = append(report.Checks, cursorSkillDoctorCheck(cfg, scope+" skill "+skill, skill, skillRoot))
		} else {
			path := filepath.Join(skillRoot, skill, "SKILL.md")
			report.Checks = append(report.Checks, skillCheck(scope+" skill "+skill, path))
		}
	}
	if scope == "project" && cfg.projectJSONPath != "" {
		projectRoot := filepath.Dir(filepath.Dir(cfg.projectJSONPath))
		report.Checks = append(report.Checks, hashManagedCheck(scope+" gitignore", filepath.Join(projectRoot, ".gitignore")))
		ok, note := jsonHasWorktrail(cfg.projectJSONPath)
		report.Checks = append(report.Checks, Check{Name: scope + " hooks/settings", Path: cfg.projectJSONPath, OK: ok, Note: note})
		report.Checks = append(report.Checks, worktrailWritableChecks(projectRoot)...)
	}
	jsons := cfg.userJSONs
	if scope == "project" {
		jsons = cfg.projectJSONs
		if len(jsons) > 0 {
			projectRoot := filepath.Dir(filepath.Dir(jsons[0].path))
			report.Checks = append(report.Checks, hashManagedCheck(scope+" gitignore", filepath.Join(projectRoot, ".gitignore")))
			report.Checks = append(report.Checks, worktrailWritableChecks(projectRoot)...)
		}
	}
	for _, jt := range jsons {
		ok, note := jsonHasTemplate(jt.path, jt.template)
		report.Checks = append(report.Checks, Check{Name: scope + " " + filepath.Base(jt.path), Path: jt.path, OK: ok, Note: note})
	}
}

func worktrailWritableChecks(projectRoot string) []Check {
	root := filepath.Join(projectRoot, ".worktrail")
	var checks []Check
	for _, rel := range []string{
		filepath.Join("state", "active"),
		filepath.Join("state", "checkpoints"),
		filepath.Join("candidates"),
		"logs",
	} {
		path := filepath.Join(root, rel)
		ok, note := checkWritable(path)
		checks = append(checks, Check{Name: "project worktrail writable " + filepath.ToSlash(rel), Path: path, OK: ok, Note: note})
	}
	return checks
}

func worktrailCommandCheck() Check {
	path, err := exec.LookPath("worktrail")
	if err != nil {
		return Check{
			Name: "worktrail command available",
			Path: "worktrail",
			OK:   false,
			Note: "not found in PATH; install the Worktrail CLI with `go install ./cmd/worktrail` or add the worktrail binary to PATH before relying on installed skills, hooks, or MCP",
		}
	}
	return Check{
		Name: "worktrail command available",
		Path: path,
		OK:   true,
		Note: "available in PATH for installed skills, hooks, and MCP",
	}
}

func checkWritable(path string) (bool, string) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return true, "not found; run worktrail init-project before using state, checkpoints, candidates, handoffs, or logs"
	}
	if err != nil {
		return false, err.Error()
	}
	if !info.IsDir() {
		return false, "not a directory"
	}
	tmp, err := os.CreateTemp(path, ".worktrail-doctor-*")
	if err != nil {
		return false, "not writable; allow sandbox write access for state, checkpoints, candidates, handoffs, and logs"
	}
	name := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return false, err.Error()
	}
	if err := os.Remove(name); err != nil {
		return false, err.Error()
	}
	return true, "writable for state, checkpoints, candidates, handoffs, and logs"
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

func applySkillManaged(path, body string) error {
	frontmatter, content, ok := splitSkillDocument(body)
	if !ok {
		return applyManaged(path, body)
	}
	next := strings.TrimSpace(frontmatter) + "\n\n" + util.ApplyManagedBlock("", content)
	return util.AtomicWrite(path, []byte(next), 0o644)
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

func removeSkillManaged(path string) error {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	next := util.RemoveManagedBlock(string(data))
	if strings.TrimSpace(next) == "" {
		return os.Remove(path)
	}
	_, rest, ok := splitSkillDocument(next)
	if ok && strings.TrimSpace(rest) == "" {
		return os.Remove(path)
	}
	return util.AtomicWrite(path, []byte(next), 0o644)
}

func cleanupLegacyProjectAgentFiles(cfg integrationConfig, report *Report) error {
	if cfg.projectRoot == "" {
		return nil
	}
	for _, path := range []string{
		filepath.Join(cfg.projectRoot, "AGENTS.md"),
		filepath.Join(cfg.projectRoot, "CLAUDE.md"),
	} {
		changed, err := removeManagedIfPresent(path)
		if err != nil {
			return err
		}
		if changed {
			report.Actions = append(report.Actions, Action{Path: path, Action: "legacy-managed-block-removed"})
		}
	}
	rulePath := filepath.Join(cfg.projectRoot, ".cursor", "rules", "worktrail.mdc")
	changed, err := removeSkillManagedIfPresent(rulePath)
	if err != nil {
		return err
	}
	if changed {
		report.Actions = append(report.Actions, Action{Path: rulePath, Action: "legacy-managed-block-removed"})
	}
	for _, root := range []string{
		filepath.Join(cfg.projectRoot, ".agents", "skills"),
		filepath.Join(cfg.projectRoot, ".claude", "skills"),
		filepath.Join(cfg.projectRoot, ".cursor", "skills"),
	} {
		for _, skill := range legacyProjectSkills {
			path := filepath.Join(root, skill, "SKILL.md")
			changed, err := removeSkillManagedIfPresent(path)
			if err != nil {
				return err
			}
			if changed {
				report.Actions = append(report.Actions, Action{Path: path, Action: "legacy-managed-block-removed"})
			}
		}
	}
	return nil
}

func removeManagedIfPresent(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	text := string(data)
	if !strings.Contains(text, util.ManagedBegin) || !strings.Contains(text, util.ManagedEnd) {
		return false, nil
	}
	next := util.RemoveManagedBlock(text)
	if strings.TrimSpace(next) == "" {
		return true, os.Remove(path)
	}
	return true, util.AtomicWrite(path, []byte(next), 0o644)
}

func removeSkillManagedIfPresent(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	text := string(data)
	if !strings.Contains(text, util.ManagedBegin) || !strings.Contains(text, util.ManagedEnd) {
		return false, nil
	}
	return true, removeSkillManaged(path)
}

func splitSkillDocument(body string) (frontmatter string, content string, ok bool) {
	body = strings.TrimLeft(body, "\ufeff")
	if !strings.HasPrefix(body, "---\n") {
		return "", "", false
	}
	end := strings.Index(body[len("---\n"):], "\n---")
	if end < 0 {
		return "", "", false
	}
	end += len("---\n")
	closeEnd := end + len("\n---")
	if len(body) > closeEnd && body[closeEnd] == '\r' {
		closeEnd++
	}
	if len(body) > closeEnd && body[closeEnd] == '\n' {
		closeEnd++
	}
	return strings.TrimSpace(body[:closeEnd]), strings.TrimSpace(body[closeEnd:]), true
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
	mergeJSONValue(current, managed)
	return writeJSONObject(path, current)
}

func mergeJSONValue(current, managed map[string]any) {
	for key, value := range managed {
		managedMap, managedOK := value.(map[string]any)
		currentMap, currentOK := current[key].(map[string]any)
		if managedOK && currentOK {
			mergeJSONValue(currentMap, managedMap)
			continue
		}
		current[key] = value
	}
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

func removeJSONTemplate(path, templatePath string) error {
	body, err := wtmpl.Read(templatePath)
	if err != nil {
		return err
	}
	var managed map[string]any
	if err := json.Unmarshal([]byte(body), &managed); err != nil {
		return err
	}
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
	removeJSONValue(current, managed)
	return writeJSONObject(path, current)
}

func removeJSONValue(current, managed map[string]any) {
	for key, value := range managed {
		if key == "version" {
			continue
		}
		switch managedValue := value.(type) {
		case map[string]any:
			if currentMap, ok := current[key].(map[string]any); ok {
				removeJSONValue(currentMap, managedValue)
				if len(currentMap) == 0 {
					delete(current, key)
				}
			}
		case []any:
			if currentSlice, ok := current[key].([]any); ok {
				current[key] = removeJSONSliceItems(currentSlice, managedValue)
				if len(current[key].([]any)) == 0 {
					delete(current, key)
				}
			}
		default:
			delete(current, key)
		}
	}
}

func removeJSONSliceItems(current, managed []any) []any {
	managedSet := map[string]bool{}
	for _, item := range managed {
		managedSet[canonicalJSON(item)] = true
	}
	next := current[:0]
	for _, item := range current {
		if !managedSet[canonicalJSON(item)] {
			next = append(next, item)
		}
	}
	return next
}

func canonicalJSON(value any) string {
	data, _ := json.Marshal(value)
	return string(data)
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

func skillCheck(name, path string) Check {
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{Name: name, Path: path, OK: false, Note: err.Error()}
	}
	text := string(data)
	_, _, hasFrontmatter := splitSkillDocument(text)
	hasManaged := strings.Contains(text, util.ManagedBegin) && strings.Contains(text, util.ManagedEnd)
	ok := hasFrontmatter && hasManaged
	note := "frontmatter and managed block present"
	if !hasFrontmatter {
		note = "missing skill frontmatter"
	} else if !hasManaged {
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

func jsonHasTemplate(path, templatePath string) (bool, string) {
	body, err := wtmpl.Read(templatePath)
	if err != nil {
		return false, err.Error()
	}
	var managed map[string]any
	if err := json.Unmarshal([]byte(body), &managed); err != nil {
		return false, err.Error()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, err.Error()
	}
	current := map[string]any{}
	if err := json.Unmarshal(data, &current); err != nil {
		return false, err.Error()
	}
	if !jsonContains(current, managed) {
		return false, "missing worktrail managed JSON entries"
	}
	return true, "worktrail managed JSON entries present"
}

func jsonContains(current, managed map[string]any) bool {
	for key, value := range managed {
		currentValue, ok := current[key]
		if !ok {
			return false
		}
		switch managedValue := value.(type) {
		case map[string]any:
			currentMap, ok := currentValue.(map[string]any)
			if !ok || !jsonContains(currentMap, managedValue) {
				return false
			}
		case []any:
			currentSlice, ok := currentValue.([]any)
			if !ok {
				return false
			}
			for _, item := range managedValue {
				if !jsonSliceContains(currentSlice, item) {
					return false
				}
			}
		default:
			if canonicalJSON(currentValue) != canonicalJSON(managedValue) {
				return false
			}
		}
	}
	return true
}

func jsonSliceContains(slice []any, item any) bool {
	want := canonicalJSON(item)
	for _, current := range slice {
		if canonicalJSON(current) == want {
			return true
		}
	}
	return false
}

func normalizeOptions(opts Options) Options {
	if !opts.User && !opts.Project {
		return Options{User: true}
	}
	return opts
}

func cursorSkillVisible(cfg integrationConfig, skill, nativeRoot string) bool {
	for _, root := range cfg.visibleSkillRoots {
		if root == nativeRoot {
			continue
		}
		if isManagedSkill(filepath.Join(root, skill, "SKILL.md")) {
			return true
		}
	}
	return false
}

func cursorVisibleSkillPath(cfg integrationConfig, skill, nativeRoot string) string {
	for _, root := range cfg.visibleSkillRoots {
		if root == nativeRoot {
			continue
		}
		path := filepath.Join(root, skill, "SKILL.md")
		if isManagedSkill(path) {
			return path
		}
	}
	return filepath.Join(nativeRoot, skill, "SKILL.md")
}

func cursorSkillDoctorCheck(cfg integrationConfig, name, skill, nativeRoot string) Check {
	var managedPaths []string
	for _, root := range cfg.visibleSkillRoots {
		path := filepath.Join(root, skill, "SKILL.md")
		if isManagedSkill(path) {
			managedPaths = append(managedPaths, path)
		}
	}
	if len(managedPaths) == 0 {
		return Check{Name: name, Path: filepath.Join(nativeRoot, skill, "SKILL.md"), OK: false, Note: "missing Cursor-visible Worktrail skill"}
	}
	note := "managed skill visible"
	if len(managedPaths) > 1 {
		note = "warning: duplicate Cursor-visible Worktrail skills: " + strings.Join(managedPaths, ", ")
	} else if !strings.HasPrefix(managedPaths[0], nativeRoot+string(os.PathSeparator)) {
		note = "managed skill visible via compatible root: " + managedPaths[0]
	}
	return Check{Name: name, Path: managedPaths[0], OK: true, Note: note}
}

func isManagedSkill(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	_, _, hasFrontmatter := splitSkillDocument(text)
	return hasFrontmatter && strings.Contains(text, util.ManagedBegin) && strings.Contains(text, util.ManagedEnd)
}
