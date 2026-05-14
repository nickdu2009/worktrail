package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/transcript"
)

type importReport struct {
	Source      string                         `json:"source"`
	Project     string                         `json:"project"`
	Matched     int                            `json:"matched"`
	Synced      int                            `json:"synced"`
	Extracted   int                            `json:"extracted"`
	Skipped     int                            `json:"skipped"`
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

func isDuplicateCandidateError(err error) bool {
	return strings.Contains(err.Error(), "candidate") && strings.Contains(err.Error(), "already exists")
}

func printImportReport(ioctx IO, report importReport, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "source: %s\nproject: %s\nmatched: %d\n", report.Source, report.Project, report.Matched)
	if report.DryRun {
		fmt.Fprintln(ioctx.Out, "dry_run: true")
		for _, session := range report.Sessions {
			fmt.Fprintf(ioctx.Out, "%s\t%s\n", session.ID, session.Path)
		}
		printImportGuidance(ioctx, report)
		return nil
	}
	fmt.Fprintf(ioctx.Out, "synced: %d\nextracted: %d\nskipped: %d\n", report.Synced, report.Extracted, report.Skipped)
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
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Default mode is a dry-run that discovers current-project Codex transcripts.")
	fmt.Fprintln(out, "`--all` syncs discovered transcripts and creates pending transcript_notes evidence candidates.")
}
