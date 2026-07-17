package generation

import (
	"encoding/binary"
	"math"
	"os"
	"strings"
	"testing"
)

func TestCandidateSchemaSealAndOpenReadOnly(t *testing.T) {
	path := t.TempDir() + "/candidate.sqlite"
	expected := testMetadata()
	candidate, err := CreateCandidate(path, expected)
	if err != nil {
		t.Fatalf("CreateCandidate() error = %v", err)
	}

	assertMetaColumns(t, candidate)
	insertDeterministicChunks(t, candidate)
	assertKNNSmoke(t, candidate)

	if err := SealCandidate(candidate); err != nil {
		t.Fatalf("SealCandidate() error = %v", err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	beforeInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}

	sealed, err := OpenSealed(path, expected)
	if err != nil {
		t.Fatalf("OpenSealed() error = %v", err)
	}
	if _, err := sealed.db.Exec(`INSERT INTO meta (
		schema, generation, profile, model_space, snapshot, sqlite_vec, dimension, build_state
	) VALUES ('write', 'write', 'write', 'write', 'write', 'write', 8, 'sealed')`); err == nil {
		t.Fatal("sealed database accepted a write")
	}
	if err := sealed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() after open error = %v", err)
	}
	afterInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() after open error = %v", err)
	}
	if string(after) != string(before) {
		t.Fatal("read-only validation changed database bytes")
	}
	if !afterInfo.ModTime().Equal(beforeInfo.ModTime()) {
		t.Fatalf("read-only validation changed mtime: got %v, want %v", afterInfo.ModTime(), beforeInfo.ModTime())
	}
}

func TestOpenSealedRejectsMetadataMismatches(t *testing.T) {
	path := t.TempDir() + "/candidate.sqlite"
	expected := testMetadata()
	candidate, err := CreateCandidate(path, expected)
	if err != nil {
		t.Fatalf("CreateCandidate() error = %v", err)
	}
	if err := candidate.SealCandidate(); err != nil {
		t.Fatalf("SealCandidate() error = %v", err)
	}

	cases := []struct {
		name   string
		change func(*Metadata)
		want   string
	}{
		{"schema", func(m *Metadata) { m.Schema = "other-schema" }, "schema mismatch"},
		{"profile", func(m *Metadata) { m.Profile = "other-profile" }, "profile mismatch"},
		{"snapshot", func(m *Metadata) { m.Snapshot = "other-snapshot" }, "snapshot mismatch"},
		{"dimension", func(m *Metadata) { m.Dimension = 7 }, "dimension mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mismatch := expected
			tc.change(&mismatch)
			sealed, err := OpenSealed(path, mismatch)
			if err == nil {
				_ = sealed.Close()
				t.Fatal("OpenSealed() error = nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("OpenSealed() error = %q, want %q", err, tc.want)
			}
		})
	}
}

func testMetadata() Metadata {
	return Metadata{
		Schema:     databaseSchema,
		Generation: "generation-test-001",
		Profile:    "profile-test-001",
		ModelSpace: "model-space-test-001",
		Snapshot:   "snapshot-test-001",
		SQLiteVec:  "sqlite-vec-test",
		Dimension:  8,
	}
}

func assertMetaColumns(t *testing.T, candidate *Candidate) {
	t.Helper()
	rows, err := candidate.db.Query(`PRAGMA table_info(meta)`)
	if err != nil {
		t.Fatalf("table_info(meta) error = %v", err)
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, primaryKey int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatalf("scan meta column error = %v", err)
		}
		columns = append(columns, name)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate meta columns error = %v", err)
	}
	want := []string{
		"schema",
		"generation",
		"profile",
		"model_space",
		"snapshot",
		"sqlite_vec",
		"dimension",
		"build_state",
	}
	if strings.Join(columns, ",") != strings.Join(want, ",") {
		t.Fatalf("meta columns = %v, want %v", columns, want)
	}
}

func insertDeterministicChunks(t *testing.T, candidate *Candidate) {
	t.Helper()
	tx, err := candidate.db.Begin()
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	for rowID, value := range []struct {
		id     string
		vector []float32
	}{
		{"chunk-1", []float32{1, 0, 0, 0, 0, 0, 0, 0}},
		{"chunk-2", []float32{0, 1, 0, 0, 0, 0, 0, 0}},
	} {
		if _, err := tx.Exec(`INSERT INTO chunks (
			chunk_id, scope, document_id, path, chunk_order, heading_breadcrumb,
			source_start, source_end, body, embedding_input, token_count, embedding_hash,
			prev_chunk_id, next_chunk_id, chunker_version
		) VALUES (?, 'project', 'doc-1', 'docs/test.md', ?, '[]', 0, 1, ?, ?, 1, ?, '', '', 'chunker-v1')`,
			value.id,
			rowID,
			"value "+value.id,
			"input "+value.id,
			"hash-"+value.id,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert chunk %d error = %v", rowID, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO chunk_fts (rowid, chunk_id, embedding_input, body) VALUES (?, ?, ?, ?)`,
			rowID+1,
			value.id,
			"input "+value.id,
			"value "+value.id,
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert FTS chunk %d error = %v", rowID, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO chunk_vec (rowid, embedding) VALUES (?, ?)`,
			rowID+1,
			vectorBlob(value.vector),
		); err != nil {
			_ = tx.Rollback()
			t.Fatalf("insert vector %d error = %v", rowID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("Commit() error = %v", err)
	}
}

func assertKNNSmoke(t *testing.T, candidate *Candidate) {
	t.Helper()
	rows, err := candidate.db.Query(`
		SELECT rowid, distance
		FROM chunk_vec
		WHERE embedding MATCH ?
		ORDER BY distance
		LIMIT 2`,
		vectorBlob([]float32{1, 0, 0, 0, 0, 0, 0, 0}),
	)
	if err != nil {
		t.Fatalf("KNN query error = %v", err)
	}
	defer rows.Close()
	var rowIDs []int64
	for rows.Next() {
		var rowID int64
		var distance float64
		if err := rows.Scan(&rowID, &distance); err != nil {
			t.Fatalf("scan KNN result error = %v", err)
		}
		rowIDs = append(rowIDs, rowID)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate KNN result error = %v", err)
	}
	if len(rowIDs) != 2 || rowIDs[0] != 1 {
		t.Fatalf("KNN row IDs = %v, want [1 ...]", rowIDs)
	}
}

func vectorBlob(vector []float32) []byte {
	blob := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(value))
	}
	return blob
}
