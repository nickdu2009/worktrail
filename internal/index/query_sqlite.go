package index

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/knowledge"
)

func searchSQLite(root string, query Query, tokenizer Tokenizer) ([]Result, error) {
	if tokenizer == nil {
		tokenizer = defaultTokenizer
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := refreshSQLite(root, tokenizer); err != nil {
		return nil, err
	}
	db, err := openSQLite(root)
	if err != nil {
		return nil, err
	}
	defer db.Close()

	needle := strings.TrimSpace(query.Content)
	if needle == "" {
		entries, err := loadFreshSearchEntries(root)
		if err != nil {
			return nil, err
		}
		return SearchEntries(entries, query), nil
	}

	tokenized := tokenizer.TokenizeQuery(needle)
	terms := tokenized.Terms
	if len(terms) == 0 {
		terms = []string{strings.ToLower(needle)}
	}
	match := buildFTSMatch(terms)
	if match == "" {
		entries, err := loadFreshSearchEntries(root)
		if err != nil {
			return nil, err
		}
		return SearchEntries(entries, query), nil
	}

	sqlQuery := `
SELECT e.id, e.scope, e.path, e.type, e.title, e.topic, e.status, e.stage, e.lifecycle,
       e.project_id, e.task_id, e.visibility,
       e.source_of_truth, e.active, e.candidate_type, e.expires_at, e.updated_at, e.excerpt, e.content,
       e.source_sessions_json, e.supersedes_json, e.superseded_by_json,
       bm25(entry_fts, 8.0, 1.0, 2.0, 2.0, 3.0) AS fts_score
FROM entry_fts
JOIN entries e ON e.id = entry_fts.entry_id
WHERE entry_fts MATCH ?
`
	args := []any{match}
	if query.Scope != "" {
		sqlQuery += ` AND e.scope = ?`
		args = append(args, query.Scope)
	}
	if query.Type != "" {
		sqlQuery += ` AND e.type = ?`
		args = append(args, query.Type)
	}
	if query.Topic != "" {
		sqlQuery += ` AND e.topic = ?`
		args = append(args, query.Topic)
	}
	if query.TaskID != "" {
		sqlQuery += ` AND e.task_id = ?`
		args = append(args, query.TaskID)
	}
	if query.Visibility != "" {
		sqlQuery += ` AND e.visibility = ?`
		args = append(args, query.Visibility)
	}
	if query.Status != "" {
		sqlQuery += ` AND e.status = ?`
		args = append(args, query.Status)
	}
	if query.Lifecycle != "" {
		sqlQuery += ` AND e.lifecycle = ?`
		args = append(args, query.Lifecycle)
	}
	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		entries, loadErr := loadFreshSearchEntries(root)
		if loadErr != nil {
			return nil, err
		}
		return SearchEntries(entries, query), nil
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		entry, ftsScore, err := scanSQLiteSearchRow(rows)
		if err != nil {
			return nil, err
		}
		tagRows, err := db.Query(`SELECT tag FROM entry_tags WHERE entry_id = ?`, entry.ID)
		if err != nil {
			return nil, err
		}
		for tagRows.Next() {
			var tag string
			if err := tagRows.Scan(&tag); err != nil {
				tagRows.Close()
				return nil, err
			}
			entry.Tags = append(entry.Tags, tag)
		}
		tagRows.Close()
		tags := append([]string{}, query.Tags...)
		if query.Tag != "" {
			tags = append(tags, query.Tag)
		}
		if len(tags) > 0 && !hasAllTags(entry.Tags, tags) {
			continue
		}
		score := -ftsScore + businessScore(entry)
		if !query.IncludeContent {
			entry.Content = ""
		}
		results = append(results, Result{Entry: entry, Score: score})
	}
	if len(results) == 0 {
		return shortQueryFallbackSQLite(root, query, tokenizer)
	}
	return RankSearchResults(results, query.Limit), nil
}

func scanSQLiteSearchRow(rows *sql.Rows) (Entry, float64, error) {
	var (
		entry                    Entry
		sourceOfTruth, activeInt int
		updatedAt                string
		expiresAt                sql.NullString
		excerpt                  string
		sourceSessionsJSON       string
		supersedesJSON           string
		supersededByJSON         string
		ftsScore                 float64
	)
	entry.Schema = "worktrail.index.entry.v1"
	if err := rows.Scan(
		&entry.ID, &entry.Scope, &entry.Path, &entry.Type, &entry.Title, &entry.Topic, &entry.Status, &entry.Stage, &entry.Lifecycle,
		&entry.ProjectID, &entry.TaskID, &entry.Visibility,
		&sourceOfTruth, &activeInt, &entry.CandidateType, &expiresAt, &updatedAt, &excerpt, &entry.Content,
		&sourceSessionsJSON, &supersedesJSON, &supersededByJSON, &ftsScore,
	); err != nil {
		return Entry{}, 0, err
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
	return entry, ftsScore, nil
}

func buildFTSMatch(terms []string) string {
	var parts []string
	for _, term := range terms {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		term = strings.ReplaceAll(term, `"`, "")
		parts = append(parts, `"`+term+`"`)
	}
	return strings.Join(parts, " ")
}

func businessScore(entry Entry) float64 {
	score := 0.0
	if entry.Active {
		score += 5
	}
	if entry.SourceOfTruth {
		score += 5
	}
	if len(entry.SupersededBy) > 0 || entry.Lifecycle == "superseded" || knowledge.IsNonCurrentLifecycle(entry.Lifecycle) || entry.Stage == "historical" || entry.Stage == "retired" {
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

func shortQueryFallbackSQLite(root string, query Query, tokenizer Tokenizer) ([]Result, error) {
	entries, err := loadFreshSearchEntries(root)
	if err != nil {
		return nil, err
	}
	return SearchEntries(entries, query), nil
}
