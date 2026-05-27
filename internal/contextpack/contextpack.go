package contextpack

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/transcript"
)

type Options struct {
	Task             string
	Stage            string
	IncludeLifecycle []string
	Limit            int
	Now              time.Time
	IncludeEvidence  bool
}

type Item struct {
	Scope         string    `json:"scope"`
	Type          string    `json:"type"`
	Title         string    `json:"title"`
	Path          string    `json:"path"`
	Status        string    `json:"status,omitempty"`
	Stage         string    `json:"stage,omitempty"`
	Lifecycle     string    `json:"lifecycle,omitempty"`
	Topic         string    `json:"topic,omitempty"`
	SourceOfTruth bool      `json:"source_of_truth,omitempty"`
	SupersededBy  []string  `json:"superseded_by,omitempty"`
	Tags          []string  `json:"tags,omitempty"`
	Content       string    `json:"content"`
	UpdatedAt     time.Time `json:"updated_at"`
	Unapproved    bool      `json:"unapproved,omitempty"`
}

type Section struct {
	Title string `json:"title"`
	Items []Item `json:"items"`
}

type Maintenance struct {
	PendingEvidenceCandidates   int      `json:"pending_evidence_candidates"`
	PendingSemanticCandidates   int      `json:"pending_semantic_candidates"`
	EvidenceLifecycleCandidates int      `json:"evidence_lifecycle_candidates"`
	ImportableCodexSessions     int      `json:"importable_codex_sessions,omitempty"`
	ObservedCursorSessions      int      `json:"observed_cursor_sessions,omitempty"`
	NextSteps                   []string `json:"next_steps"`
}

type IndexHealth struct {
	Scope               string `json:"scope"`
	Stale               bool   `json:"stale"`
	StaleEntriesSkipped int    `json:"stale_entries_skipped,omitempty"`
	MissingFromFS       int    `json:"missing_from_fs,omitempty"`
	MissingFromIndex    int    `json:"missing_from_index,omitempty"`
	Changed             int    `json:"changed,omitempty"`
	NextStep            string `json:"next_step,omitempty"`
}

type Pack struct {
	Schema                   string        `json:"schema"`
	Task                     string        `json:"task,omitempty"`
	CreatedAt                time.Time     `json:"created_at"`
	HiddenEvidenceCandidates int           `json:"hidden_evidence_candidates"`
	IndexHealth              []IndexHealth `json:"index_health,omitempty"`
	Maintenance              Maintenance   `json:"maintenance"`
	Sections                 []Section     `json:"sections"`
	EvidenceIncluded         bool          `json:"-"`
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
	stage := strings.ToLower(strings.TrimSpace(opts.Stage))
	if stage != "" && !validStage(stage) {
		return Pack{}, fmt.Errorf("invalid context stage %q", opts.Stage)
	}
	includeLifecycle := append([]string{}, opts.IncludeLifecycle...)
	if len(includeLifecycle) == 0 && (stage == knowledge.LifecycleHistorical || stage == knowledge.LifecycleRetired) {
		includeLifecycle = []string{stage}
		stage = ""
	}
	var (
		entries      []index.Entry
		indexHealths []IndexHealth
	)
	for _, rootScope := range []struct {
		root  string
		scope string
	}{
		{env.UserRoot, "user"},
		{env.ProjectWT, "project"},
	} {
		es, health, err := loadOrRebuild(rootScope.root, rootScope.scope)
		if err != nil {
			return Pack{}, err
		}
		entries = append(entries, es...)
		if health.Stale || health.StaleEntriesSkipped > 0 {
			indexHealths = append(indexHealths, health)
		}
	}
	supersededBy := supersededByMap(entries)
	pack := Pack{
		Schema:                   "worktrail.context_pack.v1",
		Task:                     opts.Task,
		CreatedAt:                now,
		HiddenEvidenceCandidates: countPendingEvidence(entries),
		IndexHealth:              append([]IndexHealth{}, indexHealths...),
		Maintenance:              buildMaintenance(env),
		EvidenceIncluded:         opts.IncludeEvidence,
	}
	sectionSpecs := sectionSpecsForStage(stage, opts.IncludeEvidence)
	for _, spec := range sectionSpecs {
		var items []Item
		for _, entry := range entries {
			if !knowledge.IncludesLifecycle(includeLifecycle, entry.Lifecycle) {
				continue
			}
			if spec.keep(entry) {
				items = append(items, itemFromEntry(entry, supersededBy[filepath.ToSlash(entry.Path)]))
			}
		}
		sort.SliceStable(items, func(i, j int) bool {
			if itemPriority(items[i], stage) != itemPriority(items[j], stage) {
				return itemPriority(items[i], stage) > itemPriority(items[j], stage)
			}
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
			if item.Stage != "" {
				b.WriteString(" [stage:")
				b.WriteString(item.Stage)
				b.WriteString("]")
			}
			if knowledge.IsNonCurrentLifecycle(item.Lifecycle) {
				b.WriteString(" [lifecycle:")
				b.WriteString(item.Lifecycle)
				b.WriteString("]")
			}
			if item.Topic != "" {
				b.WriteString(" [topic:")
				b.WriteString(item.Topic)
				b.WriteString("]")
			}
			if item.SourceOfTruth {
				b.WriteString(" [source_of_truth]")
			}
			if len(item.SupersededBy) > 0 {
				b.WriteString(" [superseded_by:")
				b.WriteString(strings.Join(item.SupersededBy, ","))
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
	if len(pack.IndexHealth) > 0 {
		b.WriteString("## Index Health\n\n")
		for _, health := range pack.IndexHealth {
			fmt.Fprintf(&b, "%s index stale: skipped %d entries (%d missing, %d changed, %d unindexed).\n", scopeLabel(health.Scope), health.StaleEntriesSkipped, health.MissingFromFS, health.Changed, health.MissingFromIndex)
			if health.NextStep != "" {
				fmt.Fprintf(&b, "Next: `%s`\n\n", health.NextStep)
			} else {
				b.WriteString("\n")
			}
		}
	}
	if hasMaintenance(pack.Maintenance) {
		b.WriteString("## Maintenance\n\n")
		if pack.Maintenance.PendingEvidenceCandidates > 0 {
			fmt.Fprintf(&b, "Pending evidence candidates: %d.\n", pack.Maintenance.PendingEvidenceCandidates)
			fmt.Fprintf(&b, "Next: run %s or ask the agent to use /worktrail-distill.\n\n", maintenanceStepList(pack.Maintenance.NextSteps, "worktrail distill"))
		}
		if pack.Maintenance.PendingSemanticCandidates > 0 {
			fmt.Fprintf(&b, "Pending review candidates: %d.\n", pack.Maintenance.PendingSemanticCandidates)
			fmt.Fprintf(&b, "Next: run %s or ask the agent to use /worktrail-review.\n\n", maintenanceStepList(pack.Maintenance.NextSteps, "worktrail review plan"))
		}
		if pack.Maintenance.EvidenceLifecycleCandidates > 0 {
			fmt.Fprintf(&b, "Evidence lifecycle actions available: %d.\n", pack.Maintenance.EvidenceLifecycleCandidates)
			fmt.Fprintf(&b, "Next: run %s.\n\n", maintenanceStepList(pack.Maintenance.NextSteps, "worktrail evidence plan"))
		}
		if pack.Maintenance.ImportableCodexSessions > 0 {
			fmt.Fprintf(&b, "Importable current-project Codex sessions: %d.\n", pack.Maintenance.ImportableCodexSessions)
			fmt.Fprintf(&b, "Next: run %s.\n\n", maintenanceStepList(pack.Maintenance.NextSteps, "worktrail import codex"))
		}
		if pack.Maintenance.ObservedCursorSessions > 0 {
			fmt.Fprintf(&b, "Observed Cursor sessions ready for import: %d.\n", pack.Maintenance.ObservedCursorSessions)
			fmt.Fprintf(&b, "Next: run %s.\n\n", maintenanceStepList(pack.Maintenance.NextSteps, "worktrail import cursor"))
		}
	}
	if pack.HiddenEvidenceCandidates > 0 && !pack.EvidenceIncluded {
		b.WriteString("## Hidden Evidence\n\n")
		fmt.Fprintf(&b, "Hidden evidence candidates: %d. Use `worktrail context --evidence <task>` to include them.\n\n", pack.HiddenEvidenceCandidates)
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func loadOrRebuild(root, scope string) ([]index.Entry, IndexHealth, error) {
	if root == "" {
		return nil, IndexHealth{}, nil
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return nil, IndexHealth{}, nil
	} else if err != nil {
		return nil, IndexHealth{}, err
	}
	db, err := index.Load(root)
	if err != nil {
		if _, rebuildErr := index.Rebuild(root, index.RebuildOptions{Scope: scope}); rebuildErr != nil {
			return nil, IndexHealth{}, rebuildErr
		}
		db, err = index.Load(root)
	}
	if err != nil {
		return nil, IndexHealth{}, err
	}
	entries, freshReport, err := index.FilterFresh(root, db)
	if err != nil {
		return nil, IndexHealth{}, err
	}
	healthReport, err := index.Health(root)
	if err != nil {
		return nil, IndexHealth{}, err
	}
	return entries, IndexHealth{
		Scope:               scope,
		Stale:               healthReport.Stale,
		StaleEntriesSkipped: len(freshReport.Deleted) + len(freshReport.Changed),
		MissingFromFS:       len(healthReport.MissingFromFS),
		MissingFromIndex:    len(healthReport.MissingFromIndex),
		Changed:             len(healthReport.Changed),
		NextStep:            "worktrail index rebuild --scope " + scope,
	}, nil
}

func itemFromEntry(entry index.Entry, supersededBy []string) Item {
	content := ""
	if entry.Type == "state" || entry.Type == "handoff" {
		content = trimContent(entry.Content)
	}
	return Item{
		Scope:         entry.Scope,
		Type:          entry.Type,
		Title:         entry.Title,
		Path:          filepath.ToSlash(entry.Path),
		Status:        entry.Status,
		Stage:         entry.Stage,
		Lifecycle:     entry.Lifecycle,
		Topic:         entry.Topic,
		SourceOfTruth: entry.SourceOfTruth,
		SupersededBy:  append([]string{}, supersededBy...),
		Tags:          entry.Tags,
		Content:       content,
		UpdatedAt:     entry.UpdatedAt,
		Unapproved:    entry.Type == "candidate" && entry.Status == "pending",
	}
}

type sectionSpec struct {
	title string
	keep  func(index.Entry) bool
}

func sectionSpecsForStage(stage string, includeEvidence bool) []sectionSpec {
	specs := map[string]sectionSpec{
		"user":         {"User Knowledge", func(e index.Entry) bool { return e.Scope == "user" && isKnowledge(e.Type) }},
		"project":      {"Project Knowledge", func(e index.Entry) bool { return e.Scope == "project" && isProjectKnowledge(e.Type) }},
		"requirements": {"Requirements", func(e index.Entry) bool { return e.Type == "requirement" }},
		"architecture": {"Architecture", func(e index.Entry) bool { return e.Type == "architecture" }},
		"integrations": {"Integrations", func(e index.Entry) bool { return e.Type == "integration" }},
		"validation":   {"Validation", func(e index.Entry) bool { return e.Type == "validation" }},
		"glossary":     {"Glossary", func(e index.Entry) bool { return e.Type == "glossary" }},
		"workflows":    {"Workflows", func(e index.Entry) bool { return e.Type == "workflow" }},
		"state":        {"Active State", func(e index.Entry) bool { return e.Type == "state" && (e.Active || e.Status == "active") }},
		"decisions":    {"Decisions", func(e index.Entry) bool { return e.Type == "decision" }},
		"handoffs":     {"Handoffs", func(e index.Entry) bool { return e.Type == "handoff" }},
		"rules":        {"Rules", func(e index.Entry) bool { return e.Type == "rule" }},
		"pending":      {"Pending Candidates", func(e index.Entry) bool { return pendingCandidateVisible(e, includeEvidence) }},
	}
	order := []string{"state", "handoffs", "user", "project", "requirements", "architecture", "decisions", "validation", "rules", "workflows", "integrations", "glossary", "pending"}
	switch stage {
	case "requirements":
		order = []string{"state", "handoffs", "user", "project", "requirements", "decisions", "glossary", "architecture", "validation", "workflows", "rules", "integrations", "pending"}
	case "design":
		order = []string{"state", "handoffs", "user", "project", "requirements", "architecture", "decisions", "glossary", "integrations", "validation", "rules", "workflows", "pending"}
	case "implementation":
		order = []string{"state", "handoffs", "user", "project", "architecture", "validation", "rules", "workflows", "decisions", "requirements", "integrations", "glossary", "pending"}
	}
	out := make([]sectionSpec, 0, len(order))
	for _, key := range order {
		out = append(out, specs[key])
	}
	return out
}

func itemPriority(item Item, requestedStage string) int {
	score := 0
	if len(item.SupersededBy) == 0 && !knowledge.IsNonCurrentLifecycle(item.Lifecycle) && item.Stage != "historical" && item.Stage != "retired" {
		score += 100
	}
	if item.SourceOfTruth {
		score += 50
	}
	if requestedStage != "" && item.Stage == requestedStage {
		score += 25
	}
	return score
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

func supersededByMap(entries []index.Entry) map[string][]string {
	out := map[string][]string{}
	for _, entry := range entries {
		path := filepath.ToSlash(entry.Path)
		for _, old := range entry.Supersedes {
			old = filepath.ToSlash(strings.TrimSpace(old))
			if old != "" {
				out[old] = appendUnique(out[old], path)
			}
		}
		for _, by := range entry.SupersededBy {
			by = filepath.ToSlash(strings.TrimSpace(by))
			if by != "" {
				out[path] = appendUnique(out[path], by)
			}
		}
	}
	return out
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func validStage(stage string) bool {
	switch stage {
	case "requirements", "design", "decision", "implementation", "validation", "historical", "retired":
		return true
	default:
		return false
	}
}

func scopeLabel(scope string) string {
	if strings.EqualFold(strings.TrimSpace(scope), "user") {
		return "User"
	}
	return "Project"
}

func pendingCandidateVisible(entry index.Entry, includeEvidence bool) bool {
	if entry.Type != "candidate" {
		return false
	}
	return model.PendingInboxVisible(entry.Status, entry.CandidateType, includeEvidence)
}

func isSemanticCandidateType(typ string) bool {
	return model.IsSemanticCandidateType(typ)
}

func countPendingEvidence(entries []index.Entry) int {
	count := 0
	for _, entry := range entries {
		if entry.Type == "candidate" && model.PendingInboxVisible(entry.Status, entry.CandidateType, true) && model.CandidateSurface(entry.CandidateType) == model.CandidateSurfaceEvidence {
			count++
		}
	}
	return count
}

func buildMaintenance(env paths.Env) Maintenance {
	records := allCandidateRecords(env)
	maintenance := Maintenance{}
	pendingEvidenceByScope := map[string]int{}
	pendingSemanticByScope := map[string]int{}
	for _, rec := range records {
		if rec.Meta.Status != candidate.StatusPending {
			continue
		}
		switch model.CandidateSurface(rec.Meta.CandidateType) {
		case model.CandidateSurfaceEvidence:
			maintenance.PendingEvidenceCandidates++
			pendingEvidenceByScope[rec.Meta.Scope]++
			continue
		case model.CandidateSurfaceSemantic:
			maintenance.PendingSemanticCandidates++
			pendingSemanticByScope[rec.Meta.Scope]++
		}
	}
	evidenceLifecycleByScope := countEvidenceLifecycleActionsByScope(records)
	for _, count := range evidenceLifecycleByScope {
		maintenance.EvidenceLifecycleCandidates += count
	}
	maintenance.ImportableCodexSessions = countImportableCodexSessions(env)
	maintenance.ObservedCursorSessions = countObservedCursorSessions(env.ProjectWT)
	maintenance.NextSteps = maintenanceNextSteps(pendingEvidenceByScope, pendingSemanticByScope, evidenceLifecycleByScope, maintenance.ImportableCodexSessions, maintenance.ObservedCursorSessions)
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

func countEvidenceLifecycleActionsByScope(records []candidate.Record) map[string]int {
	counts := map[string]int{}
	for _, rec := range records {
		if !isMaintenanceEvidence(rec) || !isActiveEvidenceStatus(rec.Meta.Status) {
			continue
		}
		pendingRefs, appliedRefs := maintenanceReferenceCounts(rec.Meta.ID, records)
		if pendingRefs > 0 || evidenceRedactionNeedsReview(rec.Meta.RedactionStatus) {
			continue
		}
		if appliedRefs > 0 || (!isMaintenanceSplitSource(rec) && strings.TrimSpace(rec.Body) == "") {
			counts[rec.Meta.Scope]++
		}
	}
	return counts
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

func maintenanceNextSteps(pendingEvidenceByScope, pendingSemanticByScope, evidenceLifecycleByScope map[string]int, importableCodex, observedCursor int) []string {
	var steps []string
	for _, scope := range []string{"project", "user"} {
		if pendingEvidenceByScope[scope] > 0 {
			steps = append(steps, scopedMaintenanceCommand("worktrail distill --pending --summary", scope))
		}
	}
	for _, scope := range []string{"project", "user"} {
		if pendingSemanticByScope[scope] > 0 {
			steps = append(steps, scopedMaintenanceCommand("worktrail review plan --format json", scope))
		}
	}
	for _, scope := range []string{"project", "user"} {
		if evidenceLifecycleByScope[scope] > 0 {
			steps = append(steps, scopedMaintenanceCommand("worktrail evidence plan --format json", scope))
		}
	}
	if importableCodex > 0 {
		steps = append(steps, "worktrail import codex --since 14d --all")
	}
	if observedCursor > 0 {
		steps = append(steps, "worktrail import cursor --limit 20 --all")
	}
	return steps
}

func countImportableCodexSessions(env paths.Env) int {
	if env.Home == "" || env.ProjectRoot == "" {
		return 0
	}
	sessions, err := transcript.DiscoverCodexSessionsBounded(env.Home, env.ProjectRoot, transcript.DiscoverOptions{
		Since: time.Now().UTC().AddDate(0, 0, -14),
		Limit: 20,
	})
	if err != nil {
		return 0
	}
	imported := transcriptHashes(env.ProjectWT, "codex")
	importedSessions := transcriptEvidenceSessionIDs(env, "project", "codex")
	count := 0
	for _, session := range sessions {
		hash := fileSHA256(session.Path)
		if hash == "" || imported[hash] || importedSessions[transcriptSessionID("codex", session.Path)] {
			continue
		}
		count++
	}
	return count
}

func transcriptEvidenceSessionIDs(env paths.Env, scope, source string) map[string]bool {
	out := map[string]bool{}
	manager := candidate.Manager{Env: env, Actor: "contextpack"}
	records, err := manager.List(scope)
	if err != nil {
		return out
	}
	prefix := source + ":"
	for _, rec := range records {
		if rec.Meta.CandidateType != model.CandidateTypeTranscriptNotes {
			continue
		}
		for _, id := range rec.Meta.SourceSessions {
			id = strings.TrimSpace(id)
			if strings.HasPrefix(id, prefix) {
				out[id] = true
			}
		}
	}
	return out
}

func transcriptSessionID(source, path string) string {
	if source == "" {
		source = "manual"
	}
	return source + ":" + filepath.Base(path)
}

func countObservedCursorSessions(root string) int {
	matches, err := filepath.Glob(filepath.Join(root, "raw", "cursor", "observed-*.metadata.json"))
	if err != nil {
		return 0
	}
	count := 0
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		var raw struct {
			Path string `json:"path"`
		}
		if err := json.Unmarshal(data, &raw); err != nil || strings.TrimSpace(raw.Path) == "" {
			continue
		}
		if _, err := os.Stat(raw.Path); err == nil {
			count++
		}
	}
	return count
}

func transcriptHashes(root, source string) map[string]bool {
	out := map[string]bool{}
	matches, err := filepath.Glob(filepath.Join(root, "raw", source, "*.metadata.json"))
	if err != nil {
		return out
	}
	for _, match := range matches {
		data, err := os.ReadFile(match)
		if err != nil {
			continue
		}
		var raw struct {
			Hash string `json:"hash"`
		}
		if err := json.Unmarshal(data, &raw); err == nil && raw.Hash != "" {
			out[raw.Hash] = true
		}
	}
	return out
}

func fileSHA256(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:])
}

func scopedMaintenanceCommand(command, scope string) string {
	if scope == "" || scope == "project" {
		return command
	}
	return command + " --scope " + scope
}

func maintenanceStepList(steps []string, prefix string) string {
	var matching []string
	for _, step := range steps {
		if strings.HasPrefix(step, prefix) {
			matching = append(matching, "`"+step+"`")
		}
	}
	if len(matching) == 0 {
		return "`" + prefix + "`"
	}
	return strings.Join(matching, " or ")
}

func hasMaintenance(maintenance Maintenance) bool {
	return maintenance.PendingEvidenceCandidates > 0 || maintenance.PendingSemanticCandidates > 0 || maintenance.EvidenceLifecycleCandidates > 0 || maintenance.ImportableCodexSessions > 0 || maintenance.ObservedCursorSessions > 0 || len(maintenance.NextSteps) > 0
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
