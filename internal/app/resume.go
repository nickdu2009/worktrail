package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/util"
)

type resumeResult struct {
	State         wtstate.Capsule  `json:"state"`
	SourceState   *wtstate.Capsule `json:"source_state,omitempty"`
	SourceHandoff *handoff.Record  `json:"source_handoff,omitempty"`
}

func runResume(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printResumeHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	sourceState, err := latestStateIfAny(env, scope)
	if err != nil {
		return err
	}
	sourceHandoff, err := latestHandoffIfAny(env, scope)
	if err != nil {
		return err
	}
	if sourceState == nil && sourceHandoff == nil {
		return errors.New("resume requires an active state or a handoff record")
	}
	title := strings.TrimSpace(joinArgs(positional))
	if title == "" {
		switch {
		case sourceState != nil:
			title = sourceState.State.Title
		case sourceHandoff != nil:
			title = sourceHandoff.Meta.Title
		default:
			title = "Resumed Session"
		}
	}
	body := renderResumeStateBody(title, sourceState, projectedArchivedStatePath(env, scope, resumedStateID(sourceState)), sourceHandoff)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:                scope,
		TaskID:               resumeTaskID(sourceState, sourceHandoff, title),
		Type:                 "session",
		Title:                title,
		SourceTool:           "worktrail",
		Tags:                 []string{"resume"},
		Body:                 body,
		ResumedFromStateID:   resumedStateID(sourceState),
		ResumedFromHandoffID: resumedHandoffID(sourceHandoff),
		Actor:                "cli:resume",
	})
	if err != nil {
		return err
	}
	if sourceState != nil {
		if _, err := wtstate.Close(env, wtstate.CloseOptions{
			Scope:   scope,
			ID:      sourceState.State.ID,
			Summary: "Superseded by worktrail resume.",
			Actor:   "cli:resume-close-source",
		}); err != nil {
			return err
		}
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(resumeResult{State: cap, SourceState: sourceState, SourceHandoff: sourceHandoff})
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\n", cap.State.ID, cap.Path)
	return nil
}

func printResumeHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail resume [--scope project|user] [--format text|json] [<task>]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Creates a fresh active state from the latest active state and/or durable handoff.")
}

func resumeTaskID(sourceState *wtstate.Capsule, sourceHandoff *handoff.Record, title string) string {
	if sourceState != nil {
		if taskID := wtstate.TaskID(*sourceState); taskID != "" {
			return taskID
		}
	}
	if sourceHandoff != nil && strings.TrimSpace(sourceHandoff.Meta.TaskID) != "" {
		return sourceHandoff.Meta.TaskID
	}
	return "task-" + util.Slug(title)
}

func resumedStateID(sourceState *wtstate.Capsule) string {
	if sourceState == nil {
		return ""
	}
	return sourceState.State.ID
}

func resumedHandoffID(sourceHandoff *handoff.Record) string {
	if sourceHandoff == nil {
		return ""
	}
	return sourceHandoff.Meta.ID
}

func renderResumeStateBody(title string, sourceState *wtstate.Capsule, sourceStatePath string, sourceHandoff *handoff.Record) string {
	var b strings.Builder
	b.WriteString("# State Capsule: ")
	b.WriteString(title)
	b.WriteString("\n\n## Original Intent\n\nResume the task from the latest Worktrail records.\n\n")
	b.WriteString("## Current Goal\n\n")
	if sourceState != nil {
		b.WriteString(sourceState.State.Title)
	} else if sourceHandoff != nil {
		b.WriteString(sourceHandoff.Meta.Title)
	} else {
		b.WriteString(title)
	}
	b.WriteString("\n\n## Constraints\n\nRead the linked state and handoff documents directly before continuing.\n\n")
	b.WriteString("## Relevant Context\n\n")
	if sourceState != nil {
		path := sourceStatePath
		if strings.TrimSpace(path) == "" {
			path = sourceState.Path
		}
		fmt.Fprintf(&b, "- Prior active state: `%s`\n", filepathToSlash(path))
	}
	if sourceHandoff != nil {
		fmt.Fprintf(&b, "- Latest handoff: `%s`\n", filepathToSlash(sourceHandoff.Path))
	}
	b.WriteString("\n## Evidence\n\nUse the linked records as the primary recovery source.\n\n")
	b.WriteString("## Decisions Made\n\n## Assumptions\n\n## Ruled Out\n\n")
	b.WriteString("## Work Done\n\n")
	if sourceState != nil {
		b.WriteString("See the prior state snapshot below.\n")
	}
	b.WriteString("\n## Current Diff Intent\n\nContinue from the last validated point instead of recomputing context manually.\n\n")
	b.WriteString("## Validation\n\n")
	if sourceHandoff != nil && strings.TrimSpace(sourceHandoff.Meta.Summary) != "" {
		b.WriteString(sourceHandoff.Meta.Summary)
	} else {
		b.WriteString("Review the latest state and handoff validation notes before making changes.")
	}
	b.WriteString("\n\n## Open Questions\n\n## Next Step\n\nRead the linked records, confirm the next safe action, and continue work.\n")
	if sourceState != nil {
		b.WriteString("\n\n## Prior State Snapshot\n\n")
		b.WriteString(strings.TrimSpace(sourceState.Body))
	}
	if sourceHandoff != nil {
		b.WriteString("\n\n## Latest Handoff\n\n")
		b.WriteString(strings.TrimSpace(sourceHandoff.Body))
	}
	return b.String()
}
