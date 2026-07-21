package generation

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
)

const (
	normalizedVectorTolerance = 1e-3
	embeddingBatchSize        = 16
)

// Embedder creates a vector for one embedding input.
type Embedder interface {
	Embed(ctx context.Context, input string) ([]float32, error)
}

// BatchEmbedder optionally creates vectors for multiple embedding inputs. The
// returned vectors must have the same count and order as inputs.
type BatchEmbedder interface {
	Embedder
	EmbedBatch(ctx context.Context, inputs []string) ([][]float32, error)
}

type embeddedChunk struct {
	chunk  chunk.Chunk
	vector []float32
}

// BuildCandidate embeds chunks, then atomically writes their structural,
// lexical, and vector representations to candidate. It does not seal,
// activate, or otherwise publish the candidate.
func BuildCandidate(ctx context.Context, candidate *Candidate, chunks []chunk.Chunk, embedder Embedder, tokenizer index.Tokenizer) error {
	return BuildCandidateWithReuse(ctx, candidate, chunks, embedder, tokenizer, nil)
}

// BuildCandidateWithReuse is BuildCandidate with optional same-profile vector
// reuse keyed by embedding-input hash.
func BuildCandidateWithReuse(
	ctx context.Context,
	candidate *Candidate,
	chunks []chunk.Chunk,
	embedder Embedder,
	tokenizer index.Tokenizer,
	reuse map[string][]float32,
) error {
	if candidate == nil || candidate.db == nil {
		return errors.New("generation candidate is required")
	}
	if embedder == nil {
		return errors.New("generation embedder is required")
	}
	if tokenizer == nil {
		return errors.New("generation tokenizer is required")
	}

	dimension, err := candidateDimension(ctx, candidate.db)
	if err != nil {
		return err
	}
	if err := validateChunkIDs(chunks); err != nil {
		return err
	}

	embedded, err := embedChunks(ctx, chunks, embedder, dimension, reuse)
	if err != nil {
		return err
	}

	tx, err := candidate.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin candidate batch: %w", err)
	}
	if err := writeEmbeddedChunks(ctx, tx, embedded, tokenizer); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit candidate batch: %w", err)
	}
	return nil
}

func embedChunks(
	ctx context.Context,
	chunks []chunk.Chunk,
	embedder Embedder,
	dimension int,
	reuse map[string][]float32,
) ([]embeddedChunk, error) {
	embedded := make([]embeddedChunk, 0, len(chunks))
	pending := make([]chunk.Chunk, 0, len(chunks))
	for _, source := range chunks {
		if vector, ok := reuse[source.EmbeddingHash]; ok {
			if err := validateVector(vector, dimension); err != nil {
				return nil, fmt.Errorf("reuse chunk %q: %w", source.ChunkID, err)
			}
			embedded = append(embedded, embeddedChunk{chunk: source, vector: append([]float32(nil), vector...)})
			continue
		}
		pending = append(pending, source)
	}
	if len(pending) == 0 {
		return embedded, nil
	}

	var fresh []embeddedChunk
	var err error
	if batchEmbedder, ok := embedder.(BatchEmbedder); ok {
		fresh, err = embedChunksInBatches(ctx, pending, batchEmbedder, dimension)
	} else {
		fresh, err = embedChunksOneByOne(ctx, pending, embedder, dimension)
	}
	if err != nil {
		return nil, err
	}
	return append(embedded, fresh...), nil
}

func embedChunksOneByOne(ctx context.Context, chunks []chunk.Chunk, embedder Embedder, dimension int) ([]embeddedChunk, error) {
	embedded := make([]embeddedChunk, 0, len(chunks))
	for _, source := range chunks {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("build candidate: %w", err)
		}
		vector, err := embedder.Embed(ctx, source.EmbeddingInput)
		if err != nil {
			return nil, fmt.Errorf("embed chunk %q: %w", source.ChunkID, err)
		}
		if err := validateVector(vector, dimension); err != nil {
			return nil, fmt.Errorf("embed chunk %q: %w", source.ChunkID, err)
		}
		embedded = append(embedded, embeddedChunk{chunk: source, vector: vector})
	}
	return embedded, nil
}

func embedChunksInBatches(ctx context.Context, chunks []chunk.Chunk, embedder BatchEmbedder, dimension int) ([]embeddedChunk, error) {
	embedded := make([]embeddedChunk, 0, len(chunks))
	for start := 0; start < len(chunks); start += embeddingBatchSize {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("build candidate: %w", err)
		}
		end := start + embeddingBatchSize
		if end > len(chunks) {
			end = len(chunks)
		}

		inputs := make([]string, end-start)
		for index, source := range chunks[start:end] {
			inputs[index] = source.EmbeddingInput
		}
		vectors, err := embedder.EmbedBatch(ctx, inputs)
		if err != nil {
			return nil, fmt.Errorf("embed chunk %q: %w", chunks[start].ChunkID, err)
		}
		if len(vectors) != len(inputs) {
			return nil, fmt.Errorf(
				"embed chunk batch starting at %q: response count is %d, want %d",
				chunks[start].ChunkID,
				len(vectors),
				len(inputs),
			)
		}
		for index, vector := range vectors {
			source := chunks[start+index]
			if err := validateVector(vector, dimension); err != nil {
				return nil, fmt.Errorf("embed chunk %q: %w", source.ChunkID, err)
			}
			embedded = append(embedded, embeddedChunk{chunk: source, vector: vector})
		}
	}
	return embedded, nil
}

func candidateDimension(ctx context.Context, db *sql.DB) (int, error) {
	var dimension int
	var buildState string
	if err := db.QueryRowContext(ctx, `SELECT dimension, build_state FROM meta`).Scan(&dimension, &buildState); err != nil {
		return 0, fmt.Errorf("read candidate metadata: %w", err)
	}
	if buildState != "candidate" {
		return 0, fmt.Errorf("write candidate chunks: build-state is %q", buildState)
	}
	return dimension, nil
}

func validateChunkIDs(chunks []chunk.Chunk) error {
	seen := make(map[string]struct{}, len(chunks))
	for _, source := range chunks {
		if source.ChunkID == "" {
			return errors.New("candidate chunk ID is required")
		}
		if _, exists := seen[source.ChunkID]; exists {
			return fmt.Errorf("duplicate candidate chunk ID %q", source.ChunkID)
		}
		seen[source.ChunkID] = struct{}{}
	}
	return nil
}

func validateVector(vector []float32, dimension int) error {
	if len(vector) != dimension {
		return fmt.Errorf("embedding dimension is %d, want %d", len(vector), dimension)
	}

	var squaredLength float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return errors.New("embedding contains a non-finite value")
		}
		squaredLength += float64(value) * float64(value)
	}
	length := math.Sqrt(squaredLength)
	if math.Abs(length-1) > normalizedVectorTolerance {
		return fmt.Errorf("embedding L2 norm is %g, want 1 within %g", length, normalizedVectorTolerance)
	}
	return nil
}

func writeEmbeddedChunks(ctx context.Context, tx *sql.Tx, embedded []embeddedChunk, tokenizer index.Tokenizer) error {
	for _, value := range embedded {
		if err := validateChunkRanges(value.chunk); err != nil {
			return fmt.Errorf("write chunk %q: %w", value.chunk.ChunkID, err)
		}
		headingBreadcrumb := []byte("[]")
		if value.chunk.HeadingBreadcrumb != nil {
			var err error
			headingBreadcrumb, err = json.Marshal(value.chunk.HeadingBreadcrumb)
			if err != nil {
				return fmt.Errorf("encode chunk %q heading breadcrumb: %w", value.chunk.ChunkID, err)
			}
		}
		contextStart, contextEnd := nullRange(value.chunk.ContextRange)
		groupStart, groupEnd := nullRange(value.chunk.GroupRange)
		kind := value.chunk.Kind
		if kind == "" {
			kind = chunk.KindText
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO chunks (
			chunk_id, scope, document_id, path, document_type, document_topic, chunk_order,
			chunk_kind, structural_group_id, fragment_ordinal, heading_breadcrumb,
			source_byte_length, source_start, source_end, context_start, context_end,
			group_start, group_end, body, embedding_input, token_count, embedding_hash,
			prev_chunk_id, next_chunk_id, chunker_version
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			value.chunk.ChunkID,
			value.chunk.Scope,
			value.chunk.DocumentID,
			value.chunk.Path,
			value.chunk.Type,
			value.chunk.Topic,
			value.chunk.Order,
			kind,
			value.chunk.StructuralGroupID,
			value.chunk.FragmentOrdinal,
			string(headingBreadcrumb),
			value.chunk.SourceSizeByte,
			value.chunk.SourceStart,
			value.chunk.SourceEnd,
			contextStart,
			contextEnd,
			groupStart,
			groupEnd,
			value.chunk.Body,
			value.chunk.EmbeddingInput,
			value.chunk.TokenCount,
			value.chunk.EmbeddingHash,
			value.chunk.PrevChunkID,
			value.chunk.NextChunkID,
			value.chunk.ChunkerVersion,
		)
		if err != nil {
			return fmt.Errorf("write chunk %q: %w", value.chunk.ChunkID, err)
		}
		rowID, err := result.LastInsertId()
		if err != nil {
			return fmt.Errorf("read chunk %q row ID: %w", value.chunk.ChunkID, err)
		}
		metadataTerms, contextTerms, bodyTerms := chunkFTSTerms(tokenizer, value.chunk)
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO chunk_fts (rowid, chunk_id, metadata_terms, context_terms, body_terms) VALUES (?, ?, ?, ?, ?)`,
			rowID,
			value.chunk.ChunkID,
			metadataTerms,
			contextTerms,
			bodyTerms,
		); err != nil {
			return fmt.Errorf("write FTS chunk %q: %w", value.chunk.ChunkID, err)
		}
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO chunk_vec (rowid, embedding) VALUES (?, ?)`,
			rowID,
			encodeVectorBlob(value.vector),
		); err != nil {
			return fmt.Errorf("write vector chunk %q: %w", value.chunk.ChunkID, err)
		}
		for _, tag := range value.chunk.Tags {
			tag = strings.TrimSpace(tag)
			if tag == "" {
				continue
			}
			if _, err := tx.ExecContext(
				ctx,
				`INSERT INTO chunk_tags (chunk_id, tag) VALUES (?, ?)`,
				value.chunk.ChunkID,
				tag,
			); err != nil {
				return fmt.Errorf("write tag for chunk %q: %w", value.chunk.ChunkID, err)
			}
		}
	}
	return nil
}

func validateChunkRanges(source chunk.Chunk) error {
	if source.SourceSizeByte < 0 {
		return errors.New("source_byte_length must be non-negative")
	}
	if source.SourceStart < 0 || source.SourceStart >= source.SourceEnd || source.SourceEnd > source.SourceSizeByte {
		return fmt.Errorf("invalid primary range [%d,%d) for source size %d", source.SourceStart, source.SourceEnd, source.SourceSizeByte)
	}
	if source.ContextRange != nil {
		if source.ContextRange.Start < 0 || source.ContextRange.Start >= source.ContextRange.End || source.ContextRange.End > source.SourceSizeByte {
			return fmt.Errorf("invalid context range [%d,%d)", source.ContextRange.Start, source.ContextRange.End)
		}
	}
	if source.GroupRange != nil {
		if source.GroupRange.Start < 0 || source.GroupRange.Start >= source.GroupRange.End || source.GroupRange.End > source.SourceSizeByte {
			return fmt.Errorf("invalid group range [%d,%d)", source.GroupRange.Start, source.GroupRange.End)
		}
		if source.SourceStart < source.GroupRange.Start || source.SourceEnd > source.GroupRange.End {
			return errors.New("primary range escapes structural group")
		}
		if source.ContextRange != nil &&
			(source.ContextRange.Start < source.GroupRange.Start || source.ContextRange.End > source.GroupRange.End) {
			return errors.New("context range escapes structural group")
		}
	}
	return nil
}

func nullRange(r *chunk.ByteRange) (any, any) {
	if r == nil {
		return nil, nil
	}
	return r.Start, r.End
}

func chunkFTSTerms(tokenizer index.Tokenizer, source chunk.Chunk) (metadataTerms, contextTerms, bodyTerms string) {
	metadataDoc := tokenizer.TokenizeDocument(index.DocumentForTokenize{
		Path:    source.Path,
		Title:   "",
		Topic:   source.Topic,
		Tags:    source.Tags,
		Type:    source.Type,
		Content: source.MetadataTerms,
	})
	contextDoc := tokenizer.TokenizeDocument(index.DocumentForTokenize{
		Content: source.ContextTerms,
	})
	bodyDoc := tokenizer.TokenizeDocument(index.DocumentForTokenize{
		Path:    source.Path,
		Type:    source.Type,
		Content: source.Body,
	})
	return joinTerms(metadataDoc.AllTerms), joinTerms(contextDoc.BodyTerms), joinTerms(append(append([]string{}, bodyDoc.BodyTerms...), bodyDoc.IdentTerms...))
}

func joinTerms(terms []string) string {
	if len(terms) == 0 {
		return ""
	}
	return strings.Join(terms, " ")
}

func encodeVectorBlob(vector []float32) []byte {
	blob := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(value))
	}
	return blob
}
