package preview

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

func Render(src Source, outDir string) (RenderResult, error) {
	temporary := false
	if outDir == "" {
		var err error
		outDir, err = os.MkdirTemp("", "worktrail-preview-*")
		if err != nil {
			return RenderResult{}, err
		}
		temporary = true
	} else if err := os.MkdirAll(outDir, 0o755); err != nil {
		return RenderResult{}, err
	}

	md := goldmark.New(goldmark.WithExtensions(extension.GFM))

	title := src.Title
	if title == "" {
		title = "Untitled Worktrail Document"
	}
	var page bytes.Buffer
	data := pageData{
		Title:    title,
		Source:   src,
		Metadata: sortedMetadata(src.Metadata),
	}
	if src.Kind == SourceDirectory {
		data.IsDirectory = true
		for _, child := range src.Children {
			rendered, err := renderMarkdown(md, child.Body)
			if err != nil {
				return RenderResult{}, err
			}
			data.Documents = append(data.Documents, documentItem{
				Title:    child.Title,
				Path:     child.Path,
				Anchor:   anchorFromPath(child.Path),
				Depth:    strings.Count(child.Path, "/"),
				Metadata: sortedMetadata(child.Metadata),
				Content:  rendered,
			})
		}
	} else {
		rendered, err := renderMarkdown(md, src.Body)
		if err != nil {
			return RenderResult{}, err
		}
		data.Content = rendered
	}
	if err := pageTemplate.Execute(&page, data); err != nil {
		return RenderResult{}, err
	}

	indexPath := filepath.Join(outDir, "index.html")
	if err := os.WriteFile(indexPath, page.Bytes(), 0o644); err != nil {
		return RenderResult{}, err
	}
	return RenderResult{
		Source:    src,
		HTML:      page.Bytes(),
		OutputDir: outDir,
		IndexPath: indexPath,
		Temporary: temporary,
	}, nil
}

func renderMarkdown(md goldmark.Markdown, body string) (template.HTML, error) {
	var rendered bytes.Buffer
	if err := md.Convert([]byte(body), &rendered); err != nil {
		return "", err
	}
	return template.HTML(rendered.String()), nil
}

func anchorFromPath(path string) string {
	var b strings.Builder
	b.WriteString("doc-")
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
