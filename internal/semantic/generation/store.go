package generation

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

// Candidate is the only writable handle for an unsealed candidate database.
type Candidate struct {
	db   *sql.DB
	path string
}

// SealedCandidate is an immutable, read-only candidate database handle.
type SealedCandidate struct {
	db *sql.DB
}

// CreateCandidate creates a new candidate database at path. The path must not
// already exist, so an existing candidate can never be overwritten.
func CreateCandidate(path string, metadata Metadata) (*Candidate, error) {
	if err := metadata.validate(); err != nil {
		return nil, err
	}
	if metadata.BuildState != "" && metadata.BuildState != "candidate" {
		return nil, errors.New("generation metadata build-state must be candidate when creating a candidate")
	}

	path, err := candidatePath(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Lstat(path); err == nil {
		return nil, fmt.Errorf("candidate database already exists: %s", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("inspect candidate database path: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open candidate database: %w", err)
	}
	candidate := &Candidate{db: db, path: path}
	if err := candidate.initialize(metadata); err != nil {
		_ = db.Close()
		_ = os.Remove(path)
		return nil, err
	}
	return candidate, nil
}

func (c *Candidate) initialize(metadata Metadata) error {
	if _, err := c.db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		return fmt.Errorf("enable candidate foreign keys: %w", err)
	}
	if _, err := c.db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
		return fmt.Errorf("enable candidate WAL: %w", err)
	}
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("begin candidate schema transaction: %w", err)
	}
	for _, statement := range candidateSchema(metadata.Dimension) {
		if _, err := tx.Exec(statement); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("create candidate schema: %w", err)
		}
	}
	if _, err := tx.Exec(
		`INSERT INTO meta (
			schema, generation, profile, model_space, snapshot, sqlite_vec, dimension, build_state
		) VALUES (?, ?, ?, ?, ?, ?, ?, 'candidate')`,
		metadata.Schema,
		metadata.Generation,
		metadata.Profile,
		metadata.ModelSpace,
		metadata.Snapshot,
		metadata.SQLiteVec,
		metadata.Dimension,
	); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("write candidate metadata: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit candidate schema: %w", err)
	}
	return nil
}

// SealCandidate writes the sealed marker, checkpoints all writes, and closes
// the writable database handle.
func (c *Candidate) SealCandidate() error {
	if c == nil || c.db == nil {
		return errors.New("candidate database is already closed")
	}
	db := c.db
	c.db = nil

	result, err := db.Exec(`UPDATE meta SET build_state = 'sealed' WHERE build_state = 'candidate'`)
	if err == nil {
		var affected int64
		affected, err = result.RowsAffected()
		if err == nil && affected != 1 {
			err = fmt.Errorf("seal candidate: expected one metadata row, updated %d", affected)
		}
	}
	if err == nil {
		_, err = db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	}
	if err == nil {
		_, err = db.Exec(`PRAGMA journal_mode = DELETE`)
	}
	closeErr := db.Close()
	if err != nil {
		return fmt.Errorf("seal candidate: %w", err)
	}
	if closeErr != nil {
		return fmt.Errorf("close sealed candidate: %w", closeErr)
	}
	return nil
}

// SealCandidate seals candidate. It is provided for callers that prefer a
// package-level operation over the Candidate method.
func SealCandidate(candidate *Candidate) error {
	if candidate == nil {
		return errors.New("candidate is required")
	}
	return candidate.SealCandidate()
}

// OpenSealed opens and validates a sealed candidate through an immutable
// read-only SQLite URI. It performs no DDL, DML, or repair.
func OpenSealed(path string, expected Metadata) (*SealedCandidate, error) {
	if err := expected.validate(); err != nil {
		return nil, err
	}
	if expected.BuildState != "" && expected.BuildState != "sealed" {
		return nil, errors.New("expected metadata build-state must be sealed or empty")
	}
	path, err := candidatePath(path)
	if err != nil {
		return nil, err
	}
	if info, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("stat sealed candidate: %w", err)
	} else if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("sealed candidate is not a regular file: %s", path)
	}

	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return nil, fmt.Errorf("open sealed candidate: %w", err)
	}
	sealed := &SealedCandidate{db: db}
	actual, err := sealed.metadata()
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if actual.BuildState != "sealed" {
		_ = db.Close()
		return nil, fmt.Errorf("sealed candidate build-state is %q, want %q", actual.BuildState, "sealed")
	}
	if err := compareMetadata(actual, expected); err != nil {
		_ = db.Close()
		return nil, err
	}
	return sealed, nil
}

func (s *SealedCandidate) metadata() (Metadata, error) {
	if s == nil || s.db == nil {
		return Metadata{}, errors.New("sealed candidate is closed")
	}
	rows, err := s.db.Query(`
		SELECT schema, generation, profile, model_space, snapshot, sqlite_vec, dimension, build_state
		FROM meta`)
	if err != nil {
		return Metadata{}, fmt.Errorf("read sealed candidate metadata: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Metadata{}, fmt.Errorf("read sealed candidate metadata: %w", err)
		}
		return Metadata{}, errors.New("sealed candidate metadata is missing")
	}
	var metadata Metadata
	if err := rows.Scan(
		&metadata.Schema,
		&metadata.Generation,
		&metadata.Profile,
		&metadata.ModelSpace,
		&metadata.Snapshot,
		&metadata.SQLiteVec,
		&metadata.Dimension,
		&metadata.BuildState,
	); err != nil {
		return Metadata{}, fmt.Errorf("read sealed candidate metadata: %w", err)
	}
	if rows.Next() {
		return Metadata{}, errors.New("sealed candidate has multiple metadata rows")
	}
	if err := rows.Err(); err != nil {
		return Metadata{}, fmt.Errorf("read sealed candidate metadata: %w", err)
	}
	return metadata, nil
}

// Close releases the read-only database handle.
func (s *SealedCandidate) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	db := s.db
	s.db = nil
	return db.Close()
}

func compareMetadata(actual, expected Metadata) error {
	fields := []struct {
		name string
		got  string
		want string
	}{
		{"schema", actual.Schema, expected.Schema},
		{"generation", actual.Generation, expected.Generation},
		{"profile", actual.Profile, expected.Profile},
		{"model-space", actual.ModelSpace, expected.ModelSpace},
		{"snapshot", actual.Snapshot, expected.Snapshot},
		{"sqlite-vec", actual.SQLiteVec, expected.SQLiteVec},
	}
	for _, field := range fields {
		if field.got != field.want {
			return fmt.Errorf("sealed candidate %s mismatch: got %q, want %q", field.name, field.got, field.want)
		}
	}
	if actual.Dimension != expected.Dimension {
		return fmt.Errorf("sealed candidate dimension mismatch: got %d, want %d", actual.Dimension, expected.Dimension)
	}
	return nil
}

func candidatePath(path string) (string, error) {
	if path == "" {
		return "", errors.New("candidate database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve candidate database path: %w", err)
	}
	return absolute, nil
}

func readOnlyURI(path string) string {
	return (&url.URL{
		Scheme:   "file",
		Path:     path,
		RawQuery: "mode=ro&immutable=1",
	}).String()
}
