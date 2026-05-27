package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

func Run(ctx context.Context, args []string, in io.Reader, out io.Writer, errw io.Writer) error {
	ioctx := IO{In: in, Out: out, Err: errw}
	if len(args) == 0 {
		usage(out)
		return nil
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		usage(out)
		return nil
	}
	env, err := paths.Discover()
	if err != nil {
		return err
	}
	switch args[0] {
	case "init-user":
		if err := store.InitUser(env); err != nil {
			return err
		}
		fmt.Fprintln(out, "initialized user worktrail:", env.UserRoot)
	case "init-project":
		if err := store.InitProject(env); err != nil {
			return err
		}
		fmt.Fprintln(out, "initialized project worktrail:", env.ProjectWT)
	case "init":
		if err := store.InitUser(env); err != nil {
			return err
		}
		if err := store.InitProject(env); err != nil {
			return err
		}
		fmt.Fprintln(out, "initialized user and project worktrail")
	case "state":
		return runState(ctx, env, ioctx, args[1:])
	case "resume":
		return runResume(ctx, env, ioctx, args[1:])
	case "candidates":
		return runCandidates(ctx, env, ioctx, args[1:])
	case "review":
		return runReview(ctx, env, ioctx, args[1:])
	case "evidence":
		return runEvidence(ctx, env, ioctx, args[1:])
	case "promote", "discard", "restore", "retire":
		return runCandidateAction(ctx, env, ioctx, args[0], args[1:])
	case "merge":
		return runMerge(ctx, env, ioctx, args[1:])
	case "redact":
		return runRedact(ctx, env, ioctx, args[1:])
	case "index":
		return runIndex(ctx, env, ioctx, args[1:])
	case "search":
		return runSearch(ctx, env, ioctx, args[1:])
	case "context":
		return runContextPack(ctx, env, ioctx, args[1:])
	case "note":
		return runNote(ctx, env, ioctx, args[1:])
	case "preview":
		return runPreview(ctx, env, ioctx, args[1:])
	case "sync":
		return runSync(ctx, env, ioctx, args[1:])
	case "extract":
		return runExtract(ctx, env, ioctx, args[1:])
	case "import":
		return runImport(ctx, env, ioctx, args[1:])
	case "migrate":
		return runMigrate(ctx, env, ioctx, args[1:])
	case "distill":
		return runDistill(ctx, env, ioctx, args[1:])
	case "maintain":
		return runMaintain(ctx, env, ioctx, args[1:])
	case "install":
		return runInstall(ctx, env, ioctx, args[1:])
	case "uninstall":
		return runUninstall(ctx, env, ioctx, args[1:])
	case "doctor":
		return runDoctor(ctx, env, ioctx, args[1:])
	case "hook":
		return runHook(ctx, env, ioctx, args[1:])
	case "handoff":
		return runHandoff(ctx, env, ioctx, args[1:])
	case "adr":
		return runADR(ctx, env, ioctx, args[1:])
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	return nil
}

func usage(out io.Writer) {
	fmt.Fprintln(out, "worktrail: local knowledge base, work log, and handoff tool")
	fmt.Fprintln(out, "usage: worktrail <command> [args]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "main workflows:")
	fmt.Fprintln(out, "  worktrail context <task>")
	fmt.Fprintln(out, "  worktrail preview [--scope project|user] [--no-open]")
	fmt.Fprintln(out, "  worktrail search <keyword>")
	fmt.Fprintln(out, "  worktrail state start <title>")
	fmt.Fprintln(out, "  worktrail state update <note>")
	fmt.Fprintln(out, "  worktrail handoff <summary>")
	fmt.Fprintln(out, "  worktrail resume [<task>]")
	fmt.Fprintln(out, "  worktrail doctor knowledge")
	fmt.Fprintln(out, "  worktrail doctor delete <path>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "knowledge and maintenance:")
	fmt.Fprintln(out, "  worktrail context --stage requirements <task>")
	fmt.Fprintln(out, "  worktrail context --include-lifecycle historical <task>")
	fmt.Fprintln(out, "  worktrail context --evidence <task>")
	fmt.Fprintln(out, "  worktrail index diff [--scope project|user|all]")
	fmt.Fprintln(out, "  worktrail import codex [--since 14d|--limit N] [--all]")
	fmt.Fprintln(out, "  worktrail import cursor [--file path] [--limit N] [--all]")
	fmt.Fprintln(out, "  worktrail migrate kdd [--write-candidates]")
	fmt.Fprintln(out, "  worktrail distill --pending [--limit N|--all] [--write-pack file]")
	fmt.Fprintln(out, "  worktrail candidates create --help")
	fmt.Fprintln(out, "  worktrail review [--semantic|--evidence|--all]")
	fmt.Fprintln(out, "  worktrail evidence plan [--status active|archived|all]")
	fmt.Fprintln(out, "  worktrail note add --type rule --target rules/example.md --title <title> --summary <summary> --evidence-label <label> <body>")
	fmt.Fprintln(out, "  worktrail maintain knowledge [--format json]")
	fmt.Fprintln(out, "  worktrail promote <candidate-id>")
	fmt.Fprintln(out, "  worktrail merge <candidate-id>")
	fmt.Fprintln(out, "  worktrail discard <candidate-id>")
	fmt.Fprintln(out, "  worktrail restore <candidate-id>")
	fmt.Fprintln(out, "  worktrail retire <candidate-id> --reason <text>")
}

func stringFlag(args []string, name, def string) (string, []string, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	value := fs.String(name, def, "")
	if err := fs.Parse(args); err != nil {
		return "", nil, err
	}
	return *value, fs.Args(), nil
}

func joinArgs(args []string) string {
	return strings.TrimSpace(strings.Join(args, " "))
}
