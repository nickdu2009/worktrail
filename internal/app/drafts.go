package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/util"
)

func runHandoff(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printHandoffHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{"handoff-only": true})
	scope := flagValue(flags, "scope", "project")
	title := flagValue(flags, "title", "Handoff")
	summary := strings.TrimSpace(joinArgs(positional))
	if summary == "" {
		return errors.New("handoff summary is required")
	}
	handoffOnly := flagValue(flags, "handoff-only", "") == "true"
	sourceState, err := latestStateIfAny(env, scope)
	if err != nil {
		return handoffWriteError(env, scope, err)
	}
	if title == "Handoff" && sourceState != nil {
		title = sourceState.State.Title
	}
	latestHandoff, err := latestHandoffIfAny(env, scope)
	if err != nil {
		return handoffWriteError(env, scope, err)
	}
	sourceStatePath := ""
	if sourceState != nil && !handoffOnly {
		sourceStatePath = projectedArchivedStatePath(env, scope, sourceState.State.ID)
	}
	rec, err := createHandoffRecord(env, createHandoffRecordOptions{
		Scope:           scope,
		Title:           title,
		Summary:         summary,
		SourceState:     sourceState,
		SourceStatePath: sourceStatePath,
		Previous:        latestHandoff,
		Tags:            []string{"handoff", "manual"},
		Actor:           "cli:handoff",
	})
	if err != nil {
		return handoffWriteError(env, scope, err)
	}
	if sourceState != nil && !handoffOnly {
		if _, err := wtstate.Close(env, wtstate.CloseOptions{
			Scope:   scope,
			ID:      sourceState.State.ID,
			Summary: summary,
			Handoff: true,
			Actor:   "cli:handoff",
		}); err != nil {
			return err
		}
	}
	return printHandoffRecord(ioctx, rec, flagValue(flags, "format", "text"))
}

type createHandoffRecordOptions struct {
	Scope           string
	Title           string
	Summary         string
	SourceState     *wtstate.Capsule
	SourceStatePath string
	Previous        *handoff.Record
	Tags            []string
	Actor           string
}

func createHandoffRecord(env paths.Env, opts createHandoffRecordOptions) (handoff.Record, error) {
	summary := strings.TrimSpace(opts.Summary)
	if summary == "" {
		return handoff.Record{}, errors.New("handoff summary is required")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Handoff"
	}
	return handoff.Create(env, handoff.CreateOptions{
		Scope:             opts.Scope,
		Title:             title,
		Summary:           summary,
		TaskID:            taskIDForHandoff(opts.SourceState, opts.Previous, title),
		SourceStateID:     sourceStateID(opts.SourceState),
		PreviousHandoffID: previousHandoffID(opts.Previous),
		Tags:              opts.Tags,
		Body:              renderHandoffRecordBody(title, summary, opts.SourceState, opts.SourceStatePath, opts.Previous),
		Actor:             opts.Actor,
	})
}

func handoffWriteError(env paths.Env, scope string, err error) error {
	root, rootErr := env.ScopeRoot(scope)
	if rootErr != nil {
		return err
	}
	return fmt.Errorf("handoff write failed for target %s; ensure the sandbox allows writes to %s: %w", filepath.Join(root, "handoffs"), strings.Join(requiredWorktrailWriteDirs(root), ", "), err)
}

func requiredWorktrailWriteDirs(root string) []string {
	return []string{
		filepath.Join(root, "handoffs"),
		filepath.Join(root, "logs"),
	}
}

func printHandoffHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail handoff [--scope project|user] [--title <title>] [--format text|json] [--handoff-only] <summary>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Writes a durable handoff record under `.worktrail/handoffs/`. By default, an active explicit state is closed after the handoff is created; use `--handoff-only` to keep the active state open.")
}

func printHandoffRecord(ioctx IO, rec handoff.Record, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(rec)
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\n", rec.Meta.ID, rec.Path)
	return nil
}

func latestStateIfAny(env paths.Env, scope string) (*wtstate.Capsule, error) {
	cap, err := wtstate.LatestExplicit(env, scope)
	if err == nil {
		return &cap, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

func latestHandoffIfAny(env paths.Env, scope string) (*handoff.Record, error) {
	rec, err := handoff.Latest(env, scope)
	if err == nil {
		return &rec, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

func taskIDForHandoff(sourceState *wtstate.Capsule, previous *handoff.Record, title string) string {
	if sourceState != nil {
		if taskID := wtstate.TaskID(*sourceState); taskID != "" {
			return taskID
		}
	}
	if previous != nil && strings.TrimSpace(previous.Meta.TaskID) != "" {
		return previous.Meta.TaskID
	}
	return "task-" + util.Slug(title)
}

func sourceStateID(sourceState *wtstate.Capsule) string {
	if sourceState == nil {
		return ""
	}
	return sourceState.State.ID
}

func previousHandoffID(previous *handoff.Record) string {
	if previous == nil {
		return ""
	}
	return previous.Meta.ID
}

func renderHandoffRecordBody(title, summary string, sourceState *wtstate.Capsule, sourceStatePath string, previous *handoff.Record) string {
	var b strings.Builder
	b.WriteString("# Handoff: ")
	b.WriteString(title)
	b.WriteString("\n\n## Summary\n\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n")
	if sourceState != nil {
		path := sourceStatePath
		if strings.TrimSpace(path) == "" {
			path = sourceState.Path
		}
		b.WriteString("## Source State\n\n")
		fmt.Fprintf(&b, "- State ID: %s\n- Task ID: %s\n- Path: `%s`\n\n", sourceState.State.ID, wtstate.TaskID(*sourceState), filepathToSlash(path))
		b.WriteString("## State Snapshot\n\n")
		b.WriteString(strings.TrimSpace(sourceState.Body))
		b.WriteString("\n\n")
	}
	if previous != nil {
		b.WriteString("## Previous Handoff\n\n")
		fmt.Fprintf(&b, "- Handoff ID: %s\n- Path: `%s`\n\n", previous.Meta.ID, filepathToSlash(previous.Path))
	}
	b.WriteString("## Next Step\n\n")
	if sourceState != nil {
		b.WriteString("Read the linked state and continue from the latest validated point.\n")
	} else {
		b.WriteString("Read the linked knowledge and update the active state before continuing.\n")
	}
	return b.String()
}

func projectedArchivedStatePath(env paths.Env, scope, id string) string {
	root, err := env.ScopeRoot(scope)
	if err != nil || strings.TrimSpace(id) == "" {
		return ""
	}
	return filepath.Join(root, "state", wtstate.DirArchived, id+".md")
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func runADR(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || args[0] != "create" {
		return errors.New("usage: worktrail adr create <title>")
	}
	flags, positional := splitFlags(args[1:])
	scope := flagValue(flags, "scope", "project")
	title := joinArgs(positional)
	if title == "" {
		title = flagValue(flags, "title", "Architecture Decision")
	}
	stamp := time.Now().UTC().Format("20060102")
	body := fmt.Sprintf("# ADR: %s\n\n## Status\n\nProposed\n\n## Context\n\n## Decision\n\n## Consequences\n", title)
	rec, err := (candidate.Manager{Env: env, Actor: "cli:adr"}).Create(candidate.CreateRequest{
		Scope:         scope,
		CandidateType: "adr",
		TargetPath:    filepath.ToSlash(filepath.Join("decisions", "ADR-"+stamp+"-"+util.Slug(title)+".md")),
		Title:         title,
		Summary:       "Draft ADR candidate.",
		Operation:     "create",
		Body:          body,
	})
	if err != nil {
		return err
	}
	return printCandidate(ioctx, rec, flagValue(flags, "format", "text"))
}
