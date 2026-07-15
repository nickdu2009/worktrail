package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/store"
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
	flags, positional := splitFlagsWithBooleans(rest, map[string]bool{"complete": true, "stdin": true})
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
		toHandoff := flagValue(flags, "to", "") == "handoff"
		if err := validateStateCloseFlags(flags, toHandoff); err != nil {
			return err
		}
		closeArgs, err := parseHandoffArgs(rest)
		if err != nil {
			return err
		}
		if toHandoff {
			if err := validateHandoffStdinArgs("state close --to handoff", closeArgs, "id", "scope", "to"); err != nil {
				return err
			}
		}
		id := flagValue(flags, "id", "latest")
		if id == "latest" {
			var err error
			id, err = latestStateID(env, scope)
			if err != nil {
				return err
			}
		}
		var handoffRecord *handoff.Record
		var closeResult wtstate.CloseResult
		if toHandoff {
			cap, err := wtstate.Show(env, wtstate.ShowOptions{Scope: scope, ID: id, Directory: wtstate.DirActive})
			if err != nil {
				return err
			}
			taskID := wtstate.TaskID(cap)
			if explicitTaskID := strings.TrimSpace(flagValue(flags, "task-id", "")); explicitTaskID != "" && explicitTaskID != taskID {
				return fmt.Errorf("--task-id %q does not match active state task %q", explicitTaskID, taskID)
			}
			var request handoff.CreateRequest
			if closeArgs.boolean("stdin") {
				if ioctx.In == nil {
					return errors.New("--stdin requires JSON input")
				}
				decoder := json.NewDecoder(ioctx.In)
				decoder.DisallowUnknownFields()
				if err := decoder.Decode(&request); err != nil {
					return fmt.Errorf("decode handoff CreateRequest JSON: %w", err)
				}
			} else {
				validation, err := validationFromFlags(closeArgs)
				if err != nil {
					return err
				}
				request = handoff.CreateRequest{
					Summary:       strings.TrimSpace(joinArgs(closeArgs.Positional)),
					Complete:      closeArgs.boolean("complete"),
					ProjectID:     closeArgs.first("project-id", ""),
					NextSteps:     nextStepsFromFlags(closeArgs.values("next-step")),
					OpenQuestions: closeArgs.values("question"),
					Risks:         closeArgs.values("risk"),
					Validation:    validation,
				}
			}
			request.Scope = scope
			request.Title = cap.State.Title
			request.TaskID = taskID
			request.SourceState = &model.Ref{Scope: scope, Kind: "state", ID: cap.State.ID, RelPath: filepath.ToSlash(filepath.Join("state", wtstate.DirArchived, cap.State.ID+".md"))}
			request.Tags = append(request.Tags, "handoff", "state-close")
			request.SourceTool = withDefaultString(request.SourceTool, "worktrail")
			request.Actor = "cli:state-close"
			rec, err := createAndMaybeCloseState(context.Background(), env, request, &cap, true)
			if err != nil {
				return handoffWriteError(env, scope, err)
			}
			closed, err := wtstate.Show(env, wtstate.ShowOptions{Scope: scope, ID: cap.State.ID, Directory: wtstate.DirArchived})
			if err != nil {
				return err
			}
			handoffRecord = &rec
			closeResult = wtstate.CloseResult{Capsule: closed}
		} else {
			result, err := wtstate.Close(env, wtstate.CloseOptions{
				Scope:   scope,
				ID:      id,
				Summary: joinArgs(positional),
				Handoff: false,
				Actor:   "cli:state-close",
			})
			if err != nil {
				return err
			}
			closeResult = result
		}
		if toHandoff {
			if handoffRecord == nil {
				return handoffWriteError(env, scope, fmt.Errorf("handoff was not created"))
			}
			fmt.Fprintf(ioctx.Out, "%s\t%s\n", closeResult.Capsule.State.ID, closeResult.Capsule.Path)
			fmt.Fprintf(ioctx.Out, "%s\t%s\n", handoffRecord.Meta.ID, handoffRecord.RelPath)
			return nil
		}
		return printState(ioctx, closeResult.Capsule, flagValue(flags, "format", "text"))
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
		result, err := wtstate.ListWithDiagnostics(env, wtstate.ListOptions{Scope: scope, Directory: dir})
		if err != nil {
			return err
		}
		if flagValue(flags, "format", "text") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(result)
		}
		for _, item := range result.Capsules {
			fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\t%s\n", item.State.ID, item.State.Status, item.State.Type, item.State.Title)
		}
		for _, diagnostic := range result.Diagnostics {
			fmt.Fprintf(ioctx.Err, "diagnostic\tinvalid_state\t%s\t%s\n", diagnostic.Path, diagnostic.Message)
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
	cap, err := wtstate.LatestExplicit(env, scope)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", errors.New("no explicit active state found")
		}
		return "", err
	}
	return cap.State.ID, nil
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
	fmt.Fprintln(out, "  start <title>                  create the current active work log")
	fmt.Fprintln(out, "  update [--id latest] <note>    append progress to the active work log")
	fmt.Fprintln(out, "  checkpoint [--id latest]       write a recovery checkpoint")
	fmt.Fprintln(out, "  inject [--id latest] <task>    inject task instructions into state")
	fmt.Fprintln(out, "  close [--id latest] <summary>  close the active work log")
	fmt.Fprintln(out, "  close [--id latest] --to handoff (--next-step <action>|--complete) <summary>")
	fmt.Fprintln(out, "  archive <id>                   mark a closed state as archived")
	fmt.Fprintln(out, "  list [--active]                list active and historical state records")
	fmt.Fprintln(out, "  show <id>                      print a state record")
	fmt.Fprintln(out)
	fmt.Fprintln(out, `handoff examples:`)
	fmt.Fprintln(out, `  worktrail state close --to handoff --next-step "continue" "ready for handoff"`)
	fmt.Fprintln(out, `  worktrail state close --to handoff --complete "task complete"`)
}

func validateStateCloseFlags(flags map[string]string, toHandoff bool) error {
	allowed := map[string]bool{
		"id": true, "scope": true, "to": true,
	}
	if toHandoff {
		for _, flag := range []string{
			"stdin", "complete", "project-id", "task-id", "next-step", "question", "risk",
			"validation-status", "validation-command", "validation-note", "validation-exit-code",
		} {
			allowed[flag] = true
		}
	} else {
		allowed["format"] = true
		if value := strings.TrimSpace(flagValue(flags, "to", "")); value != "" {
			return fmt.Errorf("state close --to only supports %q", "handoff")
		}
	}
	for flag := range flags {
		if !allowed[flag] {
			return fmt.Errorf("--%s is not valid for state close", flag)
		}
	}
	return nil
}

func createAndMaybeCloseState(ctx context.Context, env paths.Env, request handoff.CreateRequest, sourceState *wtstate.Capsule, closeState bool) (handoff.Record, error) {
	if sourceState == nil || !closeState {
		return handoff.CreateLocal(ctx, env, request)
	}
	mutation := handoff.Mutation{Build: func() (handoff.Mutation, error) {
		return buildStateCloseMutation(env, request.Scope, *sourceState, request.Summary)
	}}
	return handoff.CreateLocalWithMutation(ctx, env, request, mutation)
}

func buildStateCloseMutation(env paths.Env, scope string, cap wtstate.Capsule, summary string) (handoff.Mutation, error) {
	_, err := env.ScopeRoot(scope)
	if err != nil {
		return handoff.Mutation{}, err
	}
	cap, err = wtstate.Show(env, wtstate.ShowOptions{Scope: scope, ID: cap.State.ID, Directory: wtstate.DirActive})
	if err != nil {
		return handoff.Mutation{}, err
	}
	now := time.Now().UTC()
	meta := make(map[string]any, len(cap.Metadata)+3)
	for key, value := range cap.Metadata {
		meta[key] = value
	}
	meta["status"] = "closed"
	meta["closed_at"] = now
	meta["updated_at"] = now
	body := strings.TrimSpace(cap.Body)
	if summary = strings.TrimSpace(summary); summary != "" {
		if body != "" {
			body += "\n\n"
		}
		body += "## Close Summary\n\n" + summary
	}
	archivedData, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return handoff.Mutation{}, err
	}
	activeRel := filepath.ToSlash(filepath.Join("state", wtstate.DirActive, cap.State.ID+".md"))
	archivedRel := filepath.ToSlash(filepath.Join("state", wtstate.DirArchived, cap.State.ID+".md"))
	aliasRel := filepath.ToSlash(filepath.Join("state", wtstate.DirActive, "latest.md"))
	mutation := handoff.Mutation{
		Writes:  []handoff.FileWrite{{Path: archivedRel, Data: archivedData, Mode: 0o644}},
		Deletes: []string{activeRel},
		Events: []handoff.Event{{
			Name:  "state.close",
			ID:    cap.State.ID,
			Actor: "cli:state-close",
			Data:  map[string]any{"handoff": true, "path": archivedRel},
			Time:  now,
		}},
	}
	active, err := wtstate.List(env, wtstate.ListOptions{Scope: scope, Directory: wtstate.DirActive})
	if err != nil {
		return handoff.Mutation{}, err
	}
	for _, candidate := range active {
		if candidate.State.ID == cap.State.ID {
			continue
		}
		data, err := os.ReadFile(candidate.Path)
		if err != nil {
			return handoff.Mutation{}, err
		}
		mutation.Writes = append(mutation.Writes, handoff.FileWrite{Path: aliasRel, Data: data, Mode: 0o644})
		return mutation, nil
	}
	mutation.Deletes = append(mutation.Deletes, aliasRel)
	return mutation, nil
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
