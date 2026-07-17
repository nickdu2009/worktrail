package hooks

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/paths"
)

type guardResult struct {
	Deny       bool
	AuditOnly  bool
	ReasonCode string
	Message    string
	Path       string
}

var (
	shellRedirectPattern = regexp.MustCompile(`(?:(?:>>|>)\s+|tee(?:\s+(?:-a|--append))*\s+(?:--\s+)?)(?:"([^"]+)"|'([^']+)'|([^|&;'"$\s]+))`)
	shellCopyMoveRm      = regexp.MustCompile(`^(?:cp|mv|rm)(?:\s+\S+)+\s*$`)
	worktrailControlled  = regexp.MustCompile(`(?:^|[;&|]\s*)worktrail\s+(?:draft|adr|review)(?:\s|$)`)
)

func evaluateShellGuard(env paths.Env, command string) guardResult {
	command = strings.TrimSpace(command)
	if command == "" {
		return guardResult{ReasonCode: "shell_empty"}
	}
	targets := shellWriteTargets(command)
	// Controlled draft/adr/review still must not redirect/tee into formal paths.
	if worktrailControlled.MatchString(command) {
		if len(targets) == 1 {
			if decision := formalPathDecision(env, targets[0], "shell_formal_path_deny"); decision.Deny {
				return decision
			}
		} else if len(targets) > 1 {
			return guardResult{AuditOnly: true, ReasonCode: "shell_multi_target_audit_only"}
		}
		if !containsShellComplexity(command) {
			return guardResult{ReasonCode: "controlled_worktrail_cli_allowed"}
		}
		return guardResult{AuditOnly: true, ReasonCode: "shell_complex_audit_only"}
	}
	if containsShellComplexity(command) {
		return guardResult{AuditOnly: true, ReasonCode: "shell_complex_audit_only"}
	}
	if len(targets) == 0 {
		return guardResult{ReasonCode: "shell_no_write_target"}
	}
	if len(targets) != 1 {
		return guardResult{AuditOnly: true, ReasonCode: "shell_multi_target_audit_only"}
	}
	return formalPathDecision(env, targets[0], "shell_formal_path_deny")
}

func evaluateMCPGuard(env paths.Env, toolName string, input map[string]any) guardResult {
	lower := strings.ToLower(toolName)
	writeLike := strings.Contains(lower, "write") || strings.Contains(lower, "edit") || strings.Contains(lower, "create") || strings.Contains(lower, "delete") || strings.Contains(lower, "apply")
	if !writeLike {
		return guardResult{ReasonCode: "mcp_read_or_unknown_allowed"}
	}
	path := firstString(input, "path", "file_path")
	if path == "" {
		return guardResult{AuditOnly: true, ReasonCode: "mcp_write_missing_path_audit_only"}
	}
	return formalPathDecision(env, path, "mcp_formal_path_deny")
}

func evaluateFileEditAudit(env paths.Env, path string) guardResult {
	decision := formalPathDecision(env, path, "file_edit_formal_path_audit")
	decision.Deny = false
	decision.AuditOnly = true
	if decision.ReasonCode == "path_not_formal" {
		decision.ReasonCode = "file_edit_non_formal_audit"
	}
	return decision
}

func evaluateCodexToolGuard(env paths.Env, toolName string, input map[string]any) guardResult {
	switch strings.TrimSpace(toolName) {
	case "Bash", "bash", "Shell", "shell":
		return evaluateShellGuard(env, firstString(input, "command"))
	case "apply_patch", "Edit", "Write", "edit", "write":
		path := firstString(input, "path", "file_path")
		if path == "" {
			// apply_patch may embed path in command/patch text; audit only when unclear
			if cmd := firstString(input, "command"); cmd != "" {
				return evaluateShellGuard(env, cmd)
			}
			return guardResult{AuditOnly: true, ReasonCode: "codex_edit_path_unclear_audit_only"}
		}
		return formalPathDecision(env, path, "codex_edit_formal_path_deny")
	default:
		if strings.HasPrefix(strings.ToLower(toolName), "mcp") || strings.Contains(toolName, "__") {
			return evaluateMCPGuard(env, toolName, input)
		}
		return guardResult{ReasonCode: "codex_tool_not_guarded"}
	}
}

func formalPathDecision(env paths.Env, rawPath, denyCode string) guardResult {
	rel, ok, reason := normalizeKnowledgeRelPath(env, rawPath)
	if !ok {
		return guardResult{AuditOnly: true, ReasonCode: reason}
	}
	if knowledge.IsFormalKnowledgePath(rel) {
		return guardResult{
			Deny:       true,
			ReasonCode: denyCode,
			Message:    "Worktrail formal knowledge path writes must go through draft/adr/review flows",
			Path:       rel,
		}
	}
	return guardResult{ReasonCode: "path_not_formal", Path: rel}
}

func normalizeKnowledgeRelPath(env paths.Env, raw string) (string, bool, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false, "path_empty"
	}
	if strings.ContainsAny(raw, "*?[]$`") || strings.Contains(raw, "$(") {
		return "", false, "path_dynamic_audit_only"
	}
	projectRoot, err := filepath.Abs(env.ProjectRoot)
	if err != nil {
		return "", false, "path_resolve_failed_audit_only"
	}
	wtRoot, err := filepath.Abs(env.ProjectWT)
	if err != nil {
		return "", false, "path_resolve_failed_audit_only"
	}
	// Prefer EvalSymlinks on roots so macOS /var -> /private/var matches targets.
	if resolvedRoot, err := filepath.EvalSymlinks(projectRoot); err == nil {
		projectRoot = resolvedRoot
	}
	if resolvedWT, err := filepath.EvalSymlinks(wtRoot); err == nil {
		wtRoot = resolvedWT
	}
	abs := raw
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(projectRoot, abs)
	}
	abs = filepath.Clean(abs)

	resolved, err := resolveNoEscape(projectRoot, abs)
	if err != nil {
		return "", false, "path_resolve_failed_audit_only"
	}
	if resolvedEval, err := filepath.EvalSymlinks(resolved); err == nil {
		resolved = resolvedEval
	}
	relWT, err := filepath.Rel(wtRoot, resolved)
	if err != nil {
		return "", false, "path_outside_project_audit_only"
	}
	relWT = filepath.ToSlash(relWT)
	if strings.HasPrefix(relWT, "../") || relWT == ".." {
		relProj, err := filepath.Rel(projectRoot, resolved)
		if err != nil || strings.HasPrefix(filepath.ToSlash(relProj), "../") {
			return "", false, "path_outside_project_audit_only"
		}
		return "", true, "path_not_formal"
	}
	return relWT, true, "ok"
}

func resolveNoEscape(root, target string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	// Walk parents with Lstat; if a symlink exists, EvalSymlinks the prefix.
	current := targetAbs
	for {
		info, err := os.Lstat(current)
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			suffix, err := filepath.Rel(current, targetAbs)
			if err != nil {
				return filepath.EvalSymlinks(targetAbs)
			}
			joined := filepath.Join(resolved, suffix)
			return filepath.Abs(joined)
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
		if current == rootAbs {
			break
		}
	}
	if _, err := os.Lstat(targetAbs); err == nil {
		if resolved, err := filepath.EvalSymlinks(targetAbs); err == nil {
			return resolved, nil
		}
	}
	return targetAbs, nil
}

func shellWriteTargets(command string) []string {
	var targets []string
	for _, match := range shellRedirectPattern.FindAllStringSubmatch(command, -1) {
		for i := 1; i < len(match); i++ {
			if strings.TrimSpace(match[i]) != "" {
				targets = append(targets, match[i])
				break
			}
		}
	}
	fields := strings.Fields(command)
	if len(fields) >= 2 {
		switch fields[0] {
		case "cp", "mv", "rm":
			if shellCopyMoveRm.MatchString(command) {
				targets = append(targets, fields[len(fields)-1])
			}
		}
	}
	return uniqueStrings(targets)
}

func containsShellComplexity(command string) bool {
	if strings.ContainsAny(command, "`") {
		return true
	}
	if strings.Contains(command, "$(") || strings.Contains(command, "${") {
		return true
	}
	if strings.Contains(command, "|") || strings.Contains(command, "&&") || strings.Contains(command, "||") || strings.Contains(command, ";") {
		// allow simple worktrail controlled already handled; treat remaining as complex
		return true
	}
	if strings.ContainsAny(command, "*?[") {
		return true
	}
	return false
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := values[key].(string); ok && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func uniqueStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
