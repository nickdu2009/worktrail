package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/util"
)

const defaultDistillLimit = 5

type distillSummary struct {
	Count      int      `json:"count"`
	Scope      string   `json:"scope"`
	Mode       string   `json:"mode"`
	WritePack  string   `json:"write_pack,omitempty"`
	Candidates []string `json:"candidates"`
}

func runDistill(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printDistillHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	manager := candidate.Manager{Env: env, Actor: "cli:distill"}

	var records []candidate.Record
	if flagValue(flags, "pending", "") == "true" {
		limit, err := parsePositiveInt(flagValue(flags, "limit", ""), defaultDistillLimit, "limit")
		if err != nil {
			return err
		}
		offset, err := parsePositiveInt(flagValue(flags, "offset", ""), 0, "offset")
		if err != nil {
			return err
		}
		all, err := manager.List(scope)
		if err != nil {
			return err
		}
		var pending []candidate.Record
		for _, rec := range all {
			if rec.Meta.Status != candidate.StatusPending || rec.Meta.CandidateType != model.CandidateTypeTranscriptNotes {
				continue
			}
			pending = append(pending, rec)
		}
		if offset > len(pending) {
			offset = len(pending)
		}
		pending = pending[offset:]
		if flagValue(flags, "all", "") != "true" && len(pending) > limit {
			pending = pending[:limit]
		}
		records = pending
		if len(records) == 0 {
			return errors.New("no pending transcript_notes candidates found")
		}
	} else {
		id := firstArg(positional, flagValue(flags, "id", ""))
		if strings.TrimSpace(id) == "" {
			return errors.New("usage: worktrail distill <candidate-id> or worktrail distill --pending [--all|--limit N] [--offset N]")
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
	packPath := flagValue(flags, "write-pack", "")
	summaryOnly := flagValue(flags, "summary", "") == "true"
	jsonOutput := flagValue(flags, "json", "") == "true" || flagValue(flags, "format", "") == "json"
	if flagValue(flags, "all", "") == "true" && len(records) > defaultDistillLimit && packPath == "" && !summaryOnly && !jsonOutput {
		return fmt.Errorf("distill pack has %d evidence candidates; use --write-pack <file>, --summary, --json, or --limit/--offset to avoid flooding the terminal", len(records))
	}
	if packPath != "" {
		if err := writeDistillPack(packPath, records); err != nil {
			return err
		}
		summaryOnly = true
	}
	summary := newDistillSummary(scope, records, packPath)
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(summary)
	}
	if summaryOnly {
		printDistillSummary(ioctx.Out, summary)
		return nil
	}
	return renderDistillPack(ioctx.Out, records)
}

func parsePositiveInt(value string, def int, name string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return def, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 || name == "limit" && parsed == 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return parsed, nil
}

func renderDistillPack(out io.Writer, records []candidate.Record) error {
	fmt.Fprintln(out, "# Worktrail Distillation Pack")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Evidence candidates in this pack: %d\n\n", len(records))
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

func writeDistillPack(path string, records []candidate.Record) error {
	var b strings.Builder
	if err := renderDistillPack(&b, records); err != nil {
		return err
	}
	return util.AtomicWrite(path, []byte(b.String()), 0o600)
}

func newDistillSummary(scope string, records []candidate.Record, packPath string) distillSummary {
	summary := distillSummary{
		Count:     len(records),
		Scope:     scope,
		Mode:      "transcript_notes",
		WritePack: packPath,
	}
	for _, rec := range records {
		summary.Candidates = append(summary.Candidates, rec.Meta.ID)
	}
	return summary
}

func printDistillSummary(out io.Writer, summary distillSummary) {
	fmt.Fprintf(out, "evidence_candidates: %d\n", summary.Count)
	if summary.WritePack != "" {
		fmt.Fprintf(out, "pack: %s\n", summary.WritePack)
	}
	for _, id := range summary.Candidates {
		fmt.Fprintf(out, "candidate: %s\n", id)
	}
	if summary.WritePack != "" {
		fmt.Fprintln(out, "next: have the current AI agent read the pack and create semantic pending candidates with `worktrail candidates create`")
	} else {
		fmt.Fprintln(out, "next: run `worktrail distill --pending --limit 5 --offset <N>` or `worktrail distill --pending --all --write-pack distill.md`")
	}
}

func printDistillHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail distill <candidate-id>")
	fmt.Fprintln(out, "       worktrail distill --pending [--limit N|--all] [--offset N] [--summary|--json|--write-pack file]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Distill prints an agent-facing pack from transcript_notes evidence. It does not create, promote, merge, or discard knowledge.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "bulk options:")
	fmt.Fprintln(out, "  --pending            select pending transcript_notes")
	fmt.Fprintln(out, "  --limit N            output at most N evidence candidates (default 5)")
	fmt.Fprintln(out, "  --offset N           skip N evidence candidates for paging")
	fmt.Fprintln(out, "  --all                select all pending transcript_notes")
	fmt.Fprintln(out, "  --summary            print ids and next steps only")
	fmt.Fprintln(out, "  --json               print summary JSON only")
	fmt.Fprintln(out, "  --write-pack <file>  write the full pack to a file and print a compact summary")
}
