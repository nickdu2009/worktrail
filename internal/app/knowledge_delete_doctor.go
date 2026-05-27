package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/paths"
)

type deleteDoctorReport struct {
	Schema   string                `json:"schema"`
	Scope    string                `json:"scope"`
	Target   string                `json:"target"`
	Safe     bool                  `json:"safe"`
	Blockers []deleteDoctorFinding `json:"blockers,omitempty"`
	Warnings []deleteDoctorFinding `json:"warnings,omitempty"`
}

type deleteDoctorFinding struct {
	Scope   string `json:"scope,omitempty"`
	Kind    string `json:"kind"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
}

func runDoctorDelete(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printDeleteDoctorHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	target := firstArg(positional, flagValue(flags, "path", ""))
	target = filepath.ToSlash(strings.TrimSpace(target))
	if target == "" {
		return fmt.Errorf("doctor delete requires a target path")
	}
	if !knowledge.IsFormalKnowledgePath(target) {
		return fmt.Errorf("doctor delete only supports formal knowledge paths, got %q", target)
	}
	report := buildDeleteDoctorReport(env, scope, target)
	if flagValue(flags, "format", "text") == "json" {
		if err := json.NewEncoder(ioctx.Out).Encode(report); err != nil {
			return err
		}
	} else {
		renderDeleteDoctorText(ioctx.Out, report)
	}
	if !report.Safe {
		return fmt.Errorf("doctor delete found blockers for %s", target)
	}
	return nil
}

func printDeleteDoctorHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail doctor delete [--scope project|user|all] [--format text|json] <path>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checks whether a formal knowledge path is still referenced by index state, starter docs, candidates, or governance files before deletion.")
}

func buildDeleteDoctorReport(env paths.Env, scope, target string) deleteDoctorReport {
	if scope == "" {
		scope = "project"
	}
	report := deleteDoctorReport{
		Schema: "worktrail.doctor.delete.v1",
		Scope:  scope,
		Target: target,
		Safe:   true,
	}
	scopes := []string{scope}
	if scope == "all" {
		scopes = []string{"project", "user"}
	}
	for _, itemScope := range scopes {
		root, err := env.ScopeRoot(itemScope)
		if err != nil {
			report.Warnings = append(report.Warnings, deleteDoctorFinding{Scope: itemScope, Kind: "scope_error", Message: err.Error()})
			continue
		}
		docs, err := scanKnowledgeDocs(root, itemScope)
		if err != nil {
			report.Warnings = append(report.Warnings, deleteDoctorFinding{Scope: itemScope, Kind: "scan_error", Path: root, Message: err.Error()})
			continue
		}
		report.Blockers = append(report.Blockers, deleteDoctorBlockersFromDocs(itemScope, docs, target)...)
		report.Warnings = append(report.Warnings, deleteDoctorWarningsFromDocs(itemScope, docs, target)...)
		report.Blockers = append(report.Blockers, deleteDoctorCandidateBlockers(env, itemScope, target)...)
		report.Warnings = append(report.Warnings, deleteDoctorCandidateWarnings(env, itemScope, target)...)
		report.Warnings = append(report.Warnings, deleteDoctorIndexWarnings(itemScope, root, target)...)
		if itemScope == "project" {
			report.Warnings = append(report.Warnings, deleteDoctorGovernanceWarnings(env.ProjectRoot, target)...)
		}
	}
	report.Blockers = dedupeDeleteDoctorFindings(report.Blockers)
	report.Warnings = dedupeDeleteDoctorFindings(report.Warnings)
	report.Safe = len(report.Blockers) == 0
	return report
}

func deleteDoctorBlockersFromDocs(scope string, docs []knowledgeDoc, target string) []deleteDoctorFinding {
	var out []deleteDoctorFinding
	for _, doc := range docs {
		if !knowledge.IsFormalKnowledgePath(doc.Path) {
			continue
		}
		for _, old := range doc.Supersedes {
			if filepath.ToSlash(strings.TrimSpace(old)) == target {
				out = append(out, deleteDoctorFinding{
					Scope:   scope,
					Kind:    "supersedes",
					Path:    doc.Path,
					Message: "document supersedes the target path",
				})
			}
		}
		for _, by := range doc.SupersededBy {
			if filepath.ToSlash(strings.TrimSpace(by)) == target {
				out = append(out, deleteDoctorFinding{
					Scope:   scope,
					Kind:    "superseded_by",
					Path:    doc.Path,
					Message: "document lists the target path in superseded_by",
				})
			}
		}
		if knowledge.HasMarkdownLink(doc.Body, doc.Path, target) {
			kind := "markdown_link"
			if doc.Path == "project.md" || doc.Path == "index.md" {
				kind = "starter_link"
			}
			out = append(out, deleteDoctorFinding{
				Scope:   scope,
				Kind:    kind,
				Path:    doc.Path,
				Message: "markdown link references the target path",
			})
		}
	}
	return out
}

func deleteDoctorWarningsFromDocs(scope string, docs []knowledgeDoc, target string) []deleteDoctorFinding {
	var out []deleteDoctorFinding
	for _, doc := range docs {
		if !knowledge.IsFormalKnowledgePath(doc.Path) {
			continue
		}
		if knowledge.HasPathText(doc.Body, target) && !knowledge.HasMarkdownLink(doc.Body, doc.Path, target) {
			out = append(out, deleteDoctorFinding{
				Scope:   scope,
				Kind:    "body_text",
				Path:    doc.Path,
				Message: "plain text mentions the target path",
			})
		}
		if filepath.ToSlash(doc.Path) == target && doc.SourceOfTruth {
			out = append(out, deleteDoctorFinding{
				Scope:   scope,
				Kind:    "source_of_truth",
				Path:    doc.Path,
				Message: "target path is marked source_of_truth",
			})
		}
	}
	return out
}

func deleteDoctorCandidateBlockers(env paths.Env, scope, target string) []deleteDoctorFinding {
	manager := candidate.Manager{Env: env, Actor: "doctor-delete"}
	records, err := manager.List(scope)
	if err != nil {
		return nil
	}
	var out []deleteDoctorFinding
	for _, rec := range records {
		if filepath.ToSlash(rec.Meta.TargetPath) != target {
			continue
		}
		if rec.Meta.Status == candidate.StatusPending {
			out = append(out, deleteDoctorFinding{
				Scope:   scope,
				Kind:    "candidate_target",
				Path:    rec.Path,
				Message: "pending candidate still targets the path",
			})
		}
	}
	return out
}

func deleteDoctorCandidateWarnings(env paths.Env, scope, target string) []deleteDoctorFinding {
	manager := candidate.Manager{Env: env, Actor: "doctor-delete"}
	records, err := manager.List(scope)
	if err != nil {
		return nil
	}
	var out []deleteDoctorFinding
	for _, rec := range records {
		if filepath.ToSlash(rec.Meta.TargetPath) == target && rec.Meta.Status != candidate.StatusPending {
			out = append(out, deleteDoctorFinding{
				Scope:   scope,
				Kind:    "candidate_trail",
				Path:    rec.Path,
				Message: "applied candidate trail still points at the target path",
			})
		}
		if knowledge.HasPathText(rec.Body, target) {
			out = append(out, deleteDoctorFinding{
				Scope:   scope,
				Kind:    "candidate_body",
				Path:    rec.Path,
				Message: "candidate body mentions the target path",
			})
		}
	}
	return out
}

func deleteDoctorIndexWarnings(scope, root, target string) []deleteDoctorFinding {
	if root == "" {
		return nil
	}
	if _, err := os.Stat(root); errorsIsNotExist(err) {
		return nil
	}
	health, err := index.Health(root)
	if err != nil {
		return []deleteDoctorFinding{{Scope: scope, Kind: "index_error", Path: root, Message: err.Error()}}
	}
	var out []deleteDoctorFinding
	for _, item := range health.MissingFromFS {
		if item.Path == target {
			out = append(out, deleteDoctorFinding{Scope: scope, Kind: "stale_index", Path: item.Path, Message: "target path still exists in the index but is already missing from disk"})
		}
	}
	for _, item := range health.MissingFromIndex {
		if item.Path == target {
			out = append(out, deleteDoctorFinding{Scope: scope, Kind: "unindexed", Path: item.Path, Message: "target path exists on disk but is missing from the current index"})
		}
	}
	for _, item := range health.Changed {
		if item.Path == target {
			out = append(out, deleteDoctorFinding{Scope: scope, Kind: "changed", Path: item.Path, Message: "target path is newer than the current index"})
		}
	}
	return out
}

func deleteDoctorGovernanceWarnings(projectRoot, target string) []deleteDoctorFinding {
	if projectRoot == "" {
		return nil
	}
	var files []string
	for _, name := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(projectRoot, name)
		if _, err := os.Stat(path); err == nil {
			files = append(files, path)
		}
	}
	rulesRoot := filepath.Join(projectRoot, ".cursor", "rules")
	_ = filepath.WalkDir(rulesRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if filepath.Ext(path) == ".mdc" {
			files = append(files, path)
		}
		return nil
	})
	var out []deleteDoctorFinding
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if knowledge.HasPathText(string(data), target) {
			rel := path
			if relPath, err := filepath.Rel(projectRoot, path); err == nil {
				rel = filepath.ToSlash(relPath)
			}
			out = append(out, deleteDoctorFinding{
				Scope:   "project",
				Kind:    "governance_text",
				Path:    rel,
				Message: "governance file mentions the target path",
			})
		}
	}
	return out
}

func dedupeDeleteDoctorFindings(items []deleteDoctorFinding) []deleteDoctorFinding {
	seen := map[string]bool{}
	var out []deleteDoctorFinding
	for _, item := range items {
		key := item.Scope + "\x00" + item.Kind + "\x00" + item.Path + "\x00" + item.Message
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func renderDeleteDoctorText(out io.Writer, report deleteDoctorReport) {
	fmt.Fprintf(out, "schema: %s\nscope: %s\ntarget: %s\nsafe: %t\n", report.Schema, report.Scope, report.Target, report.Safe)
	if len(report.Blockers) > 0 {
		fmt.Fprintln(out, "blockers:")
		for _, finding := range report.Blockers {
			fmt.Fprintf(out, "- %s\t%s\t%s\t%s\n", finding.Scope, finding.Kind, finding.Path, finding.Message)
		}
	}
	if len(report.Warnings) > 0 {
		fmt.Fprintln(out, "warnings:")
		for _, finding := range report.Warnings {
			fmt.Fprintf(out, "- %s\t%s\t%s\t%s\n", finding.Scope, finding.Kind, finding.Path, finding.Message)
		}
	}
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}
