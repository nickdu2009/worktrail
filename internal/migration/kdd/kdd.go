package kdd

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	SourceName             = "kdd"
	DefaultLegacyRoot      = "docs/knowledge-driven-development"
	ProjectActiveLogTarget = "imports/kdd/project/active-knowledge-log.md"
	LocalActiveLogTarget   = "imports/kdd/local/active-knowledge-log.md"
)

type Options struct {
	Root            string
	WriteCandidates bool
	WritePack       string
	NowActor        string
}

type Report struct {
	Source                string   `json:"source"`
	Project               string   `json:"project"`
	Root                  string   `json:"root"`
	Matched               int      `json:"matched"`
	Created               int      `json:"created"`
	Skipped               int      `json:"skipped"`
	Blocked               int      `json:"blocked"`
	ProjectItems          int      `json:"project_items"`
	LocalItems            int      `json:"local_items"`
	DryRun                bool     `json:"dry_run"`
	LegacyCleanupRequired bool     `json:"legacy_cleanup_required"`
	WritePack             string   `json:"write_pack,omitempty"`
	Items                 []Item   `json:"items,omitempty"`
	Candidates            []string `json:"candidates,omitempty"`
	NextSteps             []string `json:"next_steps,omitempty"`
	GitGuidance           []string `json:"git_guidance,omitempty"`
}

type Item struct {
	SourcePath      string   `json:"source_path"`
	Scope           string   `json:"scope,omitempty"`
	CandidateID     string   `json:"candidate_id,omitempty"`
	CandidateType   string   `json:"candidate_type,omitempty"`
	TargetPath      string   `json:"target_path,omitempty"`
	Operation       string   `json:"operation,omitempty"`
	Title           string   `json:"title,omitempty"`
	Summary         string   `json:"summary,omitempty"`
	RedactionStatus string   `json:"redaction_status,omitempty"`
	Warnings        []string `json:"warnings,omitempty"`
	SkipReason      string   `json:"skip_reason,omitempty"`
	Body            string   `json:"-"`
}

func Run(env paths.Env, opts Options) (Report, error) {
	root, err := ResolveRoot(env, opts.Root)
	if err != nil {
		return Report{}, err
	}
	items, skipped, err := Discover(root)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		Source:                SourceName,
		Project:               env.ProjectRoot,
		Root:                  root,
		DryRun:                !opts.WriteCandidates,
		LegacyCleanupRequired: true,
		NextSteps:             NextSteps(!opts.WriteCandidates),
		GitGuidance:           GitGuidance(),
	}
	report.Skipped = len(skipped)
	report.Items = append(report.Items, skipped...)
	manager := candidate.Manager{Env: env, Actor: actor(opts.NowActor)}
	for _, item := range items {
		body := CandidateBody(item)
		scan := redact.Scan(body)
		item.RedactionStatus = string(scan.Status)
		item.Warnings = appendLocalPathWarning(item.Warnings, item.Body)
		if scan.Status == redact.StatusBlocked {
			report.Blocked++
			report.Items = append(report.Items, item)
			continue
		}
		report.Matched++
		if item.Scope == "user" {
			report.LocalItems++
		} else {
			report.ProjectItems++
		}
		report.Items = append(report.Items, item)
		if report.DryRun {
			continue
		}
		rec, err := manager.Create(candidate.CreateRequest{
			Scope:         item.Scope,
			ID:            item.CandidateID,
			CandidateType: item.CandidateType,
			TargetPath:    item.TargetPath,
			Title:         item.Title,
			Summary:       item.Summary,
			Operation:     item.Operation,
			Tags:          tagsFor(item),
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
			return Report{}, err
		}
		report.Created++
		report.Candidates = append(report.Candidates, rec.Meta.ID)
	}
	if opts.WritePack != "" {
		if err := WritePack(opts.WritePack, report.Items); err != nil {
			return Report{}, err
		}
		report.WritePack = opts.WritePack
	}
	return report, nil
}

func ResolveRoot(env paths.Env, root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(env.ProjectRoot, DefaultLegacyRoot)
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(env.ProjectRoot, root)
	}
	root, _ = filepath.Abs(root)
	if st, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("kdd root does not exist: %s", root)
		}
		return "", err
	} else if !st.IsDir() {
		return "", fmt.Errorf("kdd root is not a directory: %s", root)
	}
	return root, nil
}

func Discover(root string) ([]Item, []Item, error) {
	var items []Item
	var skipped []Item
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
		if isCategoryREADME(rel) {
			skipped = append(skipped, Item{SourcePath: rel, SkipReason: "category README is directory guidance and is skipped by default"})
			return nil
		}
		item, ok := MapPath(rel)
		if !ok {
			skipped = append(skipped, Item{SourcePath: rel, SkipReason: "outside supported KDD knowledge paths"})
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
	return items, skipped, err
}

func MapPath(rel string) (Item, bool) {
	switch rel {
	case "project/README.md":
		return newItem(rel, "project", "project", "project.md", candidate.OperationMerge, "KDD Project README", "Imported KDD project README."), true
	case "project/active-knowledge-log.md":
		return newItem(rel, "project", model.CandidateTypeMigrationSource, ProjectActiveLogTarget, candidate.OperationReplace, "KDD Project Active Knowledge Log", "Pending Verification: imported from KDD active knowledge log as migration source. Distill durable semantic candidates before review."), true
	case "local/active-knowledge-log.md":
		return newItem(rel, "user", model.CandidateTypeMigrationSource, LocalActiveLogTarget, candidate.OperationReplace, "KDD Local Active Knowledge Log", "Pending Verification: imported from local KDD active knowledge log as user-scope migration source."), true
	}
	if strings.HasPrefix(rel, "local/") {
		return mapLocalPath(rel), true
	}
	if !strings.HasPrefix(rel, "project/") {
		return Item{}, false
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 3 {
		return Item{}, false
	}
	category := parts[1]
	slug := util.Slug(strings.TrimSuffix(strings.Join(parts[2:], "-"), ".md"))
	switch category {
	case "architecture":
		return newItem(rel, "project", "architecture", filepath.ToSlash(filepath.Join("architecture", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported KDD architecture knowledge."), true
	case "decisions":
		return newItem(rel, "project", "decision", filepath.ToSlash(filepath.Join("decisions", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported KDD decision knowledge."), true
	case "runbooks":
		return newItem(rel, "project", "workflow", filepath.ToSlash(filepath.Join("workflows", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported KDD runbook as Worktrail workflow."), true
	case "integrations":
		return newItem(rel, "project", "integration", filepath.ToSlash(filepath.Join("integrations", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported KDD integration knowledge."), true
	case "validation":
		return newItem(rel, "project", "validation", filepath.ToSlash(filepath.Join("validation", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported KDD validation knowledge."), true
	case "glossary":
		return newItem(rel, "project", "glossary", filepath.ToSlash(filepath.Join("glossary", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported KDD glossary knowledge."), true
	default:
		item := newItem(rel, "project", model.CandidateTypeMigrationSource, filepath.ToSlash(filepath.Join("imports", "kdd", "project", slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Pending Verification: unsupported KDD project category imported as migration source and needs classification.")
		item.Warnings = append(item.Warnings, "needs_classification")
		return item, true
	}
}

func CandidateBody(item Item) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", item.Title)
	fmt.Fprintf(&b, "Imported from KDD relative path: `%s`.\n\n", item.SourcePath)
	if item.Summary != "" {
		fmt.Fprintf(&b, "Import note: %s\n\n", item.Summary)
	}
	if len(item.Warnings) > 0 {
		fmt.Fprintf(&b, "Warnings: `%s`.\n\n", strings.Join(item.Warnings, "`, `"))
	}
	b.WriteString(strings.TrimSpace(item.Body))
	b.WriteByte('\n')
	return b.String()
}

func WritePack(path string, items []Item) error {
	var b strings.Builder
	b.WriteString("# Worktrail KDD Migration Distillation Pack\n\n")
	b.WriteString("Distill migration_source evidence into semantic Worktrail candidates. Do not promote, merge, discard, or write formal knowledge from this pack.\n")
	for _, item := range items {
		if item.SkipReason != "" || item.CandidateType != model.CandidateTypeMigrationSource || item.RedactionStatus == string(redact.StatusBlocked) {
			continue
		}
		scan := redact.Scan(item.Body)
		if scan.Status == redact.StatusBlocked {
			continue
		}
		fmt.Fprintf(&b, "\n## Migration Source `%s`\n\n", item.CandidateID)
		fmt.Fprintf(&b, "- Scope: `%s`\n- Target: `%s`\n- Source path: `%s`\n\n", item.Scope, item.TargetPath, item.SourcePath)
		b.WriteString("### Source Evidence\n\n")
		b.WriteString(strings.TrimSpace(scan.Text))
		b.WriteString("\n")
	}
	return util.AtomicWrite(path, []byte(b.String()), 0o600)
}

func NextSteps(dryRun bool) []string {
	if dryRun {
		return []string{"run `worktrail migrate kdd --write-candidates` to create pending migration candidates"}
	}
	return []string{
		"run `worktrail distill --pending --split-sources --scope project` and `--scope user` as needed",
		"run `worktrail review` to inspect semantic candidates before promote, merge, or discard",
		"run `worktrail doctor migration` before cleanup, then `worktrail migrate kdd --cleanup-legacy --confirm` after review gates are clear",
	}
}

func GitGuidance() []string {
	return []string{
		".worktrail/raw, .worktrail/index, .worktrail/logs, .worktrail/state, and .worktrail/candidates are runtime/import artifacts unless your team explicitly tracks them",
		".worktrail/rules, .worktrail/decisions, .worktrail/prompts, .worktrail/workflows, .worktrail/integrations, .worktrail/validation, .worktrail/glossary, and project .gitignore changes are the usual review targets after promotion",
	}
}

func LegacyRoot(env paths.Env, root string) string {
	if strings.TrimSpace(root) == "" {
		root = filepath.Join(env.ProjectRoot, DefaultLegacyRoot)
	}
	if !filepath.IsAbs(root) {
		root = filepath.Join(env.ProjectRoot, root)
	}
	root, _ = filepath.Abs(root)
	return root
}

func HasLocalAbsolutePath(text string) bool {
	return localPathRE.MatchString(text)
}

var localPathRE = regexp.MustCompile(`(?:/Users/[^\s` + "`" + `]+|/home/[^\s` + "`" + `]+|[A-Za-z]:\\Users\\[^\s` + "`" + `]+)`)

func mapLocalPath(rel string) Item {
	slug := util.Slug(strings.TrimSuffix(strings.TrimPrefix(rel, "local/"), ".md"))
	if slug == "" {
		slug = "local-note"
	}
	item := newItem(rel, "user", "lesson", filepath.ToSlash(filepath.Join("lessons", "kdd-local-"+slug+".md")), candidate.OperationReplace, titleFromPath(rel), "Imported local KDD note as user-scope pending knowledge.")
	item.Warnings = append(item.Warnings, "local_scope_only")
	return item
}

func newItem(rel, scope, typ, target, op, title, summary string) Item {
	return Item{
		SourcePath:    rel,
		Scope:         scope,
		CandidateID:   CandidateID(rel),
		CandidateType: typ,
		TargetPath:    target,
		Operation:     op,
		Title:         title,
		Summary:       summary,
	}
}

func CandidateID(rel string) string {
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

func titleFromPath(rel string) string {
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

func isCategoryREADME(rel string) bool {
	if strings.HasSuffix(rel, "/README.md") {
		return rel != "project/README.md"
	}
	return rel == "README.md"
}

func appendLocalPathWarning(warnings []string, body string) []string {
	if HasLocalAbsolutePath(body) {
		return append(warnings, "local_path_detected")
	}
	return warnings
}

func tagsFor(item Item) []string {
	tags := []string{"kdd-migration"}
	if item.CandidateType == model.CandidateTypeMigrationSource {
		tags = append(tags, "kdd", "migration_source")
	}
	for _, warning := range item.Warnings {
		tags = append(tags, warning)
	}
	return tags
}

func actor(value string) string {
	if strings.TrimSpace(value) == "" {
		return "cli:migrate-kdd"
	}
	return value
}

func isDuplicateCandidateError(err error) bool {
	return strings.Contains(err.Error(), "candidate") && strings.Contains(err.Error(), "already exists")
}

func MarshalReport(report Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}
