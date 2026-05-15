package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

const evidencePlanSchema = "worktrail.evidence.plan.v1"

type evidencePlan struct {
	Schema       string              `json:"schema"`
	GeneratedAt  string              `json:"generated_at"`
	Scope        string              `json:"scope"`
	StatusFilter string              `json:"status_filter"`
	Summary      evidencePlanSummary `json:"summary"`
	Items        []evidencePlanItem  `json:"items"`
}

type evidencePlanSummary struct {
	Total            int `json:"total"`
	Keep             int `json:"keep"`
	Archive          int `json:"archive"`
	Discard          int `json:"discard"`
	NeedsHumanReview int `json:"needs_human_review"`
}

type evidencePlanItem struct {
	CandidateID               string   `json:"candidate_id"`
	CandidateType             string   `json:"candidate_type"`
	Status                    string   `json:"status"`
	RedactionStatus           string   `json:"redaction_status"`
	IsSplitSource             bool     `json:"is_split_source"`
	PendingSemanticReferences int      `json:"pending_semantic_references"`
	AppliedSemanticReferences int      `json:"applied_semantic_references"`
	NeededForActiveReview     bool     `json:"needed_for_active_review"`
	RecommendedAction         string   `json:"recommended_action"`
	ReasonCodes               []string `json:"reason_codes"`
	Commands                  []string `json:"commands"`
}

func runEvidence(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsHelp(args) {
		printEvidenceHelp(ioctx.Out)
		return nil
	}
	switch args[0] {
	case "plan":
		return runEvidencePlan(env, ioctx, args[1:])
	case "archive", "discard":
		return runEvidenceAction(env, ioctx, args[0], args[1:])
	default:
		return fmt.Errorf("unknown evidence subcommand %q", args[0])
	}
}

func runEvidencePlan(env paths.Env, ioctx IO, args []string) error {
	flags, _ := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	status := flagValue(flags, "status", "active")
	if status != "active" && status != "archived" && status != "all" {
		return fmt.Errorf("status must be active, archived, or all")
	}
	records, err := (candidate.Manager{Env: env, Actor: "cli:evidence-plan"}).List(scope)
	if err != nil {
		return err
	}
	plan := buildEvidencePlan(scope, status, records, time.Now().UTC())
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(plan)
	}
	return renderEvidencePlanText(ioctx.Out, plan)
}

func runEvidenceAction(env paths.Env, ioctx IO, action string, args []string) error {
	flags, positional := splitFlags(args)
	if flagValue(flags, "confirm", "") != "true" {
		return fmt.Errorf("worktrail evidence %s requires --confirm", action)
	}
	scope := flagValue(flags, "scope", "project")
	id := firstArg(positional, flagValue(flags, "id", ""))
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("worktrail evidence %s requires a candidate id", action)
	}
	manager := candidate.Manager{Env: env, Actor: "cli:evidence-" + action}
	records, err := manager.List(scope)
	if err != nil {
		return err
	}
	item, ok := evidencePlanItemByID(records, id)
	if !ok {
		return fmt.Errorf("candidate %q is not evidence lifecycle eligible", id)
	}
	if item.RecommendedAction != action {
		return fmt.Errorf("candidate %q is recommended for %s, not %s", id, item.RecommendedAction, action)
	}
	reason := flagValue(flags, "reason", "")
	if reason == "" && len(positional) > 1 {
		reason = joinArgs(positional[1:])
	}
	var rec candidate.Record
	switch action {
	case "archive":
		rec, err = manager.ArchiveEvidence(scope, id, reason)
	case "discard":
		rec, err = manager.DiscardEvidence(scope, id, reason)
	default:
		return fmt.Errorf("unknown evidence action %q", action)
	}
	if err != nil {
		return err
	}
	return printCandidate(ioctx, rec, flagValue(flags, "format", "text"))
}

func buildEvidencePlan(scope, statusFilter string, records []candidate.Record, now time.Time) evidencePlan {
	plan := evidencePlan{
		Schema:       evidencePlanSchema,
		GeneratedAt:  formatPlanTime(now),
		Scope:        scope,
		StatusFilter: statusFilter,
		Items:        []evidencePlanItem{},
	}
	for _, rec := range records {
		if !isEvidenceLifecycleCandidate(rec) || !evidencePlanIncludesStatus(rec.Meta.Status, statusFilter) {
			continue
		}
		item := buildEvidencePlanItem(rec, records)
		plan.Items = append(plan.Items, item)
		switch item.RecommendedAction {
		case "keep":
			plan.Summary.Keep++
		case "archive":
			plan.Summary.Archive++
		case "discard":
			plan.Summary.Discard++
		default:
			plan.Summary.NeedsHumanReview++
		}
	}
	plan.Summary.Total = len(plan.Items)
	return plan
}

func buildEvidencePlanItem(rec candidate.Record, records []candidate.Record) evidencePlanItem {
	pendingRefs, appliedRefs := evidenceReferenceCounts(rec.Meta.ID, records)
	item := evidencePlanItem{
		CandidateID:               rec.Meta.ID,
		CandidateType:             rec.Meta.CandidateType,
		Status:                    rec.Meta.Status,
		RedactionStatus:           rec.Meta.RedactionStatus,
		IsSplitSource:             isReviewSplitSourceLesson(rec),
		PendingSemanticReferences: pendingRefs,
		AppliedSemanticReferences: appliedRefs,
		NeededForActiveReview:     pendingRefs > 0,
		Commands:                  []string{"worktrail candidates show " + rec.Meta.ID + " --format json"},
	}
	item.RecommendedAction, item.ReasonCodes = recommendEvidenceAction(rec, item)
	return item
}

func evidencePlanItemByID(records []candidate.Record, id string) (evidencePlanItem, bool) {
	for _, rec := range records {
		if rec.Meta.ID != id || !isEvidenceLifecycleCandidate(rec) || !evidencePlanIncludesStatus(rec.Meta.Status, "active") {
			continue
		}
		return buildEvidencePlanItem(rec, records), true
	}
	return evidencePlanItem{}, false
}

func evidenceReferenceCounts(id string, records []candidate.Record) (int, int) {
	pending := 0
	applied := 0
	for _, rec := range records {
		if !model.IsSemanticCandidateType(rec.Meta.CandidateType) || isEvidenceLifecycleCandidate(rec) {
			continue
		}
		if !sourceIDsContain(rec.Meta.SourceCandidateIDs, id) {
			continue
		}
		switch rec.Meta.Status {
		case candidate.StatusPending:
			pending++
		case candidate.StatusPromoted, candidate.StatusMerged:
			applied++
		}
	}
	return pending, applied
}

func recommendEvidenceAction(rec candidate.Record, item evidencePlanItem) (string, []string) {
	var reasons []string
	if item.IsSplitSource {
		reasons = append(reasons, "kdd_split_source_evidence")
	} else {
		reasons = append(reasons, "transcript_notes_evidence")
	}
	if rec.Meta.Status == candidate.StatusArchived {
		reasons = append(reasons, "already_archived")
		return "keep", reasons
	}
	if item.PendingSemanticReferences > 0 {
		reasons = append(reasons, "referenced_by_pending_semantic", "needed_for_active_review")
		return "keep", reasons
	}
	if evidenceRedactionNeedsReview(rec.Meta.RedactionStatus) {
		reasons = append(reasons, "evidence_redaction_needs_review")
		return "keep", reasons
	}
	if item.AppliedSemanticReferences > 0 {
		reasons = append(reasons, "referenced_by_applied_semantic", "archive_after_review")
		return "archive", reasons
	}
	if !item.IsSplitSource && strings.TrimSpace(rec.Body) == "" {
		reasons = append(reasons, "empty_evidence_body", "unreferenced_evidence")
		return "discard", reasons
	}
	if item.IsSplitSource {
		reasons = append(reasons, "defer_evidence_cleanup")
	}
	reasons = append(reasons, "needs_human_confirmation")
	return "needs_human_review", reasons
}

func isEvidenceLifecycleCandidate(rec candidate.Record) bool {
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return true
	}
	return isReviewSplitSourceLesson(rec)
}

func evidencePlanIncludesStatus(status, filter string) bool {
	switch filter {
	case "archived":
		return status == candidate.StatusArchived
	case "all":
		return status != candidate.StatusDiscarded && (isActiveEvidenceStatus(status) || status == candidate.StatusArchived)
	default:
		return isActiveEvidenceStatus(status)
	}
}

func isActiveEvidenceStatus(status string) bool {
	return status == candidate.StatusPending || status == candidate.StatusPromoted || status == candidate.StatusMerged
}

func evidenceRedactionNeedsReview(status string) bool {
	return status == "blocked" || status == "redacted" || status == "" || status == "unreviewed"
}

func sourceIDsContain(ids []string, want string) bool {
	for _, id := range ids {
		if strings.TrimSpace(id) == want {
			return true
		}
	}
	return false
}

func renderEvidencePlanText(out io.Writer, plan evidencePlan) error {
	fmt.Fprintln(out, "# Worktrail Evidence Plan")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Schema: %s\n", plan.Schema)
	fmt.Fprintf(out, "Scope: %s\n", plan.Scope)
	fmt.Fprintf(out, "Status: %s\n", plan.StatusFilter)
	fmt.Fprintf(out, "Summary: total=%d keep=%d archive=%d discard=%d needs_human_review=%d\n", plan.Summary.Total, plan.Summary.Keep, plan.Summary.Archive, plan.Summary.Discard, plan.Summary.NeedsHumanReview)
	for _, group := range []struct {
		Action string
		Title  string
	}{
		{"keep", "Keep"},
		{"archive", "Archive"},
		{"discard", "Discard"},
		{"needs_human_review", "Needs human review"},
	} {
		var items []evidencePlanItem
		for _, item := range plan.Items {
			if item.RecommendedAction == group.Action {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, group.Title)
		for _, item := range items {
			fmt.Fprintf(out, "- `%s` [%s, status=%s, redaction=%s]\n", item.CandidateID, item.CandidateType, item.Status, item.RedactionStatus)
			fmt.Fprintf(out, "  references: pending=%d applied=%d active_review=%v\n", item.PendingSemanticReferences, item.AppliedSemanticReferences, item.NeededForActiveReview)
			fmt.Fprintf(out, "  reason_codes: %s\n", strings.Join(item.ReasonCodes, ", "))
			fmt.Fprintf(out, "  next: %s\n", item.Commands[0])
		}
	}
	return nil
}

func printEvidenceHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail evidence plan [--scope project|user] [--status active|archived|all] [--format text|json]")
	fmt.Fprintln(out, "       worktrail evidence archive <candidate-id> --confirm [--reason text]")
	fmt.Fprintln(out, "       worktrail evidence discard <candidate-id> --confirm [--reason text]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Builds a read-only lifecycle plan for transcript_notes and KDD split-source evidence candidates.")
	fmt.Fprintln(out, "Archive and discard require --confirm and only run when evidence plan recommends the same action.")
}
