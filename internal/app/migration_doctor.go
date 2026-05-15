package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	kddmigration "github.com/nickdu2009/worktrail/internal/migration/kdd"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/store"
)

const migrationDoctorSchema = "worktrail.doctor.migration.v1"

type migrationDoctorOptions struct {
	Root        string
	Strict      bool
	CleanupMode bool
}

type migrationDoctorReport struct {
	Schema      string                 `json:"schema"`
	GeneratedAt string                 `json:"generated_at"`
	Project     string                 `json:"project"`
	Root        string                 `json:"root"`
	OK          bool                   `json:"ok"`
	Summary     migrationDoctorSummary `json:"summary"`
	Findings    []migrationFinding     `json:"findings"`
}

type migrationDoctorSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

type migrationFinding struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

func runDoctorMigration(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printMigrationDoctorHelp(ioctx.Out)
		return nil
	}
	flags, _ := splitFlags(args)
	if flags["fix-gitignore"] == "true" {
		if err := store.EnsureProjectGitignore(env); err != nil {
			return err
		}
	}
	report := buildMigrationDoctorReport(env, migrationDoctorOptions{
		Root:   kddmigration.LegacyRoot(env, flagValue(flags, "root", "")),
		Strict: flags["strict"] == "true",
	})
	if flagValue(flags, "format", "text") == "json" {
		if err := json.NewEncoder(ioctx.Out).Encode(report); err != nil {
			return err
		}
	} else {
		renderMigrationDoctorText(ioctx.Out, report)
	}
	if report.Summary.Errors > 0 || flags["strict"] == "true" && report.Summary.Warnings > 0 {
		return fmt.Errorf("migration doctor failed: errors=%d warnings=%d", report.Summary.Errors, report.Summary.Warnings)
	}
	return nil
}

func printMigrationDoctorHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail doctor migration [--root path] [--strict] [--fix-gitignore] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checks legacy KDD cleanup, Worktrail runtime git hygiene, migration candidates, secrets, local paths, and agent governance.")
}

func buildMigrationDoctorReport(env paths.Env, opts migrationDoctorOptions) migrationDoctorReport {
	root := opts.Root
	if root == "" {
		root = kddmigration.LegacyRoot(env, "")
	}
	report := migrationDoctorReport{
		Schema:      migrationDoctorSchema,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Project:     env.ProjectRoot,
		Root:        root,
		Findings:    []migrationFinding{},
	}
	if _, err := os.Stat(root); err == nil {
		if !opts.CleanupMode {
			report.add("SRC001", "error", root, "legacy KDD root still exists; migration is not complete until it is removed")
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		report.add("SRC001", "error", root, err.Error())
	}
	report.checkLegacyReferences(env, root)
	report.checkRunbooks(env)
	report.checkGitRuntimeStatus(env)
	report.checkKnowledgeHygiene(env, root)
	report.checkPendingMigrationCandidates(env)
	report.checkGovernance(env)
	report.checkIndex(env)
	report.OK = report.Summary.Errors == 0 && (!opts.Strict || report.Summary.Warnings == 0)
	return report
}

func (r *migrationDoctorReport) add(code, severity, path, message string) {
	r.Findings = append(r.Findings, migrationFinding{Code: code, Severity: severity, Path: path, Message: message})
	if severity == "error" {
		r.Summary.Errors++
	} else {
		r.Summary.Warnings++
	}
}

func (r *migrationDoctorReport) checkLegacyReferences(env paths.Env, legacyRoot string) {
	_ = filepath.WalkDir(env.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDoctorDir(env, legacyRoot, path, d.Name(), true) {
				return filepath.SkipDir
			}
			return nil
		}
		if !doctorTextFile(path) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if strings.Contains(string(b), "docs/knowledge-driven-development") {
			r.add("SRC002", "error", path, "legacy KDD path reference remains")
		}
		return nil
	})
}

func (r *migrationDoctorReport) checkRunbooks(env paths.Env) {
	runbooks := filepath.Join(env.ProjectWT, "runbooks")
	if st, err := os.Stat(runbooks); err == nil && st.IsDir() {
		r.add("KDD002", "error", runbooks, "Worktrail uses workflows/, not runbooks/")
	}
	checkCandidateTargets := func(scope string) {
		records, err := (candidate.Manager{Env: env, Actor: "doctor:migration"}).List(scope)
		if err != nil {
			return
		}
		for _, rec := range records {
			if strings.HasPrefix(rec.Meta.TargetPath, "runbooks/") {
				r.add("KDD002", "error", rec.Path, "candidate target still points at runbooks/; use workflows/")
			}
		}
	}
	checkCandidateTargets("project")
	checkCandidateTargets("user")
}

func (r *migrationDoctorReport) checkGitRuntimeStatus(env paths.Env) {
	args := []string{"-C", env.ProjectRoot, "status", "--porcelain=v1", "--", ".worktrail/candidates", ".worktrail/index", ".worktrail/logs", ".worktrail/raw", ".worktrail/state"}
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		path := strings.TrimSpace(line)
		if len(path) > 3 {
			path = strings.TrimSpace(path[3:])
		}
		r.add("GIT001", "error", path, "Worktrail runtime/local state is tracked or staged")
	}
}

func (r *migrationDoctorReport) checkKnowledgeHygiene(env paths.Env, legacyRoot string) {
	_ = filepath.WalkDir(env.ProjectRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if shouldSkipDoctorDir(env, legacyRoot, path, d.Name(), false) {
				return filepath.SkipDir
			}
			return nil
		}
		if !doctorTextFile(path) {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		text := string(b)
		scan := redact.Scan(text)
		if scan.Status == redact.StatusBlocked {
			r.add("SEC001", "error", path, "blocked secret pattern found")
		} else if scan.Status == redact.StatusRedacted {
			r.add("SEC001", "warning", path, "redactable secret or PII pattern found")
		}
		if kddmigration.HasLocalAbsolutePath(text) {
			r.add("LOC001", "warning", path, "local absolute path found")
		}
		return nil
	})
}

func (r *migrationDoctorReport) checkPendingMigrationCandidates(env paths.Env) {
	for _, scope := range []string{"project", "user"} {
		records, err := (candidate.Manager{Env: env, Actor: "doctor:migration"}).List(scope)
		if err != nil {
			continue
		}
		for _, rec := range records {
			if rec.Meta.Status != candidate.StatusPending {
				continue
			}
			if rec.Meta.CandidateType == model.CandidateTypeMigrationSource || hasTag(rec.Meta.Tags, "kdd-migration") {
				r.add("REV001", "error", rec.Path, "pending migration candidate requires human review before migration cleanup")
			}
			if rec.Meta.RedactionStatus == string(redact.StatusBlocked) {
				r.add("SEC001", "error", rec.Path, "blocked migration candidate remains")
			}
		}
	}
}

func (r *migrationDoctorReport) checkGovernance(env paths.Env) {
	for _, rel := range []string{"AGENTS.md", "CLAUDE.md"} {
		path := filepath.Join(env.ProjectRoot, rel)
		b, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			r.add("GOV001", "error", path, "agent governance file is missing Worktrail migration guidance")
			continue
		}
		if err != nil {
			r.add("GOV001", "error", path, err.Error())
			continue
		}
		text := strings.ToLower(string(b))
		if strings.Contains(text, "knowledge-driven-development") {
			r.add("GOV001", "error", path, "agent governance still routes to legacy KDD")
		}
		for _, required := range []string{"worktrail context", "worktrail review", "worktrail handoff"} {
			if !strings.Contains(text, required) {
				r.add("GOV001", "error", path, "agent governance missing "+required)
			}
		}
	}
}

func (r *migrationDoctorReport) checkIndex(env paths.Env) {
	manifest := filepath.Join(env.ProjectWT, "index", "manifest.json")
	if _, err := os.Stat(manifest); errors.Is(err, os.ErrNotExist) {
		r.add("IDX001", "warning", manifest, "project index manifest missing; run worktrail index rebuild --scope project")
	}
}

func shouldSkipDoctorDir(env paths.Env, legacyRoot, path, name string, skipLegacy bool) bool {
	if name == ".git" || name == "node_modules" {
		return true
	}
	if skipLegacy && sameOrInside(path, legacyRoot) {
		return true
	}
	if sameOrInside(path, filepath.Join(env.ProjectWT, "index")) ||
		sameOrInside(path, filepath.Join(env.ProjectWT, "logs")) ||
		sameOrInside(path, filepath.Join(env.ProjectWT, "raw")) ||
		sameOrInside(path, filepath.Join(env.ProjectWT, "state")) {
		return true
	}
	return false
}

func sameOrInside(path, root string) bool {
	if root == "" {
		return false
	}
	path, _ = filepath.Abs(path)
	root, _ = filepath.Abs(root)
	return path == root || strings.HasPrefix(path, root+string(os.PathSeparator))
}

func doctorTextFile(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".txt", ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

func renderMigrationDoctorText(out io.Writer, report migrationDoctorReport) {
	fmt.Fprintf(out, "schema: %s\nproject: %s\nroot: %s\nok: %t\nerrors: %d\nwarnings: %d\n", report.Schema, report.Project, report.Root, report.OK, report.Summary.Errors, report.Summary.Warnings)
	for _, finding := range report.Findings {
		if finding.Path != "" {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Path, finding.Message)
		} else {
			fmt.Fprintf(out, "%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Message)
		}
	}
}
