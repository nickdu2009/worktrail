package index

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
)

type Entry struct {
	Schema         string    `json:"schema"`
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	ProjectID      string    `json:"project_id,omitempty"`
	TaskID         string    `json:"task_id,omitempty"`
	Visibility     string    `json:"visibility,omitempty"`
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
	ExpiresAt      time.Time `json:"expires_at,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
	Active         bool      `json:"active,omitempty"`
	SourceSessions []string  `json:"source_sessions,omitempty"`
	CandidateType  string    `json:"candidate_type,omitempty"`
	generatedID    bool      // synthesized from Path and safe to disambiguate during scans
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
	Ignored     int       `json:"ignored,omitempty"`
}

type IgnoredEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type IgnoredSidecar struct {
	Schema      string         `json:"schema"`
	GeneratedAt time.Time      `json:"generated_at"`
	Entries     []IgnoredEntry `json:"entries,omitempty"`
}

type StatusInfo struct {
	Exists      bool      `json:"exists"`
	Scope       string    `json:"scope,omitempty"`
	GeneratedAt time.Time `json:"generated_at,omitempty"`
	IndexPath   string    `json:"index_path"`
	Entries     int       `json:"entries"`
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
	Topic          string
	TaskID         string
	Visibility     string
	Status         string
	Lifecycle      string
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

func Refresh(root string) error {
	return refreshSQLite(root, defaultTokenizer)
}

func Rebuild(root string, opts RebuildOptions) (Manifest, error) {
	if root == "" {
		return Manifest{}, errors.New("index root is required")
	}
	return rebuildSQLite(root, opts, defaultTokenizer)
}

func Status(root string) (StatusInfo, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return StatusInfo{}, err
	}
	info := StatusInfo{
		IndexPath: filepath.Join(root, "index", SQLiteFile),
	}
	exists, err := sqliteExists(root)
	if err != nil {
		return StatusInfo{}, err
	}
	if !exists {
		return info, nil
	}
	if err := ensureSQLiteHealthy(root); err != nil {
		return StatusInfo{}, err
	}
	info.Exists = true
	db, err := openSQLite(root)
	if err != nil {
		return StatusInfo{}, err
	}
	defer db.Close()
	info.GeneratedAt, _ = sqliteStateTime(db, "generated_at")
	_ = db.QueryRow(`SELECT value FROM index_state WHERE key = 'scope'`).Scan(&info.Scope)
	if info.Scope == "" {
		info.Scope = inferScope(root)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&info.Entries); err != nil {
		return StatusInfo{}, err
	}
	return info, nil
}

func Search(root string, query Query) ([]Result, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	exists, err := sqliteExists(root)
	if err != nil {
		return nil, err
	}
	if !exists {
		if _, rebuildErr := rebuildSQLite(root, RebuildOptions{Scope: inferScope(root)}, defaultTokenizer); rebuildErr != nil {
			return nil, rebuildErr
		}
	} else if err := ensureSQLiteHealthy(root); err != nil {
		return nil, err
	}
	if strings.TrimSpace(query.Content) != "" {
		return searchSQLite(root, query, defaultTokenizer)
	}
	entries, err := loadFreshSearchEntries(root)
	if err != nil {
		return nil, err
	}
	return SearchEntries(entries, query), nil
}

func loadFreshSearchEntries(root string) ([]Entry, error) {
	if err := refreshSQLite(root, defaultTokenizer); err != nil {
		return nil, err
	}
	db, err := Load(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, rebuildErr := rebuildSQLite(root, RebuildOptions{Scope: inferScope(root)}, defaultTokenizer); rebuildErr != nil {
				return nil, rebuildErr
			}
			db, err = Load(root)
		}
	}
	if err != nil {
		return nil, err
	}
	entries, _, err := FilterFresh(root, db)
	return entries, err
}

func Load(root string) (DB, error) {
	exists, err := sqliteExists(root)
	if err != nil {
		return DB{}, err
	}
	if !exists {
		return DB{}, os.ErrNotExist
	}
	if err := ensureSQLiteHealthy(root); err != nil {
		return DB{}, err
	}
	return loadSQLite(root)
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
