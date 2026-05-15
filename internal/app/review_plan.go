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
	plan, err := buildReviewPlan(env, scope, records, time.Now().UTC())
	if err != nil {
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(plan)
	}
	return renderReviewPlanText(ioctx.Out, plan)
}

func buildReviewPlan(env wtpaths.Env, scope string, records []candidate.Record, now time.Time) (reviewPlan, error) {
	analysis := newReviewPlanAnalysis(records)
	plan := reviewPlan{
		Schema:      reviewPlanSchema,
		GeneratedAt: formatPlanTime(now),
		Scope:       scope,
		Items:       []reviewPlanItem{},
	}
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPending || !model.IsSemanticCandidateType(rec.Meta.CandidateType) {
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
		CandidateType:      rec.Meta.CandidateType,
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
	item.Commands = reviewPlanCommands(rec.Meta.ID, item.RecommendedAction)
	return item, nil
}

type reviewPlanAnalysis struct {
	ByID              map[string]candidate.Record
	SameTargetCounts  map[string]int
	DuplicatePosition map[string]int
	DuplicateCount    map[string]int
}

func newReviewPlanAnalysis(records []candidate.Record) reviewPlanAnalysis {
	analysis := reviewPlanAnalysis{
		ByID:              map[string]candidate.Record{},
		SameTargetCounts:  map[string]int{},
		DuplicatePosition: map[string]int{},
		DuplicateCount:    map[string]int{},
	}
	groups := map[string][]candidate.Record{}
	for _, rec := range records {
		analysis.ByID[rec.Meta.ID] = rec
		if rec.Meta.Status != candidate.StatusPending || !model.IsSemanticCandidateType(rec.Meta.CandidateType) {
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
			CandidateType:   source.Meta.CandidateType,
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
		if source.CandidateType == model.CandidateTypeTranscriptNotes {
			codes = append(codes, "source_type_transcript_notes")
		} else if source.IsSplitSource {
			codes = append(codes, "source_type_split_source")
		} else {
			codes = append(codes, "source_type_unexpected")
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
	return rec.Meta.Status == candidate.StatusPending &&
		model.IsSemanticCandidateType(rec.Meta.CandidateType) &&
		rec.Meta.Operation == candidate.OperationReplace &&
		!item.TargetExists &&
		analysis.SameTargetCounts[rec.Meta.TargetPath] <= 1 &&
		len(item.SourceCandidateIDs) > 0 &&
		allReviewPlanSourcesUsable(item.SourceStatuses) &&
		rec.Meta.RedactionStatus == "clean"
}

func canRecommendMerge(rec candidate.Record, item reviewPlanItem, analysis reviewPlanAnalysis) bool {
	return rec.Meta.Status == candidate.StatusPending &&
		model.IsSemanticCandidateType(rec.Meta.CandidateType) &&
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
		if status.CandidateType != model.CandidateTypeTranscriptNotes && !status.IsSplitSource {
			return false
		}
	}
	return true
}

func reviewPlanCommands(id, action string) []string {
	commands := []string{"worktrail candidates diff " + id}
	switch action {
	case "promote":
		commands = append(commands, "worktrail promote "+id)
	case "merge":
		commands = append(commands, "worktrail merge "+id)
	case "discard":
		commands = append(commands, "worktrail discard "+id)
	}
	return commands
}

func reviewPlanTargetExists(env wtpaths.Env, scope string, targetPath string) (bool, error) {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return false, err
	}
	target, err := wtpaths.SafeJoin(root, filepath.FromSlash(targetPath))
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
	fmt.Fprintln(out, "usage: worktrail review plan [--scope project|user] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Builds a read-only agent review contract for pending semantic candidates.")
}
