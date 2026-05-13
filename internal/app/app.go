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
	case "candidates":
		return runCandidates(ctx, env, ioctx, args[1:])
	case "review":
		return runReview(ctx, env, ioctx, args[1:])
	case "promote", "discard":
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
	case "sync":
		return runSync(ctx, env, ioctx, args[1:])
	case "extract":
		return runExtract(ctx, env, ioctx, args[1:])
	case "import":
		return runImport(ctx, env, ioctx, args[1:])
	case "install":
		return runInstall(ctx, env, ioctx, args[1:])
	case "uninstall":
		return runUninstall(ctx, env, ioctx, args[1:])
	case "doctor":
		return runDoctor(ctx, env, ioctx, args[1:])
	case "hook":
		return runHook(ctx, env, ioctx, args[1:])
	case "mcp":
		return runMCP(ctx, env, ioctx, args[1:])
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
	fmt.Fprintln(out, "worktrail: local AI session knowledge and state layer")
	fmt.Fprintln(out, "usage: worktrail <command> [args]")
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
