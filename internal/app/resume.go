package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/recovery"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/util"
)

type resumeResult struct {
	Schema               string      `json:"schema"`
	TaskID               string      `json:"task_id"`
	Title                string      `json:"title"`
	State                model.Ref   `json:"state"`
	Source               model.Ref   `json:"source"`
	SupportingRefs       []model.Ref `json:"supporting_refs,omitempty"`
	RecoverySource       string      `json:"recovery_source_kind"`
	RecoveryPriority     int         `json:"recovery_priority"`
	RecoveryQuality      string      `json:"recovery_quality"`
	DegradedReason       string      `json:"degraded_reason,omitempty"`
	CodeAvailabilityHint string      `json:"code_availability_hint,omitempty"`
}

func runResume(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printResumeHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	selector := recovery.TaskSelector{
		TaskID: strings.TrimSpace(flagValue(flags, "task-id", "")),
		Title:  strings.TrimSpace(flagValue(flags, "task-title", "")),
	}
	if refID := strings.TrimSpace(flagValue(flags, "ref", "")); refID != "" {
		selector.Ref = parseRecoveryRef(scope, refID)
	}
	if positionalTitle := strings.TrimSpace(joinArgs(positional)); positionalTitle != "" {
		if selector.TaskID != "" || selector.Title != "" || selector.Ref != nil {
			return errors.New("positional task title cannot be combined with --task-id, --task-title, or --ref")
		}
		selector.Title = positionalTitle
	}
	if selector.Ref != nil && strings.TrimSpace(selector.Ref.Kind) == "" {
		if err := requireUniqueBareRecoveryRef(env, scope, selector.Ref.ID); err != nil {
			return err
		}
	}
	sel, err := recovery.NewTaskScopedResolver(env).Resolve(scope, selector)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("resume could not find a recovery source for the selected task")
		}
		return err
	}
	title := sel.Title
	taskID := resumeTaskID(sel, title)
	body := renderResumeStateBody(title, sel)
	cap, err := wtstate.Resume(env, wtstate.ResumeOptions{
		StartOptions: wtstate.StartOptions{
			Scope:                scope,
			TaskID:               taskID,
			Type:                 "session",
			Title:                title,
			SourceTool:           "worktrail",
			Tags:                 []string{"resume", sel.SourceKind},
			Body:                 body,
			ResumedFromStateID:   resumedStateID(sel),
			ResumedFromHandoffID: resumedHandoffID(sel),
			Actor:                "cli:resume",
		},
		SourceActiveID: activeStateID(sel),
		CloseSummary:   "Superseded by worktrail resume.",
		CloseActor:     "cli:resume-close-source",
	})
	if err != nil {
		return err
	}
	if _, err := recovery.WriteRecoveryDashboard(env, scope); err != nil && !errors.Is(err, os.ErrNotExist) {
		if ioctx.Err != nil {
			fmt.Fprintf(ioctx.Err, "warning\tresume succeeded but recovery dashboard update failed: %v\n", err)
		}
	}
	if flagValue(flags, "format", "text") == "json" {
		stateRef := model.Ref{Scope: scope, Kind: "state", ID: cap.State.ID, RelPath: stateRelPath(env, scope, cap.Path)}
		result := resumeResult{
			Schema:               "worktrail.resume.v2",
			TaskID:               taskID,
			Title:                title,
			State:                stateRef,
			Source:               sel.SourceRef,
			SupportingRefs:       sel.SupportingRefs,
			RecoverySource:       sel.SourceKind,
			RecoveryPriority:     sel.Priority,
			RecoveryQuality:      sel.Quality,
			DegradedReason:       sel.DegradedReason,
			CodeAvailabilityHint: sel.CodeAvailabilityHint,
		}
		return json.NewEncoder(ioctx.Out).Encode(result)
	}
	if sel.Quality == recovery.QualityDegraded {
		fmt.Fprintf(ioctx.Out, "degraded\t%s\t%s\n", sel.SourceKind, sel.DegradedReason)
	}
	if sel.CodeAvailabilityHint != "" {
		fmt.Fprintf(ioctx.Out, "warning\t%s\n", sel.CodeAvailabilityHint)
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\n", cap.State.ID, stateRelPath(env, scope, cap.Path))
	return nil
}

func printResumeHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail resume [--scope project|user] [--task-id <id> | --task-title <title> | --ref [scope:]kind:id] [--format text|json] [<task-title>]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Creates a fresh explicit session state for exactly one task.")
	fmt.Fprintln(out, "Priority: local handoff, team handoff, explicit state, explicit checkpoint, runtime checkpoint, runtime session.")
	fmt.Fprintln(out, "When no selector is supplied, one task is selected automatically; multiple tasks require an explicit selector.")
}

func resumeTaskID(sel recovery.Selection, title string) string {
	if taskID := strings.TrimSpace(sel.TaskID); taskID != "" {
		return taskID
	}
	return "task-" + util.Slug(title)
}

func resumedStateID(sel recovery.Selection) string {
	if sel.State == nil {
		return ""
	}
	return sel.State.State.ID
}

func resumedHandoffID(sel recovery.Selection) string {
	if sel.Handoff == nil {
		return ""
	}
	return sel.Handoff.Meta.ID
}

func activeStateID(sel recovery.Selection) string {
	if sel.ActiveState == nil {
		return ""
	}
	return sel.ActiveState.State.ID
}

func renderResumeStateBody(title string, sel recovery.Selection) string {
	var b strings.Builder
	b.WriteString("# State Capsule: ")
	b.WriteString(title)
	b.WriteString("\n\nSchema: `worktrail.resume_state.v2`\n\n")
	b.WriteString("## Original Intent\n\nResume exactly one Worktrail task from a task-scoped recovery source.\n\n")
	b.WriteString("## Task\n\n")
	fmt.Fprintf(&b, "- Task ID: `%s`\n", resumeTaskID(sel, title))
	b.WriteString("## Recovery Source\n\n")
	fmt.Fprintf(&b, "- Kind: `%s`\n", sel.SourceKind)
	fmt.Fprintf(&b, "- Priority: `%d`\n", sel.Priority)
	fmt.Fprintf(&b, "- Quality: `%s`\n", sel.Quality)
	fmt.Fprintf(&b, "- Ref: `%s`\n", renderRef(sel.SourceRef))
	if sel.Quality == recovery.QualityDegraded && strings.TrimSpace(sel.DegradedReason) != "" {
		b.WriteString("- Degraded reason: ")
		b.WriteString(sel.DegradedReason)
		b.WriteString("\n")
	}
	if sel.CodeAvailabilityHint != "" {
		b.WriteString("- Code availability: ")
		b.WriteString(sel.CodeAvailabilityHint)
		b.WriteString("\n")
	}
	b.WriteString("\n## Structured References\n\n")
	for _, ref := range sel.SupportingRefs {
		fmt.Fprintf(&b, "- `%s`\n", renderRef(ref))
	}
	b.WriteString("\n## Current Goal\n\n")
	b.WriteString(sanitizeRecoveryText(sel.Title))
	b.WriteString("\n\n## Selected Source Snapshot\n\n")
	if snapshot := selectedSourceBody(sel); snapshot != "" {
		b.WriteString(sanitizeRecoveryText(snapshot))
	} else {
		b.WriteString("The selected source contains metadata only.")
	}
	b.WriteString("\n\n## Constraints\n\nDo not mix state, handoffs, checkpoints, or runtime records from another task.\n\n")
	b.WriteString("## Validation\n\nValidate against the selected task and its structured references.\n\n")
	b.WriteString("## Next Step\n\nContinue from the selected source snapshot, then update this task's explicit state.\n")
	return b.String()
}

func selectedSourceBody(sel recovery.Selection) string {
	switch {
	case sel.Handoff != nil:
		if body := strings.TrimSpace(sel.Handoff.Body); body != "" {
			return body
		}
		return sel.Handoff.Meta.Summary
	case sel.State != nil:
		return strings.TrimSpace(sel.State.Body)
	case sel.Runtime != nil:
		return strings.TrimSpace(sel.Runtime.Body)
	default:
		return ""
	}
}

func parseRecoveryRef(defaultScope, raw string) *model.Ref {
	parts := strings.Split(raw, ":")
	switch len(parts) {
	case 3:
		return &model.Ref{Scope: strings.TrimSpace(parts[0]), Kind: strings.TrimSpace(parts[1]), ID: strings.TrimSpace(parts[2])}
	case 2:
		return &model.Ref{Scope: defaultScope, Kind: strings.TrimSpace(parts[0]), ID: strings.TrimSpace(parts[1])}
	default:
		return &model.Ref{Scope: defaultScope, ID: strings.TrimSpace(raw)}
	}
}

func requireUniqueBareRecoveryRef(env paths.Env, scope, id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("recovery ref id is required")
	}
	var matches []string
	handoffScopes := []string{scope}
	if scope == "project" && strings.TrimSpace(env.UserRoot) != "" {
		handoffScopes = append(handoffScopes, "user")
	}
	for _, candidateScope := range handoffScopes {
		result, err := handoff.ListWithDiagnostics(env, handoff.ListOptions{Scope: candidateScope})
		if err != nil {
			return err
		}
		for _, record := range result.Records {
			if record.Meta.ID == id {
				matches = append(matches, candidateScope+":handoff")
			}
		}
	}
	states, err := wtstate.List(env, wtstate.ListOptions{Scope: scope, Directory: "all"})
	if err != nil {
		return err
	}
	for _, state := range states {
		if state.State.ID != id {
			continue
		}
		kind := "state"
		if state.Directory == wtstate.DirCheckpoints {
			kind = "checkpoint"
		}
		matches = append(matches, scope+":"+kind)
	}
	for _, directory := range []string{runtime.DirCheckpoints, runtime.DirSessions} {
		records, err := runtime.List(env, scope, directory)
		if err != nil {
			return err
		}
		for _, record := range records {
			if runtime.StringField(record.Meta, "id") == id {
				kind := "runtime_session"
				if directory == runtime.DirCheckpoints {
					kind = "runtime_checkpoint"
				}
				matches = append(matches, scope+":"+kind)
			}
		}
	}
	if len(matches) > 1 {
		return fmt.Errorf("recovery ref id %q is ambiguous across %s; use [scope:]kind:id", id, strings.Join(matches, ", "))
	}
	return nil
}

func renderRef(ref model.Ref) string {
	data, _ := json.Marshal(ref)
	return string(data)
}

func stateRelPath(env paths.Env, scope, path string) string {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return ""
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(rel)
}

var absolutePathPattern = regexp.MustCompile(`(?m)(^|[[:space:]\(\[\{=:])(/[[:alnum:]_.~@%+,\-]+(?:/[[:alnum:]_.~@%+,\-]+)+)`)

func sanitizeRecoveryText(value string) string {
	return strings.TrimSpace(absolutePathPattern.ReplaceAllString(value, `${1}<absolute-path-omitted>`))
}
