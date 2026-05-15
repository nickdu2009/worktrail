package distill

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/util"
)

const ProposalSchema = "worktrail.distill.proposal.v1"

var allowedEvidenceLabels = map[string]bool{
	"Source Code Verified":      true,
	"Live Environment Verified": true,
	"Test Verified":             true,
	"User Confirmed":            true,
	"Pending Verification":      true,
}

type Proposal struct {
	Schema             string              `json:"schema"`
	SourceCandidateIDs []string            `json:"source_candidate_ids"`
	Candidates         []ProposalCandidate `json:"candidates"`
}

type ProposalCandidate struct {
	CandidateType      string   `json:"candidate_type"`
	Title              string   `json:"title"`
	Summary            string   `json:"summary"`
	TargetPath         string   `json:"target_path"`
	Operation          string   `json:"operation"`
	Tags               []string `json:"tags"`
	Body               string   `json:"body"`
	EvidenceLabel      string   `json:"evidence_label"`
	Confidence         *float64 `json:"confidence"`
	SourceCandidateIDs []string `json:"source_candidate_ids"`
}

type Report struct {
	Valid    bool         `json:"valid"`
	Created  int          `json:"created"`
	Skipped  int          `json:"skipped"`
	Blocked  int          `json:"blocked"`
	Warnings []string     `json:"warnings"`
	Items    []ItemReport `json:"items"`
}

type ItemReport struct {
	ProposalIndex int      `json:"proposal_index"`
	CandidateID   string   `json:"candidate_id"`
	CandidateType string   `json:"candidate_type"`
	TargetPath    string   `json:"target_path"`
	Status        string   `json:"status"`
	WarningCodes  []string `json:"warning_codes"`
	Errors        []string `json:"errors"`
}

func LoadProposal(path string) (Proposal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Proposal{}, err
	}
	var proposal Proposal
	dec := json.NewDecoder(strings.NewReader(string(data)))
	if err := dec.Decode(&proposal); err != nil {
		return Proposal{}, err
	}
	if proposal.Schema != ProposalSchema {
		return Proposal{}, fmt.Errorf("proposal schema must be %q", ProposalSchema)
	}
	return proposal, nil
}

func Validate(env paths.Env, manager candidate.Manager, scope string, proposal Proposal) (Report, error) {
	return process(env, manager, scope, proposal, false)
}

func Apply(env paths.Env, manager candidate.Manager, scope string, proposal Proposal) (Report, error) {
	return process(env, manager, scope, proposal, true)
}

func process(env paths.Env, manager candidate.Manager, scope string, proposal Proposal, apply bool) (Report, error) {
	if scope == "" {
		scope = "project"
	}
	if _, err := env.ScopeRoot(scope); err != nil {
		return Report{}, err
	}
	records, err := manager.List(scope)
	if err != nil {
		return Report{}, err
	}
	sources := mapRecords(records)
	report := Report{Valid: true, Warnings: []string{}, Items: []ItemReport{}}
	for i, item := range proposal.Candidates {
		itemReport := validateItem(env, scope, item, proposal.SourceCandidateIDs, sources, records, i)
		if len(itemReport.Errors) > 0 {
			report.Valid = false
			if itemReport.Status == "blocked" {
				report.Blocked++
			}
			report.Items = append(report.Items, itemReport)
			continue
		}
		if !apply {
			itemReport.Status = "valid"
			report.Items = append(report.Items, itemReport)
			continue
		}
		if existing, err := manager.Show(scope, itemReport.CandidateID); err == nil {
			itemReport.Status = "skipped"
			if strings.TrimSpace(existing.Body) != strings.TrimSpace(item.Body) {
				itemReport.WarningCodes = appendUnique(itemReport.WarningCodes, "duplicate_id_existing_body_may_differ")
			}
			report.Skipped++
			report.Items = append(report.Items, itemReport)
			continue
		} else if !errors.Is(err, candidate.ErrNotFound) {
			itemReport.Status = "error"
			itemReport.Errors = append(itemReport.Errors, err.Error())
			report.Valid = false
			report.Items = append(report.Items, itemReport)
			continue
		}
		rec, err := manager.Create(candidate.CreateRequest{
			Scope:              scope,
			ID:                 itemReport.CandidateID,
			CandidateType:      strings.TrimSpace(item.CandidateType),
			TargetPath:         itemReport.TargetPath,
			Title:              strings.TrimSpace(item.Title),
			Summary:            strings.TrimSpace(item.Summary),
			Operation:          strings.TrimSpace(item.Operation),
			SourceCandidateIDs: effectiveSourceIDs(item.SourceCandidateIDs, proposal.SourceCandidateIDs),
			EvidenceLabel:      strings.TrimSpace(item.EvidenceLabel),
			Confidence:         confidenceValue(item.Confidence),
			Tags:               item.Tags,
			Body:               item.Body,
		})
		if err != nil {
			if errors.Is(err, candidate.ErrBlocked) {
				itemReport.Status = "blocked"
				report.Blocked++
			} else {
				itemReport.Status = "error"
				report.Valid = false
			}
			itemReport.Errors = append(itemReport.Errors, err.Error())
			report.Items = append(report.Items, itemReport)
			continue
		}
		itemReport.Status = "created"
		report.Created++
		report.Items = append(report.Items, itemReport)
		records = append(records, rec)
		sources[rec.Meta.ID] = rec
	}
	return report, nil
}

func validateItem(env paths.Env, scope string, item ProposalCandidate, topSourceIDs []string, sources map[string]candidate.Record, records []candidate.Record, idx int) ItemReport {
	typ := strings.TrimSpace(item.CandidateType)
	title := strings.TrimSpace(item.Title)
	target := strings.TrimSpace(item.TargetPath)
	op := strings.TrimSpace(item.Operation)
	itemReport := ItemReport{
		ProposalIndex: idx,
		CandidateType: typ,
		Status:        "valid",
		WarningCodes:  []string{},
		Errors:        []string{},
	}
	if typ == "" {
		itemReport.Errors = append(itemReport.Errors, "candidate_type is required")
	}
	if title == "" {
		itemReport.Errors = append(itemReport.Errors, "title is required")
	}
	if target == "" {
		itemReport.Errors = append(itemReport.Errors, "target_path is required")
	}
	if strings.TrimSpace(item.Body) == "" {
		itemReport.Errors = append(itemReport.Errors, "body is required")
	}
	if op == "" {
		itemReport.Errors = append(itemReport.Errors, "operation is required")
	} else if op != candidate.OperationReplace && op != candidate.OperationMerge {
		itemReport.Errors = append(itemReport.Errors, "operation must be replace or merge")
	}
	if typ != "" && !model.IsSemanticCandidateType(typ) {
		itemReport.Errors = append(itemReport.Errors, "candidate_type must be a semantic type")
	}
	normalizedTarget, targetErr := normalizeTargetPath(target)
	if targetErr != nil {
		itemReport.Errors = append(itemReport.Errors, targetErr.Error())
	} else {
		itemReport.TargetPath = normalizedTarget
		if typ != "" && model.IsSemanticCandidateType(typ) && !model.SemanticTargetPathMatches(typ, normalizedTarget) {
			itemReport.Errors = append(itemReport.Errors, "candidate_type does not match target_path")
		}
	}
	if label := strings.TrimSpace(item.EvidenceLabel); label != "" && !allowedEvidenceLabels[label] {
		itemReport.Errors = append(itemReport.Errors, "evidence_label is not allowed")
	}
	if item.Confidence != nil && (*item.Confidence <= 0 || *item.Confidence > 1) {
		itemReport.Errors = append(itemReport.Errors, "confidence must be greater than 0 and less than or equal to 1")
	}
	sourceIDs := effectiveSourceIDs(item.SourceCandidateIDs, topSourceIDs)
	if len(sourceIDs) == 0 {
		itemReport.Errors = append(itemReport.Errors, "source_candidate_ids is required")
	}
	for _, id := range sourceIDs {
		rec, ok := sources[id]
		if !ok {
			itemReport.Errors = append(itemReport.Errors, "source candidate not found: "+id)
			continue
		}
		if rec.Meta.Status != candidate.StatusPending {
			itemReport.Errors = append(itemReport.Errors, "source candidate is not pending: "+id)
			continue
		}
		if !IsDistillSource(rec, true) {
			itemReport.Errors = append(itemReport.Errors, "source candidate type is not allowed: "+id)
		}
	}
	if len(itemReport.Errors) == 0 {
		itemReport.CandidateID = StableCandidateID(typ, itemReport.TargetPath, title)
		if scan := redact.Scan(item.Body); scan.Status == redact.StatusBlocked {
			itemReport.Status = "blocked"
			itemReport.Errors = append(itemReport.Errors, blockedMessage(scan))
			return itemReport
		}
		if warnings, err := WarningCodes(env, scope, records, candidate.Record{
			Meta: model.Candidate{
				ID:            itemReport.CandidateID,
				Scope:         scope,
				CandidateType: typ,
				TargetPath:    itemReport.TargetPath,
				Operation:     op,
				Status:        candidate.StatusPending,
			},
		}, false); err == nil {
			itemReport.WarningCodes = append(itemReport.WarningCodes, warnings...)
		}
	}
	if itemReport.Status != "blocked" && len(itemReport.Errors) > 0 {
		itemReport.Status = "error"
	}
	return itemReport
}

func IsDistillSource(rec candidate.Record, includeSplitSources bool) bool {
	if rec.Meta.Status != candidate.StatusPending {
		return false
	}
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return true
	}
	if rec.Meta.CandidateType == model.CandidateTypeMigrationSource {
		return includeSplitSources
	}
	return includeSplitSources && IsSplitSourceLesson(rec)
}

func IsSplitSourceLesson(rec candidate.Record) bool {
	if rec.Meta.CandidateType != "lesson" || rec.Meta.Status != candidate.StatusPending {
		return false
	}
	if rec.Meta.TargetPath == "lessons/kdd-active-knowledge-log.md" {
		return true
	}
	for _, tag := range rec.Meta.Tags {
		if tag == "split-source" {
			return true
		}
	}
	return strings.Contains(rec.Meta.Summary, "Do not promote directly") || strings.Contains(rec.Body, "Do not promote directly")
}

func WarningCodes(env paths.Env, scope string, records []candidate.Record, rec candidate.Record, existingRecord bool) ([]string, error) {
	if rec.Meta.Status != candidate.StatusPending || !model.IsSemanticCandidateType(rec.Meta.CandidateType) {
		return nil, nil
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return nil, err
	}
	target, err := paths.SafeJoin(root, filepath.FromSlash(rec.Meta.TargetPath))
	if err != nil {
		return nil, err
	}
	var warnings []string
	if _, err := os.Stat(target); err == nil {
		warnings = append(warnings, "target_exists")
		if rec.Meta.Operation == candidate.OperationReplace {
			warnings = append(warnings, "replace_target_exists")
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if rec.Meta.Operation == candidate.OperationMerge {
			warnings = append(warnings, "merge_target_missing")
		}
	} else {
		return nil, err
	}
	count := 0
	for _, other := range records {
		if other.Meta.Status != candidate.StatusPending || !model.IsSemanticCandidateType(other.Meta.CandidateType) || other.Meta.TargetPath != rec.Meta.TargetPath {
			continue
		}
		count++
	}
	if existingRecord {
		if count > 1 {
			warnings = append(warnings, fmt.Sprintf("same_target_pending:%d", count))
		}
	} else if count > 0 {
		warnings = append(warnings, fmt.Sprintf("same_target_pending:%d", count+1))
	}
	return warnings, nil
}

func StableCandidateID(candidateType, targetPath, title string) string {
	input := candidateType + "\n" + targetPath + "\n" + title
	sum := sha1.Sum([]byte(input))
	suffix := hex.EncodeToString(sum[:])[:8]
	prefix := "distill-" + util.Slug(candidateType) + "-" + util.Slug(strings.TrimSuffix(targetPath, filepath.Ext(targetPath)))
	keep := 64 - len(suffix) - 1
	if len(prefix) > keep {
		prefix = strings.Trim(prefix[:keep], "-")
	}
	if prefix == "" {
		prefix = "distill"
	}
	return prefix + "-" + suffix
}

func normalizeTargetPath(target string) (string, error) {
	target = filepath.ToSlash(strings.TrimSpace(target))
	if target == "" {
		return "", errors.New("target_path is required")
	}
	if filepath.IsAbs(filepath.FromSlash(target)) {
		return "", errors.New("target_path must be relative")
	}
	clean := path.Clean(target)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errors.New("target_path escapes worktrail root")
	}
	if clean == ".worktrail" || strings.HasPrefix(clean, ".worktrail/") {
		return "", errors.New("target_path must not include .worktrail prefix")
	}
	return clean, nil
}

func mapRecords(records []candidate.Record) map[string]candidate.Record {
	out := map[string]candidate.Record{}
	for _, rec := range records {
		out[rec.Meta.ID] = rec
	}
	return out
}

func effectiveSourceIDs(itemIDs, topIDs []string) []string {
	source := itemIDs
	if len(source) == 0 {
		source = topIDs
	}
	var out []string
	seen := map[string]bool{}
	for _, id := range source {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

func confidenceValue(confidence *float64) float64 {
	if confidence == nil {
		return 0
	}
	return *confidence
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func blockedMessage(result redact.Result) string {
	labels := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		labels = append(labels, finding.Label)
	}
	return "candidate content contains blocked sensitive material: " + strings.Join(labels, ", ")
}
