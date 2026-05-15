package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/transcript"
)

type importReport struct {
	Source      string                         `json:"source"`
	Project     string                         `json:"project"`
	Matched     int                            `json:"matched"`
	Observed    int                            `json:"observed,omitempty"`
	Synced      int                            `json:"synced"`
	Extracted   int                            `json:"extracted"`
	Skipped     int                            `json:"skipped"`
	Blocked     int                            `json:"blocked,omitempty"`
	DryRun      bool                           `json:"dry_run"`
	Sessions    []transcript.DiscoveredSession `json:"sessions,omitempty"`
	Candidates  []string                       `json:"candidates,omitempty"`
	NextSteps   []string                       `json:"next_steps,omitempty"`
	GitGuidance []string                       `json:"git_guidance,omitempty"`
}

func runImport(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsHelp(args) {
		printImportHelp(ioctx.Out)
		if len(args) == 0 {
			return fmt.Errorf("import source required")
		}
		return nil
	}
	if len(args) == 0 {
		return fmt.Errorf("import source required")
	}
	source := args[0]
	flags, _ := splitFlags(args[1:])
	scope := flagValue(flags, "scope", "project")
	if source == "cursor" {
		return runImportCursor(env, ioctx, flags, scope)
	}
	if source != "codex" {
		return fmt.Errorf("unsupported import source %q", source)
	}
	sessions, err := transcript.DiscoverCodexSessions(env.Home, env.ProjectRoot)
	if err != nil {
		return err
	}
	report := importReport{
		Source:   source,
		Project:  env.ProjectRoot,
		Matched:  len(sessions),
		DryRun:   flags["all"] != "true",
		Sessions: sessions,
	}
	report.NextSteps = importNextSteps(report.DryRun)
	report.GitGuidance = importGitGuidance()
	if report.DryRun {
		return printImportReport(ioctx, report, flagValue(flags, "format", "text"))
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return err
	}
	for _, session := range sessions {
		meta, err := transcript.Sync(session.Path, root, transcript.SyncOptions{Source: source, Scope: scope})
		if err != nil {
			return err
		}
		report.Synced++
		rawPath := filepath.Join(root, filepath.FromSlash(meta.Path))
		records, err := extractSession(env, scope, source, "manual", rawPath)
		if err != nil {
			if isDuplicateCandidateError(err) {
				report.Skipped++
				continue
			}
			return err
		}
		for _, rec := range records {
			report.Candidates = append(report.Candidates, rec.Meta.ID)
		}
		report.Extracted += len(records)
	}
	return printImportReport(ioctx, report, flagValue(flags, "format", "text"))
}

func runImportCursor(env paths.Env, ioctx IO, flags map[string]string, scope string) error {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return err
	}
	sessions, err := cursorImportSessions(root, flagValue(flags, "file", ""))
	if err != nil {
		return err
	}
	report := importReport{
		Source:   "cursor",
		Project:  env.ProjectRoot,
		Matched:  len(sessions),
		Observed: len(sessions),
		DryRun:   flags["all"] != "true",
		Sessions: sessions,
	}
	report.NextSteps = cursorImportNextSteps(report.DryRun)
	report.GitGuidance = importGitGuidance()
	if report.DryRun {
		return printImportReport(ioctx, report, flagValue(flags, "format", "text"))
	}
	for _, session := range sessions {
		if _, err := os.Stat(session.Path); err != nil {
			report.Blocked++
			continue
		}
		meta, err := transcript.Sync(session.Path, root, transcript.SyncOptions{Source: "cursor", Scope: scope})
		if err != nil {
			return err
		}
		report.Synced++
		rawPath := filepath.Join(root, filepath.FromSlash(meta.Path))
		records, err := extractSession(env, scope, "cursor", "manual", rawPath)
		if err != nil {
			if isDuplicateCandidateError(err) {
				report.Skipped++
				continue
			}
			return err
		}
		for _, rec := range records {
			report.Candidates = append(report.Candidates, rec.Meta.ID)
		}
		report.Extracted += len(records)
	}
	return printImportReport(ioctx, report, flagValue(flags, "format", "text"))
}

func cursorImportSessions(root, explicitFile string) ([]transcript.DiscoveredSession, error) {
	if explicitFile != "" {
		return []transcript.DiscoveredSession{{
			Source: "cursor",
			ID:     "explicit-" + filepath.Base(explicitFile),
			Path:   explicitFile,
		}}, nil
	}
	matches, err := filepath.Glob(filepath.Join(root, "raw", "cursor", "observed-*.metadata.json"))
	if err != nil {
		return nil, err
	}
	var sessions []transcript.DiscoveredSession
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			return nil, err
		}
		var raw struct {
			ID   string `json:"id"`
			Path string `json:"path"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, err
		}
		if strings.TrimSpace(raw.Path) == "" {
			continue
		}
		sessions = append(sessions, transcript.DiscoveredSession{
			Source: "cursor",
			ID:     raw.ID,
			Path:   raw.Path,
		})
	}
	return sessions, nil
}

func isDuplicateCandidateError(err error) bool {
	return strings.Contains(err.Error(), "candidate") && strings.Contains(err.Error(), "already exists")
}

func printImportReport(ioctx IO, report importReport, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "source: %s\nproject: %s\nmatched: %d\n", report.Source, report.Project, report.Matched)
	if report.Observed > 0 {
		fmt.Fprintf(ioctx.Out, "observed: %d\n", report.Observed)
	}
	if report.DryRun {
		fmt.Fprintln(ioctx.Out, "dry_run: true")
		for _, session := range report.Sessions {
			fmt.Fprintf(ioctx.Out, "%s\t%s\n", session.ID, session.Path)
		}
		printImportGuidance(ioctx, report)
		return nil
	}
	fmt.Fprintf(ioctx.Out, "synced: %d\nextracted: %d\nskipped: %d\n", report.Synced, report.Extracted, report.Skipped)
	if report.Blocked > 0 {
		fmt.Fprintf(ioctx.Out, "blocked: %d\n", report.Blocked)
	}
	for _, id := range report.Candidates {
		fmt.Fprintf(ioctx.Out, "candidate: %s\n", id)
	}
	printImportGuidance(ioctx, report)
	return nil
}

func importNextSteps(dryRun bool) []string {
	if dryRun {
		return []string{"run `worktrail import codex --all` to sync transcripts and create pending transcript_notes evidence"}
	}
	return []string{
		"run `worktrail distill --pending --limit 5` to process evidence in small batches",
		"or run `worktrail distill --pending --all --write-pack distill.md` to write one full pack without flooding the terminal",
		"create semantic pending candidates with `worktrail candidates create`",
		"run `worktrail review` to review semantic candidates; use `worktrail review --evidence` for transcript evidence",
	}
}

func cursorImportNextSteps(dryRun bool) []string {
	if dryRun {
		return []string{"run `worktrail import cursor --all` to sync observed Cursor transcripts and create pending transcript_notes evidence"}
	}
	return importNextSteps(false)
}

func importGitGuidance() []string {
	return []string{
		".worktrail/raw, .worktrail/index, .worktrail/logs, and .worktrail/candidates are runtime/import artifacts unless your team explicitly tracks them",
		".worktrail/rules, .worktrail/decisions, .worktrail/lessons, .worktrail/prompts, .worktrail/workflows, and project .gitignore changes are the usual review targets after promotion",
	}
}

func printImportGuidance(ioctx IO, report importReport) {
	fmt.Fprintln(ioctx.Out)
	fmt.Fprintln(ioctx.Out, "next steps:")
	for _, step := range report.NextSteps {
		fmt.Fprintf(ioctx.Out, "- %s\n", step)
	}
	fmt.Fprintln(ioctx.Out)
	fmt.Fprintln(ioctx.Out, "git guidance:")
	for _, item := range report.GitGuidance {
		fmt.Fprintf(ioctx.Out, "- %s\n", item)
	}
}

func printImportHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail import codex [--all] [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail import cursor [--file path] [--all] [--scope project|user] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Default mode is a dry-run that discovers current-project Codex transcripts.")
	fmt.Fprintln(out, "`--all` syncs discovered transcripts and creates pending transcript_notes evidence candidates.")
	fmt.Fprintln(out, "`import cursor` reads explicit files or Worktrail-observed Cursor transcript metadata; it does not scan private Cursor directories.")
}
