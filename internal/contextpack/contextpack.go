package contextpack

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
)

type Options struct {
	Task            string
	Limit           int
	Now             time.Time
	IncludeEvidence bool
}

type Item struct {
	Scope      string    `json:"scope"`
	Type       string    `json:"type"`
	Title      string    `json:"title"`
	Path       string    `json:"path"`
	Status     string    `json:"status,omitempty"`
	Tags       []string  `json:"tags,omitempty"`
	Content    string    `json:"content"`
	UpdatedAt  time.Time `json:"updated_at"`
	Unapproved bool      `json:"unapproved,omitempty"`
}

type Section struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}

type Maintenance struct {
	PendingEvidenceCandidates   int      `json:"pending_evidence_candidates"`
	PendingSemanticCandidates   int      `json:"pending_semantic_candidates"`
	EvidenceLifecycleCandidates int      `json:"evidence_lifecycle_candidates"`
	NextSteps                   []string `json:"next_steps"`
}

type Pack struct {
	Schema                   string      `json:"schema"`
	Task                     string      `json:"task,omitempty"`
	CreatedAt                time.Time   `json:"created_at"`
	HiddenEvidenceCandidates int         `json:"hidden_evidence_candidates"`
	Maintenance              Maintenance `json:"maintenance"`
	Sections                 []Section   `json:"sections"`
	EvidenceIncluded         bool        `json:"-"`
}

func Build(env paths.Env, opts Options) (Pack, error) {
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	limit := opts.Limit
	if limit <= 0 {
		limit = 12
	}
	var entries []index.Entry
	for _, rootScope := range []struct {
		root  string
		scope string
	}{
		{env.UserRoot, "user"},
		{env.ProjectWT, "project"},
	} {
		es, err := loadOrRebuild(rootScope.root, rootScope.scope)
		if err != nil {
			return Pack{}, err
		}
		entries = append(entries, es...)
	}
	pack := Pack{
		Schema:                   "worktrail.context_pack.v1",
		Task:                     opts.Task,
		CreatedAt:                now,
		HiddenEvidenceCandidates: countPendingTranscriptEvidence(entries),
		Maintenance:              buildMaintenance(env),
		EvidenceIncluded:         opts.IncludeEvidence,
	}
	sectionSpecs := []struct {
		title string
		keep  func(index.Entry) bool
	}{
		{"User Knowledge", func(e index.Entry) bool { return e.Scope == "user" && isKnowledge(e.Type) }},
		{"Project Knowledge", func(e index.Entry) bool { return e.Scope == "project" && isProjectKnowledge(e.Type) }},
		{"Architecture", func(e index.Entry) bool { return e.Type == "architecture" }},
		{"Integrations", func(e index.Entry) bool { return e.Type == "integration" }},
		{"Validation", func(e index.Entry) bool { return e.Type == "validation" }},
		{"Glossary", func(e index.Entry) bool { return e.Type == "glossary" }},
		{"Workflows", func(e index.Entry) bool { return e.Type == "workflow" }},
		{"Active State", func(e index.Entry) bool { return e.Type == "state" && (e.Active || e.Status == "active") }},
		{"Decisions", func(e index.Entry) bool { return e.Type == "decision" }},
		{"Handoffs", func(e index.Entry) bool { return e.Type == "handoff" }},
		{"Rules", func(e index.Entry) bool { return e.Type == "rule" }},
		{"Pending Candidates", func(e index.Entry) bool { return pendingCandidateVisible(e, opts.IncludeEvidence) }},
	}
	for _, spec := range sectionSpecs {
		var items []Item
		for _, entry := range entries {
			if spec.keep(entry) {
				items = append(items, itemFromEntry(entry))
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].UpdatedAt.After(items[j].UpdatedAt)
		})
		if len(items) > limit {
			items = items[:limit]
		}
		if len(items) > 0 {
			pack.Sections = append(pack.Sections, Section{Title: spec.title, Items: items})
		}
	}
	return pack, nil
}

func RenderMarkdown(pack Pack) string {
	var b strings.Builder
	b.WriteString("# Worktrail Context Pack\n\n")
	if pack.Task != "" {
		b.WriteString("## Task\n\n")
		b.WriteString(strings.TrimSpace(pack.Task))
		b.WriteString("\n\n")
	}
	for _, section := range pack.Sections {
		b.WriteString("## ")
		b.WriteString(section.Title)
		b.WriteString("\n\n")
		for _, item := range section.Items {
			b.WriteString("- ")
			b.WriteString(item.Title)
			b.WriteString(" (")
			b.WriteString(item.Scope)
			b.WriteString("/")
			b.WriteString(item.Type)
			if item.Unapproved {
				b.WriteString(", unapproved")
			}
			b.WriteString(") ")
			b.WriteString("`")
			b.WriteString(item.Path)
			b.WriteString("`")
			if item.Status != "" {
				b.WriteString(" [")
				b.WriteString(item.Status)
				b.WriteString("]")
			}
			b.WriteString("\n")
			if item.Content != "" {
				b.WriteString("  ")
				b.WriteString(oneLine(item.Content))
				b.WriteString("\n")
			}
		}
		b.WriteString("\n")
	}
	if hasMaintenance(pack.Maintenance) {
		b.WriteString("## Maintenance\n\n")
		if pack.Maintenance.PendingEvidenceCandidates > 0 {
			fmt.Fprintf(&b, "Pending evidence candidates: %d.\n", pack.Maintenance.PendingEvidenceCandidates)
			b.WriteString("Next: run `worktrail distill --pending --summary` or ask the agent to use /worktrail-distill.\n\n")
		}
		if pack.Maintenance.PendingSemanticCandidates > 0 {
			fmt.Fprintf(&b, "Pending review candidates: %d.\n", pack.Maintenance.PendingSemanticCandidates)
			b.WriteString("Next: run `worktrail review plan --format json` or ask the agent to use /worktrail-review.\n\n")
		}
		if pack.Maintenance.EvidenceLifecycleCandidates > 0 {
			fmt.Fprintf(&b, "Evidence lifecycle actions available: %d.\n", pack.Maintenance.EvidenceLifecycleCandidates)
			b.WriteString("Next: run `worktrail evidence plan --format json`.\n\n")
		}
	}
	if pack.HiddenEvidenceCandidates > 0 && !pack.EvidenceIncluded {
		b.WriteString("## Hidden Evidence\n\n")
		fmt.Fprintf(&b, "Hidden transcript evidence candidates: %d. Use `worktrail context --evidence <task>` to include them.\n\n", pack.HiddenEvidenceCandidates)
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func loadOrRebuild(root, scope string) ([]index.Entry, error) {
	if root == "" {
		return nil, nil
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	db, err := index.Load(root)
	if err != nil {
		if _, rebuildErr := index.Rebuild(root, index.RebuildOptions{Scope: scope}); rebuildErr != nil {
			return nil, rebuildErr
		}
		db, err = index.Load(root)
	}
	if err != nil {
		return nil, err
	}
	return db.Entries, nil
}

func itemFromEntry(entry index.Entry) Item {
	return Item{
		Scope:      entry.Scope,
		Type:       entry.Type,
		Title:      entry.Title,
		Path:       filepath.ToSlash(entry.Path),
		Status:     entry.Status,
		Tags:       entry.Tags,
		Content:    trimContent(entry.Content),
		UpdatedAt:  entry.UpdatedAt,
		Unapproved: entry.Type == "candidate" && entry.Status == "pending",
	}
}

func isKnowledge(typ string) bool {
	switch typ {
	case "profile", "workflow", "prompt", "lesson", "knowledge":
		return true
	default:
		return false
	}
}

func isProjectKnowledge(typ string) bool {
	switch typ {
	case "project", "knowledge", "prompt":
		return true
	default:
		return false
	}
}

func pendingCandidateVisible(entry index.Entry, includeEvidence bool) bool {
	if entry.Type != "candidate" || entry.Status != "pending" {
		return false
	}
	if entry.CandidateType == model.CandidateTypeTranscriptNotes {
		return includeEvidence
	}
	return isSemanticCandidateType(entry.CandidateType)
}

func isSemanticCandidateType(typ string) bool {
	return model.IsSemanticCandidateType(typ)
}

func countPendingTranscriptEvidence(entries []index.Entry) int {
	count := 0
	for _, entry := range entries {
		if entry.Type == "candidate" && entry.Status == "pending" && entry.CandidateType == model.CandidateTypeTranscriptNotes {
			count++
		}
	}
	return count
}

func buildMaintenance(env paths.Env) Maintenance {
	records := allCandidateRecords(env)
	maintenance := Maintenance{}
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPending {
			continue
		}
		if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
			maintenance.PendingEvidenceCandidates++
			continue
		}
		if model.IsSemanticCandidateType(rec.Meta.CandidateType) {
			maintenance.PendingSemanticCandidates++
		}
	}
	maintenance.EvidenceLifecycleCandidates = countEvidenceLifecycleActions(records)
	maintenance.NextSteps = maintenanceNextSteps(maintenance)
	return maintenance
}

func allCandidateRecords(env paths.Env) []candidate.Record {
	manager := candidate.Manager{Env: env, Actor: "contextpack"}
	var records []candidate.Record
	for _, scope := range []string{"user", "project"} {
		scoped, err := manager.List(scope)
		if err != nil {
			continue
		}
		records = append(records, scoped...)
	}
	return records
}

func countEvidenceLifecycleActions(records []candidate.Record) int {
	count := 0
	for _, rec := range records {
		if !isMaintenanceEvidence(rec) || !isActiveEvidenceStatus(rec.Meta.Status) {
			continue
		}
		pendingRefs, appliedRefs := maintenanceReferenceCounts(rec.Meta.ID, records)
		if pendingRefs > 0 || evidenceRedactionNeedsReview(rec.Meta.RedactionStatus) {
			continue
		}
		if appliedRefs > 0 || (!isMaintenanceSplitSource(rec) && strings.TrimSpace(rec.Body) == "") {
			count++
		}
	}
	return count
}

func maintenanceReferenceCounts(id string, records []candidate.Record) (int, int) {
	pending := 0
	applied := 0
	for _, rec := range records {
		if !model.IsSemanticCandidateType(rec.Meta.CandidateType) || isMaintenanceEvidence(rec) {
			continue
		}
		if !containsSourceID(rec.Meta.SourceCandidateIDs, id) {
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

func maintenanceNextSteps(maintenance Maintenance) []string {
	var steps []string
	if maintenance.PendingEvidenceCandidates > 0 {
		steps = append(steps, "worktrail distill --pending --summary")
	}
	if maintenance.PendingSemanticCandidates > 0 {
		steps = append(steps, "worktrail review plan --format json")
	}
	if maintenance.EvidenceLifecycleCandidates > 0 {
		steps = append(steps, "worktrail evidence plan --format json")
	}
	return steps
}

func hasMaintenance(maintenance Maintenance) bool {
	return maintenance.PendingEvidenceCandidates > 0 || maintenance.PendingSemanticCandidates > 0 || maintenance.EvidenceLifecycleCandidates > 0 || len(maintenance.NextSteps) > 0
}

func isMaintenanceEvidence(rec candidate.Record) bool {
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return true
	}
	return isMaintenanceSplitSource(rec)
}

func isMaintenanceSplitSource(rec candidate.Record) bool {
	if rec.Meta.CandidateType != "lesson" {
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

func isActiveEvidenceStatus(status string) bool {
	return status == candidate.StatusPending || status == candidate.StatusPromoted || status == candidate.StatusMerged
}

func evidenceRedactionNeedsReview(status string) bool {
	return status == "blocked" || status == "redacted" || status == "" || status == "unreviewed"
}

func containsSourceID(ids []string, want string) bool {
	for _, id := range ids {
		if strings.TrimSpace(id) == want {
			return true
		}
	}
	return false
}

func trimContent(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= 700 {
		return s
	}
	return strings.TrimSpace(s[:700]) + "..."
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= 220 {
		return s
	}
	return fmt.Sprintf("%s...", strings.TrimSpace(s[:220]))
}
