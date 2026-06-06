package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

func runDraft(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsFlagHelpOrLeadingHelp(args) {
		printDraftHelp(ioctx.Out)
		if len(args) == 0 {
			return errors.New("draft subcommand required")
		}
		return nil
	}
	if args[0] != "create" {
		return fmt.Errorf("unknown draft subcommand %q", args[0])
	}
	flags, positional := splitFlags(args[1:])
	format := flagValue(flags, "format", "text")
	fail := func(err error) error {
		return failCLICommand(ioctx, format, "worktrail draft create", err)
	}
	body, err := noteBody(ioctx.In, flags, positional)
	if err != nil {
		return fail(err)
	}
	typ := strings.TrimSpace(flagValue(flags, "type", ""))
	target := strings.TrimSpace(flagValue(flags, "target", ""))
	title := strings.TrimSpace(flagValue(flags, "title", ""))
	summary := strings.TrimSpace(flagValue(flags, "summary", ""))
	if typ == "" || target == "" || title == "" || summary == "" || strings.TrimSpace(body) == "" {
		return fail(errors.New("draft create requires --type, --target, --title, --summary, and body"))
	}
	if !candidateTypeSupportsDraftCreate(typ) {
		return fail(fmt.Errorf("draft create requires a semantic candidate type, got %q", typ))
	}
	if !targetPathSupportsDraftCreate(typ, target) {
		return fail(fmt.Errorf("candidate type %q does not match target path %q", typ, target))
	}
	if err := validateSemanticDraftText(title, summary, body); err != nil {
		return fail(err)
	}
	rec, err := (candidate.Manager{Env: env, Actor: "cli:draft-create"}).Create(candidate.CreateRequest{
		Scope:         flagValue(flags, "scope", "project"),
		ID:            flagValue(flags, "id", ""),
		CandidateType: typ,
		Topic:         flagValue(flags, "topic", ""),
		TargetPath:    target,
		Title:         title,
		Summary:       summary,
		Operation:     flagValue(flags, "operation", candidate.OperationReplace),
		Tags:          splitCSV(flagValue(flags, "tags", "")),
		Body:          body,
	})
	if err != nil {
		return fail(err)
	}
	if isJSONFormat(format) {
		return printCandidate(ioctx, rec, "json")
	}
	if err := printCandidate(ioctx, rec, "text"); err != nil {
		return err
	}
	fmt.Fprintf(ioctx.Out, "next: worktrail review plan --format json --scope %s\n", rec.Meta.Scope)
	return nil
}

func candidateTypeSupportsDraftCreate(candidateType string) bool {
	return model.IsSemanticCandidateType(candidateType)
}

func targetPathSupportsDraftCreate(candidateType, targetPath string) bool {
	return model.SemanticTargetPathMatches(candidateType, targetPath)
}

func printDraftHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail draft create --type <semantic-type> --target <path> --title <title> --summary <summary> [options] [body]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Creates a pending draft/candidate without requiring an evidence label. Use `note add` when you want to attach explicit evidence labeling metadata.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "options:")
	fmt.Fprintln(out, "  --scope project|user       default project")
	fmt.Fprintln(out, "  --topic <topic>            optional topic/thread identifier")
	fmt.Fprintln(out, "  --from-file draft.md       read draft body from file")
	fmt.Fprintln(out, "  --operation replace|merge  default replace")
	fmt.Fprintln(out, "  --format text|json         JSON failures return worktrail.cli.error.v1 on stdout; check ok, not exit code")
}

func runCheckpoint(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	return runState(ctx, env, ioctx, append([]string{"checkpoint"}, args...))
}

func runTakeover(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printTakeoverHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	id := flagValue(flags, "id", "latest")
	if id == "latest" {
		var err error
		id, err = latestStateID(env, scope)
		if err != nil {
			return err
		}
	}
	note := strings.TrimSpace(joinArgs(positional))
	if note == "" {
		return errors.New("takeover requires a note")
	}
	cap, err := wtstate.Inject(env, wtstate.InjectOptions{
		Scope: scope,
		ID:    id,
		Title: "Takeover Note",
		Body:  note,
		Actor: "cli:takeover",
	})
	if err != nil {
		return err
	}
	return printState(ioctx, cap, flagValue(flags, "format", "text"))
}

func printTakeoverHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail takeover [--scope project|user] [--id latest] [--format text|json] <note>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Appends a takeover note to the active runtime state so the next session can resume from the same task thread.")
}
