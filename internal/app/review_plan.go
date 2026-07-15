package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	wtdistill "github.com/nickdu2009/worktrail/internal/distill"
	"github.com/nickdu2009/worktrail/internal/model"
	wtpaths "github.com/nickdu2009/worktrail/internal/paths"
)

const reviewPlanSchema = "worktrail.review.plan.v1"
const reviewApplyPlanReportSchema = "worktrail.review.apply_plan.report.v1"
const reviewApplyCandidatesReportSchema = "worktrail.review.apply_candidates.report.v1"

type reviewPlan struct {
	Schema      string            `json:"schema"`
	GeneratedAt string            `json:"generated_at"`
	Scope       string            `json:"scope"`
	Summary     reviewPlanSummary `json:"summary"`
	Items       []reviewPlanItem  `json:"items"`
}

type reviewPlanSummary struct {
	Total            int `json:"total"`
	Promote          int `json:"promote"`
	Merge            int `json:"merge"`
	Discard          int `json:"discard"`
	NeedsHumanReview int `json:"needs_human_review"`
}

type reviewApplyPlanReport struct {
	Schema       string                 `json:"schema"`
	PlanSchema   string                 `json:"plan_schema"`
	Scope        string                 `json:"scope"`
	Summary      reviewApplyPlanSummary `json:"summary"`
	Items        []reviewApplyPlanItem  `json:"items"`
	IndexRebuild *indexRebuildResult    `json:"index_rebuild,omitempty"`
}

type reviewApplyPlanSummary struct {
	Total   int `json:"total"`
	Applied int `json:"applied"`
	Skipped int `json:"skipped"`
	Stale   int `json:"stale"`
	Failed  int `json:"failed"`
}

type reviewApplyCandidatesSummary struct {
	Total   int `json:"total"`
	Applied int `json:"applied"`
	Failed  int `json:"failed"`
}

type reviewApplyPlanItem struct {
	CandidateID   string   `json:"candidate_id"`
	PlannedAction string   `json:"planned_action"`
	Result        string   `json:"result"`
	Status        string   `json:"status,omitempty"`
	TargetPath    string   `json:"target_path,omitempty"`
	ReasonCodes   []string `json:"reason_codes,omitempty"`
	ErrorCodes    []string `json:"error_codes,omitempty"`
	Error         string   `json:"error,omitempty"`
}

type reviewApplyCandidatesReport struct {
	Schema       string                       `json:"schema"`
	Scope        string                       `json:"scope"`
	Action       string                       `json:"action"`
	Summary      reviewApplyCandidatesSummary `json:"summary"`
	Items        []reviewApplyPlanItem        `json:"items"`
	IndexRebuild *indexRebuildResult          `json:"index_rebuild,omitempty"`
}

type reviewApplyCandidatesOptions struct {
	Action string
	Scope  string
	Format string
	IDs    []string
}

type reviewPlanItem struct {
	CandidateID        string                   `json:"candidate_id"`
	CandidateType      string                   `json:"candidate_type"`
	Status             string                   `json:"status"`
	Operation          string                   `json:"operation"`
	TargetPath         string                   `json:"target_path"`
	TargetExists       bool                     `json:"target_exists"`
	SourceCandidateIDs []string                 `json:"source_candidate_ids"`
	SourceStatuses     []reviewPlanSourceStatus `json:"source_statuses"`
	Warnings           []string                 `json:"warnings"`
	ReasonCodes        []string                 `json:"reason_codes"`
	RecommendedAction  string                   `json:"recommended_action"`
	Commands           []string                 `json:"commands"`
	Snapshot           reviewPlanSnapshot       `json:"snapshot"`
}

type reviewPlanSourceStatus struct {
	CandidateID     string `json:"candidate_id"`
	CandidateType   string `json:"candidate_type"`
	Status          string `json:"status"`
	RedactionStatus string `json:"redaction_status"`
	Exists          bool   `json:"exists"`
	IsSplitSource   bool   `json:"is_split_source"`
}

type reviewPlanSnapshot struct {
	CandidateStatus          string `json:"candidate_status"`
	CandidateOperation       string `json:"candidate_operation"`
	CandidateTargetPath      string `json:"candidate_target_path"`
	CandidateRedactionStatus string `json:"candidate_redaction_status"`
	CandidateCreatedAt       string `json:"candidate_created_at"`
	CandidateUpdatedAt       string `json:"candidate_updated_at"`
	CandidateBodyHash        string `json:"candidate_body_hash"`
	CandidateMetadataHash    string `json:"candidate_metadata_hash"`
	TargetExists             bool   `json:"target_exists"`
	SourceCandidateIDsHash   string `json:"source_candidate_ids_hash"`
}

func runReviewPlan(env wtpaths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printReviewPlanHelp(ioctx.Out)
		return nil
	}
	flags, _ := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	records, err := (candidate.Manager{Env: env, Actor: "cli:review-plan"}).List(scope)
	if err != nil {
		return err
	}
	plan, err := buildReviewPlan(env, scope, records, time.Now().UTC(), flagValue(flags, "topic", ""))
	if err != nil {
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(plan)
	}
	return renderReviewPlanText(ioctx.Out, plan)
}

func runReviewApplyPlan(env wtpaths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printReviewApplyPlanHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlags(args)
	format := flagValue(flags, "format", "text")
	if flagValue(flags, "confirm", "") != "true" {
		return failCLICommand(ioctx, format, "worktrail review apply-plan", fmt.Errorf("worktrail review apply-plan requires --confirm"))
	}
	planPath := firstArg(positional, flagValue(flags, "plan", ""))
	if strings.TrimSpace(planPath) == "" {
		return failCLICommand(ioctx, format, "worktrail review apply-plan", fmt.Errorf("worktrail review apply-plan requires a plan file"))
	}
	data, err := os.ReadFile(planPath)
	if err != nil {
		return failCLICommand(ioctx, format, "worktrail review apply-plan", err)
	}
	var plan reviewPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return failCLICommand(ioctx, format, "worktrail review apply-plan", err)
	}
	if plan.Schema != reviewPlanSchema {
		return failCLICommand(ioctx, format, "worktrail review apply-plan", fmt.Errorf("unsupported review plan schema %q", plan.Schema))
	}
	planScope := strings.TrimSpace(plan.Scope)
	if planScope == "" {
		planScope = "project"
	}
	scope := planScope
	if requestedScope, ok := flags["scope"]; ok {
		requestedScope = strings.TrimSpace(requestedScope)
		if requestedScope == "" || requestedScope == "true" {
			return failCLICommand(ioctx, format, "worktrail review apply-plan", fmt.Errorf("worktrail review apply-plan --scope must be project or user"))
		}
		if requestedScope != planScope {
			return failCLICommand(ioctx, format, "worktrail review apply-plan", fmt.Errorf("review apply-plan scope mismatch: plan scope is %q but --scope %q was requested; omit --scope or rerun with `--scope %s`", planScope, requestedScope, planScope))
		}
		scope = requestedScope
	}
	report := applyReviewPlan(env, scope, plan)
	if isJSONFormat(format) {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	return renderReviewApplyPlanText(ioctx.Out, report)
}

func runReviewApplyCandidates(env wtpaths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printReviewApplyCandidatesHelp(ioctx.Out)
		return nil
	}
	opts, err := parseReviewApplyCandidatesArgs(args)
	if err != nil {
		format := "text"
		if inferJSONMode(args) {
			format = "json"
		}
		return failCLICommand(ioctx, format, "worktrail review apply-candidates", err)
	}
	report := applyReviewCandidates(env, opts.Scope, opts.Action, opts.IDs)
	if opts.Format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	return renderReviewApplyCandidatesText(ioctx.Out, report)
}

func parseReviewApplyCandidatesArgs(args []string) (reviewApplyCandidatesOptions, error) {
	opts := reviewApplyCandidatesOptions{
		Scope:  "project",
		Format: "text",
	}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case strings.HasPrefix(arg, "--scope="):
			scope := strings.TrimSpace(strings.TrimPrefix(arg, "--scope="))
			if err := setReviewApplyCandidatesScope(&opts, scope); err != nil {
				return reviewApplyCandidatesOptions{}, err
			}
		case arg == "--scope":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return reviewApplyCandidatesOptions{}, fmt.Errorf("worktrail review apply-candidates --scope must be project or user")
			}
			i++
			if err := setReviewApplyCandidatesScope(&opts, args[i]); err != nil {
				return reviewApplyCandidatesOptions{}, err
			}
		case strings.HasPrefix(arg, "--format="):
			format := strings.TrimSpace(strings.TrimPrefix(arg, "--format="))
			if err := setReviewApplyCandidatesFormat(&opts, format); err != nil {
				return reviewApplyCandidatesOptions{}, err
			}
		case arg == "--format":
			if i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return reviewApplyCandidatesOptions{}, fmt.Errorf("worktrail review apply-candidates --format must be text or json")
			}
			i++
			if err := setReviewApplyCandidatesFormat(&opts, args[i]); err != nil {
				return reviewApplyCandidatesOptions{}, err
			}
		case isReviewApplyCandidatesActionFlag(arg):
			action, value := splitReviewApplyCandidatesActionFlag(arg)
			if opts.Action != "" {
				return reviewApplyCandidatesOptions{}, fmt.Errorf("worktrail review apply-candidates requires exactly one of --promote, --merge, or --discard")
			}
			opts.Action = action
			if strings.TrimSpace(value) != "" {
				opts.IDs = append(opts.IDs, strings.TrimSpace(value))
			}
		case strings.HasPrefix(arg, "--"):
			return reviewApplyCandidatesOptions{}, fmt.Errorf("unknown review apply-candidates flag %q", arg)
		default:
			id := strings.TrimSpace(arg)
			if id == "" {
				continue
			}
			if opts.Action == "" {
				return reviewApplyCandidatesOptions{}, fmt.Errorf("worktrail review apply-candidates requires exactly one of --promote, --merge, or --discard before candidate ids")
			}
			opts.IDs = append(opts.IDs, id)
		}
	}
	if opts.Action == "" {
		return reviewApplyCandidatesOptions{}, fmt.Errorf("worktrail review apply-candidates requires exactly one of --promote, --merge, or --discard")
	}
	if len(opts.IDs) == 0 {
		return reviewApplyCandidatesOptions{}, fmt.Errorf("worktrail review apply-candidates requires at least one candidate id")
	}
	return opts, nil
}

func setReviewApplyCandidatesScope(opts *reviewApplyCandidatesOptions, scope string) error {
	switch scope {
	case "project", "user":
		opts.Scope = scope
		return nil
	default:
		return fmt.Errorf("worktrail review apply-candidates --scope must be project or user")
	}
}

func setReviewApplyCandidatesFormat(opts *reviewApplyCandidatesOptions, format string) error {
	switch format {
	case "text", "json":
		opts.Format = format
		return nil
	default:
		return fmt.Errorf("worktrail review apply-candidates --format must be text or json")
	}
}

func isReviewApplyCandidatesActionFlag(arg string) bool {
	action, _ := splitReviewApplyCandidatesActionFlag(arg)
	return action != ""
}

func splitReviewApplyCandidatesActionFlag(arg string) (string, string) {
	for _, action := range []string{"promote", "merge", "discard"} {
		flag := "--" + action
		if arg == flag {
			return action, ""
		}
		if strings.HasPrefix(arg, flag+"=") {
			return action, strings.TrimPrefix(arg, flag+"=")
		}
	}
	return "", ""
}

func applyReviewCandidates(env wtpaths.Env, scope, action string, ids []string) reviewApplyCandidatesReport {
	report := reviewApplyCandidatesReport{
		Schema: reviewApplyCandidatesReportSchema,
		Scope:  scope,
		Action: action,
		Items:  []reviewApplyPlanItem{},
	}
	manager := candidate.Manager{Env: env, Actor: "cli:review-apply-candidates"}
	for _, id := range ids {
		item := applyReviewCandidate(manager, scope, action, id)
		if item.Result == "applied" {
			report.Summary.Applied++
		} else {
			report.Summary.Failed++
		}
		report.Items = append(report.Items, item)
	}
	report.Summary.Total = len(report.Items)
	if report.Summary.Applied > 0 {
		indexRebuild := rebuildIndexForScope(env, scope)
		report.IndexRebuild = &indexRebuild
	}
	return report
}

func applyReviewCandidate(manager candidate.Manager, scope, action, id string) reviewApplyPlanItem {
	item := reviewApplyPlanItem{
		CandidateID:   id,
		PlannedAction: action,
	}
	rec, err := manager.Show(scope, id)
	if err != nil {
		item.Result = "failed"
		item.ErrorCodes = reviewApplyErrorCodes(err)
		item.Error = err.Error()
		return item
	}
	item.TargetPath = rec.Meta.TargetPath
	if reviewApplyCandidatesBlocked(rec) {
		item.Result = "failed"
		item.Error = "review apply-candidates blocks transcript_notes, migration_source, and KDD split-source lesson candidates; distill semantic candidates before applying"
		return item
	}
	status, err := applyReviewPlanAction(manager, scope, id, action)
	if err != nil {
		item.Result = "failed"
		item.ErrorCodes = reviewApplyErrorCodes(err)
		item.Error = err.Error()
		return item
	}
	item.Result = "applied"
	item.Status = status
	return item
}

func reviewApplyCandidatesBlocked(rec candidate.Record) bool {
	switch {
	case rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes:
		return true
	case rec.Meta.CandidateType == model.CandidateTypeMigrationSource:
		return true
	case isReviewSplitSourceLesson(rec):
		return true
	default:
		return false
	}
}

func applyReviewPlan(env wtpaths.Env, scope string, plan reviewPlan) reviewApplyPlanReport {
	report := reviewApplyPlanReport{
		Schema:     reviewApplyPlanReportSchema,
		PlanSchema: plan.Schema,
		Scope:      scope,
		Items:      []reviewApplyPlanItem{},
	}
	manager := candidate.Manager{Env: env, Actor: "cli:review-apply-plan"}
	records, err := manager.List(scope)
	if err != nil {
		item := reviewApplyPlanItem{
			Result:      "failed",
			ReasonCodes: []string{"candidate_list_failed"},
			Error:       err.Error(),
		}
		report.Items = append(report.Items, item)
		report.Summary.Total = 1
		report.Summary.Failed = 1
		return report
	}
	byID := map[string]candidate.Record{}
	for _, rec := range records {
		byID[rec.Meta.ID] = rec
	}
	analysis := newReviewPlanAnalysis(records, "")
	for _, planned := range plan.Items {
		item := reviewApplyPlanItem{
			CandidateID:   planned.CandidateID,
			PlannedAction: planned.RecommendedAction,
			TargetPath:    planned.TargetPath,
		}
		switch planned.RecommendedAction {
		case "needs_human_review":
			item.Result = "skipped"
			item.ReasonCodes = []string{"needs_human_review_skipped"}
			report.Summary.Skipped++
		case "promote", "merge", "discard":
			rec, ok := byID[planned.CandidateID]
			if !ok {
				item.Result = "stale"
				item.ReasonCodes = []string{"candidate_missing"}
				report.Summary.Stale++
				break
			}
			targetExists, err := reviewPlanTargetExists(env, scope, rec.Meta.TargetPath)
			if err != nil {
				item.Result = "failed"
				item.ReasonCodes = []string{"target_check_failed"}
				item.ErrorCodes = reviewApplyErrorCodes(err)
				item.Error = err.Error()
				report.Summary.Failed++
				break
			}
			currentSnapshot := reviewPlanSnapshotFor(rec, targetExists)
			mismatches := reviewPlanSnapshotMismatches(planned.Snapshot, currentSnapshot)
			currentItem, err := buildReviewPlanItem(env, scope, records, rec, analysis)
			if err != nil {
				item.Result = "failed"
				item.ReasonCodes = []string{"current_plan_item_failed"}
				item.ErrorCodes = reviewApplyErrorCodes(err)
				item.Error = err.Error()
				report.Summary.Failed++
				break
			}
			if currentItem.RecommendedAction != planned.RecommendedAction {
				mismatches = appendReviewPlanReasonCodes(mismatches, "recommended_action_changed")
			}
			if len(mismatches) > 0 {
				item.Result = "stale"
				item.ReasonCodes = mismatches
				report.Summary.Stale++
				break
			}
			status, err := applyReviewPlanAction(manager, scope, planned.CandidateID, planned.RecommendedAction)
			if err != nil {
				item.Result = "failed"
				item.ReasonCodes = []string{"apply_failed"}
				item.ErrorCodes = reviewApplyErrorCodes(err)
				item.Error = err.Error()
				report.Summary.Failed++
				break
			}
			item.Result = "applied"
			item.Status = status
			report.Summary.Applied++
		default:
			item.Result = "skipped"
			item.ReasonCodes = []string{"unsupported_recommended_action"}
			report.Summary.Skipped++
		}
		report.Items = append(report.Items, item)
	}
	report.Summary.Total = len(report.Items)
	if report.Summary.Applied > 0 {
		indexRebuild := rebuildIndexForScope(env, scope)
		report.IndexRebuild = &indexRebuild
	}
	return report
}

func applyReviewPlanAction(manager candidate.Manager, scope, id, action string) (string, error) {
	switch action {
	case "promote":
		result, err := manager.Promote(scope, id)
		return result.Status, err
	case "merge":
		result, err := manager.Merge(scope, id)
		return result.Status, err
	case "discard":
		rec, err := manager.Discard(scope, id)
		return rec.Meta.Status, err
	default:
		return "", fmt.Errorf("unsupported review plan action %q", action)
	}
}

func reviewApplyErrorCodes(err error) []string {
	return cliErrorCodes(err)
}

func reviewPlanSnapshotMismatches(planned, current reviewPlanSnapshot) []string {
	var mismatches []string
	if planned.CandidateStatus != current.CandidateStatus {
		mismatches = append(mismatches, "candidate_status_changed")
	}
	if planned.CandidateOperation != current.CandidateOperation {
		mismatches = append(mismatches, "candidate_operation_changed")
	}
	if planned.CandidateTargetPath != current.CandidateTargetPath {
		mismatches = append(mismatches, "candidate_target_path_changed")
	}
	if planned.CandidateRedactionStatus != current.CandidateRedactionStatus {
		mismatches = append(mismatches, "candidate_redaction_status_changed")
	}
	if planned.CandidateBodyHash != current.CandidateBodyHash {
		mismatches = append(mismatches, "candidate_body_hash_changed")
	}
	if planned.CandidateMetadataHash != current.CandidateMetadataHash {
		mismatches = append(mismatches, "candidate_metadata_hash_changed")
	}
	if planned.TargetExists != current.TargetExists {
		mismatches = append(mismatches, "target_exists_changed")
	}
	if planned.SourceCandidateIDsHash != current.SourceCandidateIDsHash {
		mismatches = append(mismatches, "source_candidate_ids_hash_changed")
	}
	return mismatches
}

func buildReviewPlan(env wtpaths.Env, scope string, records []candidate.Record, now time.Time, topic string) (reviewPlan, error) {
	topic = strings.TrimSpace(topic)
	analysis := newReviewPlanAnalysis(records, topic)
	plan := reviewPlan{
		Schema:      reviewPlanSchema,
		GeneratedAt: formatPlanTime(now),
		Scope:       scope,
		Items:       []reviewPlanItem{},
	}
	for _, rec := range records {
		objectMeta := rec.ObjectMeta()
		if topic != "" && objectMeta.Topic != topic {
			continue
		}
		if objectMeta.LifecycleStatus != model.LifecyclePendingReview || !objectMeta.IsDraft() || objectMeta.DraftKind != model.DraftKindSemantic {
			continue
		}
		item, err := buildReviewPlanItem(env, scope, records, rec, analysis)
		if err != nil {
			return reviewPlan{}, err
		}
		plan.Items = append(plan.Items, item)
		switch item.RecommendedAction {
		case "promote":
			plan.Summary.Promote++
		case "merge":
			plan.Summary.Merge++
		case "discard":
			plan.Summary.Discard++
		default:
			plan.Summary.NeedsHumanReview++
		}
	}
	plan.Summary.Total = len(plan.Items)
	return plan, nil
}

func buildReviewPlanItem(env wtpaths.Env, scope string, records []candidate.Record, rec candidate.Record, analysis reviewPlanAnalysis) (reviewPlanItem, error) {
	targetExists, err := reviewPlanTargetExists(env, scope, rec.Meta.TargetPath)
	if err != nil {
		return reviewPlanItem{}, err
	}
	warnings, err := wtdistill.WarningCodes(env, scope, records, rec, true)
	if err != nil {
		return reviewPlanItem{}, err
	}
	_, sourceWarnings := reviewSourceSummary(records, rec)
	warnings = append(warnings, sourceWarnings...)
	item := reviewPlanItem{
		CandidateID:        rec.Meta.ID,
		CandidateType:      model.CanonicalSemanticCandidateType(rec.Meta.CandidateType),
		Status:             rec.Meta.Status,
		Operation:          rec.Meta.Operation,
		TargetPath:         rec.Meta.TargetPath,
		TargetExists:       targetExists,
		SourceCandidateIDs: trimmedSourceIDs(rec.Meta.SourceCandidateIDs),
		SourceStatuses:     reviewPlanSourceStatuses(analysis.ByID, rec.Meta.SourceCandidateIDs),
		Warnings:           warnings,
		ReasonCodes:        []string{"semantic_candidate"},
		Snapshot:           reviewPlanSnapshotFor(rec, targetExists),
	}
	item.ReasonCodes = appendReviewPlanReasonCodes(item.ReasonCodes, baseReviewPlanReasonCodes(rec, item, analysis)...)
	item.RecommendedAction = recommendReviewAction(rec, item, analysis)
	switch item.RecommendedAction {
	case "discard":
		item.ReasonCodes = appendReviewPlanReasonCodes(item.ReasonCodes, "conservative_discard_only")
	case "needs_human_review":
		item.ReasonCodes = appendReviewPlanReasonCodes(item.ReasonCodes, "needs_human_confirmation")
	}
	item.Commands = reviewPlanCommands(scope, rec.Meta.ID, item.RecommendedAction)
	return item, nil
}

type reviewPlanAnalysis struct {
	ByID              map[string]candidate.Record
	SameTargetCounts  map[string]int
	DuplicatePosition map[string]int
	DuplicateCount    map[string]int
}

func newReviewPlanAnalysis(records []candidate.Record, topic string) reviewPlanAnalysis {
	analysis := reviewPlanAnalysis{
		ByID:              map[string]candidate.Record{},
		SameTargetCounts:  map[string]int{},
		DuplicatePosition: map[string]int{},
		DuplicateCount:    map[string]int{},
	}
	groups := map[string][]candidate.Record{}
	for _, rec := range records {
		analysis.ByID[rec.Meta.ID] = rec
		objectMeta := rec.ObjectMeta()
		if topic != "" && objectMeta.Topic != topic {
			continue
		}
		if objectMeta.LifecycleStatus != model.LifecyclePendingReview || !objectMeta.IsDraft() || objectMeta.DraftKind != model.DraftKindSemantic {
			continue
		}
		analysis.SameTargetCounts[rec.Meta.TargetPath]++
		key := rec.Meta.TargetPath + "\x00" + canonicalBodyHash(rec.Body)
		groups[key] = append(groups[key], rec)
	}
	for _, group := range groups {
		if len(group) < 2 {
			continue
		}
		sort.SliceStable(group, func(i, j int) bool {
			if group[i].Meta.CreatedAt.Equal(group[j].Meta.CreatedAt) {
				return group[i].Meta.ID < group[j].Meta.ID
			}
			return group[i].Meta.CreatedAt.Before(group[j].Meta.CreatedAt)
		})
		for i, rec := range group {
			analysis.DuplicatePosition[rec.Meta.ID] = i
			analysis.DuplicateCount[rec.Meta.ID] = len(group)
		}
	}
	return analysis
}

func reviewPlanSourceStatuses(byID map[string]candidate.Record, ids []string) []reviewPlanSourceStatus {
	var statuses []reviewPlanSourceStatus
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		source, ok := byID[id]
		if !ok {
			statuses = append(statuses, reviewPlanSourceStatus{
				CandidateID: id,
				Status:      "missing",
				Exists:      false,
			})
			continue
		}
		statuses = append(statuses, reviewPlanSourceStatus{
			CandidateID:     id,
			CandidateType:   reviewObjectLabel(source.ObjectMeta(), source.Meta.CandidateType),
			Status:          source.Meta.Status,
			RedactionStatus: source.Meta.RedactionStatus,
			Exists:          true,
			IsSplitSource:   isReviewSplitSourceLesson(source),
		})
	}
	return statuses
}

func baseReviewPlanReasonCodes(rec candidate.Record, item reviewPlanItem, analysis reviewPlanAnalysis) []string {
	var codes []string
	if item.TargetExists {
		codes = append(codes, "target_exists")
		if rec.Meta.Operation == candidate.OperationReplace {
			codes = append(codes, "replace_target_exists")
		}
		if rec.Meta.Operation == candidate.OperationMerge {
			codes = append(codes, "merge_target_exists")
		}
	} else {
		codes = append(codes, "new_target")
		if rec.Meta.Operation == candidate.OperationMerge {
			codes = append(codes, "merge_target_missing")
		}
	}
	if analysis.SameTargetCounts[rec.Meta.TargetPath] > 1 {
		codes = append(codes, "same_target_pending")
	}
	if count := analysis.DuplicateCount[rec.Meta.ID]; count > 1 {
		codes = append(codes, "duplicate_body_same_target")
		if analysis.DuplicatePosition[rec.Meta.ID] == 0 {
			codes = append(codes, "older_duplicate_retained")
		} else {
			codes = append(codes, "newer_duplicate_discardable")
		}
	}
	if len(item.SourceCandidateIDs) == 0 {
		codes = append(codes, "source_candidate_ids_empty")
	}
	for _, source := range item.SourceStatuses {
		if !source.Exists {
			codes = append(codes, "source_missing")
			continue
		}
		if source.Status == candidate.StatusPending {
			codes = append(codes, "source_pending")
		} else {
			codes = append(codes, "source_not_pending")
		}
		switch source.CandidateType {
		case "evidence:transcript":
			codes = append(codes, "source_type_transcript_notes")
		case "evidence:migration_source":
			codes = append(codes, "source_type_migration_source")
		default:
			if source.IsSplitSource {
				codes = append(codes, "source_type_split_source")
			} else {
				codes = append(codes, "source_type_unexpected")
			}
		}
		codes = append(codes, redactionReasonCode("source", source.RedactionStatus))
	}
	codes = append(codes, redactionReasonCode("candidate", rec.Meta.RedactionStatus))
	if strings.TrimSpace(rec.Body) == "" {
		codes = append(codes, "empty_candidate_body")
	}
	if isReviewSplitSourceLesson(rec) {
		codes = append(codes, "kdd_split_source_not_promotable", "defer_evidence_cleanup")
	}
	return codes
}

func recommendReviewAction(rec candidate.Record, item reviewPlanItem, analysis reviewPlanAnalysis) string {
	if isReviewSplitSourceLesson(rec) {
		return "needs_human_review"
	}
	if strings.TrimSpace(rec.Body) == "" {
		return "discard"
	}
	if analysis.DuplicateCount[rec.Meta.ID] > 1 && analysis.DuplicatePosition[rec.Meta.ID] > 0 {
		return "discard"
	}
	if canRecommendPromote(rec, item, analysis) {
		return "promote"
	}
	if canRecommendMerge(rec, item, analysis) {
		return "merge"
	}
	return "needs_human_review"
}

func canRecommendPromote(rec candidate.Record, item reviewPlanItem, analysis reviewPlanAnalysis) bool {
	objectMeta := rec.ObjectMeta()
	return objectMeta.LifecycleStatus == model.LifecyclePendingReview &&
		objectMeta.IsDraft() &&
		objectMeta.DraftKind == model.DraftKindSemantic &&
		rec.Meta.Operation == candidate.OperationReplace &&
		!item.TargetExists &&
		analysis.SameTargetCounts[rec.Meta.TargetPath] <= 1 &&
		len(item.SourceCandidateIDs) > 0 &&
		allReviewPlanSourcesUsable(item.SourceStatuses) &&
		rec.Meta.RedactionStatus == "clean"
}

func canRecommendMerge(rec candidate.Record, item reviewPlanItem, analysis reviewPlanAnalysis) bool {
	objectMeta := rec.ObjectMeta()
	return objectMeta.LifecycleStatus == model.LifecyclePendingReview &&
		objectMeta.IsDraft() &&
		objectMeta.DraftKind == model.DraftKindSemantic &&
		rec.Meta.Operation == candidate.OperationMerge &&
		item.TargetExists &&
		analysis.SameTargetCounts[rec.Meta.TargetPath] <= 1 &&
		len(item.SourceCandidateIDs) > 0 &&
		allReviewPlanSourcesUsable(item.SourceStatuses) &&
		rec.Meta.RedactionStatus == "clean"
}

func allReviewPlanSourcesUsable(statuses []reviewPlanSourceStatus) bool {
	if len(statuses) == 0 {
		return false
	}
	for _, status := range statuses {
		if !status.Exists || status.Status != candidate.StatusPending {
			return false
		}
		if status.CandidateType != "evidence:transcript" && status.CandidateType != "evidence:migration_source" && !status.IsSplitSource {
			return false
		}
	}
	return true
}

func reviewPlanCommands(scope, id, action string) []string {
	commands := []string{scopeAwareCommand(scope, "worktrail", "candidates", "diff", id)}
	switch action {
	case "promote":
		commands = append(commands, scopeAwareCommand(scope, "worktrail", "promote", id))
	case "merge":
		commands = append(commands, scopeAwareCommand(scope, "worktrail", "merge", id))
	case "discard":
		commands = append(commands, scopeAwareCommand(scope, "worktrail", "discard", id))
	}
	return commands
}

func reviewPlanTargetExists(env wtpaths.Env, scope string, targetPath string) (bool, error) {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return false, err
	}
	target, err := wtpaths.SafeJoin(root, filepath.FromSlash(model.NormalizeTargetPath(targetPath)))
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(target); err == nil {
		return true, nil
	} else if os.IsNotExist(err) {
		return false, nil
	} else {
		return false, err
	}
}

func reviewPlanSnapshotFor(rec candidate.Record, targetExists bool) reviewPlanSnapshot {
	return reviewPlanSnapshot{
		CandidateStatus:          rec.Meta.Status,
		CandidateOperation:       rec.Meta.Operation,
		CandidateTargetPath:      rec.Meta.TargetPath,
		CandidateRedactionStatus: rec.Meta.RedactionStatus,
		CandidateCreatedAt:       formatPlanTime(rec.Meta.CreatedAt),
		CandidateUpdatedAt:       formatPlanTime(rec.Meta.UpdatedAt),
		CandidateBodyHash:        canonicalBodyHash(rec.Body),
		CandidateMetadataHash:    candidateMetadataHash(rec.Meta),
		TargetExists:             targetExists,
		SourceCandidateIDsHash:   sourceCandidateIDsHash(rec.Meta.SourceCandidateIDs),
	}
}

func canonicalBodyHash(body string) string {
	canonical := strings.TrimRight(body, " \t\r\n") + "\n"
	return sha256Hex([]byte(canonical))
}

func sourceCandidateIDsHash(ids []string) string {
	trimmed := trimmedSourceIDs(ids)
	b, _ := json.Marshal(trimmed)
	return sha256Hex(b)
}

func candidateMetadataHash(meta model.Candidate) string {
	payload := map[string]any{
		"candidate_type":       meta.CandidateType,
		"confidence":           meta.Confidence,
		"created_at":           formatPlanTime(meta.CreatedAt),
		"evidence_label":       meta.EvidenceLabel,
		"id":                   meta.ID,
		"operation":            meta.Operation,
		"redaction_status":     meta.RedactionStatus,
		"scope":                meta.Scope,
		"source_candidate_ids": trimmedSourceIDs(meta.SourceCandidateIDs),
		"status":               meta.Status,
		"tags":                 append([]string(nil), meta.Tags...),
		"target_path":          meta.TargetPath,
		"updated_at":           formatPlanTime(meta.UpdatedAt),
	}
	b, _ := json.Marshal(payload)
	return sha256Hex(b)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func trimmedSourceIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			out = append(out, id)
		}
	}
	return out
}

func formatPlanTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func redactionReasonCode(prefix, status string) string {
	switch status {
	case "clean":
		return prefix + "_redaction_clean"
	case "redacted":
		return prefix + "_redaction_redacted"
	case "blocked":
		return prefix + "_redaction_blocked"
	default:
		return prefix + "_redaction_unreviewed"
	}
}

func appendReviewPlanReasonCodes(values []string, additions ...string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	for _, value := range additions {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		values = append(values, value)
	}
	return values
}

func renderReviewPlanText(out io.Writer, plan reviewPlan) error {
	fmt.Fprintln(out, "# Worktrail Review Plan")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Schema: %s\n", plan.Schema)
	fmt.Fprintf(out, "Scope: %s\n", plan.Scope)
	fmt.Fprintf(out, "Summary: total=%d promote=%d merge=%d discard=%d needs_human_review=%d\n", plan.Summary.Total, plan.Summary.Promote, plan.Summary.Merge, plan.Summary.Discard, plan.Summary.NeedsHumanReview)
	for _, group := range []struct {
		Action string
		Title  string
	}{
		{"promote", "Recommended promote"},
		{"merge", "Recommended merge"},
		{"discard", "Recommended discard"},
		{"needs_human_review", "Needs human review"},
	} {
		var items []reviewPlanItem
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
			fmt.Fprintf(out, "- `%s` %s -> `%s` [%s]\n", item.CandidateID, item.Operation, item.TargetPath, item.CandidateType)
			fmt.Fprintf(out, "  sources: %s\n", reviewPlanSourceSummary(item))
			if len(item.Warnings) > 0 {
				fmt.Fprintf(out, "  warnings: %s\n", strings.Join(item.Warnings, ", "))
			}
			fmt.Fprintf(out, "  reason_codes: %s\n", strings.Join(item.ReasonCodes, ", "))
			fmt.Fprintf(out, "  next: %s\n", item.Commands[0])
		}
	}
	return nil
}

func renderReviewApplyPlanText(out io.Writer, report reviewApplyPlanReport) error {
	fmt.Fprintln(out, "# Worktrail Review Apply Plan")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Schema: %s\n", report.Schema)
	fmt.Fprintf(out, "Scope: %s\n", report.Scope)
	fmt.Fprintf(out, "Summary: total=%d applied=%d skipped=%d stale=%d failed=%d\n", report.Summary.Total, report.Summary.Applied, report.Summary.Skipped, report.Summary.Stale, report.Summary.Failed)
	for _, group := range []struct {
		Result string
		Title  string
	}{
		{"applied", "Applied"},
		{"skipped", "Skipped"},
		{"stale", "Stale"},
		{"failed", "Failed"},
	} {
		var items []reviewApplyPlanItem
		for _, item := range report.Items {
			if item.Result == group.Result {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, group.Title)
		for _, item := range items {
			fmt.Fprintf(out, "- `%s` action=%s", item.CandidateID, item.PlannedAction)
			if item.Status != "" {
				fmt.Fprintf(out, " status=%s", item.Status)
			}
			if item.TargetPath != "" {
				fmt.Fprintf(out, " target=`%s`", item.TargetPath)
			}
			fmt.Fprintln(out)
			if len(item.ReasonCodes) > 0 {
				fmt.Fprintf(out, "  reason_codes: %s\n", strings.Join(item.ReasonCodes, ", "))
			}
			if len(item.ErrorCodes) > 0 {
				fmt.Fprintf(out, "  error_codes: %s\n", strings.Join(item.ErrorCodes, ", "))
			}
			if item.Error != "" {
				fmt.Fprintf(out, "  error: %s\n", item.Error)
			}
		}
	}
	if report.IndexRebuild != nil {
		fmt.Fprintln(out)
		if report.IndexRebuild.Error != "" {
			fmt.Fprintf(out, "index rebuild failed: %s\n", report.IndexRebuild.Error)
			fmt.Fprintf(out, "next: %s\n", report.IndexRebuild.NextStep)
		} else {
			fmt.Fprintf(out, "index rebuilt\t%s\t%d\t%s\n", report.IndexRebuild.Scope, report.IndexRebuild.Entries, report.IndexRebuild.IndexPath)
		}
	}
	return nil
}

func renderReviewApplyCandidatesText(out io.Writer, report reviewApplyCandidatesReport) error {
	fmt.Fprintln(out, "# Worktrail Review Apply Candidates")
	fmt.Fprintln(out)
	fmt.Fprintf(out, "Schema: %s\n", report.Schema)
	fmt.Fprintf(out, "Scope: %s\n", report.Scope)
	fmt.Fprintf(out, "Action: %s\n", report.Action)
	fmt.Fprintf(out, "Summary: total=%d applied=%d failed=%d\n", report.Summary.Total, report.Summary.Applied, report.Summary.Failed)
	for _, group := range []struct {
		Result string
		Title  string
	}{
		{"applied", "Applied"},
		{"failed", "Failed"},
	} {
		var items []reviewApplyPlanItem
		for _, item := range report.Items {
			if item.Result == group.Result {
				items = append(items, item)
			}
		}
		if len(items) == 0 {
			continue
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, group.Title)
		for _, item := range items {
			fmt.Fprintf(out, "- `%s` action=%s", item.CandidateID, item.PlannedAction)
			if item.Status != "" {
				fmt.Fprintf(out, " status=%s", item.Status)
			}
			if item.TargetPath != "" {
				fmt.Fprintf(out, " target=`%s`", item.TargetPath)
			}
			fmt.Fprintln(out)
			if len(item.ReasonCodes) > 0 {
				fmt.Fprintf(out, "  reason_codes: %s\n", strings.Join(item.ReasonCodes, ", "))
			}
			if len(item.ErrorCodes) > 0 {
				fmt.Fprintf(out, "  error_codes: %s\n", strings.Join(item.ErrorCodes, ", "))
			}
			if item.Error != "" {
				fmt.Fprintf(out, "  error: %s\n", item.Error)
			}
		}
	}
	if report.IndexRebuild != nil {
		fmt.Fprintln(out)
		if report.IndexRebuild.Error != "" {
			fmt.Fprintf(out, "index rebuild failed: %s\n", report.IndexRebuild.Error)
			fmt.Fprintf(out, "next: %s\n", report.IndexRebuild.NextStep)
		} else {
			fmt.Fprintf(out, "index rebuilt\t%s\t%d\t%s\n", report.IndexRebuild.Scope, report.IndexRebuild.Entries, report.IndexRebuild.IndexPath)
		}
	}
	return nil
}

func reviewPlanSourceSummary(item reviewPlanItem) string {
	if len(item.SourceCandidateIDs) == 0 {
		return "none"
	}
	parts := make([]string, 0, len(item.SourceStatuses))
	for _, status := range item.SourceStatuses {
		if !status.Exists {
			parts = append(parts, fmt.Sprintf("%s:missing", status.CandidateID))
			continue
		}
		kind := status.CandidateType
		if status.IsSplitSource {
			kind += "/split-source"
		}
		parts = append(parts, fmt.Sprintf("%s:%s/%s", status.CandidateID, kind, status.Status))
	}
	return strings.Join(parts, ", ")
}

func printReviewPlanHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail review plan [--scope project|user] [--topic <topic>] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Builds a read-only agent review contract for pending semantic candidates.")
	fmt.Fprintln(out, "Scope defaults to project; pass --scope user for user-level candidates.")
}

func printReviewApplyPlanHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail review apply-plan <plan.json> --confirm [--scope project|user] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Applies promote, merge, and discard actions from a fresh worktrail.review.plan.v1 file.")
	fmt.Fprintln(out, "When --scope is omitted, the plan scope is used; an explicit mismatched --scope is rejected.")
	fmt.Fprintln(out, "JSON preflight failures return worktrail.cli.error.v1 on stdout; check ok, not exit code.")
	fmt.Fprintln(out, "Candidates with stale snapshots or needs_human_review are skipped without evidence cleanup.")
}

func printReviewApplyCandidatesHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail review apply-candidates --promote <id...> [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail review apply-candidates --merge <id...> [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail review apply-candidates --discard <id...> [--scope project|user] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Applies exactly one action to one or more candidate ids and rebuilds the same-scope index when at least one item is applied.")
	fmt.Fprintln(out, "JSON argument parse failures return worktrail.cli.error.v1 on stdout; check ok, not exit code.")
	fmt.Fprintln(out, "transcript_notes, migration_source, and KDD split-source lesson candidates are blocked in this review automation path.")
}
