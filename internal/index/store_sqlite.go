package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	SQLiteFile       = "index.sqlite"
	schemaVersionKey = "schema_version"
	currentSchema    = "worktrail.index.sqlite.v2"
	brokenRetention  = 3
)

const sqliteSchema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS entries (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
  project_id TEXT NOT NULL DEFAULT '',
  task_id TEXT NOT NULL DEFAULT '',
  visibility TEXT NOT NULL DEFAULT '',
  path TEXT NOT NULL UNIQUE,
  object_kind TEXT,
  type TEXT NOT NULL,
  title TEXT NOT NULL,
  topic TEXT,
  status TEXT,
  stage TEXT,
  lifecycle TEXT,
  source_of_truth INTEGER NOT NULL DEFAULT 0,
  active INTEGER NOT NULL DEFAULT 0,
  candidate_type TEXT,
  expires_at TEXT,
  updated_at TEXT NOT NULL,
  file_mtime TEXT NOT NULL,
  file_size INTEGER NOT NULL,
  content_hash TEXT NOT NULL,
  excerpt TEXT,
  content TEXT,
  raw_meta_json TEXT NOT NULL DEFAULT '{}',
  source_sessions_json TEXT NOT NULL DEFAULT '[]',
  supersedes_json TEXT NOT NULL DEFAULT '[]',
  superseded_by_json TEXT NOT NULL DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS entry_tags (
  entry_id TEXT NOT NULL,
  tag TEXT NOT NULL,
  PRIMARY KEY (entry_id, tag),
  FOREIGN KEY (entry_id) REFERENCES entries(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS entry_edges (
  from_entry_id TEXT NOT NULL,
  edge_type TEXT NOT NULL,
  to_path TEXT NOT NULL DEFAULT '',
  to_entry_id TEXT,
  PRIMARY KEY (from_entry_id, edge_type, to_path),
  FOREIGN KEY (from_entry_id) REFERENCES entries(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS index_state (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE VIRTUAL TABLE IF NOT EXISTS entry_fts USING fts5(
  entry_id UNINDEXED,
  title_terms,
  body_terms,
  topic_terms,
  tag_terms,
  ident_terms,
  tokenize='unicode61'
);
`

func sqlitePath(root string) string {
	return filepath.Join(root, "index", SQLiteFile)
}

func openSQLite(root string) (*sql.DB, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if _, err := ensureIndexOutputDir(root); err != nil {
		return nil, err
	}
	if _, _, err := indexArtifactStatus(root, SQLiteFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	db, err := sql.Open("sqlite", sqlitePath(root)+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(sqliteSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	if exists, _, err := indexArtifactStatus(root, SQLiteFile); err != nil {
		_ = db.Close()
		return nil, err
	} else if !exists {
		_ = db.Close()
		return nil, errors.New("sqlite index was not created as a regular file")
	}
	if err := ensureSQLiteColumns(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, schemaVersionKey, currentSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func ensureSQLiteColumns(db *sql.DB) error {
	required := map[string]string{
		"project_id": "TEXT NOT NULL DEFAULT ''",
		"task_id":    "TEXT NOT NULL DEFAULT ''",
		"visibility": "TEXT NOT NULL DEFAULT ''",
		"expires_at": "TEXT",
	}
	rows, err := db.Query(`PRAGMA table_info(entries)`)
	if err != nil {
		return err
	}
	existing := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &primaryKey); err != nil {
			rows.Close()
			return err
		}
		existing[name] = true
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for name, typ := range required {
		if existing[name] {
			continue
		}
		if _, err := db.Exec(`ALTER TABLE entries ADD COLUMN ` + name + ` ` + typ); err != nil {
			return err
		}
	}
	return nil
}

func contentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", sum[:])
}

func rebuildSQLite(root string, opts RebuildOptions, tokenizer Tokenizer) (Manifest, error) {
	if tokenizer == nil {
		tokenizer = defaultTokenizer
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
	entries, ignored, err := scanAt(root, scope, now)
	if err != nil {
		return Manifest{}, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	_ = tokenizer.LoadProjectDictionary(deriveProjectDictionary(entries))

	db, err := openSQLite(root)
	if err != nil {
		return Manifest{}, err
	}
	defer db.Close()

	tx, err := db.Begin()
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`DELETE FROM entry_fts`); err != nil {
		return Manifest{}, err
	}
	if _, err := tx.Exec(`DELETE FROM entry_edges`); err != nil {
		return Manifest{}, err
	}
	if _, err := tx.Exec(`DELETE FROM entry_tags`); err != nil {
		return Manifest{}, err
	}
	if _, err := tx.Exec(`DELETE FROM entries`); err != nil {
		return Manifest{}, err
	}

	for _, entry := range entries {
		if err := upsertSQLiteEntry(tx, root, entry, tokenizer); err != nil {
			return Manifest{}, err
		}
	}
	if _, err := tx.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "scope", scope); err != nil {
		return Manifest{}, err
	}
	if _, err := tx.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "generated_at", now.Format(time.RFC3339Nano)); err != nil {
		return Manifest{}, err
	}
	if _, err := tx.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "last_refresh_at", now.Format(time.RFC3339Nano)); err != nil {
		return Manifest{}, err
	}
	if err := tx.Commit(); err != nil {
		return Manifest{}, err
	}

	for _, legacy := range []string{"index.db", "manifest.json"} {
		if err := removeIndexArtifact(root, legacy); err != nil && !errors.Is(err, os.ErrNotExist) {
			return Manifest{}, err
		}
	}

	return Manifest{
		Schema:      "worktrail.index.manifest.v1",
		Scope:       scope,
		GeneratedAt: now,
		IndexPath:   filepath.ToSlash(filepath.Join("index", SQLiteFile)),
		Entries:     len(entries),
		Ignored:     len(ignored),
	}, nil
}

func upsertSQLiteEntry(tx *sql.Tx, root string, entry Entry, tokenizer Tokenizer) error {
	modTime, err := entryModTime(root, entry)
	if err != nil {
		return err
	}
	path := filepath.Join(root, filepath.FromSlash(entry.Path))
	if err := validateIndexPath(root, path); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("index source %q is not a regular file", path)
	}
	tokens := tokenizeEntry(entry, tokenizer)
	rawMeta, _ := json.Marshal(map[string]any{
		"schema": entry.Schema,
		"id":     entry.ID,
	})
	sourceSessions, _ := json.Marshal(entry.SourceSessions)
	supersedes, _ := json.Marshal(entry.Supersedes)
	supersededBy, _ := json.Marshal(entry.SupersededBy)
	excerpt := tokens.Excerpt
	if excerpt == "" {
		excerpt = excerptContent(entry.Content)
	}
	_, err = tx.Exec(`
INSERT INTO entries(
  id, scope, project_id, task_id, visibility, path, type, title, topic, status, stage, lifecycle,
  source_of_truth, active, candidate_type, expires_at, updated_at, file_mtime, file_size,
  content_hash, excerpt, content, raw_meta_json, source_sessions_json,
  supersedes_json, superseded_by_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Scope, entry.ProjectID, entry.TaskID, entry.Visibility,
		entry.Path, entry.Type, entry.Title, entry.Topic, entry.Status, entry.Stage, entry.Lifecycle,
		boolToInt(entry.SourceOfTruth), boolToInt(entry.Active), entry.CandidateType,
		formatOptionalTime(entry.ExpiresAt),
		entry.UpdatedAt.UTC().Format(time.RFC3339Nano), modTime.Format(time.RFC3339Nano), info.Size(),
		contentHash(entry.Content), excerpt, entry.Content, string(rawMeta), string(sourceSessions),
		string(supersedes), string(supersededBy),
	)
	if err != nil {
		return err
	}
	seenTags := map[string]struct{}{}
	for _, tag := range entry.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, ok := seenTags[tag]; ok {
			continue
		}
		seenTags[tag] = struct{}{}
		if _, err := tx.Exec(`INSERT INTO entry_tags(entry_id, tag) VALUES(?, ?)`, entry.ID, tag); err != nil {
			return err
		}
	}
	for _, path := range entry.Supersedes {
		if _, err := tx.Exec(`INSERT INTO entry_edges(from_entry_id, edge_type, to_path) VALUES(?, 'supersedes', ?)`, entry.ID, path); err != nil {
			return err
		}
	}
	for _, path := range entry.SupersededBy {
		if _, err := tx.Exec(`INSERT INTO entry_edges(from_entry_id, edge_type, to_path) VALUES(?, 'superseded_by', ?)`, entry.ID, path); err != nil {
			return err
		}
	}
	_, err = tx.Exec(`
INSERT INTO entry_fts(entry_id, title_terms, body_terms, topic_terms, tag_terms, ident_terms)
VALUES (?, ?, ?, ?, ?, ?)`,
		entry.ID,
		joinTerms(tokens.TitleTerms),
		joinTerms(tokens.BodyTerms),
		joinTerms(tokens.TopicTerms),
		joinTerms(tokens.TagTerms),
		joinTerms(tokens.IdentTerms),
	)
	return err
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func loadSQLite(root string) (DB, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return DB{}, err
	}
	exists, _, err := indexArtifactStatus(root, SQLiteFile)
	if err != nil {
		return DB{}, err
	}
	if !exists {
		return DB{}, os.ErrNotExist
	}
	db, err := openSQLite(root)
	if err != nil {
		return DB{}, err
	}
	defer db.Close()

	generatedAt, err := sqliteStateTime(db, "generated_at")
	if err != nil {
		return DB{}, err
	}
	rows, err := db.Query(`
SELECT id, scope, path, type, title, topic, status, stage, lifecycle,
       project_id, task_id, visibility, source_of_truth, active, candidate_type, expires_at, updated_at, excerpt, content,
       source_sessions_json, supersedes_json, superseded_by_json
FROM entries ORDER BY path`)
	if err != nil {
		return DB{}, err
	}
	defer rows.Close()

	var entries []Entry
	for rows.Next() {
		entry, err := scanSQLiteEntry(rows)
		if err != nil {
			return DB{}, err
		}
		tagRows, err := db.Query(`SELECT tag FROM entry_tags WHERE entry_id = ? ORDER BY tag`, entry.ID)
		if err != nil {
			return DB{}, err
		}
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err != nil {
				tagRows.Close()
				return DB{}, err
			}
			entry.Tags = append(entry.Tags, tag)
		}
		tagRows.Close()
		entries = append(entries, entry)
	}
	return DB{
		Schema:      currentSchema,
		GeneratedAt: generatedAt,
		Entries:     entries,
	}, rows.Err()
}

func scanSQLiteEntry(rows *sql.Rows) (Entry, error) {
	var (
		entry                    Entry
		sourceOfTruth, activeInt int
		updatedAt                string
		expiresAt                sql.NullString
		excerpt                  string
		sourceSessionsJSON       string
		supersedesJSON           string
		supersededByJSON         string
	)
	entry.Schema = "worktrail.index.entry.v1"
	if err := rows.Scan(
		&entry.ID, &entry.Scope, &entry.Path, &entry.Type, &entry.Title, &entry.Topic, &entry.Status, &entry.Stage, &entry.Lifecycle,
		&entry.ProjectID, &entry.TaskID, &entry.Visibility,
		&sourceOfTruth, &activeInt, &entry.CandidateType, &expiresAt, &updatedAt, &excerpt, &entry.Content,
		&sourceSessionsJSON, &supersedesJSON, &supersededByJSON,
	); err != nil {
		return Entry{}, err
	}
	entry.SourceOfTruth = sourceOfTruth == 1
	entry.Active = activeInt == 1
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		entry.UpdatedAt = t
	}
	if expiresAt.Valid {
		entry.ExpiresAt, _ = time.Parse(time.RFC3339Nano, expiresAt.String)
	}
	_ = json.Unmarshal([]byte(sourceSessionsJSON), &entry.SourceSessions)
	_ = json.Unmarshal([]byte(supersedesJSON), &entry.Supersedes)
	_ = json.Unmarshal([]byte(supersededByJSON), &entry.SupersededBy)
	_ = excerpt
	return entry, nil
}

func sqliteStateTime(db *sql.DB, key string) (time.Time, error) {
	var value string
	err := db.QueryRow(`SELECT value FROM index_state WHERE key = ?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) || value == "" {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Parse(time.RFC3339, value)
	}
	return t, nil
}

func formatOptionalTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func sqliteExists(root string) (bool, error) {
	exists, _, err := indexArtifactStatus(root, SQLiteFile)
	return exists, err
}

func pingSQLite(root string) error {
	db, err := openSQLite(root)
	if err != nil {
		return err
	}
	defer db.Close()
	var check string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return fmt.Errorf("sqlite quick_check: %s", check)
	}
	var count int
	return db.QueryRow(`SELECT COUNT(*) FROM entries`).Scan(&count)
}

func ensureSQLiteHealthy(root string) error {
	exists, err := sqliteExists(root)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	pingErr := pingSQLite(root)
	if pingErr == nil {
		return nil
	}
	if recoverErr := recoverSQLite(root); recoverErr != nil {
		if removeErr := os.Remove(sqlitePath(root)); removeErr != nil {
			return fmt.Errorf("sqlite index corrupt and recovery failed: %w (rename: %v, remove: %v)", pingErr, recoverErr, removeErr)
		}
	}
	_, rebuildErr := rebuildSQLite(root, RebuildOptions{Scope: inferScope(root)}, defaultTokenizer)
	if rebuildErr != nil {
		return fmt.Errorf("sqlite index corrupt, recovery rebuild failed: %w", rebuildErr)
	}
	return nil
}

func refreshSQLite(root string, tokenizer Tokenizer) error {
	return refreshSQLiteWithRefreshTimestamp(root, tokenizer, true)
}

// refreshSQLiteForRead reconciles source changes before a read-only lookup
// without recording a timestamp when the index is already current.
func refreshSQLiteForRead(root string, tokenizer Tokenizer) error {
	return refreshSQLiteWithRefreshTimestamp(root, tokenizer, false)
}

func refreshSQLiteWithRefreshTimestamp(root string, tokenizer Tokenizer, recordRefresh bool) error {
	if tokenizer == nil {
		tokenizer = defaultTokenizer
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	exists, err := sqliteExists(root)
	if err != nil {
		return err
	}
	if !exists {
		_, err := rebuildSQLite(root, RebuildOptions{Scope: inferScope(root)}, tokenizer)
		return err
	}
	if err := ensureSQLiteHealthy(root); err != nil {
		return err
	}
	scope := inferScope(root)
	currentEntries, _, err := scanAt(root, scope, time.Now().UTC())
	if err != nil {
		return err
	}
	_ = tokenizer.LoadProjectDictionary(deriveProjectDictionary(currentEntries))

	db, err := openSQLite(root)
	if err != nil {
		return err
	}
	defer db.Close()

	indexed := map[string]struct {
		mtime      string
		size       int64
		hash       string
		projectID  string
		taskID     string
		visibility string
		status     string
		lifecycle  string
		expiresAt  string
	}{}
	rows, err := db.Query(`
SELECT path, file_mtime, file_size, content_hash,
       project_id, task_id, visibility, status, lifecycle, COALESCE(expires_at, '')
FROM entries`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var path, mtime, hash, projectID, taskID, visibility, status, lifecycle, expiresAt string
		var size int64
		if err := rows.Scan(&path, &mtime, &size, &hash, &projectID, &taskID, &visibility, &status, &lifecycle, &expiresAt); err != nil {
			rows.Close()
			return err
		}
		indexed[path] = struct {
			mtime      string
			size       int64
			hash       string
			projectID  string
			taskID     string
			visibility string
			status     string
			lifecycle  string
			expiresAt  string
		}{
			mtime: mtime, size: size, hash: hash, projectID: projectID, taskID: taskID,
			visibility: visibility, status: status, lifecycle: lifecycle, expiresAt: expiresAt,
		}
	}
	rows.Close()

	currentByPath := map[string]Entry{}
	for _, entry := range currentEntries {
		currentByPath[entry.Path] = entry
	}

	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	changed := false
	for path := range indexed {
		if _, ok := currentByPath[path]; !ok {
			changed = true
			if _, err := tx.Exec(`DELETE FROM entry_fts WHERE entry_id IN (SELECT id FROM entries WHERE path = ?)`, path); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM entries WHERE path = ?`, path); err != nil {
				return err
			}
		}
	}
	for _, entry := range currentEntries {
		modTime, err := entryModTime(root, entry)
		if err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(entry.Path))
		if err := validateIndexPath(root, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("index source %q is not a regular file", path)
		}
		hash := contentHash(entry.Content)
		prev, ok := indexed[entry.Path]
		expiresAt := ""
		if !entry.ExpiresAt.IsZero() {
			expiresAt = entry.ExpiresAt.UTC().Format(time.RFC3339Nano)
		}
		needsUpdate := !ok ||
			prev.hash != hash ||
			prev.size != info.Size() ||
			prev.mtime != modTime.Format(time.RFC3339Nano) ||
			prev.projectID != entry.ProjectID ||
			prev.taskID != entry.TaskID ||
			prev.visibility != entry.Visibility ||
			prev.status != entry.Status ||
			prev.lifecycle != entry.Lifecycle ||
			prev.expiresAt != expiresAt
		if !needsUpdate {
			continue
		}
		changed = true
		if _, err := tx.Exec(`DELETE FROM entries WHERE path = ?`, entry.Path); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM entry_fts WHERE entry_id = ?`, entry.ID); err != nil {
			return err
		}
		if err := upsertSQLiteEntry(tx, root, entry, tokenizer); err != nil {
			return err
		}
	}
	now := time.Now().UTC()
	if changed {
		generatedAt := now
		for _, entry := range currentEntries {
			modTime, modErr := entryModTime(root, entry)
			if modErr != nil {
				continue
			}
			if modTime.After(generatedAt) {
				generatedAt = modTime
			}
		}
		if _, err := tx.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "generated_at", generatedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if changed || recordRefresh {
		if _, err := tx.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, "last_refresh_at", now.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func recoverSQLite(root string) error {
	path := sqlitePath(root)
	exists, _, err := indexArtifactStatus(root, SQLiteFile)
	if err != nil {
		return err
	}
	if !exists {
		return os.ErrNotExist
	}
	broken := path + ".broken-" + time.Now().UTC().Format("20060102T150405.000000000Z")
	if err := os.Rename(path, broken); err != nil {
		return err
	}
	return pruneBrokenSQLite(root, brokenRetention)
}

func pruneBrokenSQLite(root string, keep int) error {
	if keep < 0 {
		keep = 0
	}
	matches, err := filepath.Glob(sqlitePath(root) + ".broken-*")
	if err != nil {
		return err
	}
	sort.Slice(matches, func(i, j int) bool {
		left, leftErr := os.Lstat(matches[i])
		right, rightErr := os.Lstat(matches[j])
		if leftErr == nil && rightErr == nil && !left.ModTime().Equal(right.ModTime()) {
			return left.ModTime().After(right.ModTime())
		}
		return matches[i] > matches[j]
	})
	for _, path := range matches {
		if err := validateIndexPath(root, path); err != nil {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("broken index artifact %q is not a regular file", path)
		}
	}
	if keep > len(matches) {
		keep = len(matches)
	}
	for _, path := range matches[keep:] {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
