package preview

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/util"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func Render(src Source, outDir string) (RenderResult, error) {
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	title := src.Title
	if title == "" {
		title = "Untitled Worktrail Preview"
	}
	site, err := buildSite(md, src, title)
	if err != nil {
		return RenderResult{}, err
	}
	stageDir, err := stageSiteDir(outDir)
	if err != nil {
		return RenderResult{}, err
	}
	published := false
	defer func() {
		if !published {
			_ = os.RemoveAll(stageDir)
		}
	}()
	entryHTML, err := writeSite(stageDir, site)
	if err != nil {
		return RenderResult{}, err
	}
	if err := publishSite(stageDir, outDir); err != nil {
		return RenderResult{}, err
	}
	published = true
	indexPath := filepath.Join(outDir, filepath.FromSlash(site.HomePath))
	return RenderResult{
		Source:    src,
		HTML:      entryHTML,
		OutputDir: outDir,
		IndexPath: indexPath,
	}, nil
}

func writeSite(root string, site site) ([]byte, error) {
	if err := writeSiteFile(root, path.Join("assets", "site.css"), []byte(siteCSS)); err != nil {
		return nil, err
	}
	entryHTML, err := renderPage(homeTemplate, buildHomePageData(site))
	if err != nil {
		return nil, err
	}
	if err := writeSiteFile(root, site.HomePath, entryHTML); err != nil {
		return nil, err
	}
	for _, section := range site.Sections {
		sectionHTML, err := renderPage(sectionTemplate, buildSectionPageData(site, section))
		if err != nil {
			return nil, err
		}
		if err := writeSiteFile(root, section.OutputPath, sectionHTML); err != nil {
			return nil, err
		}
		for _, doc := range section.Documents {
			docHTML, err := renderPage(documentTemplate, buildDocumentPageData(site, section, doc))
			if err != nil {
				return nil, err
			}
			if err := writeSiteFile(root, doc.OutputPath, docHTML); err != nil {
				return nil, err
			}
		}
	}
	candidatesHTML, err := renderPage(candidatesTemplate, buildCandidatesPageData(site))
	if err != nil {
		return nil, err
	}
	if err := writeSiteFile(root, site.CandidatesPath, candidatesHTML); err != nil {
		return nil, err
	}
	for _, bucket := range site.CandidateBuckets {
		for _, candidate := range bucket.Candidates {
			candidateHTML, err := renderPage(candidateTemplate, buildCandidatePageData(site, bucket, candidate))
			if err != nil {
				return nil, err
			}
			if err := writeSiteFile(root, candidate.OutputPath, candidateHTML); err != nil {
				return nil, err
			}
		}
	}
	return entryHTML, nil
}

func renderMarkdown(md goldmark.Markdown, body string) (template.HTML, error) {
	var rendered bytes.Buffer
	if err := md.Convert([]byte(body), &rendered); err != nil {
		return "", err
	}
	return template.HTML(rendered.String()), nil
}

func renderPage(tmpl *template.Template, data any) ([]byte, error) {
	var page bytes.Buffer
	if err := tmpl.Execute(&page, data); err != nil {
		return nil, err
	}
	return page.Bytes(), nil
}

func writeSiteFile(root, outputPath string, data []byte) error {
	target := filepath.Join(root, filepath.FromSlash(outputPath))
	return util.AtomicWrite(target, data, 0o644)
}

func stageSiteDir(outDir string) (string, error) {
	parent := filepath.Dir(outDir)
	if err := ensureDir(parent); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, ".preview-site-*")
}

func publishSite(stageDir, outDir string) error {
	return publishSiteWithOps(stageDir, outDir, os.Stat, os.Rename, os.RemoveAll)
}

func publishSiteWithOps(
	stageDir, outDir string,
	stat func(string) (os.FileInfo, error),
	rename func(string, string) error,
	removeAll func(string) error,
) error {
	if _, err := stat(outDir); err != nil {
		if os.IsNotExist(err) {
			return rename(stageDir, outDir)
		}
		return err
	}
	backupDir := outDir + ".bak"
	if err := removeAll(backupDir); err != nil {
		return err
	}
	if err := rename(outDir, backupDir); err != nil {
		return err
	}
	if err := rename(stageDir, outDir); err != nil {
		if rollbackErr := rename(backupDir, outDir); rollbackErr != nil {
			return fmt.Errorf("publish preview site: activate failed: %w; rollback failed: %v", err, rollbackErr)
		}
		return fmt.Errorf("publish preview site: activate failed: %w", err)
	}
	return removeAll(backupDir)
}

func sectionForPath(path string) (string, string) {
	first := path
	if idx := strings.Index(path, "/"); idx >= 0 {
		first = path[:idx]
	}
	if !strings.Contains(path, "/") {
		first = "overview"
	}
	switch first {
	case "overview":
		return "overview", "Overview"
	case "requirements":
		return "requirements", "Requirements"
	case "architecture":
		return "architecture", "Architecture"
	case "decisions":
		return "decisions", "Decisions"
	case "workflows":
		return "workflows", "Workflows"
	case "validation":
		return "validation", "Validation"
	case "handoffs":
		return "handoffs", "Handoffs"
	case "lessons":
		return "lessons", "Lessons"
	case "rules":
		return "rules", "Rules"
	case "prompts":
		return "prompts", "Prompts"
	case "glossary":
		return "glossary", "Glossary"
	case "integrations":
		return "integrations", "Integrations"
	case "profile":
		return "profile", "Profile"
	default:
		return first, prettySectionTitle(first)
	}
}

func sectionOrder(title string) int {
	order := map[string]int{
		"Overview":     0,
		"Requirements": 10,
		"Architecture": 20,
		"Decisions":    30,
		"Workflows":    40,
		"Validation":   50,
		"Rules":        60,
		"Prompts":      70,
		"Profile":      80,
		"Lessons":      90,
		"Glossary":     100,
		"Integrations": 110,
		"Handoffs":     120,
	}
	if value, ok := order[title]; ok {
		return value
	}
	return 1000
}

func prettySectionTitle(name string) string {
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	for i, part := range parts {
		if part == "" {
			continue
		}
		parts[i] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}
