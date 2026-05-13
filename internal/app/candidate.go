package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
)

func runCandidates(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("candidates subcommand required")
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

func runReview(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	flags, _ := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	records, err := (candidate.Manager{Env: env, Actor: "cli:review"}).List(scope)
	if err != nil {
		return err
	}
	fmt.Fprintln(ioctx.Out, "# Worktrail Candidate Review")
	fmt.Fprintln(ioctx.Out)
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPending {
			continue
		}
		fmt.Fprintf(ioctx.Out, "- `%s` %s -> `%s` [%s, redaction=%s]\n", rec.Meta.ID, rec.Meta.Title, rec.Meta.TargetPath, rec.Meta.CandidateType, rec.Meta.RedactionStatus)
		if rec.Meta.Summary != "" {
			fmt.Fprintf(ioctx.Out, "  %s\n", rec.Meta.Summary)
		}
	}
	fmt.Fprintln(ioctx.Out, "\nUse `worktrail candidates diff <id>` and, after explicit user confirmation, `worktrail promote|merge|discard <id>`.")
	return nil
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
