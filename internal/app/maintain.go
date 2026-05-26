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
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

const maintainProposalSchema = "worktrail.knowledge.maintenance.proposal.v1"
const maintainReportSchema = "worktrail.knowledge.maintenance.report.v1"

type maintainKnowledgeReport struct {
	Schema          string                `json:"schema"`
	GeneratedAt     string                `json:"generated_at"`
	Scope           string                `json:"scope"`
	KnowledgeDoctor knowledgeDoctorReport `json:"knowledge_doctor"`
	ReviewPlan      reviewPlan            `json:"review_plan"`
	EvidencePlan    evidencePlan          `json:"evidence_plan"`
}

type maintainProposal struct {
	Schema  string           `json:"schema"`
	Scope   string           `json:"scope,omitempty"`
	Actions []maintainAction `json:"actions"`
}

type maintainAction struct {
	Action        string   `json:"action"`
	SourcePaths   []string `json:"source_paths,omitempty"`
	TargetPath    string   `json:"target_path,omitempty"`
	CandidateID   string   `json:"candidate_id,omitempty"`
	CandidateType string   `json:"candidate_type,omitempty"`
	Title         string   `json:"title,omitempty"`
	Summary       string   `json:"summary,omitempty"`
	EvidenceLabel string   `json:"evidence_label,omitempty"`
	Confidence    float64  `json:"confidence,omitempty"`
	Body          string   `json:"body,omitempty"`
	Reason        string   `json:"reason,omitempty"`
}

type maintainProposalReport struct {
	Schema  string               `json:"schema"`
	Scope   string               `json:"scope"`
	Valid   bool                 `json:"valid"`
	Applied int                  `json:"applied,omitempty"`
	Items   []maintainActionItem `json:"items"`
}

type maintainActionItem struct {
	Index       int      `json:"index"`
	Action      string   `json:"action"`
	Status      string   `json:"status"`
	CandidateID string   `json:"candidate_id,omitempty"`
	TargetPath  string   `json:"target_path,omitempty"`
	Errors      []string `json:"errors,omitempty"`
	Warnings    []string `json:"warnings,omitempty"`
}

func runMaintain(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsFlagHelpOrLeadingHelp(args) {
		printMaintainHelp(ioctx.Out)
		if len(args) == 0 {
			return errors.New("maintain subcommand required")
		}
		return nil
	}
	switch args[0] {
	case "knowledge":
		return runMaintainKnowledge(env, ioctx, args[1:])
	case "validate":
		return runMaintainValidate(env, ioctx, args[1:])
	case "apply":
		return runMaintainApply(env, ioctx, args[1:])
	default:
		return fmt.Errorf("unknown maintain subcommand %q", args[0])
	}
}

func runMaintainKnowledge(env paths.Env, ioctx IO, args []string) error {
	flags, _ := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	records, err := (candidate.Manager{Env: env, Actor: "cli:maintain-knowledge"}).List(scope)
	if err != nil {
		return err
	}
	review, err := buildReviewPlan(env, scope, records, time.Now().UTC())
	if err != nil {
		return err
	}
	report := maintainKnowledgeReport{
		Schema:          maintainReportSchema,
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		Scope:           scope,
		KnowledgeDoctor: buildKnowledgeDoctorReport(env, scope, false),
		ReviewPlan:      review,
		EvidencePlan:    buildEvidencePlan(scope, "active", records, time.Now().UTC()),
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "schema: %s\nscope: %s\nknowledge_findings: %d\nreview_items: %d\nevidence_items: %d\n", report.Schema, report.Scope, len(report.KnowledgeDoctor.Findings), len(report.ReviewPlan.Items), len(report.EvidencePlan.Items))
	return nil
}

func runMaintainValidate(env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	proposal, err := readMaintainProposal(firstArg(positional, flagValue(flags, "proposal", "")))
	if err != nil {
		return err
	}
	report := validateMaintainProposal(env, proposal)
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	renderMaintainProposalReport(ioctx.Out, report)
	if !report.Valid {
		return fmt.Errorf("maintain proposal validation failed")
	}
	return nil
}

func runMaintainApply(env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	if flagValue(flags, "confirm", "") != "true" {
		return fmt.Errorf("worktrail maintain apply requires --confirm")
	}
	proposal, err := readMaintainProposal(firstArg(positional, flagValue(flags, "proposal", "")))
	if err != nil {
		return err
	}
	report := validateMaintainProposal(env, proposal)
	if !report.Valid {
		if flagValue(flags, "format", "text") == "json" {
			_ = json.NewEncoder(ioctx.Out).Encode(report)
		}
		return fmt.Errorf("maintain proposal validation failed")
	}
	applyMaintainProposal(env, proposal, &report)
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	renderMaintainProposalReport(ioctx.Out, report)
	return nil
}

func readMaintainProposal(path string) (maintainProposal, error) {
	if strings.TrimSpace(path) == "" {
		return maintainProposal{}, fmt.Errorf("maintain proposal path is required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return maintainProposal{}, err
	}
	var proposal maintainProposal
	if err := json.Unmarshal(data, &proposal); err != nil {
		return maintainProposal{}, err
	}
	return proposal, nil
}

func validateMaintainProposal(env paths.Env, proposal maintainProposal) maintainProposalReport {
	scope := strings.TrimSpace(proposal.Scope)
	if scope == "" {
		scope = "project"
	}
	report := maintainProposalReport{Schema: maintainProposalSchema, Scope: scope, Valid: true}
	if proposal.Schema != maintainProposalSchema {
		report.Valid = false
		report.Items = append(report.Items, maintainActionItem{Status: "error", Errors: []string{"unsupported proposal schema"}})
		return report
	}
	for i, action := range proposal.Actions {
		item := validateMaintainAction(env, scope, i, action)
		if len(item.Errors) > 0 {
			report.Valid = false
		}
		report.Items = append(report.Items, item)
	}
	return report
}

func validateMaintainAction(env paths.Env, scope string, index int, action maintainAction) maintainActionItem {
	item := maintainActionItem{Index: index, Action: action.Action, CandidateID: action.CandidateID, TargetPath: action.TargetPath, Status: "valid"}
	manager := candidate.Manager{Env: env, Actor: "cli:maintain-validate"}
	switch action.Action {
	case "create_candidate":
		if strings.TrimSpace(action.CandidateType) == "" || strings.TrimSpace(action.TargetPath) == "" || strings.TrimSpace(action.Title) == "" || strings.TrimSpace(action.Summary) == "" || strings.TrimSpace(action.Body) == "" {
			item.Errors = append(item.Errors, "create_candidate requires candidate_type, target_path, title, summary, and body")
		}
		if !model.IsSemanticCandidateType(action.CandidateType) || !model.SemanticTargetPathMatches(action.CandidateType, action.TargetPath) {
			item.Errors = append(item.Errors, "candidate_type does not match target_path")
		}
	case "promote_candidate", "merge_candidate":
		rec, err := manager.Show(scope, action.CandidateID)
		if err != nil {
			item.Errors = append(item.Errors, err.Error())
			break
		}
		if rec.Meta.Status != candidate.StatusPending {
			item.Errors = append(item.Errors, "candidate is not pending")
		}
		if action.Action == "promote_candidate" && rec.Meta.Operation != candidate.OperationReplace {
			item.Errors = append(item.Errors, "promote_candidate requires replace operation")
		}
		if action.Action == "merge_candidate" && rec.Meta.Operation != candidate.OperationMerge {
			item.Errors = append(item.Errors, "merge_candidate requires merge operation")
		}
		item.TargetPath = rec.Meta.TargetPath
	case "retire_missing_target":
		if strings.TrimSpace(action.Reason) == "" {
			item.Errors = append(item.Errors, "retire_missing_target requires reason")
		}
		rec, err := manager.Show(scope, action.CandidateID)
		if err != nil {
			item.Errors = append(item.Errors, err.Error())
			break
		}
		root, err := env.ScopeRoot(scope)
		if err != nil {
			item.Errors = append(item.Errors, err.Error())
			break
		}
		if _, err := os.Stat(filepathJoinSlash(root, rec.Meta.TargetPath)); err == nil {
			item.Errors = append(item.Errors, "retire_missing_target only supports already missing formal targets")
		}
		item.TargetPath = rec.Meta.TargetPath
	case "archive_evidence":
		records, err := manager.List(scope)
		if err != nil {
			item.Errors = append(item.Errors, err.Error())
			break
		}
		planItem, ok := evidencePlanItemByID(records, action.CandidateID)
		if !ok || planItem.RecommendedAction != "archive" {
			item.Errors = append(item.Errors, "evidence plan does not recommend archive")
		}
	case "rebuild_index":
	default:
		item.Errors = append(item.Errors, "unsupported action type")
	}
	if len(item.Errors) > 0 {
		item.Status = "error"
	}
	return item
}

func applyMaintainProposal(env paths.Env, proposal maintainProposal, report *maintainProposalReport) {
	scope := report.Scope
	manager := candidate.Manager{Env: env, Actor: "cli:maintain-apply"}
	for i, action := range proposal.Actions {
		if len(report.Items[i].Errors) > 0 {
			continue
		}
		var err error
		switch action.Action {
		case "create_candidate":
			_, err = manager.Create(candidate.CreateRequest{
				Scope:         scope,
				CandidateType: action.CandidateType,
				TargetPath:    action.TargetPath,
				Title:         action.Title,
				Summary:       action.Summary,
				Operation:     candidate.OperationReplace,
				EvidenceLabel: action.EvidenceLabel,
				Confidence:    action.Confidence,
				Body:          action.Body,
			})
		case "promote_candidate":
			_, err = manager.Promote(scope, action.CandidateID)
		case "merge_candidate":
			_, err = manager.Merge(scope, action.CandidateID)
		case "retire_missing_target":
			_, err = manager.Retire(scope, action.CandidateID, action.Reason)
		case "archive_evidence":
			_, err = manager.ArchiveEvidence(scope, action.CandidateID, action.Reason)
		case "rebuild_index":
			result := rebuildIndexForScope(env, scope)
			if result.Error != "" {
				err = errors.New(result.Error)
			}
		}
		if err != nil {
			report.Valid = false
			report.Items[i].Status = "error"
			report.Items[i].Errors = append(report.Items[i].Errors, err.Error())
			continue
		}
		report.Items[i].Status = "applied"
		report.Applied++
	}
}

func filepathJoinSlash(root, rel string) string {
	return filepath.Join(root, filepath.FromSlash(rel))
}

func renderMaintainProposalReport(out io.Writer, report maintainProposalReport) {
	fmt.Fprintf(out, "schema: %s\nscope: %s\nvalid: %t\napplied: %d\n", report.Schema, report.Scope, report.Valid, report.Applied)
	for _, item := range report.Items {
		fmt.Fprintf(out, "%d\t%s\t%s\t%s\t%s\n", item.Index, item.Status, item.Action, item.CandidateID, strings.Join(item.Errors, "; "))
	}
}

func printMaintainHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail maintain knowledge [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "       worktrail maintain validate <proposal.json> [--format text|json]")
	fmt.Fprintln(out, "       worktrail maintain apply <proposal.json> --confirm [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Maintains knowledge through read-only scanning, agent-authored proposals, validation, and explicit apply.")
}
