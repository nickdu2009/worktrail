package recovery

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

const (
	QualityPrimary  = "primary"
	QualityDegraded = "degraded"
)

type Selection struct {
	SourceKind     string           `json:"source_kind"`
	Quality        string           `json:"quality"`
	DegradedReason string           `json:"degraded_reason,omitempty"`
	Title          string           `json:"title"`
	Handoff        *handoff.Record  `json:"handoff,omitempty"`
	State          *wtstate.Capsule `json:"state,omitempty"`
	Runtime        *runtime.Record  `json:"runtime,omitempty"`
}

func Select(env paths.Env, scope string) (Selection, error) {
	handoffRec, handoffErr := handoff.Latest(env, scope)
	stateRec, stateErr := wtstate.LatestExplicit(env, scope)
	checkpointRec, checkpointErr := runtime.Latest(env, scope, runtime.DirCheckpoints)
	sessionRec, sessionErr := runtime.Latest(env, scope, runtime.DirSessions)

	var candidates []candidate
	if stateErr == nil {
		candidates = append(candidates, candidate{
			kind:      model.ResumePriorityExplicitSession,
			quality:   QualityPrimary,
			title:     stateRec.State.Title,
			state:     &stateRec,
			updatedAt: stateRec.UpdatedTime,
		})
	} else if !errors.Is(stateErr, os.ErrNotExist) {
		return Selection{}, stateErr
	}
	if handoffErr == nil {
		candidates = append(candidates, handoffCandidate(handoffRec, stateErr == nil, &stateRec))
	} else if !errors.Is(handoffErr, os.ErrNotExist) {
		return Selection{}, handoffErr
	}
	if checkpointErr == nil {
		candidates = append(candidates, candidate{
			kind:           model.ResumePriorityRuntimeCheckpoint,
			quality:        QualityDegraded,
			degradedReason: "Recovery is based on a hook runtime checkpoint rather than a manual handoff or explicit session state.",
			title:          titleFromRuntime(checkpointRec),
			runtime:        &checkpointRec,
			updatedAt:      checkpointRec.UpdatedAt,
		})
	} else if !errors.Is(checkpointErr, os.ErrNotExist) {
		return Selection{}, checkpointErr
	}
	if sessionErr == nil {
		candidates = append(candidates, candidate{
			kind:           model.ResumePriorityHookRuntimeState,
			quality:        QualityDegraded,
			degradedReason: "Recovery is based on hook runtime session state rather than a manual handoff or explicit session state.",
			title:          titleFromRuntime(sessionRec),
			runtime:        &sessionRec,
			updatedAt:      sessionRec.UpdatedAt,
		})
	} else if !errors.Is(sessionErr, os.ErrNotExist) {
		return Selection{}, sessionErr
	}
	if len(candidates) == 0 {
		return Selection{}, os.ErrNotExist
	}
	best := candidates[0]
	for _, item := range candidates[1:] {
		if priorityRank(item.kind) < priorityRank(best.kind) {
			best = item
		}
	}
	sel := Selection{
		SourceKind:     best.kind,
		Quality:        best.quality,
		DegradedReason: best.degradedReason,
		Title:          best.title,
	}
	if handoffErr == nil {
		sel.Handoff = &handoffRec
	}
	if stateErr == nil {
		sel.State = &stateRec
	}
	sel.Runtime = best.runtime
	return sel, nil
}

func RenderRecoveryDashboard(sel Selection) string {
	var b strings.Builder
	b.WriteString("# Worktrail Recovery Dashboard\n\n")
	b.WriteString("## Primary Recovery Source\n\n")
	fmt.Fprintf(&b, "- Kind: `%s`\n", sel.SourceKind)
	fmt.Fprintf(&b, "- Quality: `%s`\n", sel.Quality)
	fmt.Fprintf(&b, "- Title: %s\n", sel.Title)
	if sel.Quality == QualityDegraded && strings.TrimSpace(sel.DegradedReason) != "" {
		b.WriteString("\n> ")
		b.WriteString(sel.DegradedReason)
		b.WriteString("\n")
	}
	b.WriteString("\n## Linked Records\n\n")
	if sel.Handoff != nil {
		fmt.Fprintf(&b, "- Manual handoff: `%s`\n", filepathToSlash(sel.Handoff.Path))
	}
	if sel.State != nil {
		fmt.Fprintf(&b, "- Explicit session state: `%s`\n", filepathToSlash(sel.State.Path))
	}
	if sel.Runtime != nil {
		fmt.Fprintf(&b, "- Runtime artifact: `%s`\n", filepathToSlash(sel.Runtime.Path))
	}
	b.WriteString("\n## Next Step\n\n")
	switch sel.SourceKind {
	case model.ResumePriorityManualHandoff:
		b.WriteString("Read the latest manual handoff and continue from its next step.\n")
	case model.ResumePriorityExplicitSession:
		b.WriteString("Read the explicit session state and continue from the latest validated point.\n")
	default:
		b.WriteString("Treat this as degraded recovery. Confirm the task with the linked runtime artifact, then update explicit state or write a manual handoff.\n")
	}
	return b.String()
}

func WriteRecoveryDashboard(env paths.Env, scope string) (string, error) {
	sel, err := Select(env, scope)
	if err != nil {
		return "", err
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return "", err
	}
	return runtime.NewRecorder(root).WriteRecoveryDashboard(RenderRecoveryDashboard(sel))
}

type candidate struct {
	kind           string
	quality        string
	degradedReason string
	title          string
	handoff        *handoff.Record
	state          *wtstate.Capsule
	runtime        *runtime.Record
	updatedAt      any
}

func priorityRank(kind string) int {
	switch kind {
	case model.ResumePriorityExplicitSession:
		return 1
	case model.ResumePriorityManualHandoff:
		return 2
	case model.ResumePriorityRuntimeCheckpoint:
		return 3
	case model.ResumePriorityHookRuntimeState:
		return 4
	default:
		return 99
	}
}

func titleFromRuntime(rec runtime.Record) string {
	if title := runtime.StringField(rec.Meta, "title"); title != "" {
		return title
	}
	return "Runtime recovery artifact"
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func handoffCandidate(rec handoff.Record, hasState bool, state *wtstate.Capsule) candidate {
	cand := candidate{
		kind:      model.ResumePriorityManualHandoff,
		quality:   QualityPrimary,
		title:     rec.Meta.Title,
		handoff:   &rec,
		updatedAt: rec.Meta.UpdatedAt,
	}
	switch {
	case strings.TrimSpace(rec.Meta.Status) != "" && strings.TrimSpace(rec.Meta.Status) != "current":
		cand.quality = QualityDegraded
		cand.degradedReason = "Recovery is based on a superseded handoff. Prefer the latest explicit session state or a newer current handoff."
	case strings.TrimSpace(rec.Meta.SourceStateID) == "":
		cand.quality = QualityDegraded
		cand.degradedReason = "Recovery is based on a handoff that is not bound to an explicit session state."
	case hasState && state != nil && rec.Meta.SourceStateID != state.State.ID:
		cand.quality = QualityDegraded
		cand.degradedReason = "Recovery is based on a handoff that points to an older explicit state than the current active session."
	}
	return cand
}
