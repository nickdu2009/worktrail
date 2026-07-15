package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtruntime "github.com/nickdu2009/worktrail/internal/runtime"
)

type runtimePruneReport struct {
	Schema  string                  `json:"schema"`
	OK      bool                    `json:"ok"`
	Applied bool                    `json:"applied"`
	Plan    wtruntime.PruneProposal `json:"plan"`
	Result  wtruntime.PruneResult   `json:"result"`
}

func runRuntime(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || isHelpArg(args[0]) {
		printRuntimeHelp(ioctx.Out)
		return nil
	}
	switch args[0] {
	case "prune":
		return runRuntimePrune(ctx, env, ioctx, args[1:])
	default:
		return fmt.Errorf("unknown runtime subcommand %q", args[0])
	}
}

func runRuntimePrune(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	for _, arg := range args {
		if isHelpArg(arg) {
			printRuntimeHelp(ioctx.Out)
			return nil
		}
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{
		"apply":   true,
		"confirm": true,
		"json":    true,
	})
	format := flagValue(flags, "format", "text")
	if flags["json"] == "true" {
		format = "json"
	}
	fail := func(err error) error {
		return failCLICommand(ioctx, format, "worktrail runtime prune", err)
	}
	if len(positional) > 0 {
		return fail(fmt.Errorf("usage: worktrail runtime prune [--scope project|user] [--apply --confirm] [--format text|json]"))
	}
	for key := range flags {
		switch key {
		case "scope", "format", "apply", "confirm", "json":
		default:
			return fail(fmt.Errorf("unknown runtime prune flag --%s", key))
		}
	}
	if format != "text" && format != "json" {
		return fail(fmt.Errorf("runtime prune format must be text or json"))
	}
	apply := flags["apply"] == "true"
	confirm := flags["confirm"] == "true"
	if apply != confirm {
		return fail(errors.New("worktrail runtime prune deletion requires both --apply and --confirm"))
	}
	select {
	case <-ctx.Done():
		return fail(ctx.Err())
	default:
	}
	scope := strings.TrimSpace(flagValue(flags, "scope", "project"))
	plan, err := wtruntime.PrunePlan(env, scope, time.Now().UTC())
	if err != nil {
		return fail(err)
	}
	report := runtimePruneReport{
		Schema: "worktrail.runtime.prune.v1",
		OK:     true,
		Plan:   plan,
	}
	if apply {
		report.Result, err = wtruntime.ApplyPrune(plan)
		if err != nil {
			return fail(err)
		}
		report.Applied = true
	}
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "runtime prune scope=%s mode=%s items=%d\n", plan.Scope, runtimePruneMode(report.Applied), len(plan.Items))
	for _, item := range plan.Items {
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", item.Reason, item.RelPath, item.ContentHash)
	}
	if report.Applied {
		fmt.Fprintf(ioctx.Out, "deleted=%d operation=%s\n", report.Result.Deleted, report.Result.OperationID)
	} else {
		fmt.Fprintln(ioctx.Out, "dry-run: no files deleted; use --apply --confirm to apply")
	}
	return nil
}

func runtimePruneMode(applied bool) string {
	if applied {
		return "applied"
	}
	return "dry-run"
}

func isHelpArg(arg string) bool {
	return arg == "help" || arg == "--help" || arg == "-h"
}

func printRuntimeHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail runtime prune [--scope project|user] [--apply --confirm] [--format text|json]")
	fmt.Fprintln(out, "Runtime pruning is a dry-run by default. Deletion requires both --apply and --confirm.")
}
