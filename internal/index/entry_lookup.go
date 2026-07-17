package index

import (
	"database/sql"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// EntriesByID loads complete entries for the requested IDs in first-requested
// order. Duplicate IDs are returned once, and unknown IDs are omitted so
// callers can detect them by comparing the returned IDs with their request.
//
// It performs the same source reconciliation as Search before reading, without
// recording a fresh-index refresh timestamp. The lookup itself uses a read-only
// SQLite connection and does not issue FTS queries, so lexical FTS lookup
// failures cannot affect it.
func EntriesByID(root string, ids []string) ([]Entry, error) {
	ids, err := normalizedEntryIDs(ids)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return []Entry{}, nil
	}

	root, err = filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := refreshSQLiteForRead(root, defaultTokenizer); err != nil {
		return nil, err
	}

	db, err := openSQLiteReadOnly(root)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	entriesByID, err := queryEntriesByID(db, ids)
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(entriesByID))
	for _, id := range ids {
		if entry, ok := entriesByID[id]; ok {
			entries = append(entries, entry)
		}
	}
	if err := hydrateEntryTags(db, entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func normalizedEntryIDs(ids []string) ([]string, error) {
	unique := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			return nil, errors.New("entry ID is required")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique, nil
}

func openSQLiteReadOnly(root string) (*sql.DB, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(sqlitePath(root)); err != nil {
		return nil, err
	}
	dsn := (&url.URL{
		Scheme:   "file",
		Path:     sqlitePath(root),
		RawQuery: "mode=ro&_pragma=busy_timeout(5000)",
	}).String()
	return sql.Open("sqlite", dsn)
}

func queryEntriesByID(db *sql.DB, ids []string) (map[string]Entry, error) {
	const batchSize = 900
	entriesByID := make(map[string]Entry, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		args := make([]any, 0, end-start)
		placeholders := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
			placeholders = append(placeholders, "?")
		}
		rows, err := db.Query(`
SELECT id, scope, path, type, title, topic, status, stage, lifecycle,
       source_of_truth, active, candidate_type, updated_at, excerpt, content,
       source_sessions_json, supersedes_json, superseded_by_json
FROM entries WHERE id IN (`+strings.Join(placeholders, ", ")+`)`, args...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			entry, err := scanSQLiteEntry(rows)
			if err != nil {
				rows.Close()
				return nil, err
			}
			entriesByID[entry.ID] = entry
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	return entriesByID, nil
}

func hydrateEntryTags(db *sql.DB, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	ids := make([]string, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if _, ok := seen[entry.ID]; ok {
			continue
		}
		seen[entry.ID] = struct{}{}
		ids = append(ids, entry.ID)
	}

	const batchSize = 900
	tagsByEntryID := make(map[string][]string, len(ids))
	for start := 0; start < len(ids); start += batchSize {
		end := min(start+batchSize, len(ids))
		args := make([]any, 0, end-start)
		placeholders := make([]string, 0, end-start)
		for _, id := range ids[start:end] {
			args = append(args, id)
			placeholders = append(placeholders, "?")
		}
		rows, err := db.Query(
			`SELECT entry_id, tag FROM entry_tags WHERE entry_id IN (`+strings.Join(placeholders, ", ")+`) ORDER BY entry_id, tag`,
			args...,
		)
		if err != nil {
			return err
		}
		for rows.Next() {
			var entryID, tag string
			if err := rows.Scan(&entryID, &tag); err != nil {
				rows.Close()
				return err
			}
			tagsByEntryID[entryID] = append(tagsByEntryID[entryID], tag)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	for i := range entries {
		entries[i].Tags = append([]string(nil), tagsByEntryID[entries[i].ID]...)
	}
	return nil
}
