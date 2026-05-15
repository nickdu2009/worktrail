package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	wtdistill "github.com/nickdu2009/worktrail/internal/distill"
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
	if len(args) > 0 && (args[0] == "validate" || args[0] == "apply") {
		return runDistillProposal(env, ioctx, args[0], args[1:])
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
		includeSplitSources := flagValue(flags, "split-sources", "") == "true"
		for _, rec := range all {
			if !wtdistill.IsDistillSource(rec, includeSplitSources) {
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
			if includeSplitSources {
				return errors.New("no pending distillation source candidates found")
			}
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
		if !wtdistill.IsDistillSource(rec, true) {
			return fmt.Errorf("candidate %q is not a supported distillation source", rec.Meta.ID)
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

func runDistillProposal(env paths.Env, ioctx IO, cmd string, args []string) error {
	flags, positional := splitFlags(args)
	format := flagValue(flags, "format", flagValue(flags, "json", "text"))
	path := firstArg(positional, "")
	if path == "" {
		return fmt.Errorf("usage: worktrail distill %s <proposal.json> [--scope project|user] [--format text|json]", cmd)
	}
	proposal, err := wtdistill.LoadProposal(path)
	if err != nil {
		if format != "json" && format != "true" {
			fmt.Fprintf(ioctx.Out, "Distill %s: failed\n\nError: %s\n", cmd, distillFatalProposalError(err))
		}
		return err
	}
	scope := flagValue(flags, "scope", "project")
	manager := candidate.Manager{Env: env, Actor: "cli:distill-" + cmd}
	var report wtdistill.Report
	if cmd == "apply" {
		report, err = wtdistill.Apply(env, manager, scope, proposal)
	} else {
		report, err = wtdistill.Validate(env, manager, scope, proposal)
	}
	if err != nil {
		return err
	}
	return printDistillProposalReport(ioctx, cmd, proposal, report, format)
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
	fmt.Fprintln(out, "You are the current AI coding agent. Distill the evidence below into a semantic Worktrail proposal JSON.")
	fmt.Fprintln(out, "Do not promote, merge, discard, restore, retire, or write formal knowledge from this pack.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Create only useful pending candidates with semantic knowledge types such as `rule`, `decision`, `architecture`, `integration`, `validation`, `glossary`, `lesson`, `project`, `prompt`, or `workflow`.")
	fmt.Fprintln(out, "Prefer a small number of durable, reusable items over transcript summaries.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Write a JSON file, then run `worktrail distill validate <proposal.json>` and `worktrail distill apply <proposal.json>`.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Proposal JSON shape:")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "```json")
	fmt.Fprintln(out, `{"schema":"worktrail.distill.proposal.v1","source_candidate_ids":["source-id"],"candidates":[{"candidate_type":"rule|decision|architecture|integration|validation|glossary|lesson|project|prompt|workflow","title":"Durable knowledge title","summary":"Short semantic summary","target_path":"rules/name.md|decisions/name.md|architecture/name.md|integrations/name.md|validation/name.md|glossary/name.md|lessons/name.md|project.md|prompts/name.md|workflows/name.md","operation":"replace|merge","tags":["tag"],"evidence_label":"Pending Verification","confidence":0.7,"body":"Markdown content to create as a pending candidate"}]}`)
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
		fmt.Fprintln(out, "### Source Evidence")
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
	mode := "transcript_notes"
	for _, rec := range records {
		if rec.Meta.CandidateType != "transcript_notes" {
			mode = "distillation_sources"
			break
		}
	}
	summary := distillSummary{
		Count:     len(records),
		Scope:     scope,
		Mode:      mode,
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
		fmt.Fprintln(out, "next: have the current AI agent read the pack, write proposal JSON, then run `worktrail distill validate <proposal.json>` and `worktrail distill apply <proposal.json>`")
	} else {
		fmt.Fprintln(out, "next: run `worktrail distill --pending --limit 5 --offset <N>` or `worktrail distill --pending --all --write-pack distill.md`")
	}
}

func printDistillProposalReport(ioctx IO, cmd string, proposal wtdistill.Proposal, report wtdistill.Report, format string) error {
	if format == "json" || format == "true" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	errorsCount := distillReportErrorCount(report)
	fmt.Fprintf(ioctx.Out, "Distill %s: %s\n\n", cmd, distillReportTextStatus(report, errorsCount))
	fmt.Fprintf(ioctx.Out, "Summary: created=%d skipped=%d blocked=%d errors=%d warnings=%d\n", report.Created, report.Skipped, report.Blocked, errorsCount, len(report.Warnings))
	if len(report.Warnings) > 0 {
		fmt.Fprintf(ioctx.Out, "Warnings: %s\n", strings.Join(report.Warnings, ", "))
	}
	for _, section := range []struct {
		Title  string
		Status string
	}{
		{"Created", "created"},
		{"Skipped", "skipped"},
		{"Blocked", "blocked"},
		{"Errors", "error"},
	} {
		var items []wtdistill.ItemReport
		for _, item := range report.Items {
			if item.Status == section.Status {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintln(ioctx.Out)
		fmt.Fprintln(ioctx.Out, section.Title)
		for _, item := range items {
			renderDistillReportItem(ioctx.Out, proposal, item)
		}
	}
	return nil
}

func distillReportErrorCount(report wtdistill.Report) int {
	count := 0
	for _, item := range report.Items {
		if item.Status == "error" {
			count++
		}
	}
	return count
}

func distillReportTextStatus(report wtdistill.Report, errorsCount int) string {
	switch {
	case report.Created > 0 && report.Skipped == 0 && report.Blocked == 0 && errorsCount == 0:
		return "success"
	case report.Created > 0 && (report.Skipped > 0 || report.Blocked > 0 || errorsCount > 0):
		return "partial success"
	case report.Created == 0 && (report.Blocked > 0 || errorsCount > 0):
		return "completed with issues"
	default:
		return "no changes"
	}
}

func renderDistillReportItem(out io.Writer, proposal wtdistill.Proposal, item wtdistill.ItemReport) {
	operation := ""
	if item.ProposalIndex >= 0 && item.ProposalIndex < len(proposal.Candidates) {
		operation = strings.TrimSpace(proposal.Candidates[item.ProposalIndex].Operation)
	}
	if operation == "" {
		operation = "unknown"
	}
	if strings.TrimSpace(item.CandidateID) != "" {
		fmt.Fprintf(out, "- [%d] %s -> %s (%s, %s)\n", item.ProposalIndex, item.CandidateID, item.TargetPath, item.CandidateType, operation)
	} else {
		fmt.Fprintf(out, "- [%d] %s (%s, %s)\n", item.ProposalIndex, item.TargetPath, item.CandidateType, operation)
	}
	if len(item.WarningCodes) > 0 {
		fmt.Fprintf(out, "  warnings: %s\n", strings.Join(item.WarningCodes, ", "))
	}
	if len(item.Errors) > 0 {
		fmt.Fprintf(out, "  errors: %s\n", strings.Join(item.Errors, "; "))
	}
}

func distillFatalProposalError(err error) string {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		return fmt.Sprintf("failed to %s proposal %s: %v", pathErr.Op, filepath.Base(pathErr.Path), pathErr.Err)
	}
	return err.Error()
}

func printDistillHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail distill <candidate-id>")
	fmt.Fprintln(out, "       worktrail distill --pending [--limit N|--all] [--offset N] [--summary|--json|--write-pack file]")
	fmt.Fprintln(out, "       worktrail distill validate <proposal.json> [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail distill apply <proposal.json> [--scope project|user] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Distill prints an agent-facing pack from evidence or validates/applies proposal JSON into pending semantic candidates. It never promotes, merges, discards, restores, or retires knowledge.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "bulk options:")
	fmt.Fprintln(out, "  --pending            select pending transcript_notes")
	fmt.Fprintln(out, "  --split-sources      include allowed pending split-source lessons with transcript_notes")
	fmt.Fprintln(out, "  --limit N            output at most N evidence candidates (default 5)")
	fmt.Fprintln(out, "  --offset N           skip N evidence candidates for paging")
	fmt.Fprintln(out, "  --all                select all pending sources for the chosen mode")
	fmt.Fprintln(out, "  --summary            print ids and next steps only")
	fmt.Fprintln(out, "  --json               print summary JSON only")
	fmt.Fprintln(out, "  --write-pack <file>  write the full pack to a file and print a compact summary")
}
