package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

func runState(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("state subcommand required")
	}
	if wantsStateHelp(args) {
		printStateHelp(ioctx.Out)
		return nil
	}
	cmd, rest := args[0], args[1:]
	flags, positional := splitFlags(rest)
	scope := flagValue(flags, "scope", "project")
	switch cmd {
	case "start":
		title := joinArgs(positional)
		cap, err := wtstate.Start(env, wtstate.StartOptions{
			Scope:      scope,
			Type:       flagValue(flags, "type", "session"),
			Title:      title,
			SourceTool: flagValue(flags, "source-tool", "worktrail"),
			Tags:       splitCSV(flagValue(flags, "tags", "")),
			Body:       defaultStateBody(title),
			Actor:      "cli:state-start",
		})
		if err != nil {
			return err
		}
		return printState(ioctx, cap, flagValue(flags, "format", "text"))
	case "update":
		id := flagValue(flags, "id", "latest")
		if id == "latest" {
			var err error
			id, err = latestStateID(env, scope)
			if err != nil {
				return err
			}
		}
		session := flagValue(flags, "session", "")
		note := strings.TrimSpace(joinArgs(positional))
		if session != "" {
			note = strings.TrimSpace(note + "\n\nSession: " + session)
		}
		cap, err := wtstate.Update(env, wtstate.UpdateOptions{
			Scope:      scope,
			ID:         id,
			AppendBody: note,
			Actor:      "cli:state-update",
		})
		if err != nil {
			return err
		}
		return printState(ioctx, cap, flagValue(flags, "format", "text"))
	case "checkpoint":
		id := flagValue(flags, "id", "latest")
		if id == "latest" {
			var err error
			id, err = latestStateID(env, scope)
			if err != nil {
				return err
			}
		}
		cap, err := wtstate.Checkpoint(env, wtstate.CheckpointOptions{
			Scope: scope,
			ID:    id,
			Note:  flagValue(flags, "reason", joinArgs(positional)),
			Actor: "cli:state-checkpoint",
		})
		if err != nil {
			return err
		}
		return printState(ioctx, cap, flagValue(flags, "format", "text"))
	case "inject":
		id := flagValue(flags, "id", "latest")
		if id == "latest" {
			var err error
			id, err = latestStateID(env, scope)
			if err != nil {
				return err
			}
		}
		cap, err := wtstate.Inject(env, wtstate.InjectOptions{
			Scope: scope,
			ID:    id,
			Title: "Task Instruction",
			Body:  joinArgs(positional),
			Actor: "cli:state-inject",
		})
		if err != nil {
			return err
		}
		return printState(ioctx, cap, flagValue(flags, "format", "text"))
	case "close":
		id := flagValue(flags, "id", "latest")
		if id == "latest" {
			var err error
			id, err = latestStateID(env, scope)
			if err != nil {
				return err
			}
		}
		result, err := wtstate.Close(env, wtstate.CloseOptions{
			Scope:   scope,
			ID:      id,
			Summary: joinArgs(positional),
			Handoff: flagValue(flags, "to", "") == "handoff",
			Actor:   "cli:state-close",
		})
		if err != nil {
			return err
		}
		if result.HandoffBody != "" {
			fmt.Fprintln(ioctx.Out, result.HandoffBody)
			return nil
		}
		return printState(ioctx, result.Capsule, flagValue(flags, "format", "text"))
	case "archive":
		id := firstArg(positional, flagValue(flags, "id", ""))
		cap, err := wtstate.Archive(env, wtstate.ArchiveOptions{Scope: scope, ID: id, Actor: "cli:state-archive"})
		if err != nil {
			return err
		}
		return printState(ioctx, cap, flagValue(flags, "format", "text"))
	case "list":
		dir := "all"
		if _, ok := flags["active"]; ok {
			dir = wtstate.DirActive
		}
		items, err := wtstate.List(env, wtstate.ListOptions{Scope: scope, Directory: dir})
		if err != nil {
			return err
		}
		if flagValue(flags, "format", "text") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(items)
		}
		for _, item := range items {
			fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\t%s\n", item.State.ID, item.State.Status, item.State.Type, item.State.Title)
		}
		return nil
	case "show":
		id := firstArg(positional, flagValue(flags, "id", ""))
		cap, err := wtstate.Show(env, wtstate.ShowOptions{Scope: scope, ID: id, Directory: flagValue(flags, "directory", wtstate.DirActive)})
		if err != nil {
			return err
		}
		if flagValue(flags, "format", "markdown") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(cap)
		}
		fmt.Fprint(ioctx.Out, cap.Body)
		return nil
	default:
		return fmt.Errorf("unknown state subcommand %q", cmd)
	}
}

func latestStateID(env paths.Env, scope string) (string, error) {
	items, err := wtstate.List(env, wtstate.ListOptions{Scope: scope, Directory: wtstate.DirActive})
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", errors.New("no active state found")
	}
	return items[0].State.ID, nil
}

func printState(ioctx IO, cap wtstate.Capsule, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(cap)
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", cap.State.ID, cap.State.Status, cap.Path)
	return nil
}

func printStateHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail state <start|update|checkpoint|inject|close|archive|list|show> [options]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "subcommands:")
	fmt.Fprintln(out, "  start <title>                 create an active state capsule")
	fmt.Fprintln(out, "  update [--id latest] <note>    append progress to an active state")
	fmt.Fprintln(out, "  checkpoint [--id latest]       write a checkpoint from active state")
	fmt.Fprintln(out, "  inject [--id latest] <task>    inject task instructions into state")
	fmt.Fprintln(out, "  close [--id latest] <summary>  close active state")
	fmt.Fprintln(out, "  archive <id>                   archive a state capsule")
	fmt.Fprintln(out, "  list [--active]                list state capsules")
	fmt.Fprintln(out, "  show <id>                      print a state capsule")
}

func defaultStateBody(title string) string {
	if strings.TrimSpace(title) == "" {
		title = "Untitled"
	}
	return "# State Capsule: " + title + "\n\n## Original Intent\n\n## Current Goal\n\n## Constraints\n\n## Relevant Context\n\n## Evidence\n\n## Decisions Made\n\n## Assumptions\n\n## Ruled Out\n\n## Work Done\n\n## Current Diff Intent\n\n## Validation\n\n## Open Questions\n\n## Next Step\n\n## Do Not Forget\n"
}

func splitFlags(args []string) (map[string]string, []string) {
	return splitFlagsWithBooleans(args, nil)
}

func splitFlagsWithBooleans(args []string, booleanFlags map[string]bool) (map[string]string, []string) {
	flags := map[string]string{}
	var positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "--") {
			positional = append(positional, arg)
			continue
		}
		key := strings.TrimPrefix(arg, "--")
		if strings.Contains(key, "=") {
			parts := strings.SplitN(key, "=", 2)
			flags[parts[0]] = parts[1]
			continue
		}
		if booleanFlags[key] {
			flags[key] = "true"
			continue
		}
		if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			flags[key] = args[i+1]
			i++
			continue
		}
		flags[key] = "true"
	}
	return flags, positional
}

func flagValue(flags map[string]string, key, def string) string {
	if value, ok := flags[key]; ok {
		return value
	}
	return def
}

func scopeAwareCommand(scope string, parts ...string) string {
	if scope == "user" {
		parts = append(parts, "--scope", "user")
	}
	return strings.Join(parts, " ")
}

func firstArg(args []string, fallback string) string {
	if len(args) > 0 {
		return args[0]
	}
	return fallback
}

func wantsHelp(args []string) bool {
	for _, arg := range args {
		if arg == "--help" || arg == "-h" || arg == "help" {
			return true
		}
	}
	return false
}

func wantsFlagHelpOrLeadingHelp(args []string) bool {
	if len(args) == 0 {
		return false
	}
	if args[0] == "--help" || args[0] == "-h" {
		return true
	}
	if len(args) == 1 && args[0] == "help" {
		return true
	}
	for _, arg := range args {
		if arg == "--help" || arg == "-h" {
			return true
		}
	}
	return false
}

func wantsStateHelp(args []string) bool {
	if wantsFlagHelpOrLeadingHelp(args) {
		return true
	}
	return len(args) == 2 && args[1] == "help"
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}
