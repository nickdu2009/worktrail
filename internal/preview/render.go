package preview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"html/template"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/nickdu2009/worktrail/internal/util"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func Render(src Source, outDir string) (RenderResult, error) {
	if err := ensureDir(outDir); err != nil {
		return RenderResult{}, err
	}

	md := goldmark.New(goldmark.WithExtensions(extension.GFM))

	title := src.Title
	if title == "" {
		title = "Untitled Worktrail Preview"
	}
	data, err := buildPageData(md, src, title)
	if err != nil {
		return RenderResult{}, err
	}

	var page bytes.Buffer
	if err := pageTemplate.Execute(&page, data); err != nil {
		return RenderResult{}, err
	}
	indexPath := filepath.Join(outDir, renderFileName(src.Scope, page.Bytes()))
	if err := util.AtomicWrite(indexPath, page.Bytes(), 0o644); err != nil {
		return RenderResult{}, err
	}
	return RenderResult{
		Source:    src,
		HTML:      page.Bytes(),
		OutputDir: outDir,
		IndexPath: indexPath,
	}, nil
}

func buildPageData(md goldmark.Markdown, src Source, title string) (pageData, error) {
	data := pageData{
		Title:    title,
		Source:   src,
		Metadata: sortedMetadata(src.Metadata),
	}
	usedAnchors := map[string]int{}
	sectionsByKey := map[string]*sectionItem{}
	for _, child := range src.Children {
		rendered, err := renderMarkdown(md, child.Body)
		if err != nil {
			return pageData{}, err
		}
		key, sectionTitle := sectionForPath(child.Path)
		section, ok := sectionsByKey[key]
		if !ok {
			data.Sections = append(data.Sections, sectionItem{
				ID:    "section-" + key,
				Title: sectionTitle,
			})
			section = &data.Sections[len(data.Sections)-1]
			sectionsByKey[key] = section
		}
		section.Documents = append(section.Documents, documentItem{
			Title:    child.Title,
			Path:     child.Path,
			Anchor:   uniqueAnchor("doc-"+anchorFromPath(child.Path), usedAnchors),
			Metadata: sortedMetadata(child.Metadata),
			Content:  rendered,
		})
	}
	sort.SliceStable(data.Sections, func(i, j int) bool {
		return sectionOrder(data.Sections[i].Title) < sectionOrder(data.Sections[j].Title)
	})
	for _, pending := range src.PendingCandidates {
		rendered, err := renderMarkdown(md, pending.Body)
		if err != nil {
			return pageData{}, err
		}
		data.PendingCandidates = append(data.PendingCandidates, candidateItem{
			ID:       pending.ID,
			Title:    pending.Title,
			Path:     pending.Path,
			Anchor:   uniqueAnchor("candidate-"+anchorFromPath(pending.ID), usedAnchors),
			Metadata: sortedMetadata(pending.Metadata),
			Content:  rendered,
		})
	}
	return data, nil
}

func renderMarkdown(md goldmark.Markdown, body string) (template.HTML, error) {
	var rendered bytes.Buffer
	if err := md.Convert([]byte(body), &rendered); err != nil {
		return "", err
	}
	return template.HTML(rendered.String()), nil
}

func renderFileName(scope string, html []byte) string {
	sum := sha256.Sum256(html)
	return scope + "-" + hex.EncodeToString(sum[:8]) + ".html"
}

func anchorFromPath(path string) string {
	var b strings.Builder
	for _, r := range path {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
		case r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return strings.TrimRight(b.String(), "-")
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

func uniqueAnchor(base string, used map[string]int) string {
	count := used[base]
	used[base] = count + 1
	if count == 0 {
		return base
	}
	return base + "-" + strconv.Itoa(count+1)
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
