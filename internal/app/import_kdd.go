package app

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/util"
)

type kddImportReport struct {
	Source       string          `json:"source"`
	Project      string          `json:"project"`
	Root         string          `json:"root"`
	Matched      int             `json:"matched"`
	Created      int             `json:"created"`
	Skipped      int             `json:"skipped"`
	Blocked      int             `json:"blocked"`
	LocalSkipped int             `json:"local_skipped"`
	DryRun       bool            `json:"dry_run"`
	Items        []kddImportItem `json:"items,omitempty"`
	Candidates   []string        `json:"candidates,omitempty"`
	NextSteps    []string        `json:"next_steps,omitempty"`
	GitGuidance  []string        `json:"git_guidance,omitempty"`
}

type kddImportItem struct {
	SourcePath      string `json:"source_path"`
	CandidateID     string `json:"candidate_id,omitempty"`
	CandidateType   string `json:"candidate_type,omitempty"`
	TargetPath      string `json:"target_path,omitempty"`
	Operation       string `json:"operation,omitempty"`
	Title           string `json:"title,omitempty"`
	Summary         string `json:"summary,omitempty"`
	RedactionStatus string `json:"redaction_status,omitempty"`
	SkipReason      string `json:"skip_reason,omitempty"`
	Body            string `json:"-"`
}

func runImportKDD(env paths.Env, ioctx IO, flags map[string]string, scope string) error {
	if scope != "project" && scope != "" {
		return errors.New("kdd import currently supports project scope only")
	}
	root := flagValue(flags, "root", filepath.Join(env.ProjectRoot, "docs", "knowledge-driven-development"))
	if !filepath.IsAbs(root) {
		root = filepath.Join(env.ProjectRoot, root)
	}
	root, _ = filepath.Abs(root)
	if st, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("kdd root does not exist: %s", root)
		}
		return err
	} else if !st.IsDir() {
		return fmt.Errorf("kdd root is not a directory: %s", root)
	}

	report := kddImportReport{
		Source:      "kdd",
		Project:     env.ProjectRoot,
		Root:        root,
		DryRun:      flags["all"] != "true",
		NextSteps:   kddImportNextSteps(flags["all"] != "true"),
		GitGuidance: importGitGuidance(),
	}
	items, skippedItems, localSkipped, err := discoverKDDImportItems(root)
	if err != nil {
		return err
	}
	report.LocalSkipped = localSkipped
	report.Skipped = len(skippedItems)
	report.Items = append(report.Items, skippedItems...)
	manager := candidate.Manager{Env: env, Actor: "cli:import-kdd"}
	for _, item := range items {
		body := kddCandidateBody(item)
		scan := redact.Scan(body)
		item.RedactionStatus = string(scan.Status)
		if scan.Status == redact.StatusBlocked {
			report.Blocked++
			report.Items = append(report.Items, item)
			continue
		}
		report.Matched++
		report.Items = append(report.Items, item)
		if report.DryRun {
			continue
		}
		tags := []string{"kdd-import"}
		if item.SourcePath == "project/active-knowledge-log.md" {
			tags = append(tags, "kdd", "split-source")
		}
		rec, err := manager.Create(candidate.CreateRequest{
			Scope:         "project",
			ID:            item.CandidateID,
			CandidateType: item.CandidateType,
			TargetPath:    item.TargetPath,
			Title:         item.Title,
			Summary:       item.Summary,
			Operation:     item.Operation,
			Tags:          tags,
			Body:          body,
		})
		if err != nil {
			if errors.Is(err, candidate.ErrBlocked) {
				report.Blocked++
				continue
			}
			if isDuplicateCandidateError(err) {
				report.Skipped++
				continue
			}
			return err
		}
		report.Created++
		report.Candidates = append(report.Candidates, rec.Meta.ID)
	}
	return printKDDImportReport(ioctx, report, flagValue(flags, "format", "text"))
}

func discoverKDDImportItems(root string) ([]kddImportItem, []kddImportItem, int, error) {
	var items []kddImportItem
	var skippedItems []kddImportItem
	localSkipped := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(d.Name()) != ".md" {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "local" || strings.HasPrefix(rel, "local/") {
			localSkipped++
			return nil
		}
		if isKDDCategoryREADME(rel) {
			skippedItems = append(skippedItems, kddImportItem{SourcePath: rel, SkipReason: "category README is directory guidance and is skipped by default"})
			return nil
		}
		item, ok := mapKDDPath(rel)
		if !ok {
			skippedItems = append(skippedItems, kddImportItem{SourcePath: rel, SkipReason: "outside supported KDD project knowledge paths"})
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		item.Body = string(b)
		items = append(items, item)
		return nil
	})
	return items, skippedItems, localSkipped, err
}

func mapKDDPath(rel string) (kddImportItem, bool) {
	if rel == "project/README.md" {
		return newKDDImportItem(rel, "project", "project.md", candidate.OperationMerge, "KDD Project README", "Imported KDD project README."), true
	}
	if rel == "project/active-knowledge-log.md" {
		item := newKDDImportItem(rel, "lesson", "lessons/kdd-active-knowledge-log.md", candidate.OperationReplace, "KDD Active Knowledge Log", "Pending Verification: imported from KDD active knowledge log as a split source. Do not promote directly without extracting durable semantic candidates.")
		return item, true
	}
	if !strings.HasPrefix(rel, "project/") {
		return kddImportItem{}, false
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return kddImportItem{}, false
	}
	category := parts[1]
	slug := util.Slug(strings.TrimSuffix(strings.Join(parts[2:], "-"), ".md"))
	switch category {
	case "architecture":
		return newKDDImportItem(rel, "architecture", filepath.ToSlash(filepath.Join("architecture", slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported KDD architecture knowledge."), true
	case "decisions":
		return newKDDImportItem(rel, "decision", filepath.ToSlash(filepath.Join("decisions", slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported KDD decision knowledge."), true
	case "runbooks":
		return newKDDImportItem(rel, "workflow", filepath.ToSlash(filepath.Join("workflows", slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported KDD runbook as Worktrail workflow."), true
	case "integrations":
		return newKDDImportItem(rel, "integration", filepath.ToSlash(filepath.Join("integrations", slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported KDD integration knowledge."), true
	case "validation":
		return newKDDImportItem(rel, "validation", filepath.ToSlash(filepath.Join("validation", slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported KDD validation knowledge."), true
	case "glossary":
		return newKDDImportItem(rel, "glossary", filepath.ToSlash(filepath.Join("glossary", slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported KDD glossary knowledge."), true
	default:
		return newKDDImportItem(rel, "lesson", filepath.ToSlash(filepath.Join("lessons", "kdd-"+slug+".md")), candidate.OperationReplace, titleFromKDDPath(rel), "Imported uncategorized KDD project knowledge."), true
	}
}

func isKDDCategoryREADME(rel string) bool {
	if !strings.HasPrefix(rel, "project/") || !strings.HasSuffix(rel, "/README.md") {
		return false
	}
	return rel != "project/README.md"
}

func newKDDImportItem(rel, typ, target, op, title, summary string) kddImportItem {
	return kddImportItem{
		SourcePath:    rel,
		CandidateID:   kddCandidateID(rel),
		CandidateType: typ,
		TargetPath:    target,
		Operation:     op,
		Title:         title,
		Summary:       summary,
	}
}

func kddCandidateID(rel string) string {
	base := "kdd-" + util.Slug(strings.TrimSuffix(rel, ".md"))
	if len(base) <= 64 {
		return base
	}
	sum := sha1.Sum([]byte(rel))
	suffix := hex.EncodeToString(sum[:])[:8]
	keep := 64 - len(suffix) - 1
	base = strings.Trim(base[:keep], "-")
	return base + "-" + suffix
}

func titleFromKDDPath(rel string) string {
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	words := strings.Split(util.Slug(base), "-")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

func kddCandidateBody(item kddImportItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", item.Title)
	fmt.Fprintf(&b, "Imported from KDD relative path: `%s`.\n\n", item.SourcePath)
	if item.Summary != "" {
		fmt.Fprintf(&b, "Import note: %s\n\n", item.Summary)
	}
	b.WriteString(strings.TrimSpace(item.Body))
	b.WriteByte('\n')
	return b.String()
}

func kddImportNextSteps(dryRun bool) []string {
	if dryRun {
		return []string{"run `worktrail import kdd --all` to create pending semantic candidates"}
	}
	return []string{"run `worktrail review` to inspect imported KDD candidates before promote, merge, or discard"}
}

func printKDDImportReport(ioctx IO, report kddImportReport, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "source: %s\nproject: %s\nroot: %s\nmatched: %d\n", report.Source, report.Project, report.Root, report.Matched)
	fmt.Fprintf(ioctx.Out, "dry_run: %t\ncreated: %d\nskipped: %d\nblocked: %d\nlocal_skipped: %d\n", report.DryRun, report.Created, report.Skipped, report.Blocked, report.LocalSkipped)
	for _, item := range report.Items {
		if item.SkipReason != "" {
			fmt.Fprintf(ioctx.Out, "skipped: %s\t%s\n", item.SourcePath, item.SkipReason)
			continue
		}
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\t%s\t%s\n", item.SourcePath, item.CandidateID, item.CandidateType, item.Operation, item.TargetPath)
	}
	for _, id := range report.Candidates {
		fmt.Fprintf(ioctx.Out, "candidate: %s\n", id)
	}
	printImportGuidance(ioctx, importReport{NextSteps: report.NextSteps, GitGuidance: report.GitGuidance})
	if report.LocalSkipped > 0 {
		fmt.Fprintln(ioctx.Out, "- local/** was skipped by default; migrate personal knowledge with a separate user-scope workflow if needed.")
	}
	return nil
}
