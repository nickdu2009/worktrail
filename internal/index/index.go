package index

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	DBFile       = "index.db"
	ManifestFile = "manifest.json"
)

type Entry struct {
	Schema         string    `json:"schema"`
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	Type           string    `json:"type"`
	Path           string    `json:"path"`
	Title          string    `json:"title"`
	Status         string    `json:"status,omitempty"`
	Stage          string    `json:"stage,omitempty"`
	Lifecycle      string    `json:"lifecycle,omitempty"`
	Topic          string    `json:"topic,omitempty"`
	SourceOfTruth  bool      `json:"source_of_truth,omitempty"`
	Supersedes     []string  `json:"supersedes,omitempty"`
	SupersededBy   []string  `json:"superseded_by,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Content        string    `json:"content"`
	UpdatedAt      time.Time `json:"updated_at"`
	Active         bool      `json:"active,omitempty"`
	SourceSessions []string  `json:"source_sessions,omitempty"`
	CandidateType  string    `json:"candidate_type,omitempty"`
}

type DB struct {
	Schema      string    `json:"schema"`
	GeneratedAt time.Time `json:"generated_at"`
	Entries     []Entry   `json:"entries"`
}

type Manifest struct {
	Schema      string    `json:"schema"`
	Scope       string    `json:"scope"`
	GeneratedAt time.Time `json:"generated_at"`
	IndexPath   string    `json:"index_path"`
	Entries     int       `json:"entries"`
}

type StatusInfo struct {
	Exists       bool      `json:"exists"`
	Scope        string    `json:"scope,omitempty"`
	GeneratedAt  time.Time `json:"generated_at,omitempty"`
	IndexPath    string    `json:"index_path"`
	ManifestPath string    `json:"manifest_path"`
	Entries      int       `json:"entries"`
}

type DiffSummary struct {
	Deleted   int `json:"deleted"`
	Unindexed int `json:"unindexed"`
	New       int `json:"new"`
	Changed   int `json:"changed"`
}

type DiffItem struct {
	Path   string `json:"path"`
	Type   string `json:"type,omitempty"`
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type DiffReport struct {
	Schema      string      `json:"schema"`
	Scope       string      `json:"scope"`
	GeneratedAt time.Time   `json:"generated_at,omitempty"`
	Stale       bool        `json:"stale"`
	Summary     DiffSummary `json:"summary"`
	Deleted     []DiffItem  `json:"deleted,omitempty"`
	Unindexed   []DiffItem  `json:"unindexed,omitempty"`
	New         []DiffItem  `json:"new,omitempty"`
	Changed     []DiffItem  `json:"changed,omitempty"`
}

type HealthInfo struct {
	Schema           string     `json:"schema"`
	Scope            string     `json:"scope"`
	GeneratedAt      time.Time  `json:"generated_at,omitempty"`
	Stale            bool       `json:"stale"`
	IndexedEntries   int        `json:"indexed_entries"`
	FreshEntries     int        `json:"fresh_entries"`
	MissingFromFS    []DiffItem `json:"missing_from_fs,omitempty"`
	MissingFromIndex []DiffItem `json:"missing_from_index,omitempty"`
	Changed          []DiffItem `json:"changed,omitempty"`
}

type FreshReport struct {
	Stale          bool       `json:"stale"`
	IndexedEntries int        `json:"indexed_entries"`
	FreshEntries   int        `json:"fresh_entries"`
	Deleted        []DiffItem `json:"deleted,omitempty"`
	Changed        []DiffItem `json:"changed,omitempty"`
}

type RebuildOptions struct {
	Scope string
	Now   time.Time
}

type Query struct {
	Scope          string
	Type           string
	Tag            string
	Tags           []string
	Content        string
	Limit          int
	IncludeContent bool
}

type Result struct {
	Entry Entry   `json:"entry"`
	Score float64 `json:"score"`
}

func Rebuild(root string, opts RebuildOptions) (Manifest, error) {
	if root == "" {
		return Manifest{}, errors.New("index root is required")
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Manifest{}, err
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	scope := opts.Scope
	if scope == "" {
		scope = inferScope(root)
	}
	entries, err := scan(root, scope)
	if err != nil {
		return Manifest{}, err
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	db := DB{Schema: "worktrail.index.db.v1", GeneratedAt: now, Entries: entries}
	dbBytes, err := json.MarshalIndent(db, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	indexPath := filepath.Join(root, "index", DBFile)
	if err := util.AtomicWrite(indexPath, append(dbBytes, '\n'), 0o644); err != nil {
		return Manifest{}, err
	}
	manifest := Manifest{
		Schema:      "worktrail.index.manifest.v1",
		Scope:       scope,
		GeneratedAt: now,
		IndexPath:   filepath.ToSlash(filepath.Join("index", DBFile)),
		Entries:     len(entries),
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	if err := util.AtomicWrite(filepath.Join(root, "index", ManifestFile), append(manifestBytes, '\n'), 0o644); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func Status(root string) (StatusInfo, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return StatusInfo{}, err
	}
	info := StatusInfo{
		IndexPath:    filepath.Join(root, "index", DBFile),
		ManifestPath: filepath.Join(root, "index", ManifestFile),
	}
	b, err := os.ReadFile(info.ManifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return info, nil
	}
	if err != nil {
		return StatusInfo{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return StatusInfo{}, err
	}
	info.Exists = true
	info.Scope = manifest.Scope
	info.GeneratedAt = manifest.GeneratedAt
	info.Entries = manifest.Entries
	return info, nil
}

func Search(root string, query Query) ([]Result, error) {
	db, err := Load(root)
	if err != nil {
		return nil, err
	}
	return SearchEntries(db.Entries, query), nil
}

func SearchEntries(entries []Entry, query Query) []Result {
	needle := strings.ToLower(strings.TrimSpace(query.Content))
	tags := append([]string{}, query.Tags...)
	if query.Tag != "" {
		tags = append(tags, query.Tag)
	}
	var results []Result
	for _, entry := range entries {
		if query.Scope != "" && entry.Scope != query.Scope {
			continue
		}
		if query.Type != "" && entry.Type != query.Type {
			continue
		}
		if len(tags) > 0 && !hasAllTags(entry.Tags, tags) {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(entry.Title+"\n"+entry.Content), needle) {
			continue
		}
		score := scoreEntry(entry, needle)
		if !query.IncludeContent {
			entry.Content = ""
		}
		results = append(results, Result{Entry: entry, Score: score})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return results[i].Entry.UpdatedAt.After(results[j].Entry.UpdatedAt)
		}
		return results[i].Score > results[j].Score
	})
	if query.Limit > 0 && len(results) > query.Limit {
		results = results[:query.Limit]
	}
	return results
}

func Load(root string) (DB, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return DB{}, err
	}
	b, err := os.ReadFile(filepath.Join(root, "index", DBFile))
	if err != nil {
		return DB{}, err
	}
	var db DB
	if err := json.Unmarshal(b, &db); err != nil {
		return DB{}, err
	}
	return db, nil
}

func Diff(root string) (DiffReport, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return DiffReport{}, err
	}
	scope := inferScope(root)
	report := DiffReport{
		Schema: "worktrail.index.diff.v1",
		Scope:  scope,
	}
	if _, err := os.Stat(root); errors.Is(err, os.ErrNotExist) {
		return report, nil
	} else if err != nil {
		return DiffReport{}, err
	}
	currentEntries, err := scan(root, scope)
	if err != nil {
		return DiffReport{}, err
	}
	db, err := Load(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return DiffReport{}, err
	}
	report.GeneratedAt = db.GeneratedAt
	indexedByPath := make(map[string]Entry, len(db.Entries))
	currentByPath := make(map[string]Entry, len(currentEntries))
	for _, entry := range db.Entries {
		indexedByPath[entry.Path] = entry
	}
	for _, entry := range currentEntries {
		currentByPath[entry.Path] = entry
	}
	for path, entry := range indexedByPath {
		current, ok := currentByPath[path]
		if !ok {
			report.Deleted = append(report.Deleted, DiffItem{
				Path:   path,
				Type:   entry.Type,
				Title:  entry.Title,
				Reason: "indexed file is missing from filesystem",
			})
			continue
		}
		modTime, statErr := entryModTime(root, current)
		if statErr != nil {
			return DiffReport{}, statErr
		}
		if !db.GeneratedAt.IsZero() && modTime.After(db.GeneratedAt) {
			report.Changed = append(report.Changed, DiffItem{
				Path:   path,
				Type:   current.Type,
				Title:  current.Title,
				Reason: "filesystem file is newer than the index",
			})
		}
	}
	for path, entry := range currentByPath {
		if _, ok := indexedByPath[path]; ok {
			continue
		}
		item := DiffItem{
			Path:   path,
			Type:   entry.Type,
			Title:  entry.Title,
			Reason: "filesystem file is not indexed",
		}
		report.Unindexed = append(report.Unindexed, item)
		modTime, statErr := entryModTime(root, entry)
		if statErr != nil {
			return DiffReport{}, statErr
		}
		if db.GeneratedAt.IsZero() || modTime.After(db.GeneratedAt) {
			item.Reason = "filesystem file was created after the last index build"
			report.New = append(report.New, item)
		}
	}
	sortDiffItems(report.Deleted)
	sortDiffItems(report.Unindexed)
	sortDiffItems(report.New)
	sortDiffItems(report.Changed)
	report.Summary = DiffSummary{
		Deleted:   len(report.Deleted),
		Unindexed: len(report.Unindexed),
		New:       len(report.New),
		Changed:   len(report.Changed),
	}
	report.Stale = report.Summary.Deleted > 0 || report.Summary.Unindexed > 0 || report.Summary.Changed > 0
	return report, nil
}

func Health(root string) (HealthInfo, error) {
	report, err := Diff(root)
	if err != nil {
		return HealthInfo{}, err
	}
	indexedEntries := 0
	if !report.GeneratedAt.IsZero() {
		db, loadErr := Load(root)
		if loadErr != nil && !errors.Is(loadErr, os.ErrNotExist) {
			return HealthInfo{}, loadErr
		}
		indexedEntries = len(db.Entries)
	}
	freshEntries := indexedEntries - len(report.Deleted) - len(report.Changed)
	if freshEntries < 0 {
		freshEntries = 0
	}
	return HealthInfo{
		Schema:           "worktrail.index.health.v1",
		Scope:            report.Scope,
		GeneratedAt:      report.GeneratedAt,
		Stale:            report.Stale,
		IndexedEntries:   indexedEntries,
		FreshEntries:     freshEntries,
		MissingFromFS:    append([]DiffItem{}, report.Deleted...),
		MissingFromIndex: append([]DiffItem{}, report.Unindexed...),
		Changed:          append([]DiffItem{}, report.Changed...),
	}, nil
}

func FilterFresh(root string, db DB) ([]Entry, FreshReport, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, FreshReport{}, err
	}
	report := FreshReport{
		IndexedEntries: len(db.Entries),
	}
	var fresh []Entry
	for _, entry := range db.Entries {
		modTime, modErr := entryModTime(root, entry)
		if errors.Is(modErr, os.ErrNotExist) {
			report.Deleted = append(report.Deleted, DiffItem{
				Path:   entry.Path,
				Type:   entry.Type,
				Title:  entry.Title,
				Reason: "indexed file is missing from filesystem",
			})
			continue
		}
		if modErr != nil {
			return nil, FreshReport{}, modErr
		}
		if !db.GeneratedAt.IsZero() && modTime.After(db.GeneratedAt) {
			report.Changed = append(report.Changed, DiffItem{
				Path:   entry.Path,
				Type:   entry.Type,
				Title:  entry.Title,
				Reason: "filesystem file is newer than the index",
			})
			continue
		}
		fresh = append(fresh, entry)
	}
	sortDiffItems(report.Deleted)
	sortDiffItems(report.Changed)
	report.FreshEntries = len(fresh)
	report.Stale = len(report.Deleted) > 0 || len(report.Changed) > 0
	return fresh, report, nil
}

func scan(root, scope string) ([]Entry, error) {
	var entries []Entry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "index", "logs", "raw", "exports":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldSkip(rel) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".json" {
			return nil
		}
		entry, ok, err := buildEntry(root, path, rel, scope)
		if err != nil {
			return err
		}
		if ok {
			entries = append(entries, entry)
		}
		return nil
	})
	return entries, err
}

func buildEntry(root, path, rel, scope string) (Entry, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Entry{}, false, err
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, false, err
	}
	body := string(b)
	meta := map[string]any{}
	if strings.HasSuffix(strings.ToLower(path), ".md") {
		if doc, err := store.ParseMarkdown(b); err == nil {
			meta = doc.Meta
			body = doc.Body
		}
	}
	entry := Entry{
		Schema:        "worktrail.index.entry.v1",
		ID:            stringMeta(meta, "id", util.Slug(strings.TrimSuffix(rel, filepath.Ext(rel)))),
		Scope:         stringMeta(meta, "scope", scope),
		Type:          inferType(rel, meta),
		Path:          rel,
		Title:         stringMeta(meta, "title", inferTitle(rel, body)),
		Status:        stringMeta(meta, "status", ""),
		Stage:         stringMeta(meta, "stage", ""),
		Lifecycle:     knowledge.NormalizeLifecycle(stringMeta(meta, "lifecycle", ""), stringMeta(meta, "stage", ""), stringMeta(meta, "status", "")),
		Topic:         stringMeta(meta, "topic", ""),
		SourceOfTruth: boolMeta(meta, "source_of_truth"),
		Supersedes:    stringListMeta(meta, "supersedes"),
		SupersededBy:  stringListMeta(meta, "superseded_by"),
		Tags:          stringSliceMeta(meta, "tags"),
		Content:       strings.TrimSpace(body),
		UpdatedAt:     timeMeta(meta, "updated_at", info.ModTime().UTC()),
		Active:        strings.HasPrefix(rel, "state/active/") || stringMeta(meta, "status", "") == "active",
	}
	entry.SourceSessions = stringSliceMeta(meta, "source_sessions")
	entry.CandidateType = stringMeta(meta, "candidate_type", "")
	if entry.Scope == "" {
		entry.Scope = scope
	}
	if entry.Type == "config" {
		return Entry{}, false, nil
	}
	return entry, true, nil
}

func shouldSkip(rel string) bool {
	base := filepath.Base(rel)
	return base == "config.json" || base == ".DS_Store" || rel == "state/active/latest.md"
}

func inferScope(root string) string {
	if b, err := os.ReadFile(filepath.Join(root, "config.json")); err == nil {
		var cfg struct {
			Scope string `json:"scope"`
		}
		if json.Unmarshal(b, &cfg) == nil && cfg.Scope != "" {
			return cfg.Scope
		}
	}
	if filepath.Base(root) == ".worktrail" {
		return "project"
	}
	return "user"
}

func inferType(rel string, meta map[string]any) string {
	if typ := stringMeta(meta, "type", ""); typ != "" {
		return typ
	}
	if typ := stringMeta(meta, "candidate_type", ""); typ != "" {
		return "candidate"
	}
	switch {
	case strings.HasPrefix(rel, "candidates/"):
		return "candidate"
	case strings.HasPrefix(rel, "state/"):
		return "state"
	case strings.HasPrefix(rel, "architecture/"):
		return "architecture"
	case strings.HasPrefix(rel, "requirements/"):
		return "requirement"
	case strings.HasPrefix(rel, "decisions/"):
		return "decision"
	case strings.HasPrefix(rel, "glossary/"):
		return "glossary"
	case strings.HasPrefix(rel, "handoffs/"):
		return "handoff"
	case strings.HasPrefix(rel, "integrations/"):
		return "integration"
	case strings.HasPrefix(rel, "rules/"):
		return "rule"
	case strings.HasPrefix(rel, "validation/"):
		return "validation"
	case strings.HasPrefix(rel, "prompts/"):
		return "prompt"
	case strings.HasPrefix(rel, "profile/"):
		return "profile"
	case strings.HasPrefix(rel, "workflows/"):
		return "workflow"
	case strings.HasPrefix(rel, "lessons/"):
		return "lesson"
	case rel == "current-state.md":
		return "state"
	case rel == "project.md":
		return "project"
	case rel == "index.md":
		return "index"
	case rel == "log.md":
		return "log"
	default:
		return "knowledge"
	}
}

func InferType(rel string, meta map[string]any) string {
	return inferType(rel, meta)
}

func inferTitle(rel, body string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			return strings.TrimSpace(strings.TrimLeft(line, "#"))
		}
	}
	name := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	return strings.ReplaceAll(name, "-", " ")
}

func stringMeta(meta map[string]any, key, fallback string) string {
	v, ok := meta[key]
	if !ok {
		return fallback
	}
	switch x := v.(type) {
	case string:
		if x == "" {
			return fallback
		}
		return x
	case fmt.Stringer:
		return x.String()
	default:
		return fallback
	}
}

func stringSliceMeta(meta map[string]any, key string) []string {
	v, ok := meta[key]
	if !ok {
		return nil
	}
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func stringListMeta(meta map[string]any, key string) []string {
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
	case []string:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s := strings.TrimSpace(item); s != "" {
				out = append(out, filepath.ToSlash(s))
			}
		}
		return out
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

func boolMeta(meta map[string]any, key string) bool {
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

func timeMeta(meta map[string]any, key string, fallback time.Time) time.Time {
	s := stringMeta(meta, key, "")
	if s == "" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fallback
	}
	return t
}

func hasAllTags(have []string, want []string) bool {
	set := map[string]bool{}
	for _, tag := range have {
		set[strings.ToLower(tag)] = true
	}
	for _, tag := range want {
		if !set[strings.ToLower(tag)] {
			return false
		}
	}
	return true
}

func scoreEntry(entry Entry, needle string) float64 {
	score := 1.0
	if needle != "" {
		title := strings.ToLower(entry.Title)
		content := strings.ToLower(entry.Content)
		if strings.Contains(title, needle) {
			score += 8
		}
		score += float64(strings.Count(content, needle))
	}
	if entry.Active {
		score += 5
	}
	if entry.Type == "state" && entry.Status == "active" {
		score += 3
	}
	if entry.SourceOfTruth {
		score += 5
	}
	if len(entry.SupersededBy) > 0 || knowledge.IsNonCurrentLifecycle(entry.Lifecycle) || entry.Stage == "historical" || entry.Stage == "retired" {
		score -= 5
	}
	age := time.Since(entry.UpdatedAt)
	if age < 0 {
		age = 0
	}
	switch {
	case age <= 24*time.Hour:
		score += 3
	case age <= 7*24*time.Hour:
		score += 2
	case age <= 30*24*time.Hour:
		score += 1
	}
	return score
}

func entryModTime(root string, entry Entry) (time.Time, error) {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime().UTC(), nil
}

func sortDiffItems(items []DiffItem) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].Path < items[j].Path
	})
}

func RebuildEnv(env paths.Env, scope string) (Manifest, error) {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return Manifest{}, err
	}
	return Rebuild(root, RebuildOptions{Scope: scope})
}
