package preview

import (
	"html/template"
	"path"
	"path/filepath"
	"sort"
)

type metadataItem struct {
	Key   string
	Value string
}

type navItem struct {
	Title   string
	Href    string
	Count   int
	Current bool
}

type linkItem struct {
	Title string
	Href  string
}

type homeSectionCard struct {
	Title     string
	Href      string
	Count     int
	Documents []linkItem
}

type homeCandidateBucketCard struct {
	ID          string
	Title       string
	Description string
	Href        string
	Count       int
	Candidates  []linkItem
}

type homePageData struct {
	PageTitle        string
	SiteTitle        string
	StylesheetHref   string
	Source           Source
	Metadata         []metadataItem
	DocumentCount    int
	PendingCount     int
	SectionNav       []navItem
	CandidatesHref   string
	Sections         []homeSectionCard
	CandidateBuckets []homeCandidateBucketCard
}

type sectionDocumentCard struct {
	Title   string
	Path    string
	Href    string
	Summary string
}

type sectionPageData struct {
	PageTitle      string
	SiteTitle      string
	StylesheetHref string
	SectionNav     []navItem
	CandidatesHref string
	HomeHref       string
	Title          string
	Count          int
	Documents      []sectionDocumentCard
}

type documentPageData struct {
	PageTitle      string
	SiteTitle      string
	StylesheetHref string
	SectionNav     []navItem
	CandidatesHref string
	HomeHref       string
	SectionHref    string
	SectionTitle   string
	Title          string
	Path           string
	Metadata       []metadataItem
	Content        template.HTML
}

type candidateLinkCard struct {
	Title   string
	Href    string
	Type    string
	Status  string
	Summary string
}

type candidateBucketCard struct {
	ID          string
	Title       string
	Description string
	Href        string
	Count       int
	Candidates  []candidateLinkCard
}

type candidatesPageData struct {
	PageTitle      string
	SiteTitle      string
	StylesheetHref string
	SectionNav     []navItem
	CandidatesHref string
	HomeHref       string
	Total          int
	Buckets        []candidateBucketCard
}

type candidatePageData struct {
	PageTitle      string
	SiteTitle      string
	StylesheetHref string
	SectionNav     []navItem
	CandidatesHref string
	HomeHref       string
	BucketHref     string
	BucketTitle    string
	Title          string
	Path           string
	Metadata       []metadataItem
	Content        template.HTML
}

func buildHomePageData(site site) homePageData {
	data := homePageData{
		PageTitle:        site.Title + " - Worktrail Preview",
		SiteTitle:        site.Title,
		StylesheetHref:   relativeHref(site.HomePath, path.Join("assets", "site.css")),
		Source:           site.Source,
		Metadata:         site.Metadata,
		DocumentCount:    site.DocumentCount,
		PendingCount:     site.PendingCount,
		SectionNav:       buildSectionNav(site, site.HomePath, ""),
		CandidatesHref:   relativeHref(site.HomePath, site.CandidatesPath),
		CandidateBuckets: make([]homeCandidateBucketCard, 0, len(site.CandidateBuckets)),
	}
	for _, section := range site.Sections {
		card := homeSectionCard{
			Title: section.Title,
			Href:  relativeHref(site.HomePath, section.OutputPath),
			Count: len(section.Documents),
		}
		for _, doc := range section.Documents {
			if len(card.Documents) >= 3 {
				break
			}
			card.Documents = append(card.Documents, linkItem{
				Title: doc.Title,
				Href:  relativeHref(site.HomePath, doc.OutputPath),
			})
		}
		data.Sections = append(data.Sections, card)
	}
	for _, bucket := range site.CandidateBuckets {
		card := homeCandidateBucketCard{
			ID:          bucket.Key,
			Title:       bucket.Title,
			Description: bucket.Description,
			Href:        relativeHref(site.HomePath, site.CandidatesPath) + "#bucket-" + bucket.Key,
			Count:       len(bucket.Candidates),
		}
		for _, candidate := range bucket.Candidates {
			if len(card.Candidates) >= 3 {
				break
			}
			card.Candidates = append(card.Candidates, linkItem{
				Title: candidate.Title,
				Href:  relativeHref(site.HomePath, candidate.OutputPath),
			})
		}
		data.CandidateBuckets = append(data.CandidateBuckets, card)
	}
	return data
}

func buildSectionPageData(site site, section siteSection) sectionPageData {
	data := sectionPageData{
		PageTitle:      section.Title + " - " + site.Title,
		SiteTitle:      site.Title,
		StylesheetHref: relativeHref(section.OutputPath, path.Join("assets", "site.css")),
		SectionNav:     buildSectionNav(site, section.OutputPath, section.Key),
		CandidatesHref: relativeHref(section.OutputPath, site.CandidatesPath),
		HomeHref:       relativeHref(section.OutputPath, site.HomePath),
		Title:          section.Title,
		Count:          len(section.Documents),
		Documents:      make([]sectionDocumentCard, 0, len(section.Documents)),
	}
	for _, doc := range section.Documents {
		data.Documents = append(data.Documents, sectionDocumentCard{
			Title:   doc.Title,
			Path:    doc.Path,
			Href:    relativeHref(section.OutputPath, doc.OutputPath),
			Summary: doc.Summary,
		})
	}
	return data
}

func buildDocumentPageData(site site, section siteSection, doc siteDocument) documentPageData {
	return documentPageData{
		PageTitle:      doc.Title + " - " + site.Title,
		SiteTitle:      site.Title,
		StylesheetHref: relativeHref(doc.OutputPath, path.Join("assets", "site.css")),
		SectionNav:     buildSectionNav(site, doc.OutputPath, section.Key),
		CandidatesHref: relativeHref(doc.OutputPath, site.CandidatesPath),
		HomeHref:       relativeHref(doc.OutputPath, site.HomePath),
		SectionHref:    relativeHref(doc.OutputPath, section.OutputPath),
		SectionTitle:   section.Title,
		Title:          doc.Title,
		Path:           doc.Path,
		Metadata:       doc.Metadata,
		Content:        doc.Content,
	}
}

func buildCandidatesPageData(site site) candidatesPageData {
	data := candidatesPageData{
		PageTitle:      "Pending Candidates - " + site.Title,
		SiteTitle:      site.Title,
		StylesheetHref: relativeHref(site.CandidatesPath, path.Join("assets", "site.css")),
		SectionNav:     buildSectionNav(site, site.CandidatesPath, ""),
		CandidatesHref: relativeHref(site.CandidatesPath, site.CandidatesPath),
		HomeHref:       relativeHref(site.CandidatesPath, site.HomePath),
		Total:          site.PendingCount,
		Buckets:        make([]candidateBucketCard, 0, len(site.CandidateBuckets)),
	}
	for _, bucket := range site.CandidateBuckets {
		card := candidateBucketCard{
			ID:          bucket.Key,
			Title:       bucket.Title,
			Description: bucket.Description,
			Href:        "#bucket-" + bucket.Key,
			Count:       len(bucket.Candidates),
			Candidates:  make([]candidateLinkCard, 0, len(bucket.Candidates)),
		}
		for _, candidate := range bucket.Candidates {
			card.Candidates = append(card.Candidates, candidateLinkCard{
				Title:   candidate.Title,
				Href:    relativeHref(site.CandidatesPath, candidate.OutputPath),
				Type:    candidate.Type,
				Status:  candidate.Status,
				Summary: candidate.Summary,
			})
		}
		data.Buckets = append(data.Buckets, card)
	}
	return data
}

func buildCandidatePageData(site site, bucket siteCandidateBucket, candidate siteCandidate) candidatePageData {
	return candidatePageData{
		PageTitle:      candidate.Title + " - " + site.Title,
		SiteTitle:      site.Title,
		StylesheetHref: relativeHref(candidate.OutputPath, path.Join("assets", "site.css")),
		SectionNav:     buildSectionNav(site, candidate.OutputPath, ""),
		CandidatesHref: relativeHref(candidate.OutputPath, site.CandidatesPath),
		HomeHref:       relativeHref(candidate.OutputPath, site.HomePath),
		BucketHref:     relativeHref(candidate.OutputPath, site.CandidatesPath) + "#bucket-" + bucket.Key,
		BucketTitle:    bucket.Title,
		Title:          candidate.Title,
		Path:           candidate.Path,
		Metadata:       candidate.Metadata,
		Content:        candidate.Content,
	}
}

func buildSectionNav(site site, fromPath, currentKey string) []navItem {
	items := make([]navItem, 0, len(site.Sections))
	for _, section := range site.Sections {
		items = append(items, navItem{
			Title:   section.Title,
			Href:    relativeHref(fromPath, section.OutputPath),
			Count:   len(section.Documents),
			Current: section.Key == currentKey,
		})
	}
	return items
}

func relativeHref(fromPath, toPath string) string {
	rel, err := filepath.Rel(filepath.FromSlash(path.Dir(fromPath)), filepath.FromSlash(toPath))
	if err != nil {
		return toPath
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return path.Base(toPath)
	}
	return rel
}

var homeTemplate = template.Must(template.New("preview-home").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .PageTitle }}</title>
  <link rel="stylesheet" href="{{ .StylesheetHref }}">
</head>
<body>
  <div class="layout">
    <aside class="sidebar" aria-label="Preview navigation">
      <h2>Sections</h2>
      <ul class="nav-list">
        {{- range .SectionNav }}
        <li><a class="nav-link{{ if .Current }} current{{ end }}" href="{{ .Href }}">{{ .Title }} <span>{{ .Count }}</span></a></li>
        {{- end }}
      </ul>
      <h2>Pending</h2>
      <a class="nav-link" href="{{ .CandidatesHref }}">Pending Candidates <span>{{ .PendingCount }}</span></a>
    </aside>
    <main class="content">
      <header class="hero">
        <p class="eyebrow">Worktrail Preview</p>
        <h1>{{ .SiteTitle }}</h1>
        <p class="lede">Browse the current knowledge scope without loading every document body into one page.</p>
        <div class="stats">
          <div class="stat"><strong>{{ .DocumentCount }}</strong><span>documents</span></div>
          <div class="stat"><strong>{{ .PendingCount }}</strong><span>pending candidates</span></div>
          <div class="stat"><strong>{{ len .SectionNav }}</strong><span>sections</span></div>
        </div>
        <dl>
          <dt>scope</dt><dd>{{ .Source.Scope }}</dd>
          <dt>kind</dt><dd>{{ .Source.Kind }}</dd>
          <dt>source</dt><dd>{{ .Source.Path }}</dd>
          {{- range .Metadata }}
          <dt>{{ .Key }}</dt><dd>{{ .Value }}</dd>
          {{- end }}
        </dl>
      </header>

      <section class="panel">
        <div class="panel-header">
          <h2>Knowledge Sections</h2>
          <p>Open a section to read summaries first, then drill into full documents.</p>
        </div>
        {{- if .Sections }}
        <div class="card-grid">
          {{- range .Sections }}
          <article class="card">
            <p class="card-eyebrow">{{ .Count }} documents</p>
            <h3><a href="{{ .Href }}">{{ .Title }}</a></h3>
            {{- if .Documents }}
            <ul class="link-list">
              {{- range .Documents }}
              <li><a href="{{ .Href }}">{{ .Title }}</a></li>
              {{- end }}
            </ul>
            {{- else }}
            <p class="muted">No documents in this section yet.</p>
            {{- end }}
          </article>
          {{- end }}
        </div>
        {{- else }}
        <p class="empty">No formal Worktrail documents were found for this scope.</p>
        {{- end }}
      </section>

      <section class="panel">
        <div class="panel-header">
          <h2>Pending Candidates</h2>
          <p>Pending candidates are grouped by intent. Operational items remain visible here for inspection but stay hidden from the default review and context inbox.</p>
        </div>
        {{- if .CandidateBuckets }}
        <div class="card-grid">
          {{- range .CandidateBuckets }}
          <article class="card">
            <p class="card-eyebrow">{{ .Count }} items</p>
            <h3><a href="{{ .Href }}">{{ .Title }}</a></h3>
            <p class="muted">{{ .Description }}</p>
            {{- if .Candidates }}
            <ul class="link-list">
              {{- range .Candidates }}
              <li><a href="{{ .Href }}">{{ .Title }}</a></li>
              {{- end }}
            </ul>
            {{- end }}
          </article>
          {{- end }}
        </div>
        {{- else }}
        <p class="empty">No pending candidates for this scope.</p>
        {{- end }}
      </section>
    </main>
  </div>
</body>
</html>
`))

var sectionTemplate = template.Must(template.New("preview-section").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .PageTitle }}</title>
  <link rel="stylesheet" href="{{ .StylesheetHref }}">
</head>
<body>
  <div class="layout">
    <aside class="sidebar" aria-label="Preview navigation">
      <h2>Sections</h2>
      <ul class="nav-list">
        {{- range .SectionNav }}
        <li><a class="nav-link{{ if .Current }} current{{ end }}" href="{{ .Href }}">{{ .Title }} <span>{{ .Count }}</span></a></li>
        {{- end }}
      </ul>
      <h2>Pending</h2>
      <a class="nav-link" href="{{ .CandidatesHref }}">Pending Candidates</a>
      <a class="nav-link secondary" href="{{ .HomeHref }}">Back to overview</a>
    </aside>
    <main class="content">
      <header class="hero compact">
        <p class="eyebrow">Section</p>
        <h1>{{ .Title }}</h1>
        <p class="lede">{{ .Count }} documents in this section.</p>
      </header>
      {{- if .Documents }}
      <section class="panel">
        <div class="list-stack">
          {{- range .Documents }}
          <article class="list-card">
            <p class="path">{{ .Path }}</p>
            <h2><a href="{{ .Href }}">{{ .Title }}</a></h2>
            <p class="muted">{{ .Summary }}</p>
          </article>
          {{- end }}
        </div>
      </section>
      {{- else }}
      <section class="panel">
        <p class="empty">No documents were found in this section.</p>
      </section>
      {{- end }}
    </main>
  </div>
</body>
</html>
`))

var documentTemplate = template.Must(template.New("preview-document").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .PageTitle }}</title>
  <link rel="stylesheet" href="{{ .StylesheetHref }}">
</head>
<body>
  <div class="layout">
    <aside class="sidebar" aria-label="Preview navigation">
      <h2>Sections</h2>
      <ul class="nav-list">
        {{- range .SectionNav }}
        <li><a class="nav-link{{ if .Current }} current{{ end }}" href="{{ .Href }}">{{ .Title }} <span>{{ .Count }}</span></a></li>
        {{- end }}
      </ul>
      <h2>Pending</h2>
      <a class="nav-link" href="{{ .CandidatesHref }}">Pending Candidates</a>
      <a class="nav-link secondary" href="{{ .HomeHref }}">Back to overview</a>
    </aside>
    <main class="content">
      <header class="hero compact">
        <p class="eyebrow">Document</p>
        <h1>{{ .Title }}</h1>
        <p class="path">{{ .Path }}</p>
        <p class="crumbs"><a href="{{ .HomeHref }}">Overview</a> / <a href="{{ .SectionHref }}">{{ .SectionTitle }}</a></p>
        {{- if .Metadata }}
        <dl>
          {{- range .Metadata }}
          <dt>{{ .Key }}</dt><dd>{{ .Value }}</dd>
          {{- end }}
        </dl>
        {{- end }}
      </header>
      <article class="panel prose">
        {{ .Content }}
      </article>
    </main>
  </div>
</body>
</html>
`))

var candidatesTemplate = template.Must(template.New("preview-candidates").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .PageTitle }}</title>
  <link rel="stylesheet" href="{{ .StylesheetHref }}">
</head>
<body>
  <div class="layout">
    <aside class="sidebar" aria-label="Preview navigation">
      <h2>Sections</h2>
      <ul class="nav-list">
        {{- range .SectionNav }}
        <li><a class="nav-link{{ if .Current }} current{{ end }}" href="{{ .Href }}">{{ .Title }} <span>{{ .Count }}</span></a></li>
        {{- end }}
      </ul>
      <h2>Pending Buckets</h2>
      <ul class="nav-list">
        {{- range .Buckets }}
        <li><a class="nav-link" href="{{ .Href }}">{{ .Title }} <span>{{ .Count }}</span></a></li>
        {{- end }}
      </ul>
      <a class="nav-link secondary" href="{{ .HomeHref }}">Back to overview</a>
    </aside>
    <main class="content">
      <header class="hero compact">
        <p class="eyebrow">Pending Candidates</p>
        <h1>Pending Candidates</h1>
        <p class="lede">{{ .Total }} pending candidates grouped by intent.</p>
      </header>
      {{- if .Buckets }}
      {{- range .Buckets }}
      <section id="bucket-{{ .ID }}" class="panel">
        <div class="panel-header">
          <h2>{{ .Title }}</h2>
          <p>{{ .Description }}</p>
        </div>
        {{- if .Candidates }}
        <div class="list-stack">
          {{- range .Candidates }}
          <article class="list-card">
            <p class="meta-inline"><span>{{ .Type }}</span><span>{{ .Status }}</span></p>
            <h3><a href="{{ .Href }}">{{ .Title }}</a></h3>
            <p class="muted">{{ .Summary }}</p>
          </article>
          {{- end }}
        </div>
        {{- else }}
        <p class="empty">No candidates in this bucket.</p>
        {{- end }}
      </section>
      {{- end }}
      {{- else }}
      <section class="panel">
        <p class="empty">No pending candidates for this scope.</p>
      </section>
      {{- end }}
    </main>
  </div>
</body>
</html>
`))

var candidateTemplate = template.Must(template.New("preview-candidate").Parse(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{ .PageTitle }}</title>
  <link rel="stylesheet" href="{{ .StylesheetHref }}">
</head>
<body>
  <div class="layout">
    <aside class="sidebar" aria-label="Preview navigation">
      <h2>Sections</h2>
      <ul class="nav-list">
        {{- range .SectionNav }}
        <li><a class="nav-link{{ if .Current }} current{{ end }}" href="{{ .Href }}">{{ .Title }} <span>{{ .Count }}</span></a></li>
        {{- end }}
      </ul>
      <h2>Pending</h2>
      <a class="nav-link current" href="{{ .CandidatesHref }}">Pending Candidates</a>
      <a class="nav-link secondary" href="{{ .HomeHref }}">Back to overview</a>
    </aside>
    <main class="content">
      <header class="hero compact">
        <p class="eyebrow">Candidate</p>
        <h1>{{ .Title }}</h1>
        <p class="path">{{ .Path }}</p>
        <p class="crumbs"><a href="{{ .HomeHref }}">Overview</a> / <a href="{{ .CandidatesHref }}">Pending Candidates</a> / <a href="{{ .BucketHref }}">{{ .BucketTitle }}</a></p>
        {{- if .Metadata }}
        <dl>
          {{- range .Metadata }}
          <dt>{{ .Key }}</dt><dd>{{ .Value }}</dd>
          {{- end }}
        </dl>
        {{- end }}
      </header>
      <article class="panel prose">
        {{ .Content }}
      </article>
    </main>
  </div>
</body>
</html>
`))

const siteCSS = `:root {
  color-scheme: light dark;
  --bg: #f8fafc;
  --fg: #0f172a;
  --muted: #64748b;
  --panel: #ffffff;
  --border: #e2e8f0;
  --code: #f1f5f9;
  --accent: #2563eb;
  --accent-soft: rgba(37, 99, 235, 0.12);
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
    --accent-soft: rgba(96, 165, 250, 0.14);
  }
}
* { box-sizing: border-box; }
body {
  margin: 0;
  background: var(--bg);
  color: var(--fg);
  font: 16px/1.7 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
a { color: var(--accent); }
.layout {
  max-width: 1320px;
  margin: 0 auto;
  padding: 32px 24px 64px;
  display: grid;
  grid-template-columns: minmax(240px, 300px) minmax(0, 1fr);
  gap: 24px;
  align-items: start;
}
@media (max-width: 960px) {
  .layout { grid-template-columns: 1fr; }
  .sidebar { position: static; }
}
.sidebar {
  position: sticky;
  top: 24px;
  padding: 24px;
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 18px;
  max-height: calc(100vh - 48px);
  overflow: auto;
}
.sidebar h2 {
  margin: 0 0 12px;
  font-size: 0.98rem;
}
.sidebar h2 + .nav-list,
.sidebar .nav-link + .nav-link,
.sidebar .nav-list + h2 {
  margin-bottom: 20px;
}
.nav-list {
  list-style: none;
  padding: 0;
  margin: 0;
}
.nav-list li + li {
  margin-top: 8px;
}
.nav-link {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 9px 12px;
  color: inherit;
  text-decoration: none;
  border-radius: 10px;
}
.nav-link:hover,
.nav-link.current {
  background: var(--accent-soft);
}
.nav-link span {
  color: var(--muted);
  font-size: 13px;
}
.nav-link.secondary {
  margin-top: 8px;
  border: 1px solid var(--border);
}
.content {
  min-width: 0;
}
.hero,
.panel,
.card,
.list-card {
  background: var(--panel);
  border: 1px solid var(--border);
  border-radius: 18px;
}
.hero {
  padding: 28px;
  margin-bottom: 24px;
}
.hero.compact {
  margin-bottom: 20px;
}
.eyebrow {
  margin: 0 0 6px;
  color: var(--accent);
  font-size: 13px;
  font-weight: 700;
  letter-spacing: 0.08em;
  text-transform: uppercase;
}
h1 {
  margin: 0 0 10px;
  font-size: clamp(2rem, 4vw, 3rem);
  line-height: 1.08;
}
.lede {
  margin: 0;
  max-width: 70ch;
  color: var(--muted);
}
.stats {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(140px, 1fr));
  gap: 12px;
  margin: 20px 0;
}
.stat {
  padding: 14px 16px;
  border: 1px solid var(--border);
  border-radius: 14px;
  background: color-mix(in srgb, var(--panel) 88%, var(--code));
}
.stat strong {
  display: block;
  font-size: 1.4rem;
}
.stat span {
  color: var(--muted);
  font-size: 14px;
}
dl {
  display: grid;
  grid-template-columns: max-content 1fr;
  gap: 6px 14px;
  margin: 18px 0 0;
  color: var(--muted);
  font-size: 14px;
}
dt {
  font-weight: 700;
  color: var(--fg);
}
dd {
  margin: 0;
  overflow-wrap: anywhere;
}
.panel {
  padding: 24px;
}
.panel + .panel {
  margin-top: 24px;
}
.panel-header {
  margin-bottom: 18px;
}
.panel-header h2,
.list-card h2,
.list-card h3,
.card h3 {
  margin: 0 0 8px;
  line-height: 1.25;
}
.panel-header p,
.muted,
.empty {
  color: var(--muted);
}
.card-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(250px, 1fr));
  gap: 16px;
}
.card {
  padding: 20px;
}
.card-eyebrow,
.path,
.meta-inline {
  margin: 0 0 8px;
  color: var(--muted);
  font-size: 13px;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}
.meta-inline span + span::before {
  content: " • ";
}
.link-list {
  margin: 12px 0 0;
  padding-left: 18px;
}
.link-list li + li {
  margin-top: 6px;
}
.list-stack {
  display: grid;
  gap: 16px;
}
.list-card {
  padding: 20px;
}
.crumbs {
  margin: 10px 0 0;
  color: var(--muted);
}
.prose {
  padding: 28px;
}
.prose > :first-child { margin-top: 0; }
.prose > :last-child { margin-bottom: 0; }
.prose h2, .prose h3, .prose h4 {
  line-height: 1.25;
  margin-top: 1.8em;
}
.prose blockquote {
  margin: 1.5em 0;
  padding: .2em 1em;
  color: var(--muted);
  border-left: 4px solid var(--border);
}
.prose code {
  padding: .15em .35em;
  border-radius: 6px;
  background: var(--code);
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: .92em;
}
.prose pre {
  overflow: auto;
  padding: 16px;
  border-radius: 12px;
  background: var(--code);
}
.prose pre code {
  padding: 0;
  background: transparent;
}
.prose table {
  width: 100%;
  border-collapse: collapse;
  margin: 1.5em 0;
  font-size: .95em;
}
.prose th,
.prose td {
  padding: 8px 10px;
  border: 1px solid var(--border);
  text-align: left;
  vertical-align: top;
}
.prose th {
  background: var(--code);
}
.prose img {
  max-width: 100%;
}
`

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
