package generation

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// QueryErrorKind identifies the stage that rejected or failed a generation
// query. It lets callers distinguish malformed input from a sealed metadata or
// SQLite query failure without depending on a retrieval package.
type QueryErrorKind string

const (
	QueryErrorInvalidInput QueryErrorKind = "invalid_input"
	QueryErrorMetadata     QueryErrorKind = "metadata"
	QueryErrorQuery        QueryErrorKind = "query"
)

// QueryError reports a query failure from an active generation.
type QueryError struct {
	Kind      QueryErrorKind
	Operation string
	Err       error
}

func (e *QueryError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return fmt.Sprintf("semantic generation %s %s error", e.Operation, e.Kind)
	}
	return fmt.Sprintf("semantic generation %s %s error: %v", e.Operation, e.Kind, e.Err)
}

// Unwrap returns the underlying SQLite or validation error.
func (e *QueryError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func newQueryError(kind QueryErrorKind, operation string, err error) error {
	return &QueryError{Kind: kind, Operation: operation, Err: err}
}

// ChunkHit is one chunk returned from a lexical or vector generation query.
// EntryID is the source document identifier retained in the generation.
type ChunkHit struct {
	ChunkID       string
	EntryID       string
	Path          string
	DocumentType  string
	DocumentTopic string
	ChunkOrder    int
	PrevChunkID   string
	NextChunkID   string
	Rank          int
	Score         float64
	Distance      float64
}

// ChunkFilters are exact metadata predicates pushed into generation SQL when
// supported. Empty fields are ignored.
type ChunkFilters struct {
	Type  string
	Topic string
	Tag   string
}

// ChunkFTS performs an exact FTS5 term query against the already-open active
// generation. Prefer ChunkFTSTerms when the caller already holds a tokenizer.
func (a *Active) ChunkFTS(query string, limit int) ([]ChunkHit, error) {
	return a.ChunkFTSFiltered(query, ChunkFilters{}, limit)
}

// ChunkFTSFiltered performs an exact FTS5 term query with optional SQL filters.
func (a *Active) ChunkFTSFiltered(query string, filters ChunkFilters, limit int) ([]ChunkHit, error) {
	match, err := chunkFTSMatch(strings.Fields(query))
	if err != nil {
		return nil, newQueryError(QueryErrorInvalidInput, "chunk FTS", err)
	}
	return a.chunkFTSMatchLimit(match, filters, limit)
}

// ChunkFTSTerms performs an exact FTS5 term query using pre-tokenized terms.
func (a *Active) ChunkFTSTerms(terms []string, limit int) ([]ChunkHit, error) {
	return a.ChunkFTSTermsFiltered(terms, ChunkFilters{}, limit)
}

// ChunkFTSTermsFiltered performs an exact FTS5 term query with optional SQL filters.
func (a *Active) ChunkFTSTermsFiltered(terms []string, filters ChunkFilters, limit int) ([]ChunkHit, error) {
	match, err := chunkFTSMatch(terms)
	if err != nil {
		return nil, newQueryError(QueryErrorInvalidInput, "chunk FTS", err)
	}
	return a.chunkFTSMatchLimit(match, filters, limit)
}

func (a *Active) chunkFTSMatchLimit(match string, filters ChunkFilters, limit int) ([]ChunkHit, error) {
	if err := validateSearchLimit(limit); err != nil {
		return nil, newQueryError(QueryErrorInvalidInput, "chunk FTS", err)
	}

	query, args := chunkFTSStatement(match, filters, limit)
	var hits []ChunkHit
	err := a.withReadOnlyDB(func(db *sql.DB) error {
		rows, err := db.Query(query, args...)
		if err != nil {
			return newQueryError(QueryErrorQuery, "chunk FTS", err)
		}
		defer rows.Close()

		for rows.Next() {
			var hit ChunkHit
			if err := rows.Scan(
				&hit.ChunkID,
				&hit.EntryID,
				&hit.Path,
				&hit.DocumentType,
				&hit.DocumentTopic,
				&hit.ChunkOrder,
				&hit.PrevChunkID,
				&hit.NextChunkID,
				&hit.Score,
			); err != nil {
				return newQueryError(QueryErrorQuery, "chunk FTS", err)
			}
			hit.Rank = len(hits) + 1
			hits = append(hits, hit)
		}
		if err := rows.Err(); err != nil {
			return newQueryError(QueryErrorQuery, "chunk FTS", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

func chunkFTSStatement(match string, filters ChunkFilters, limit int) (string, []any) {
	var b strings.Builder
	b.WriteString(`
			SELECT
				chunks.chunk_id,
				chunks.document_id,
				chunks.path,
				chunks.document_type,
				chunks.document_topic,
				chunks.chunk_order,
				chunks.prev_chunk_id,
				chunks.next_chunk_id,
				bm25(chunk_fts, 0.0, 5.0, 1.0, 3.0)
			FROM chunk_fts
			JOIN chunks ON chunks.rowid = chunk_fts.rowid
			WHERE chunk_fts MATCH ?`)
	args := []any{match}
	if typeFilter := strings.TrimSpace(filters.Type); typeFilter != "" {
		b.WriteString(` AND chunks.document_type = ?`)
		args = append(args, typeFilter)
	}
	if topicFilter := strings.TrimSpace(filters.Topic); topicFilter != "" {
		b.WriteString(` AND chunks.document_topic = ?`)
		args = append(args, topicFilter)
	}
	if tagFilter := strings.TrimSpace(filters.Tag); tagFilter != "" {
		b.WriteString(` AND EXISTS (
			SELECT 1 FROM chunk_tags
			WHERE chunk_tags.chunk_id = chunks.chunk_id AND chunk_tags.tag = ?
		)`)
		args = append(args, tagFilter)
	}
	b.WriteString(`
			ORDER BY bm25(chunk_fts, 0.0, 5.0, 1.0, 3.0) ASC, chunks.chunk_id ASC
			LIMIT ?`)
	args = append(args, limit)
	return b.String(), args
}

// VectorKNN performs an exact cosine KNN query against the already-open active
// generation. sqlite-vec receives k equal to limit; distance and chunk-ID
// ordering only resolves ties within that exact top-K candidate set. The caller
// supplies an already normalized embedding; this method never invokes an
// embedder.
func (a *Active) VectorKNN(embedding []float32, limit int) ([]ChunkHit, error) {
	if err := validateSearchLimit(limit); err != nil {
		return nil, newQueryError(QueryErrorInvalidInput, "vector KNN", err)
	}

	var hits []ChunkHit
	err := a.withReadOnlyDB(func(db *sql.DB) error {
		dimension, err := sealedDimension(db)
		if err != nil {
			return newQueryError(QueryErrorMetadata, "vector KNN", err)
		}
		if err := validateVector(embedding, dimension); err != nil {
			return newQueryError(QueryErrorInvalidInput, "vector KNN", err)
		}

		rows, err := vectorKNNRows(db, embedding, limit)
		if err != nil {
			return newQueryError(QueryErrorQuery, "vector KNN", err)
		}
		defer rows.Close()

		for rows.Next() {
			var hit ChunkHit
			if err := rows.Scan(
				&hit.ChunkID,
				&hit.EntryID,
				&hit.Path,
				&hit.DocumentType,
				&hit.DocumentTopic,
				&hit.ChunkOrder,
				&hit.PrevChunkID,
				&hit.NextChunkID,
				&hit.Distance,
			); err != nil {
				return newQueryError(QueryErrorQuery, "vector KNN", err)
			}
			hit.Rank = len(hits) + 1
			hits = append(hits, hit)
		}
		if err := rows.Err(); err != nil {
			return newQueryError(QueryErrorQuery, "vector KNN", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hits, nil
}

// EntryTagSets loads distinct tags for the requested entry IDs from the sealed
// generation. Missing entries are omitted so callers can detect them.
func (a *Active) EntryTagSets(entryIDs []string) (map[string][]string, error) {
	unique := uniqueNonEmpty(entryIDs)
	if len(unique) == 0 {
		return map[string][]string{}, nil
	}

	placeholders := make([]string, len(unique))
	args := make([]any, len(unique))
	for i, id := range unique {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `
		SELECT chunks.document_id, chunk_tags.tag
		FROM chunks
		JOIN chunk_tags ON chunk_tags.chunk_id = chunks.chunk_id
		WHERE chunks.document_id IN (` + strings.Join(placeholders, ",") + `)
		ORDER BY chunks.document_id ASC, chunk_tags.tag ASC`

	out := make(map[string][]string, len(unique))
	err := a.withReadOnlyDB(func(db *sql.DB) error {
		rows, err := db.Query(query, args...)
		if err != nil {
			return newQueryError(QueryErrorQuery, "entry tags", err)
		}
		defer rows.Close()
		for rows.Next() {
			var entryID, tag string
			if err := rows.Scan(&entryID, &tag); err != nil {
				return newQueryError(QueryErrorQuery, "entry tags", err)
			}
			out[entryID] = append(out[entryID], tag)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

const vectorKNNQuery = `
	SELECT
		chunks.chunk_id,
		chunks.document_id,
		chunks.path,
		chunks.document_type,
		chunks.document_topic,
		chunks.chunk_order,
		chunks.prev_chunk_id,
		chunks.next_chunk_id,
		nearest.distance
	FROM (
		SELECT rowid, distance
		FROM chunk_vec
		WHERE embedding MATCH ? AND k = ?
	) AS nearest
	JOIN chunks ON chunks.rowid = nearest.rowid
	ORDER BY nearest.distance ASC, chunks.chunk_id ASC
	LIMIT ?`

func vectorKNNRows(db *sql.DB, embedding []float32, limit int) (*sql.Rows, error) {
	query, args := vectorKNNStatement(embedding, limit)
	return db.Query(query, args...)
}

func vectorKNNStatement(embedding []float32, limit int) (string, []any) {
	return vectorKNNQuery, []any{encodeVectorBlob(embedding), limit, limit}
}

func validateSearchLimit(limit int) error {
	if limit <= 0 {
		return errors.New("search limit must be greater than zero")
	}
	return nil
}

func chunkFTSMatch(terms []string) (string, error) {
	parts := make([]string, 0, len(terms))
	for _, term := range terms {
		term = strings.TrimSpace(term)
		term = strings.ReplaceAll(term, `"`, "")
		if term != "" {
			parts = append(parts, `"`+term+`"`)
		}
	}
	if len(parts) == 0 {
		return "", errors.New("search query produced no FTS terms")
	}
	return strings.Join(parts, " "), nil
}

func sealedDimension(db *sql.DB) (int, error) {
	var dimension int
	var buildState string
	if err := db.QueryRow(`SELECT dimension, build_state FROM meta`).Scan(&dimension, &buildState); err != nil {
		return 0, fmt.Errorf("read sealed metadata: %w", err)
	}
	if buildState != "sealed" {
		return 0, fmt.Errorf("sealed metadata build-state is %q", buildState)
	}
	if dimension <= 0 {
		return 0, fmt.Errorf("sealed metadata dimension is %d", dimension)
	}
	return dimension, nil
}

func uniqueNonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}
