package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/nickdu2009/worktrail/internal/hooks"
	"github.com/nickdu2009/worktrail/internal/integrations"
	"github.com/nickdu2009/worktrail/internal/mcp"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func runInstall(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("install target required")
	}
	flags, _ := splitFlags(args[1:])
	opts := integrationOptions(flags)
	target := args[0]
	if target == "all" {
		for _, tool := range []integrations.Tool{integrations.ToolCodex, integrations.ToolClaude} {
			report, err := integrations.Install(env, tool, opts)
			if err != nil {
				return err
			}
			printIntegrationReport(ioctx, report, flagValue(flags, "format", "text"))
		}
		return nil
	}
	report, err := integrations.Install(env, integrations.Tool(target), opts)
	if err != nil {
		return err
	}
	printIntegrationReport(ioctx, report, flagValue(flags, "format", "text"))
	return nil
}

func runUninstall(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("uninstall target required")
	}
	flags, _ := splitFlags(args[1:])
	report, err := integrations.Uninstall(env, integrations.Tool(args[0]), integrationOptions(flags))
	if err != nil {
		return err
	}
	printIntegrationReport(ioctx, report, flagValue(flags, "format", "text"))
	return nil
}

func runDoctor(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(ioctx.Out, "user: %s\nproject: %s\n", env.UserRoot, env.ProjectWT)
		return nil
	}
	flags, _ := splitFlags(args[1:])
	report, err := integrations.Doctor(env, integrations.Tool(args[0]), integrationOptions(flags))
	if err != nil {
		return err
	}
	printIntegrationReport(ioctx, report, flagValue(flags, "format", "text"))
	return nil
}

func runHook(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) < 2 {
		return errors.New("usage: worktrail hook <codex|claude> <event>")
	}
	return hooks.Run(ctx, env, args[0], args[1], ioctx.In, ioctx.Out)
}

func runMCP(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) != 2 || args[0] != "serve" || args[1] != "--stdio" {
		return errors.New("usage: worktrail mcp serve --stdio")
	}
	return mcp.Serve(ctx, env, ioctx.In, ioctx.Out)
}

func integrationOptions(flags map[string]string) integrations.Options {
	user := flags["user"] == "true"
	project := flags["project"] == "true"
	if !user && !project {
		return integrations.Options{User: true}
	}
	return integrations.Options{User: user, Project: project}
}

func printIntegrationReport(ioctx IO, report integrations.Report, format string) {
	if format == "json" {
		_ = json.NewEncoder(ioctx.Out).Encode(report)
		return
	}
	if len(report.Actions) > 0 {
		for _, action := range report.Actions {
			fmt.Fprintf(ioctx.Out, "%s\t%s\n", action.Action, action.Path)
		}
	}
	if len(report.Checks) > 0 {
		for _, check := range report.Checks {
			fmt.Fprintf(ioctx.Out, "%v\t%s\t%s\t%s\n", check.OK, check.Name, check.Path, check.Note)
		}
	}
}
