package preview

import (
	"html/template"
	"sort"
)

type metadataItem struct {
	Key   string
	Value string
}

type pageData struct {
	Title       string
	Source      Source
	Metadata    []metadataItem
	Content     template.HTML
	IsDirectory bool
	Documents   []documentItem
}

type documentItem struct {
	Title    string
	Path     string
	Anchor   string
	Depth    int
	Metadata []metadataItem
	Content  template.HTML
}

var pageTemplate = template.Must(template.New("preview").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .Title }} - Worktrail Preview</title>
  <style>
    :root {
      color-scheme: light dark;
      --bg: #f8fafc;
      --fg: #0f172a;
      --muted: #64748b;
      --panel: #ffffff;
      --border: #e2e8f0;
      --code: #f1f5f9;
      --accent: #2563eb;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #020617;
        --fg: #e2e8f0;
        --muted: #94a3b8;
        --panel: #0f172a;
        --border: #1e293b;
        --code: #111827;
        --accent: #60a5fa;
      }
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--fg);
      font: 16px/1.65 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main {
      max-width: 980px;
      margin: 0 auto;
      padding: 40px 24px 64px;
    }
    header.hero {
      margin-bottom: 24px;
      padding: 24px;
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
    }
    .eyebrow {
      margin: 0 0 4px;
      color: var(--accent);
      font-size: 13px;
      font-weight: 700;
      letter-spacing: .08em;
      text-transform: uppercase;
    }
    h1 {
      margin: 0 0 12px;
      font-size: clamp(2rem, 4vw, 3rem);
      line-height: 1.1;
    }
    dl {
      display: grid;
      grid-template-columns: max-content 1fr;
      gap: 6px 16px;
      margin: 16px 0 0;
      color: var(--muted);
      font-size: 14px;
    }
    dt { font-weight: 700; color: var(--fg); }
    dd { margin: 0; overflow-wrap: anywhere; }
    .directory,
    article {
      padding: 32px;
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
    }
    .directory { margin-bottom: 24px; }
    .directory h2 { margin: 0 0 12px; }
    .directory ol {
      margin: 0;
      padding: 0;
      list-style: none;
    }
    .directory li {
      margin: 4px 0;
      padding-left: calc(var(--depth) * 18px);
    }
    .directory a {
      display: inline-block;
      padding: 3px 0;
      text-decoration: none;
    }
    .directory a:hover { text-decoration: underline; }
    .path,
    .document-path {
      color: var(--muted);
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: 13px;
    }
    .document { margin-top: 24px; }
    .document-header {
      margin: 0 0 20px;
      padding: 0 0 16px;
      border-bottom: 1px solid var(--border);
      background: transparent;
      border-radius: 0;
    }
    .document-header h2 {
      margin: 4px 0 0;
      font-size: 1.6rem;
    }
    .document-path { margin: 0; }
    article > :first-child { margin-top: 0; }
    article > :last-child { margin-bottom: 0; }
    h2, h3, h4 { line-height: 1.25; margin-top: 1.8em; }
    a { color: var(--accent); }
    blockquote {
      margin: 1.5em 0;
      padding: .2em 1em;
      color: var(--muted);
      border-left: 4px solid var(--border);
    }
    code {
      padding: .15em .35em;
      border-radius: 6px;
      background: var(--code);
      font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
      font-size: .92em;
    }
    pre {
      overflow: auto;
      padding: 16px;
      border-radius: 12px;
      background: var(--code);
    }
    pre code { padding: 0; background: transparent; }
    table {
      width: 100%;
      border-collapse: collapse;
      margin: 1.5em 0;
      font-size: .95em;
    }
    th, td {
      padding: 8px 10px;
      border: 1px solid var(--border);
      text-align: left;
      vertical-align: top;
    }
    th { background: var(--code); }
    img { max-width: 100%; }
  </style>
</head>
<body>
  <main>
    <header class="hero">
      <p class="eyebrow">Worktrail Preview</p>
      <h1>{{ .Title }}</h1>
      <dl>
        <dt>scope</dt><dd>{{ .Source.Scope }}</dd>
        <dt>kind</dt><dd>{{ .Source.Kind }}</dd>
        <dt>source</dt><dd>{{ .Source.Path }}</dd>
        {{- range .Metadata }}
        <dt>{{ .Key }}</dt><dd>{{ .Value }}</dd>
        {{- end }}
      </dl>
    </header>
    {{- if .IsDirectory }}
    {{- if .Documents }}
    <nav class="directory" aria-label="Directory">
      <h2>Directory</h2>
      <ol>
        {{- range .Documents }}
        <li style="--depth: {{ .Depth }}"><a href="#{{ .Anchor }}"><span class="path">{{ .Path }}</span></a></li>
        {{- end }}
      </ol>
    </nav>
    {{- range .Documents }}
    <article id="{{ .Anchor }}" class="document">
      <header class="document-header">
        <p class="document-path">{{ .Path }}</p>
        <h2>{{ .Title }}</h2>
      </header>
      <div class="document-content">
        {{ .Content }}
      </div>
    </article>
    {{- end }}
    {{- else }}
    <article>
      <p>No Markdown files found in this directory.</p>
    </article>
    {{- end }}
    {{- else }}
    <article>
      {{ .Content }}
    </article>
    {{- end }}
  </main>
</body>
</html>
`))

func sortedMetadata(meta map[string]string) []metadataItem {
	items := make([]metadataItem, 0, len(meta))
	for key, value := range meta {
		if value != "" {
			items = append(items, metadataItem{Key: key, Value: value})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].Key < items[j].Key
	})
	return items
}
