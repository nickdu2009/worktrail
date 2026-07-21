// Package generation creates and validates sealed semantic-index candidates.
package generation

import (
	"fmt"
	"strings"
)

const databaseSchema = "worktrail.semantic.generation.sqlite.v2"

// Metadata identifies the immutable inputs and format of one candidate
// database. BuildState is set by CreateCandidate and SealCandidate; callers
// may leave it empty when opening a sealed candidate.
type Metadata struct {
	Schema     string
	Generation string
	Profile    string
	ModelSpace string
	Snapshot   string
	SQLiteVec  string
	Dimension  int
	BuildState string
}

// NewRebuildMetadata creates the static metadata required before rebuilding a
// semantic generation. Rebuild supplies Generation, Snapshot, and BuildState.
func NewRebuildMetadata(recallProfileID, modelSpaceID, sqliteVecVersion string, dimension int) (Metadata, error) {
	if err := validateRebuildMetadata(recallProfileID, modelSpaceID, sqliteVecVersion, dimension); err != nil {
		return Metadata{}, err
	}
	return Metadata{
		Schema:     databaseSchema,
		Profile:    recallProfileID,
		ModelSpace: modelSpaceID,
		SQLiteVec:  sqliteVecVersion,
		Dimension:  dimension,
	}, nil
}

func (m Metadata) validate() error {
	if err := validateMetadataFields([]metadataField{
		{"schema", m.Schema},
		{"generation", m.Generation},
		{"profile", m.Profile},
		{"model-space", m.ModelSpace},
		{"snapshot", m.Snapshot},
		{"sqlite-vec", m.SQLiteVec},
	}); err != nil {
		return err
	}
	return validateMetadataDimension(m.Dimension, m.BuildState)
}

type metadataField struct {
	name  string
	value string
}

func validateRebuildMetadata(recallProfileID, modelSpaceID, sqliteVecVersion string, dimension int) error {
	if err := validateMetadataFields([]metadataField{
		{"profile", recallProfileID},
		{"model-space", modelSpaceID},
		{"sqlite-vec", sqliteVecVersion},
	}); err != nil {
		return err
	}
	return validateMetadataDimension(dimension, "")
}

func validateMetadataFields(fields []metadataField) error {
	for _, field := range fields {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("generation metadata %s is required", field.name)
		}
	}
	return nil
}

func validateMetadataDimension(dimension int, buildState string) error {
	if dimension <= 0 || dimension > 8192 {
		return fmt.Errorf("generation metadata dimension must be between 1 and 8192")
	}
	if buildState != "" && buildState != "candidate" && buildState != "sealed" {
		return fmt.Errorf("generation metadata build-state must be candidate or sealed")
	}
	return nil
}

func candidateSchema(dimension int) []string {
	return []string{
		`CREATE TABLE meta (
			schema TEXT NOT NULL,
			generation TEXT NOT NULL,
			profile TEXT NOT NULL,
			model_space TEXT NOT NULL,
			snapshot TEXT NOT NULL,
			sqlite_vec TEXT NOT NULL,
			dimension INTEGER NOT NULL CHECK (dimension > 0),
			build_state TEXT NOT NULL CHECK (build_state IN ('candidate', 'sealed')),
			UNIQUE (schema, generation, profile, model_space, snapshot, sqlite_vec, dimension)
		)`,
		`CREATE TABLE chunks (
			chunk_id TEXT PRIMARY KEY,
			scope TEXT NOT NULL,
			document_id TEXT NOT NULL,
			path TEXT NOT NULL,
			document_type TEXT NOT NULL,
			document_topic TEXT NOT NULL,
			chunk_order INTEGER NOT NULL,
			chunk_kind TEXT NOT NULL CHECK (chunk_kind IN ('text', 'table_row_group', 'table_cell_fragment')),
			structural_group_id TEXT NOT NULL,
			fragment_ordinal INTEGER NOT NULL,
			heading_breadcrumb TEXT NOT NULL,
			source_byte_length INTEGER NOT NULL CHECK (source_byte_length >= 0),
			source_start INTEGER NOT NULL,
			source_end INTEGER NOT NULL,
			context_start INTEGER,
			context_end INTEGER,
			group_start INTEGER,
			group_end INTEGER,
			body TEXT NOT NULL,
			embedding_input TEXT NOT NULL,
			token_count INTEGER NOT NULL,
			embedding_hash TEXT NOT NULL,
			prev_chunk_id TEXT NOT NULL,
			next_chunk_id TEXT NOT NULL,
			chunker_version TEXT NOT NULL,
			CHECK (source_start >= 0 AND source_start < source_end AND source_end <= source_byte_length),
			CHECK (
				(context_start IS NULL AND context_end IS NULL) OR
				(
					context_start IS NOT NULL AND context_end IS NOT NULL AND
					context_start >= 0 AND context_start < context_end AND
					context_end <= source_byte_length
				)
			),
			CHECK (
				(group_start IS NULL AND group_end IS NULL) OR
				(
					group_start IS NOT NULL AND group_end IS NOT NULL AND
					group_start >= 0 AND group_start < group_end AND
					group_end <= source_byte_length
				)
			),
			CHECK (
				group_start IS NULL OR
				(source_start >= group_start AND source_end <= group_end)
			),
			CHECK (
				context_start IS NULL OR group_start IS NULL OR
				(context_start >= group_start AND context_end <= group_end)
			)
		)`,
		`CREATE VIRTUAL TABLE chunk_fts USING fts5(
			chunk_id UNINDEXED,
			metadata_terms,
			context_terms,
			body_terms,
			tokenize = 'unicode61'
		)`,
		`CREATE TABLE chunk_tags (
			chunk_id TEXT NOT NULL REFERENCES chunks(chunk_id),
			tag TEXT NOT NULL,
			PRIMARY KEY (chunk_id, tag)
		)`,
		fmt.Sprintf(
			`CREATE VIRTUAL TABLE chunk_vec USING vec0(
				embedding FLOAT[%d] distance_metric=cosine
			)`,
			dimension,
		),
	}
}
