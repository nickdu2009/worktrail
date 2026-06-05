package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func runNote(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsFlagHelpOrLeadingHelp(args) {
		printNoteHelp(ioctx.Out)
		if len(args) == 0 {
			return errors.New("note subcommand required")
		}
		return nil
	}
	if args[0] != "add" {
		return fmt.Errorf("unknown note subcommand %q", args[0])
	}
	flags, positional := splitFlags(args[1:])
	body, err := noteBody(ioctx.In, flags, positional)
	if err != nil {
		return err
	}
	typ := strings.TrimSpace(flagValue(flags, "type", ""))
	target := strings.TrimSpace(flagValue(flags, "target", ""))
	title := strings.TrimSpace(flagValue(flags, "title", ""))
	summary := strings.TrimSpace(flagValue(flags, "summary", ""))
	evidenceLabel := strings.TrimSpace(flagValue(flags, "evidence-label", ""))
	if typ == "" || target == "" || title == "" || summary == "" || evidenceLabel == "" || strings.TrimSpace(body) == "" {
		return errors.New("note add requires --type, --target, --title, --summary, --evidence-label, and body")
	}
	if !model.IsSemanticCandidateType(typ) {
		return fmt.Errorf("note add requires a semantic candidate type, got %q", typ)
	}
	if !model.SemanticTargetPathMatches(typ, target) {
		return fmt.Errorf("candidate type %q does not match target path %q", typ, target)
	}
	confidence := 0.7
	if raw := strings.TrimSpace(flagValue(flags, "confidence", "")); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err != nil || parsed <= 0 || parsed > 1 {
			return fmt.Errorf("--confidence must be > 0 and <= 1")
		}
		confidence = parsed
	}
	rec, err := (candidate.Manager{Env: env, Actor: "cli:note-add"}).Create(candidate.CreateRequest{
		Scope:         flagValue(flags, "scope", "project"),
		ID:            flagValue(flags, "id", ""),
		CandidateType: typ,
		Topic:         flagValue(flags, "topic", ""),
		TargetPath:    target,
		Title:         title,
		Summary:       summary,
		Operation:     flagValue(flags, "operation", candidate.OperationReplace),
		EvidenceLabel: evidenceLabel,
		Confidence:    confidence,
		Tags:          splitCSV(flagValue(flags, "tags", "")),
		Body:          body,
	})
	if err != nil {
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		return printCandidate(ioctx, rec, "json")
	}
	if err := printCandidate(ioctx, rec, "text"); err != nil {
		return err
	}
	fmt.Fprintf(ioctx.Out, "next: worktrail review plan --format json --scope %s\n", rec.Meta.Scope)
	return nil
}

func noteBody(in io.Reader, flags map[string]string, positional []string) (string, error) {
	if path := strings.TrimSpace(flagValue(flags, "from-file", "")); path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		return string(b), nil
	}
	body := joinArgs(positional)
	if body == "" && in != nil {
		b, err := io.ReadAll(in)
		if err != nil {
			return "", err
		}
		body = string(b)
	}
	return body, nil
}

func printNoteHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail note add --type <semantic-type> --target <path> --title <title> --summary <summary> --evidence-label <label> [options] [body]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Creates a pending semantic candidate only. It never edits formal knowledge or promotes candidates.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "options:")
	fmt.Fprintln(out, "  --scope project|user       default project")
	fmt.Fprintln(out, "  --topic <topic>            optional topic/thread identifier")
	fmt.Fprintln(out, "  --confidence 0.7           confidence > 0 and <= 1")
	fmt.Fprintln(out, "  --from-file draft.md       read candidate body from file")
	fmt.Fprintln(out, "  --operation replace|merge  default replace")
	fmt.Fprintln(out, "  --format text|json")
}
