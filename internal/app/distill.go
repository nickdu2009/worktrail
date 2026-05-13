package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

const defaultDistillLimit = 5

func runDistill(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	manager := candidate.Manager{Env: env, Actor: "cli:distill"}

	var records []candidate.Record
	if flagValue(flags, "pending", "") == "true" {
		limit, err := parseDistillLimit(flagValue(flags, "limit", ""))
		if err != nil {
			return err
		}
		all, err := manager.List(scope)
		if err != nil {
			return err
		}
		for _, rec := range all {
			if rec.Meta.Status != candidate.StatusPending || rec.Meta.CandidateType != model.CandidateTypeTranscriptNotes {
				continue
			}
			records = append(records, rec)
			if len(records) >= limit {
				break
			}
		}
		if len(records) == 0 {
			return errors.New("no pending transcript_notes candidates found")
		}
	} else {
		id := firstArg(positional, flagValue(flags, "id", ""))
		if strings.TrimSpace(id) == "" {
			return errors.New("usage: worktrail distill <candidate-id> or worktrail distill --pending [--limit N]")
		}
		rec, err := manager.Show(scope, id)
		if err != nil {
			return err
		}
		if rec.Meta.CandidateType != model.CandidateTypeTranscriptNotes {
			return fmt.Errorf("candidate %q is %q, want %q", rec.Meta.ID, rec.Meta.CandidateType, model.CandidateTypeTranscriptNotes)
		}
		records = append(records, rec)
	}
	return renderDistillPack(ioctx.Out, records)
}

func parseDistillLimit(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return defaultDistillLimit, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 0, fmt.Errorf("limit must be a positive integer")
	}
	return limit, nil
}

func renderDistillPack(out io.Writer, records []candidate.Record) error {
	fmt.Fprintln(out, "# Worktrail Distillation Pack")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "You are the current AI coding agent. Distill the transcript evidence below into semantic Worktrail candidates.")
	fmt.Fprintln(out, "Do not promote, merge, discard, or write formal knowledge from this pack.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Create only useful pending candidates with normal knowledge types: `rule`, `decision`, `lesson`, `prompt`, or `workflow`.")
	fmt.Fprintln(out, "Prefer a small number of durable, reusable items over transcript summaries.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "For each distilled item, run `worktrail candidates create` with the chosen type, target path, title, summary, and body.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Candidate JSON shape for reasoning:")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "```json")
	fmt.Fprintln(out, `{"candidates":[{"candidate_type":"rule|decision|lesson|prompt|workflow","title":"Durable knowledge title","summary":"Short semantic summary","target_path":"rules/name.md|decisions/ADR-name.md|lessons/name.md|prompts/name.md|workflows/name.md","operation":"create|merge","tags":["tag"],"body":"Markdown content to create as a pending candidate"}]}`)
	fmt.Fprintln(out, "```")
	for _, rec := range records {
		fmt.Fprintln(out)
		fmt.Fprintf(out, "## Evidence Candidate `%s`\n\n", rec.Meta.ID)
		fmt.Fprintf(out, "- Status: `%s`\n", rec.Meta.Status)
		fmt.Fprintf(out, "- Type: `%s`\n", rec.Meta.CandidateType)
		fmt.Fprintf(out, "- Evidence target: `%s`\n", rec.Meta.TargetPath)
		if len(rec.Meta.SourceSessions) > 0 {
			fmt.Fprintf(out, "- Source sessions: `%s`\n", strings.Join(rec.Meta.SourceSessions, "`, `"))
		}
		if len(rec.Meta.Tags) > 0 {
			fmt.Fprintf(out, "- Tags: `%s`\n", strings.Join(rec.Meta.Tags, "`, `"))
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "### Transcript Evidence")
		fmt.Fprintln(out)
		fmt.Fprintln(out, rec.Body)
	}
	return nil
}
