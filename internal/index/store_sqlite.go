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
	currentSchema    = "worktrail.index.sqlite.v1"
)

const sqliteSchema = `
PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS entries (
  id TEXT PRIMARY KEY,
  scope TEXT NOT NULL,
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
	if err := os.MkdirAll(filepath.Join(root, "index"), 0o755); err != nil {
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
	if _, err := db.Exec(`INSERT INTO index_state(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, schemaVersionKey, currentSchema); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
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
	entries, err := scan(root, scope)
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

	_ = os.Remove(filepath.Join(root, "index", "index.db"))
	_ = os.Remove(filepath.Join(root, "index", "manifest.json"))

	return Manifest{
		Schema:      "worktrail.index.manifest.v1",
		Scope:       scope,
		GeneratedAt: now,
		IndexPath:   filepath.ToSlash(filepath.Join("index", SQLiteFile)),
		Entries:     len(entries),
	}, nil
}

func upsertSQLiteEntry(tx *sql.Tx, root string, entry Entry, tokenizer Tokenizer) error {
	modTime, err := entryModTime(root, entry)
	if err != nil {
		return err
	}
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry.Path)))
	if err != nil {
		return err
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
  id, scope, path, type, title, topic, status, stage, lifecycle,
  source_of_truth, active, candidate_type, updated_at, file_mtime, file_size,
  content_hash, excerpt, content, raw_meta_json, source_sessions_json,
  supersedes_json, superseded_by_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		entry.ID, entry.Scope, entry.Path, entry.Type, entry.Title, entry.Topic, entry.Status, entry.Stage, entry.Lifecycle,
		boolToInt(entry.SourceOfTruth), boolToInt(entry.Active), entry.CandidateType,
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
	if _, err := os.Stat(sqlitePath(root)); errors.Is(err, os.ErrNotExist) {
		return DB{}, os.ErrNotExist
	} else if err != nil {
		return DB{}, err
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
       source_of_truth, active, candidate_type, updated_at, excerpt, content,
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
		excerpt                  string
		sourceSessionsJSON       string
		supersedesJSON           string
		supersededByJSON         string
	)
	entry.Schema = "worktrail.index.entry.v1"
	if err := rows.Scan(
		&entry.ID, &entry.Scope, &entry.Path, &entry.Type, &entry.Title, &entry.Topic, &entry.Status, &entry.Stage, &entry.Lifecycle,
		&sourceOfTruth, &activeInt, &entry.CandidateType, &updatedAt, &excerpt, &entry.Content,
		&sourceSessionsJSON, &supersedesJSON, &supersededByJSON,
	); err != nil {
		return Entry{}, err
	}
	entry.SourceOfTruth = sourceOfTruth == 1
	entry.Active = activeInt == 1
	if t, err := time.Parse(time.RFC3339Nano, updatedAt); err == nil {
		entry.UpdatedAt = t
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

func sqliteExists(root string) bool {
	_, err := os.Stat(sqlitePath(root))
	return err == nil
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
	if !sqliteExists(root) {
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
	if !sqliteExists(root) {
		_, err := rebuildSQLite(root, RebuildOptions{Scope: inferScope(root)}, tokenizer)
		return err
	}
	if err := ensureSQLiteHealthy(root); err != nil {
		return err
	}
	scope := inferScope(root)
	currentEntries, err := scan(root, scope)
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
		mtime string
		size  int64
		hash  string
	}{}
	rows, err := db.Query(`SELECT path, file_mtime, file_size, content_hash FROM entries`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var path, mtime, hash string
		var size int64
		if err := rows.Scan(&path, &mtime, &size, &hash); err != nil {
			rows.Close()
			return err
		}
		indexed[path] = struct {
			mtime string
			size  int64
			hash  string
		}{mtime: mtime, size: size, hash: hash}
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
		info, err := os.Stat(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			return err
		}
		hash := contentHash(entry.Content)
		prev, ok := indexed[entry.Path]
		needsUpdate := !ok || prev.hash != hash || prev.size != info.Size() || prev.mtime != modTime.Format(time.RFC3339Nano)
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
	if _, err := os.Stat(path); err != nil {
		return err
	}
	broken := path + ".broken-" + time.Now().UTC().Format("20060102T150405Z")
	return os.Rename(path, broken)
}
