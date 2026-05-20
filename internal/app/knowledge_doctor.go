package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/index"
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
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Scope    string `json:"scope,omitempty"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
}

type knowledgeDoc struct {
	Scope         string
	Root          string
	Path          string
	Type          string
	Title         string
	Body          string
	Stage         string
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
	report.OK = report.Summary.Errors == 0 && (!strict || report.Summary.Warnings == 0)
	return report
}

func (r *knowledgeDoctorReport) add(code, severity, scope, path, message string) {
	r.Findings = append(r.Findings, knowledgeFinding{Code: code, Severity: severity, Scope: scope, Path: path, Message: message})
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
		docs = append(docs, knowledgeDoc{
			Scope:         scope,
			Root:          root,
			Path:          rel,
			Type:          index.InferType(rel, meta),
			Title:         knowledgeStringMeta(meta, "title", rel),
			Body:          body,
			Stage:         strings.TrimSpace(knowledgeStringMeta(meta, "stage", "")),
			Status:        strings.TrimSpace(knowledgeStringMeta(meta, "status", "")),
			Topic:         strings.TrimSpace(knowledgeStringMeta(meta, "topic", "")),
			SourceOfTruth: knowledgeBoolMeta(meta, "source_of_truth"),
			Supersedes:    knowledgeStringListMeta(meta, "supersedes"),
			SupersededBy:  knowledgeStringListMeta(meta, "superseded_by"),
		})
		return nil
	})
	return docs, err
}

func shouldSkipKnowledgeDoctorDir(path, root, name string) bool {
	if path == root {
		return false
	}
	switch name {
	case "candidates", "state", "raw", "index", "logs", "exports":
		return true
	default:
		return false
	}
}

func (r *knowledgeDoctorReport) checkDocs(docs []knowledgeDoc) {
	sotByTopic := map[string][]knowledgeDoc{}
	supersededBy := map[string][]string{}
	for _, doc := range docs {
		if doc.Stage != "" && !validKnowledgeStage(doc.Stage) {
			r.add("STAGE001", "error", doc.Scope, doc.Path, "stage metadata is not one of requirements, design, decision, implementation, validation, historical, retired")
		}
		if doc.SourceOfTruth && (doc.Stage == "historical" || doc.Stage == "retired" || doc.Status == "retired") {
			r.add("STAGE002", "error", doc.Scope, doc.Path, "retired or historical knowledge cannot be source_of_truth")
		}
		if doc.SourceOfTruth && doc.Topic == "" {
			r.add("SOT002", "warning", doc.Scope, doc.Path, "source_of_truth should declare a topic")
		}
		if doc.SourceOfTruth && doc.Topic != "" {
			key := doc.Scope + "\x00" + doc.Topic
			sotByTopic[key] = append(sotByTopic[key], doc)
		}
		for _, old := range doc.Supersedes {
			old = filepath.ToSlash(strings.TrimSpace(old))
			if old != "" {
				supersededBy[doc.Scope+"\x00"+old] = append(supersededBy[doc.Scope+"\x00"+old], doc.Path)
			}
		}
		for _, by := range doc.SupersededBy {
			by = filepath.ToSlash(strings.TrimSpace(by))
			if by != "" {
				supersededBy[doc.Scope+"\x00"+doc.Path] = append(supersededBy[doc.Scope+"\x00"+doc.Path], by)
			}
		}
		r.checkDocShape(doc)
	}
	for key, docs := range sotByTopic {
		if len(docs) <= 1 {
			continue
		}
		parts := strings.SplitN(key, "\x00", 2)
		for _, doc := range docs {
			r.add("SOT001", "error", doc.Scope, doc.Path, "multiple source_of_truth documents for topic "+parts[1])
		}
	}
	for _, doc := range docs {
		if len(supersededBy[doc.Scope+"\x00"+doc.Path]) > 0 && doc.SourceOfTruth {
			r.add("SUPER002", "error", doc.Scope, doc.Path, "superseded document is still marked source_of_truth")
		}
	}
	r.checkStarterIndex(docs, supersededBy)
}

func (r *knowledgeDoctorReport) checkDocShape(doc knowledgeDoc) {
	text := strings.ToLower(doc.Title + "\n" + doc.Body)
	signals := requirementSignalCount(text)
	switch doc.Type {
	case "decision":
		if !hasMarkdownHeading(text, "decision") {
			r.add("DEC001", "warning", doc.Scope, doc.Path, "decision document is missing a clear Decision section")
		}
		if signals >= 2 {
			r.add("REQ001", "warning", doc.Scope, doc.Path, "requirements-like content appears under decisions/; consider requirements/ for PRD, MVP scope, user goals, or acceptance intent")
		}
	case "architecture":
		if signals >= 3 {
			r.add("ARCH001", "warning", doc.Scope, doc.Path, "architecture document appears to mix substantial PRD, MVP scope, user problem, or acceptance intent")
		}
	}
}

func (r *knowledgeDoctorReport) checkStarterIndex(docs []knowledgeDoc, supersededBy map[string][]string) {
	bodyByScope := map[string]string{}
	for _, doc := range docs {
		if doc.Path == "index.md" {
			bodyByScope[doc.Scope] = doc.Body
		}
	}
	for _, doc := range docs {
		if len(supersededBy[doc.Scope+"\x00"+doc.Path]) == 0 {
			continue
		}
		if strings.Contains(bodyByScope[doc.Scope], doc.Path) {
			r.add("SUPER001", "warning", doc.Scope, "index.md", "starter knowledge index still references superseded document "+doc.Path)
		}
	}
}

func requirementSignalCount(text string) int {
	signals := []string{
		"prd", "mvp", "out of scope", "out-of-scope", "persona", "primary user",
		"user goal", "user problem", "workflow problem", "acceptance criteria",
		"business capability", "failure exit", "requirement-stage", "requirements clarification",
	}
	count := 0
	for _, signal := range signals {
		if strings.Contains(text, signal) {
			count++
		}
	}
	return count
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
	}
}
