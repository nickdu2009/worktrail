package app

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/transcript"
)

type importReport struct {
	Source     string                         `json:"source"`
	Project    string                         `json:"project"`
	Matched    int                            `json:"matched"`
	Synced     int                            `json:"synced"`
	Extracted  int                            `json:"extracted"`
	Skipped    int                            `json:"skipped"`
	DryRun     bool                           `json:"dry_run"`
	Sessions   []transcript.DiscoveredSession `json:"sessions,omitempty"`
	Candidates []string                       `json:"candidates,omitempty"`
}

func runImport(_ context.Context, env paths.Env, ioctx IO, args []string) error {
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
		fmt.Fprintln(ioctx.Out, "run `worktrail import codex --all` to sync and extract pending candidates")
		return nil
	}
	fmt.Fprintf(ioctx.Out, "synced: %d\nextracted: %d\nskipped: %d\n", report.Synced, report.Extracted, report.Skipped)
	for _, id := range report.Candidates {
		fmt.Fprintf(ioctx.Out, "candidate: %s\n", id)
	}
	return nil
}
