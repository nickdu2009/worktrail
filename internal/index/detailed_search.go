package index

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

const DetailedLexicalLane = "fts5"

// DetailedHit preserves the lexical retrieval signals before business ranking.
// RawScore is SQLite FTS5's raw bm25 score, where lower values rank first.
type DetailedHit struct {
	Entry       Entry    `json:"entry"`
	LexicalLane string   `json:"lexical_lane"`
	LexicalRank int      `json:"lexical_rank"`
	RawScore    float64  `json:"raw_score"`
	Tags        []string `json:"tags"`
}

// DetailedQueryError identifies failures in the detailed lexical query path.
type DetailedQueryError struct {
	Err error
}

func (err *DetailedQueryError) Error() string {
	return fmt.Sprintf("detailed lexical query: %v", err.Err)
}

func (err *DetailedQueryError) Unwrap() error {
	return err.Err
}

// DetailedSearch returns unadjusted FTS5 hits for callers that need lexical
// signals. It intentionally does not fall back to substring matching or apply
// business ranking.
func DetailedSearch(root string, query Query) ([]DetailedHit, error) {
	needle := strings.TrimSpace(query.Content)
	if needle == "" {
		return nil, detailedQueryError(errors.New("content query is required"))
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return nil, detailedQueryError(err)
	}
	if err := refreshSQLite(root, defaultTokenizer); err != nil {
		return nil, detailedQueryError(err)
	}

	db, err := openSQLite(root)
	if err != nil {
		return nil, detailedQueryError(err)
	}
	defer db.Close()

	tokenized := defaultTokenizer.TokenizeQuery(needle)
	terms := tokenized.Terms
	if len(terms) == 0 {
		terms = []string{strings.ToLower(needle)}
	}
	match := buildFTSMatch(terms)
	if match == "" {
		return nil, detailedQueryError(errors.New("content query produced no FTS terms"))
	}

	hits, err := queryDetailedLexicalHits(db, match, query)
	if err != nil {
		return nil, detailedQueryError(err)
	}
	if err := hydrateDetailedHitTags(db, hits); err != nil {
		return nil, detailedQueryError(err)
	}

	tags := append([]string{}, query.Tags...)
	if query.Tag != "" {
		tags = append(tags, query.Tag)
	}
	filtered := hits[:0]
	for _, hit := range hits {
		if len(tags) > 0 && !hasAllTags(hit.Entry.Tags, tags) {
			continue
		}
		if !query.IncludeContent {
			hit.Entry.Content = ""
		}
		hit.LexicalRank = len(filtered) + 1
		filtered = append(filtered, hit)
		if query.Limit > 0 && len(filtered) == query.Limit {
			break
		}
	}
	return filtered, nil
}

func detailedQueryError(err error) error {
	return &DetailedQueryError{Err: err}
}

func queryDetailedLexicalHits(db *sql.DB, match string, query Query) ([]DetailedHit, error) {
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
	sqlQuery += ` ORDER BY fts_score ASC, e.updated_at DESC, e.id ASC`

	rows, err := db.Query(sqlQuery, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var hits []DetailedHit
	for rows.Next() {
		entry, rawScore, err := scanSQLiteSearchRow(rows)
		if err != nil {
			return nil, err
		}
		hits = append(hits, DetailedHit{
			Entry:       entry,
			LexicalLane: DetailedLexicalLane,
			RawScore:    rawScore,
		})
	}
	return hits, rows.Err()
}

func hydrateDetailedHitTags(db *sql.DB, hits []DetailedHit) error {
	if len(hits) == 0 {
		return nil
	}

	entries := make([]Entry, len(hits))
	for i := range hits {
		entries[i] = hits[i].Entry
	}
	if err := hydrateEntryTags(db, entries); err != nil {
		return err
	}
	for i := range hits {
		hits[i].Entry.Tags = entries[i].Tags
		hits[i].Tags = append([]string(nil), entries[i].Tags...)
	}
	return nil
}
