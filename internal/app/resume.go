package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/recovery"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/util"
)

type resumeResult struct {
	State           wtstate.Capsule  `json:"state"`
	SourceState     *wtstate.Capsule `json:"source_state,omitempty"`
	SourceHandoff   *handoff.Record  `json:"source_handoff,omitempty"`
	RecoverySource  string           `json:"recovery_source_kind,omitempty"`
	RecoveryQuality string           `json:"recovery_quality,omitempty"`
	DegradedReason  string           `json:"degraded_reason,omitempty"`
	RuntimePath     string           `json:"runtime_path,omitempty"`
}

func runResume(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printResumeHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	sel, err := recovery.Select(env, scope)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("resume requires a manual handoff, explicit session state, or runtime recovery artifact")
		}
		return err
	}
	title := strings.TrimSpace(joinArgs(positional))
	if title == "" {
		title = sel.Title
	}
	body := renderResumeStateBody(title, sel)
	cap, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:                scope,
		TaskID:               resumeTaskID(sel.State, sel.Handoff, title),
		Type:                 "session",
		Title:                title,
		SourceTool:           "worktrail",
		Tags:                 []string{"resume", sel.SourceKind},
		Body:                 body,
		ResumedFromStateID:   resumedStateID(sel.State),
		ResumedFromHandoffID: resumedHandoffID(sel.Handoff),
		Actor:                "cli:resume",
	})
	if err != nil {
		return err
	}
	if sel.State != nil {
		if _, err := wtstate.Close(env, wtstate.CloseOptions{
			Scope:   scope,
			ID:      sel.State.State.ID,
			Summary: "Superseded by worktrail resume.",
			Actor:   "cli:resume-close-source",
		}); err != nil {
			return err
		}
	}
	if _, err := recovery.WriteRecoveryDashboard(env, scope); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		result := resumeResult{
			State:           cap,
			SourceState:     sel.State,
			SourceHandoff:   sel.Handoff,
			RecoverySource:  sel.SourceKind,
			RecoveryQuality: sel.Quality,
			DegradedReason:  sel.DegradedReason,
		}
		if sel.Runtime != nil {
			result.RuntimePath = sel.Runtime.Path
		}
		return json.NewEncoder(ioctx.Out).Encode(result)
	}
	if sel.Quality == recovery.QualityDegraded {
		fmt.Fprintf(ioctx.Out, "degraded\t%s\t%s\n", sel.SourceKind, sel.DegradedReason)
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\n", cap.State.ID, cap.Path)
	return nil
}

func printResumeHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail resume [--scope project|user] [--format text|json] [<task>]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Creates a fresh explicit session state from the prioritized recovery selector.")
	fmt.Fprintln(out, "Fresh explicit session state ranks above manual handoffs; both rank above hook runtime artifacts.")
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

func renderResumeStateBody(title string, sel recovery.Selection) string {
	var b strings.Builder
	b.WriteString("# State Capsule: ")
	b.WriteString(title)
	b.WriteString("\n\n## Original Intent\n\nResume the task from prioritized Worktrail recovery sources.\n\n")
	b.WriteString("## Recovery Source\n\n")
	fmt.Fprintf(&b, "- Kind: `%s`\n", sel.SourceKind)
	fmt.Fprintf(&b, "- Quality: `%s`\n", sel.Quality)
	if sel.Quality == recovery.QualityDegraded && strings.TrimSpace(sel.DegradedReason) != "" {
		b.WriteString("- Degraded reason: ")
		b.WriteString(sel.DegradedReason)
		b.WriteString("\n")
	}
	b.WriteString("\n## Current Goal\n\n")
	switch {
	case sel.SourceKind == model.ResumePriorityExplicitSession && sel.State != nil:
		b.WriteString(sel.State.State.Title)
	case sel.SourceKind == model.ResumePriorityManualHandoff && sel.Handoff != nil:
		b.WriteString(sel.Handoff.Meta.Title)
	case sel.State != nil:
		b.WriteString(sel.State.State.Title)
	case sel.Handoff != nil:
		b.WriteString(sel.Handoff.Meta.Title)
	case sel.Runtime != nil:
		b.WriteString(sel.Title)
	default:
		b.WriteString(title)
	}
	b.WriteString("\n\n## Constraints\n\nRead the linked records directly before continuing. Hook runtime artifacts are secondary recovery sources.\n\n")
	b.WriteString("## Relevant Context\n\n")
	if sel.State != nil {
		fmt.Fprintf(&b, "- Prior explicit session state: `%s`\n", filepathToSlash(sel.State.Path))
	}
	if sel.Handoff != nil {
		fmt.Fprintf(&b, "- Latest manual handoff: `%s`\n", filepathToSlash(sel.Handoff.Path))
	}
	if sel.Runtime != nil {
		fmt.Fprintf(&b, "- Runtime fallback artifact: `%s`\n", filepathToSlash(sel.Runtime.Path))
	}
	b.WriteString("\n## Evidence\n\nUse the linked records as the primary recovery source.\n\n")
	b.WriteString("## Decisions Made\n\n## Assumptions\n\n## Ruled Out\n\n")
	b.WriteString("## Work Done\n\n")
	if sel.State != nil {
		b.WriteString("See the prior explicit state snapshot below.\n")
	} else if sel.Runtime != nil {
		b.WriteString("See the runtime fallback snapshot below.\n")
	}
	b.WriteString("\n## Current Diff Intent\n\nContinue from the last validated point instead of recomputing context manually.\n\n")
	b.WriteString("## Validation\n\n")
	if sel.SourceKind == model.ResumePriorityManualHandoff && sel.Handoff != nil && strings.TrimSpace(sel.Handoff.Meta.Summary) != "" {
		b.WriteString(sel.Handoff.Meta.Summary)
	} else {
		b.WriteString("Review the latest handoff, explicit state, or runtime fallback notes before making changes.")
	}
	b.WriteString("\n\n## Open Questions\n\n## Next Step\n\nRead the linked records, confirm the next safe action, and continue work.\n")
	if sel.State != nil {
		b.WriteString("\n\n## Prior Explicit State Snapshot\n\n")
		b.WriteString(strings.TrimSpace(sel.State.Body))
	}
	if sel.SourceKind == model.ResumePriorityManualHandoff && sel.Handoff != nil {
		b.WriteString("\n\n## Latest Handoff\n\n")
		b.WriteString(strings.TrimSpace(sel.Handoff.Body))
	} else if sel.Handoff != nil {
		b.WriteString("\n\n## Latest Handoff Summary\n\n")
		if strings.TrimSpace(sel.Handoff.Meta.Summary) != "" {
			b.WriteString(sel.Handoff.Meta.Summary)
		} else {
			b.WriteString("A durable handoff exists but is not the primary recovery source.\n")
		}
	}
	if sel.Runtime != nil && sel.State == nil {
		b.WriteString("\n\n## Runtime Fallback Snapshot\n\n")
		b.WriteString(strings.TrimSpace(sel.Runtime.Body))
	}
	return b.String()
}
