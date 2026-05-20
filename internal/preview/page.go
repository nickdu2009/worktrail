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
	Title    string
	Source   Source
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
    header {
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
    article {
      padding: 32px;
      background: var(--panel);
      border: 1px solid var(--border);
      border-radius: 16px;
    }
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
    <header>
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
    <article>
      {{ .Content }}
    </article>
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
