package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/transcript"
	"github.com/nickdu2009/worktrail/internal/util"
)

type importReport struct {
	Source             string                         `json:"source"`
	Project            string                         `json:"project"`
	Matched            int                            `json:"matched"`
	Observed           int                            `json:"observed,omitempty"`
	AlreadyImported    int                            `json:"already_imported,omitempty"`
	Synced             int                            `json:"synced"`
	Extracted          int                            `json:"extracted"`
	Skipped            int                            `json:"skipped"`
	Reused             int                            `json:"reused,omitempty"`
	Blocked            int                            `json:"blocked,omitempty"`
	DryRun             bool                           `json:"dry_run"`
	Sessions           []transcript.DiscoveredSession `json:"sessions,omitempty"`
	Candidates         []string                       `json:"candidates,omitempty"`
	ExistingCandidates []string                       `json:"existing_candidates,omitempty"`
	NextSteps          []string                       `json:"next_steps,omitempty"`
	GitGuidance        []string                       `json:"git_guidance,omitempty"`
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
	bounds, err := importDiscoverOptions(flags)
	if err != nil {
		return err
	}
	sessions, err := transcript.DiscoverCodexSessionsBounded(env.Home, env.ProjectRoot, bounds)
	if err != nil {
		return err
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return err
	}
	report := importReport{
		Source:             source,
		Project:            env.ProjectRoot,
		Matched:            len(sessions),
		DryRun:             flags["all"] != "true",
		Sessions:           sessions,
		ExistingCandidates: existingTranscriptEvidenceIDs(root, scope, source, sessions),
	}
	report.AlreadyImported = len(report.ExistingCandidates)
	report.NextSteps = importNextSteps(report.DryRun, report.Matched-report.AlreadyImported)
	report.GitGuidance = importGitGuidance()
	if report.DryRun {
		return printImportReport(ioctx, report, flagValue(flags, "format", "text"))
	}
	for _, session := range sessions {
		if _, ok := existingTranscriptEvidence(root, scope, source, session.Path); ok {
			report.Skipped++
			report.Reused++
			continue
		}
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
	bounds, err := importDiscoverOptions(flags)
	if err != nil {
		return err
	}
	sessions = transcript.BoundSessions(sessions, bounds)
	report := importReport{
		Source:             "cursor",
		Project:            env.ProjectRoot,
		Matched:            len(sessions),
		Observed:           len(sessions),
		DryRun:             flags["all"] != "true",
		Sessions:           sessions,
		ExistingCandidates: existingTranscriptEvidenceIDs(root, scope, "cursor", sessions),
	}
	report.AlreadyImported = len(report.ExistingCandidates)
	report.NextSteps = cursorImportNextSteps(report.DryRun, report.Matched-report.AlreadyImported)
	report.GitGuidance = importGitGuidance()
	if report.DryRun {
		return printImportReport(ioctx, report, flagValue(flags, "format", "text"))
	}
	for _, session := range sessions {
		if _, ok := existingTranscriptEvidence(root, scope, "cursor", session.Path); ok {
			report.Skipped++
			report.Reused++
			continue
		}
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

func importDiscoverOptions(flags map[string]string) (transcript.DiscoverOptions, error) {
	var opts transcript.DiscoverOptions
	if since := strings.TrimSpace(flagValue(flags, "since", "")); since != "" {
		d, err := parseImportSince(since)
		if err != nil {
			return opts, fmt.Errorf("invalid --since duration %q", since)
		}
		opts.Since = time.Now().UTC().Add(-d)
	}
	if limit := strings.TrimSpace(flagValue(flags, "limit", "")); limit != "" {
		n, err := strconv.Atoi(limit)
		if err != nil || n <= 0 {
			return opts, fmt.Errorf("--limit must be a positive integer")
		}
		opts.Limit = n
	}
	return opts, nil
}

func parseImportSince(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err != nil || days <= 0 {
			return 0, fmt.Errorf("days must be positive")
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(value)
}

func existingTranscriptEvidenceIDs(root, scope, source string, sessions []transcript.DiscoveredSession) []string {
	var ids []string
	for _, session := range sessions {
		if id, ok := existingTranscriptEvidence(root, scope, source, session.Path); ok {
			ids = append(ids, id)
		}
	}
	return ids
}

func existingTranscriptEvidence(root, scope, source, path string) (string, bool) {
	id := transcriptEvidenceCandidateID(source, path)
	if id == "" {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(root, "candidates", scope, id+".md")); err == nil {
		return id, true
	}
	return "", false
}

func transcriptEvidenceCandidateID(source, path string) string {
	return util.Slug(extractionCandidateID(source, path, 0, ""))
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
			Source:    "cursor",
			ID:        raw.ID,
			Path:      raw.Path,
			UpdatedAt: fileModTime(raw.Path),
		})
	}
	return sessions, nil
}

func fileModTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
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
	if report.AlreadyImported > 0 {
		fmt.Fprintf(ioctx.Out, "already_imported: %d\n", report.AlreadyImported)
	}
	if report.DryRun {
		fmt.Fprintln(ioctx.Out, "dry_run: true")
		for _, session := range report.Sessions {
			fmt.Fprintf(ioctx.Out, "%s\t%s\n", session.ID, session.Path)
		}
		for _, id := range report.ExistingCandidates {
			fmt.Fprintf(ioctx.Out, "existing_candidate: %s\n", id)
		}
		printImportGuidance(ioctx, report)
		return nil
	}
	fmt.Fprintf(ioctx.Out, "synced: %d\nextracted: %d\nskipped: %d\n", report.Synced, report.Extracted, report.Skipped)
	if report.Reused > 0 {
		fmt.Fprintf(ioctx.Out, "reused: %d\n", report.Reused)
	}
	if report.Blocked > 0 {
		fmt.Fprintf(ioctx.Out, "blocked: %d\n", report.Blocked)
	}
	for _, id := range report.Candidates {
		fmt.Fprintf(ioctx.Out, "candidate: %s\n", id)
	}
	for _, id := range report.ExistingCandidates {
		fmt.Fprintf(ioctx.Out, "existing_candidate: %s\n", id)
	}
	printImportGuidance(ioctx, report)
	return nil
}

func importNextSteps(dryRun bool, unimported int) []string {
	if dryRun {
		if unimported <= 0 {
			return []string{"no unimported Codex sessions found in the current bounds; run `worktrail distill --pending --summary` if transcript_notes evidence is already pending"}
		}
		return []string{"rerun the bounded import with `--all`, for example `worktrail import codex --since 14d --all`, to sync transcripts and create pending transcript_notes evidence"}
	}
	return []string{
		"run `worktrail distill --pending --limit 5` to process evidence in small batches",
		"or run `worktrail distill --pending --all --write-pack distill.md` to write one full pack without flooding the terminal",
		"capture confirmed findings as semantic pending candidates with `worktrail note add`",
		"run `worktrail review` to review semantic candidates; use `worktrail review --evidence` for transcript evidence",
	}
}

func cursorImportNextSteps(dryRun bool, unimported int) []string {
	if dryRun {
		if unimported <= 0 {
			return []string{"no unimported Cursor sessions found in the current bounds; run `worktrail distill --pending --summary` if transcript_notes evidence is already pending"}
		}
		return []string{"rerun the bounded import with `--all`, for example `worktrail import cursor --limit 20 --all`, to sync observed Cursor transcripts and create pending transcript_notes evidence"}
	}
	return importNextSteps(false, unimported)
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
	fmt.Fprintln(out, "usage: worktrail import codex [--all] [--since duration] [--limit N] [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail import cursor [--file path] [--all] [--since duration] [--limit N] [--scope project|user] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Default mode is a dry-run that discovers current-project Codex transcripts.")
	fmt.Fprintln(out, "`--all` syncs discovered transcripts and creates pending transcript_notes evidence candidates.")
	fmt.Fprintln(out, "`--since` accepts Go durations such as 336h or day shorthand such as 14d.")
	fmt.Fprintln(out, "`import cursor` reads explicit files or Worktrail-observed Cursor transcript metadata; it does not scan private Cursor directories.")
}
