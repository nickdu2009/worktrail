package preview

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"

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

	var rendered bytes.Buffer
	md := goldmark.New(goldmark.WithExtensions(extension.GFM))
	if err := md.Convert([]byte(src.Body), &rendered); err != nil {
		return RenderResult{}, err
	}

	title := src.Title
	if title == "" {
		title = "Untitled Worktrail Document"
	}
	var page bytes.Buffer
	if err := pageTemplate.Execute(&page, pageData{
		Title:    title,
		Source:   src,
		Metadata: sortedMetadata(src.Metadata),
		Content:  template.HTML(rendered.String()),
	}); err != nil {
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
