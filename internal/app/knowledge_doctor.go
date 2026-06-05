package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

const knowledgeDoctorSchema = "worktrail.doctor.knowledge.v1"

type knowledgeDoctorReport struct {
	Schema      string                 `json:"schema"`
	GeneratedAt string                 `json:"generated_at"`
	Project     string                 `json:"project"`
	Scope       string                 `json:"scope"`
	OK          bool                   `json:"ok"`
	Summary     knowledgeDoctorSummary `json:"summary"`
	Findings    []knowledgeFinding     `json:"findings"`
}

type knowledgeDoctorSummary struct {
	Errors   int `json:"errors"`
	Warnings int `json:"warnings"`
}

type knowledgeFinding struct {
	Code     string   `json:"code"`
	Severity string   `json:"severity"`
	Scope    string   `json:"scope,omitempty"`
	Path     string   `json:"path,omitempty"`
	Message  string   `json:"message"`
	Hint     string   `json:"hint,omitempty"`
	Commands []string `json:"commands,omitempty"`
}

type knowledgeDoc struct {
	Scope         string
	Root          string
	Path          string
	Type          string
	Title         string
	Body          string
	Stage         string
	LifecycleMeta string
	Lifecycle     string
	Status        string
	Topic         string
	SourceOfTruth bool
	Supersedes    []string
	SupersededBy  []string
}

func runDoctorKnowledge(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printKnowledgeDoctorHelp(ioctx.Out)
		return nil
	}
	flags, _ := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	report := buildKnowledgeDoctorReport(env, scope, flags["strict"] == "true")
	if flagValue(flags, "format", "text") == "json" {
		if err := json.NewEncoder(ioctx.Out).Encode(report); err != nil {
			return err
		}
	} else {
		renderKnowledgeDoctorText(ioctx.Out, report)
	}
	if report.Summary.Errors > 0 || flags["strict"] == "true" && report.Summary.Warnings > 0 {
		return fmt.Errorf("knowledge doctor failed: errors=%d warnings=%d", report.Summary.Errors, report.Summary.Warnings)
	}
	return nil
}

func printKnowledgeDoctorHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail doctor knowledge [--scope project|user|all] [--strict] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Checks knowledge stage metadata, requirements/design/decision separation, source-of-truth conflicts, and superseded index entries.")
}

func buildKnowledgeDoctorReport(env paths.Env, scope string, strict bool) knowledgeDoctorReport {
	if scope == "" {
		scope = "project"
	}
	report := knowledgeDoctorReport{
		Schema:      knowledgeDoctorSchema,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Project:     env.ProjectRoot,
		Scope:       scope,
		Findings:    []knowledgeFinding{},
	}
	scopes := []string{scope}
	if scope == "all" {
		scopes = []string{"project", "user"}
	}
	var docs []knowledgeDoc
	for _, s := range scopes {
		root, err := env.ScopeRoot(s)
		if err != nil {
			report.add("ROOT001", "error", s, "", err.Error())
			continue
		}
		scanned, err := scanKnowledgeDocs(root, s)
		if err != nil {
			report.add("ROOT001", "error", s, root, err.Error())
			continue
		}
		docs = append(docs, scanned...)
	}
	report.checkDocs(docs)
	report.checkIndexHealth(env, scopes)
	report.checkWriteEscapes(env, docs)
	report.OK = report.Summary.Errors == 0 && (!strict || report.Summary.Warnings == 0)
	return report
}

func (r *knowledgeDoctorReport) add(code, severity, scope, path, message string) {
	hint, commands := knowledgeFindingRemediation(code, scope, path)
	r.Findings = append(r.Findings, knowledgeFinding{
		Code:     code,
		Severity: severity,
		Scope:    scope,
		Path:     path,
		Message:  message,
		Hint:     hint,
		Commands: commands,
	})
	if severity == "error" {
		r.Summary.Errors++
	} else {
		r.Summary.Warnings++
	}
}

func scanKnowledgeDocs(root, scope string) ([]knowledgeDoc, error) {
	var docs []knowledgeDoc
	if root == "" {
		return docs, nil
	}
	if _, err := os.Stat(root); os.IsNotExist(err) {
		return docs, nil
	} else if err != nil {
		return nil, err
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipKnowledgeDoctorDir(path, root, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		body := string(b)
		meta := map[string]any{}
		if doc, err := store.ParseMarkdown(b); err == nil {
			meta = doc.Meta
			body = doc.Body
		}
		norm, normErr := model.NormalizeObjectMeta(rel, meta)
		docs = append(docs, knowledgeDoc{
			Scope:         scope,
			Root:          root,
			Path:          rel,
			Type:          index.InferType(rel, meta),
			Title:         knowledgeStringMeta(meta, "title", rel),
			Body:          body,
			Stage:         strings.TrimSpace(knowledgeStringMeta(meta, "stage", "")),
			LifecycleMeta: strings.TrimSpace(knowledgeStringMeta(meta, "lifecycle", "")),
			Lifecycle:     knowledge.NormalizeLifecycle(knowledgeStringMeta(meta, "lifecycle", ""), knowledgeStringMeta(meta, "stage", ""), knowledgeStringMeta(meta, "status", "")),
			Status:        strings.TrimSpace(knowledgeStringMeta(meta, "status", "")),
			Topic:         strings.TrimSpace(knowledgeStringMeta(meta, "topic", "")),
			SourceOfTruth: knowledgeBoolMeta(meta, "source_of_truth"),
			Supersedes:    knowledgeStringListMeta(meta, "supersedes"),
			SupersededBy:  knowledgeStringListMeta(meta, "superseded_by"),
		})
		if normErr == nil {
			last := &docs[len(docs)-1]
			if norm.IsKnowledgeDoc() && norm.KnowledgeType != "" {
				last.Type = norm.KnowledgeType
			}
			if last.Title == rel && strings.TrimSpace(norm.Title) != "" {
				last.Title = norm.Title
			}
			if strings.TrimSpace(last.Stage) == "" {
				last.Stage = strings.TrimSpace(norm.Stage)
			}
			if strings.TrimSpace(last.Lifecycle) == "" {
				last.Lifecycle = normalizeDoctorLifecycle(norm)
			}
			if strings.TrimSpace(last.Status) == "" {
				last.Status = normalizeDoctorStatus(norm)
			}
			if strings.TrimSpace(last.Topic) == "" {
				last.Topic = strings.TrimSpace(norm.Topic)
			}
			last.SourceOfTruth = last.SourceOfTruth || norm.SourceOfTruth
			if len(last.Supersedes) == 0 {
				last.Supersedes = append([]string(nil), norm.Supersedes...)
			}
			if len(last.SupersededBy) == 0 {
				last.SupersededBy = append([]string(nil), norm.SupersededBy...)
			}
		}
		return nil
	})
	return docs, err
}

func shouldSkipKnowledgeDoctorDir(path, root, name string) bool {
	if path == root {
		return false
	}
	switch name {
	case "candidates", "state", "raw", "index", "logs", "exports", "staging", "runtime", "derived":
		return true
	default:
		return false
	}
}

func (r *knowledgeDoctorReport) checkDocs(docs []knowledgeDoc) {
	sotByTopicTypeStage := map[string][]knowledgeDoc{}
	supersededBy := map[string][]string{}
	docsByScopePath := map[string]bool{}
	for _, doc := range docs {
		docsByScopePath[doc.Scope+"\x00"+doc.Path] = true
	}
	for _, doc := range docs {
		if doc.Stage != "" && !validKnowledgeStage(doc.Stage) {
			r.add("STAGE001", "error", doc.Scope, doc.Path, "stage metadata is not one of requirements, design, decision, implementation, validation, historical, retired")
		}
		if doc.LifecycleMeta != "" && !knowledge.IsValidLifecycle(doc.LifecycleMeta) {
			r.add("LIFE001", "error", doc.Scope, doc.Path, "lifecycle metadata is not one of current, historical, or retired")
		}
		if doc.SourceOfTruth && knowledge.IsNonCurrentLifecycle(doc.Lifecycle) {
			r.add("STAGE002", "error", doc.Scope, doc.Path, "retired or historical knowledge cannot be source_of_truth")
		}
		if doc.SourceOfTruth && doc.Topic == "" {
			r.add("SOT002", "warning", doc.Scope, doc.Path, "source_of_truth should declare a topic")
		}
		if shouldRequireTopic(doc) && doc.Topic == "" {
			r.add("TOPIC001", "warning", doc.Scope, doc.Path, "current durable knowledge should declare a topic for thread-aware context and governance")
		}
		if doc.SourceOfTruth && doc.Topic != "" && !knowledge.IsNonCurrentLifecycle(doc.Lifecycle) {
			key := doc.Scope + "\x00" + doc.Topic + "\x00" + doc.Type + "\x00" + activeStageForDoctor(doc)
			sotByTopicTypeStage[key] = append(sotByTopicTypeStage[key], doc)
		}
		for _, old := range doc.Supersedes {
			old = filepath.ToSlash(strings.TrimSpace(old))
			if old != "" {
				supersededBy[doc.Scope+"\x00"+old] = append(supersededBy[doc.Scope+"\x00"+old], doc.Path)
				if !docsByScopePath[doc.Scope+"\x00"+old] {
					r.add("REF001", "warning", doc.Scope, doc.Path, "supersedes references missing document "+old)
				}
			}
		}
		for _, by := range doc.SupersededBy {
			by = filepath.ToSlash(strings.TrimSpace(by))
			if by != "" {
				supersededBy[doc.Scope+"\x00"+doc.Path] = append(supersededBy[doc.Scope+"\x00"+doc.Path], by)
				if !docsByScopePath[doc.Scope+"\x00"+by] {
					r.add("REF002", "warning", doc.Scope, doc.Path, "superseded_by references missing document "+by)
				}
			}
		}
		r.checkDocShape(doc)
	}
	for key, docs := range sotByTopicTypeStage {
		if len(docs) <= 1 {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		detail := ""
		if len(parts) == 2 {
			detail = parts[1]
		}
		for _, doc := range docs {
			r.add("SOT001", "error", doc.Scope, doc.Path, "multiple source_of_truth documents for topic/type/stage "+detail)
		}
	}
	for _, doc := range docs {
		if len(supersededBy[doc.Scope+"\x00"+doc.Path]) > 0 && doc.SourceOfTruth {
			r.add("SUPER002", "error", doc.Scope, doc.Path, "superseded document is still marked source_of_truth")
		}
	}
	r.checkStarterRefs(docs, supersededBy)
}

func (r *knowledgeDoctorReport) checkWriteEscapes(env paths.Env, docs []knowledgeDoc) {
	candidateTrails := appliedCandidateTargets(env)
	for _, doc := range docs {
		if !knowledge.IsFormalKnowledgePath(doc.Path) {
			continue
		}
		if !shouldRequireAppliedCandidateTrail(doc) {
			continue
		}
		if !candidateTrails[doc.Scope+"\x00"+doc.Path] {
			r.add("ESCAPE003", "warning", doc.Scope, doc.Path, "formal knowledge has no applied candidate trail; recover with `worktrail note add` or create a pending candidate before promote/merge")
		}
	}
	if env.ProjectRoot == "" || env.ProjectWT == "" {
		return
	}
	for _, item := range gitFormalStatus(env.ProjectRoot) {
		path := strings.TrimPrefix(filepath.ToSlash(item.Path), ".worktrail/")
		if !knowledge.IsFormalKnowledgePath(path) {
			continue
		}
		switch item.Status {
		case "untracked":
			if shouldSuppressUntrackedFormalWarning(env.ProjectWT, path) {
				continue
			}
			r.add("ESCAPE001", "warning", "project", path, "untracked formal knowledge file may bypass review; recover with `worktrail note add` or remove it if unintended")
		case "modified":
			r.add("ESCAPE002", "warning", "project", path, "modified formal knowledge file may bypass review; create a pending candidate or promote/merge through Worktrail")
		case "deleted":
			r.add("ESCAPE005", "warning", "project", path, "deleted formal knowledge file may bypass retire flow; use `worktrail retire <id> --reason <text>` when intentional")
		}
	}
}

func appliedCandidateTargets(env paths.Env) map[string]bool {
	out := map[string]bool{}
	manager := candidate.Manager{Env: env, Actor: "doctor-knowledge"}
	for _, scope := range []string{"project", "user"} {
		records, err := manager.List(scope)
		if err != nil {
			continue
		}
		for _, rec := range records {
			switch rec.Meta.Status {
			case candidate.StatusPromoted, candidate.StatusMerged, candidate.StatusRetired:
				out[rec.Meta.Scope+"\x00"+filepath.ToSlash(rec.Meta.TargetPath)] = true
			}
		}
	}
	return out
}

type gitStatusItem struct {
	Status string
	Path   string
}

func gitFormalStatus(projectRoot string) []gitStatusItem {
	if projectRoot == "" {
		return nil
	}
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	cmd := exec.Command("git", "-C", projectRoot, "status", "--porcelain", "--", ".worktrail")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var items []gitStatusItem
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		code := line[:2]
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = parts[len(parts)-1]
		}
		status := "modified"
		if code == "??" {
			status = "untracked"
		} else if strings.Contains(code, "D") {
			status = "deleted"
		}
		items = append(items, gitStatusItem{Status: status, Path: path})
	}
	return items
}

func (r *knowledgeDoctorReport) checkDocShape(doc knowledgeDoc) {
	text := strings.ToLower(doc.Title + "\n" + doc.Body)
	bodyLower := strings.ToLower(doc.Body)
	signals := requirementHeadingSignalCount(bodyLower)
	switch doc.Type {
	case "decision":
		if !hasMarkdownHeading(text, "decision") {
			r.add("DEC001", "warning", doc.Scope, doc.Path, "decision document is missing a clear Decision section")
		}
		if signals >= 2 {
			r.add("REQ001", "warning", doc.Scope, doc.Path, "requirements-like content appears under decisions/ (signals detected in H2/H3/H4 section headings); consider requirements/ for PRD, MVP scope, user goals, or acceptance intent")
		}
	case "architecture":
		if signals >= 2 {
			r.add("ARCH001", "warning", doc.Scope, doc.Path, "architecture document appears to mix substantial PRD, MVP scope, user problem, or acceptance intent (signals detected in H2/H3/H4 section headings)")
		}
	}
}

func (r *knowledgeDoctorReport) checkStarterRefs(docs []knowledgeDoc, supersededBy map[string][]string) {
	bodyByScope := map[string]map[string]string{}
	for _, doc := range docs {
		if doc.Path != "index.md" && doc.Path != "project.md" {
			continue
		}
		if bodyByScope[doc.Scope] == nil {
			bodyByScope[doc.Scope] = map[string]string{}
		}
		bodyByScope[doc.Scope][doc.Path] = doc.Body
	}
	for _, doc := range docs {
		if len(supersededBy[doc.Scope+"\x00"+doc.Path]) == 0 {
			continue
		}
		if strings.Contains(bodyByScope[doc.Scope]["index.md"], doc.Path) {
			r.add("SUPER001", "warning", doc.Scope, "index.md", "starter knowledge index still references superseded document "+doc.Path)
		}
		if strings.Contains(bodyByScope[doc.Scope]["project.md"], doc.Path) {
			r.add("SUPER003", "warning", doc.Scope, "project.md", "project knowledge overview still references superseded document "+doc.Path)
		}
	}
}

// requirementSignals are phrases that, when they show up in section headings,
// indicate the document is carrying PRD-style requirements content. Each entry
// is compiled into a word-boundary regex so substrings like "personal-accident"
// do not collide with "persona" and "personality" does not collide with "persona".
var requirementSignals = []string{
	"prd", "mvp", "out of scope", "out-of-scope", "persona", "primary user",
	"user goal", "user problem", "workflow problem", "acceptance criteria",
	"business capability", "failure exit", "requirement-stage", "requirements clarification",
}

var requirementSignalPatterns = func() []*regexp.Regexp {
	patterns := make([]*regexp.Regexp, 0, len(requirementSignals))
	for _, signal := range requirementSignals {
		// Allow an optional trailing "s" for English plurals (e.g. persona -> personas,
		// user goal -> user goals) without matching unrelated longer words like
		// "personal-accident" or "personality".
		patterns = append(patterns, regexp.MustCompile(`(?i)(?:^|[^a-z0-9_-])`+regexp.QuoteMeta(signal)+`s?(?:$|[^a-z0-9_-])`))
	}
	return patterns
}()

// requirementHeadingSignalCount counts how many H2/H3/H4 section headings in
// the document body contain at least one PRD-style signal. H1 is excluded on
// purpose: a decision document is allowed to mention "primary user" in its own
// title (e.g. "Delivery Case As Primary User Entrypoint") without being
// flagged as smuggling requirements content. Counting headings (rather than
// distinct signals) keeps documents like "MVP scope / MVP must-have / MVP
// non-goals / MVP boundary" classified as PRD-heavy even though they only use
// one signal phrase.
func requirementHeadingSignalCount(bodyLower string) int {
	headings := collectSectionHeadings(bodyLower)
	if len(headings) == 0 {
		return 0
	}
	count := 0
	for _, heading := range headings {
		for _, re := range requirementSignalPatterns {
			if re.MatchString(heading) {
				count++
				break
			}
		}
	}
	return count
}

// collectSectionHeadings returns the heading text of every H2/H3/H4 line in the
// body. The body must already be lower-cased. H1 lines are skipped because
// the document title itself is not a section heading.
func collectSectionHeadings(bodyLower string) []string {
	var out []string
	for _, line := range strings.Split(bodyLower, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "##") {
			continue
		}
		// Skip H5+ as well; we only care about substantial section headings.
		if strings.HasPrefix(trimmed, "#####") {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	return out
}

func hasMarkdownHeading(text, heading string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		if line == heading {
			return true
		}
	}
	return false
}

func validKnowledgeStage(stage string) bool {
	switch stage {
	case "requirements", "design", "decision", "implementation", "validation", "historical", "retired":
		return true
	default:
		return false
	}
}

func normalizeDoctorLifecycle(meta model.ObjectMetaV2) string {
	switch meta.LifecycleStatus {
	case "", model.LifecycleCurrent, model.LifecyclePendingReview, model.LifecyclePendingDistill:
		return ""
	default:
		return meta.LifecycleStatus
	}
}

func normalizeDoctorStatus(meta model.ObjectMetaV2) string {
	switch meta.LifecycleStatus {
	case model.LifecyclePendingReview, model.LifecyclePendingDistill:
		return "pending"
	default:
		return meta.LifecycleStatus
	}
}

func activeStageForDoctor(doc knowledgeDoc) string {
	stage := strings.TrimSpace(doc.Stage)
	if stage != "" {
		return stage
	}
	return "current"
}

func shouldRequireTopic(doc knowledgeDoc) bool {
	if knowledge.IsNonCurrentLifecycle(doc.Lifecycle) {
		return false
	}
	switch doc.Type {
	case "project", "index", "profile", "prompt", "handoff", "state", "candidate", "knowledge", "decision", "rule", "log":
		return false
	default:
		return true
	}
}

func shouldRequireAppliedCandidateTrail(doc knowledgeDoc) bool {
	if !knowledge.IsFormalKnowledgePath(doc.Path) {
		return false
	}
	if doc.Type == "handoff" {
		return false
	}
	return !(doc.Scope == "project" && store.IsProjectBootstrapKnowledgePath(doc.Path))
}

func shouldSuppressUntrackedFormalWarning(projectWT, path string) bool {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return false
	}
	if store.IsProjectBootstrapKnowledgePath(path) || strings.HasPrefix(path, "handoffs/") {
		return true
	}
	abs := filepath.Join(projectWT, filepath.FromSlash(path))
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := untrackedFormalMarkdownPaths(projectWT, abs)
	if err != nil || len(entries) == 0 {
		return false
	}
	for _, rel := range entries {
		if !(store.IsProjectBootstrapKnowledgePath(rel) || strings.HasPrefix(rel, "handoffs/")) {
			return false
		}
	}
	return true
}

func untrackedFormalMarkdownPaths(root, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if knowledge.IsFormalKnowledgePath(rel) {
			out = append(out, rel)
		}
		return nil
	})
	return out, err
}

func (r *knowledgeDoctorReport) checkIndexHealth(env paths.Env, scopes []string) {
	for _, scope := range scopes {
		root, err := env.ScopeRoot(scope)
		if err != nil || root == "" {
			continue
		}
		if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
			continue
		} else if err != nil {
			r.add("IDX000", "warning", scope, "", err.Error())
			continue
		}
		health, err := index.Health(root)
		if err != nil {
			r.add("IDX000", "warning", scope, "", err.Error())
			continue
		}
		for _, item := range health.MissingFromFS {
			if !knowledge.IsFormalKnowledgePath(item.Path) {
				continue
			}
			r.add("IDX001", "warning", scope, item.Path, "formal knowledge is still indexed but missing from filesystem")
		}
		for _, item := range health.MissingFromIndex {
			if !knowledge.IsFormalKnowledgePath(item.Path) {
				continue
			}
			r.add("IDX002", "warning", scope, item.Path, "formal knowledge exists on disk but is missing from the current index")
		}
		for _, item := range health.Changed {
			if !knowledge.IsFormalKnowledgePath(item.Path) {
				continue
			}
			r.add("IDX003", "warning", scope, item.Path, "formal knowledge is newer than the current index")
		}
	}
}

func knowledgeStringMeta(meta map[string]any, key, fallback string) string {
	v, ok := meta[key]
	if !ok {
		return fallback
	}
	if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
		return s
	}
	return fallback
}

func knowledgeBoolMeta(meta map[string]any, key string) bool {
	v, ok := meta[key]
	if !ok {
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		return strings.EqualFold(strings.TrimSpace(x), "true")
	default:
		return false
	}
}

func knowledgeStringListMeta(meta map[string]any, key string) []string {
	v, ok := meta[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{filepath.ToSlash(strings.TrimSpace(x))}
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, filepath.ToSlash(s))
				}
			}
		}
		return out
	default:
		return nil
	}
}

func renderKnowledgeDoctorText(out io.Writer, report knowledgeDoctorReport) {
	fmt.Fprintf(out, "schema: %s\nproject: %s\nscope: %s\nok: %t\nerrors: %d\nwarnings: %d\n", report.Schema, report.Project, report.Scope, report.OK, report.Summary.Errors, report.Summary.Warnings)
	for _, finding := range report.Findings {
		if finding.Path != "" {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Scope, finding.Path, finding.Message)
		} else {
			fmt.Fprintf(out, "%s\t%s\t%s\t%s\n", finding.Severity, finding.Code, finding.Scope, finding.Message)
		}
		if finding.Hint != "" {
			fmt.Fprintf(out, "  hint: %s\n", finding.Hint)
		}
		for _, command := range finding.Commands {
			fmt.Fprintf(out, "  next: %s\n", command)
		}
	}
}

func knowledgeFindingRemediation(code, scope, path string) (string, []string) {
	rebuild := "worktrail index rebuild --scope " + normalizeKnowledgeScope(scope)
	switch code {
	case "DEC001":
		return `add a markdown heading named "Decision"; if you already have "Migration Decision", rename it to "Decision"`, nil
	case "STAGE001":
		return "use one of requirements, design, decision, implementation, validation, historical, or retired", nil
	case "LIFE001":
		return "use one of current, historical, or retired for lifecycle metadata", nil
	case "SUPER001":
		return "remove the stale reference from index.md or update it to the superseding document", nil
	case "SUPER003":
		return "remove the stale reference from project.md or update it to the superseding document", nil
	case "REF001", "REF002":
		return "update the supersedes metadata to point at an existing document or remove the broken reference", nil
	case "ESCAPE005":
		return "prefer retiring the applied candidate trail instead of deleting the formal document directly", nil
	case "IDX001", "IDX002", "IDX003":
		return "rebuild the local text index so context and search read the current filesystem state", []string{rebuild}
	case "ESCAPE001", "ESCAPE002", "ESCAPE003":
		return "recover through Worktrail-managed note/promote/merge flow instead of editing formal knowledge directly", nil
	default:
		return "", nil
	}
}

func normalizeKnowledgeScope(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return "project"
	}
	return scope
}
