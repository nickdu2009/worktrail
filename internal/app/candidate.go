package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
)

func runCandidates(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("candidates subcommand required")
	}
	if wantsHelp(args) {
		printCandidatesHelp(ioctx.Out, firstArg(args, ""))
		return nil
	}
	manager := candidate.Manager{Env: env, Actor: "cli:candidates"}
	cmd, rest := args[0], args[1:]
	flags, positional := splitFlags(rest)
	scope := flagValue(flags, "scope", "project")
	switch cmd {
	case "create":
		body := joinArgs(positional)
		if body == "" && ioctx.In != nil {
			b, err := io.ReadAll(ioctx.In)
			if err != nil {
				return err
			}
			body = string(b)
		}
		rec, err := manager.Create(candidate.CreateRequest{
			Scope:         scope,
			ID:            flagValue(flags, "id", ""),
			CandidateType: flagValue(flags, "type", "knowledge"),
			TargetPath:    flagValue(flags, "target", ""),
			Title:         flagValue(flags, "title", ""),
			Summary:       flagValue(flags, "summary", ""),
			Operation:     flagValue(flags, "operation", "replace"),
			Tags:          splitCSV(flagValue(flags, "tags", "")),
			Body:          body,
		})
		if err != nil {
			return err
		}
		return printCandidate(ioctx, rec, flagValue(flags, "format", "text"))
	case "list":
		records, err := manager.List(scope)
		if err != nil {
			return err
		}
		records = filterCandidateRecords(records, candidateFilters{
			Type:     flagValue(flags, "type", ""),
			Status:   flagValue(flags, "status", ""),
			Semantic: flagValue(flags, "semantic", "") == "true",
			Evidence: flagValue(flags, "evidence", "") == "true",
		})
		if flagValue(flags, "format", "text") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(records)
		}
		for _, rec := range records {
			fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\t%s\t%s\n", rec.Meta.ID, rec.Meta.Status, rec.Meta.Scope, rec.Meta.CandidateType, rec.Meta.Title)
		}
		return nil
	case "show":
		rec, err := manager.Show(scope, firstArg(positional, flagValue(flags, "id", "")))
		if err != nil {
			return err
		}
		return printCandidate(ioctx, rec, flagValue(flags, "format", "markdown"))
	case "diff":
		diff, err := manager.Diff(scope, firstArg(positional, flagValue(flags, "id", "")))
		if err != nil {
			return err
		}
		fmt.Fprint(ioctx.Out, diff)
		return nil
	default:
		return fmt.Errorf("unknown candidates subcommand %q", cmd)
	}
}

type candidateFilters struct {
	Type     string
	Status   string
	Semantic bool
	Evidence bool
}

func filterCandidateRecords(records []candidate.Record, filters candidateFilters) []candidate.Record {
	filters.Type = strings.TrimSpace(filters.Type)
	filters.Status = strings.TrimSpace(filters.Status)
	if filters.Type == "" && filters.Status == "" && !filters.Semantic && !filters.Evidence {
		return records
	}
	filtered := records[:0]
	for _, rec := range records {
		if filters.Type != "" && rec.Meta.CandidateType != filters.Type {
			continue
		}
		if filters.Status != "" && rec.Meta.Status != filters.Status {
			continue
		}
		if filters.Semantic && !isSemanticCandidateType(rec.Meta.CandidateType) {
			continue
		}
		if filters.Evidence && rec.Meta.CandidateType != model.CandidateTypeTranscriptNotes {
			continue
		}
		filtered = append(filtered, rec)
	}
	return filtered
}

func runReview(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printReviewHelp(ioctx.Out)
		return nil
	}
	flags, _ := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	records, err := (candidate.Manager{Env: env, Actor: "cli:review"}).List(scope)
	if err != nil {
		return err
	}
	fmt.Fprintln(ioctx.Out, "# Worktrail Candidate Review")
	fmt.Fprintln(ioctx.Out)
	showEvidence := flagValue(flags, "evidence", "") == "true"
	showAll := flagValue(flags, "all", "") == "true"
	showSemantic := flagValue(flags, "semantic", "") == "true"
	hiddenEvidence := 0
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPending {
			continue
		}
		if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
			if !showEvidence && !showAll {
				hiddenEvidence++
				continue
			}
		} else if showEvidence {
			continue
		}
		if showSemantic && !isSemanticCandidateType(rec.Meta.CandidateType) {
			continue
		}
		fmt.Fprintf(ioctx.Out, "- `%s` %s -> `%s` [%s, redaction=%s]\n", rec.Meta.ID, rec.Meta.Title, rec.Meta.TargetPath, rec.Meta.CandidateType, rec.Meta.RedactionStatus)
		if rec.Meta.Summary != "" {
			fmt.Fprintf(ioctx.Out, "  %s\n", rec.Meta.Summary)
		}
	}
	if hiddenEvidence > 0 {
		fmt.Fprintf(ioctx.Out, "\nHidden transcript evidence candidates: %d. Use `worktrail review --evidence` to inspect them or `worktrail distill --pending --limit 5` to distill them.\n", hiddenEvidence)
	}
	missingAppliedTargets, err := missingAppliedCandidateTargets(env, records)
	if err != nil {
		return err
	}
	if len(missingAppliedTargets) > 0 {
		fmt.Fprintln(ioctx.Out, "\nApplied candidate target warnings:")
		for _, issue := range missingAppliedTargets {
			fmt.Fprintf(ioctx.Out, "- `%s` is %s but `%s` is missing; context will not load it as formal knowledge.\n", issue.ID, issue.Status, issue.TargetPath)
		}
	}
	fmt.Fprintln(ioctx.Out, "\nUse `worktrail candidates diff <id>` and, after explicit user confirmation, `worktrail promote|merge|discard <id>`.")
	return nil
}

type appliedTargetIssue struct {
	ID         string
	Status     string
	TargetPath string
}

func missingAppliedCandidateTargets(env paths.Env, records []candidate.Record) ([]appliedTargetIssue, error) {
	var issues []appliedTargetIssue
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPromoted && rec.Meta.Status != candidate.StatusMerged {
			continue
		}
		root, err := env.ScopeRoot(rec.Meta.Scope)
		if err != nil {
			return nil, err
		}
		target := filepath.Join(root, filepath.FromSlash(rec.Meta.TargetPath))
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return nil, err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("candidate %q target escapes scope: %s", rec.Meta.ID, rec.Meta.TargetPath)
		}
		if _, err := os.Stat(target); err == nil {
			continue
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		issues = append(issues, appliedTargetIssue{
			ID:         rec.Meta.ID,
			Status:     rec.Meta.Status,
			TargetPath: rec.Meta.TargetPath,
		})
	}
	return issues, nil
}

func runCandidateAction(_ context.Context, env paths.Env, ioctx IO, action string, args []string) error {
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	id := firstArg(positional, flagValue(flags, "id", ""))
	manager := candidate.Manager{Env: env, Actor: "cli:" + action}
	switch action {
	case "promote":
		result, err := manager.Promote(scope, id)
		if err != nil {
			return err
		}
		return printApplyResult(ioctx, result, flagValue(flags, "format", "text"))
	case "discard":
		rec, err := manager.Discard(scope, id)
		if err != nil {
			return err
		}
		return printCandidate(ioctx, rec, flagValue(flags, "format", "text"))
	default:
		return fmt.Errorf("unknown candidate action %q", action)
	}
}

func runMerge(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	id := firstArg(positional, flagValue(flags, "id", ""))
	manager := candidate.Manager{Env: env, Actor: "cli:merge"}
	rec, err := manager.Show(scope, id)
	if err != nil {
		return err
	}
	if len(positional) > 1 && positional[1] != rec.Meta.TargetPath {
		return fmt.Errorf("merge target %q does not match candidate target %q", positional[1], rec.Meta.TargetPath)
	}
	result, err := manager.Merge(scope, id)
	if err != nil {
		return err
	}
	return printApplyResult(ioctx, result, flagValue(flags, "format", "text"))
}

func runRedact(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || args[0] != "scan" {
		return errors.New("usage: worktrail redact scan <file>")
	}
	flags, positional := splitFlags(args[1:])
	path := firstArg(positional, "")
	if flagValue(flags, "session", "") == "latest" {
		var err error
		path, err = latestTranscriptPath(env.ProjectWT, flagValue(flags, "source", "codex"))
		if err != nil {
			return err
		}
	}
	if path == "" {
		return errors.New("redact scan requires a file path")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	result := redact.Scan(string(b))
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(result)
	}
	fmt.Fprintf(ioctx.Out, "status: %s\nfindings: %d\n", result.Status, len(result.Findings))
	return nil
}

func printCandidate(ioctx IO, rec candidate.Record, format string) error {
	switch format {
	case "json":
		return json.NewEncoder(ioctx.Out).Encode(rec)
	case "markdown":
		fmt.Fprint(ioctx.Out, rec.Body)
	default:
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", rec.Meta.ID, rec.Meta.Status, rec.Path)
	}
	return nil
}

func printApplyResult(ioctx IO, result candidate.ApplyResult, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(result)
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", result.Candidate.ID, result.Status, result.TargetPath)
	if result.BackupPath != "" {
		fmt.Fprintf(ioctx.Out, "backup\t%s\n", result.BackupPath)
	}
	return nil
}

func isSemanticCandidateType(typ string) bool {
	switch typ {
	case "rule", "decision", "lesson", "prompt", "workflow":
		return true
	default:
		return false
	}
}

func printCandidatesHelp(out io.Writer, subcommand string) {
	switch subcommand {
	case "create":
		fmt.Fprintln(out, "usage: worktrail candidates create --target <path> [options] [body]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "options:")
		fmt.Fprintln(out, "  --id <id>              optional stable candidate id")
		fmt.Fprintln(out, "  --scope <scope>        project or user (default project)")
		fmt.Fprintln(out, "  --type <type>          rule, decision, lesson, prompt, workflow, or transcript_notes")
		fmt.Fprintln(out, "  --target <path>        target knowledge path, for example rules/testing.md")
		fmt.Fprintln(out, "  --title <title>        candidate title")
		fmt.Fprintln(out, "  --summary <text>       short review summary")
		fmt.Fprintln(out, "  --operation <op>       replace or merge (default replace)")
		fmt.Fprintln(out, "  --tags a,b             comma-separated tags")
		fmt.Fprintln(out, "  --format text|json     output format")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "body can be passed as positional text or via stdin.")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "examples:")
		fmt.Fprintln(out, "  worktrail candidates create --type rule --target rules/testing.md --title \"Testing Rules\" \"Keep tests focused.\"")
		fmt.Fprintln(out, "  printf '# Workflow\\n\\nRun focused tests first.\\n' | worktrail candidates create --type workflow --target workflows/testing.md --title \"Testing Workflow\"")
	default:
		fmt.Fprintln(out, "usage: worktrail candidates <create|list|show|diff> [options]")
		fmt.Fprintln(out)
		fmt.Fprintln(out, "list filters:")
		fmt.Fprintln(out, "  --status pending|promoted|merged|discarded")
		fmt.Fprintln(out, "  --type <candidate_type>")
		fmt.Fprintln(out, "  --semantic     rule/decision/lesson/prompt/workflow only")
		fmt.Fprintln(out, "  --evidence     transcript_notes only")
		fmt.Fprintln(out, "  --format json")
	}
}

func printReviewHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail review [--semantic|--evidence|--all] [--scope project|user]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "By default, review shows pending semantic candidates and hides transcript_notes evidence.")
	fmt.Fprintln(out, "Use --evidence to inspect transcript evidence, or --all to show both.")
}
