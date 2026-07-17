//go:build linux || darwin || freebsd || openbsd || windows

package eval

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	_ "modernc.org/sqlite"
	_ "modernc.org/sqlite/vec"
)

const VecReportSchema = "worktrail.semantic.eval.vec.v1"

type VecOptions struct {
	Path      string
	Count     int
	Dimension int
	Queries   int
	Limit     int
	Seed      uint64
}

type VecReport struct {
	Schema       string  `json:"schema"`
	VecVersion   string  `json:"vec_version"`
	Count        int     `json:"count"`
	Dimension    int     `json:"dimension"`
	Queries      int     `json:"queries"`
	Limit        int     `json:"limit"`
	DatabasePath string  `json:"database_path,omitempty"`
	DatabaseSize int64   `json:"database_size"`
	InsertMS     float64 `json:"insert_ms"`
	QueryP50MS   float64 `json:"query_p50_ms"`
	QueryP95MS   float64 `json:"query_p95_ms"`
}

func RunVecSpike(ctx context.Context, opts VecOptions) (VecReport, error) {
	if err := validateVecOptions(opts); err != nil {
		return VecReport{}, err
	}
	path, temporary, err := vecDatabasePath(opts.Path)
	if err != nil {
		return VecReport{}, err
	}
	if temporary {
		defer func() {
			_ = os.Remove(path)
			_ = os.Remove(path + "-shm")
			_ = os.Remove(path + "-wal")
		}()
	}

	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)")
	if err != nil {
		return VecReport{}, err
	}
	defer db.Close()

	var version string
	if err := db.QueryRowContext(ctx, `SELECT vec_version()`).Scan(&version); err != nil {
		return VecReport{}, fmt.Errorf("sqlite-vec unavailable: %w", err)
	}
	schema := fmt.Sprintf(`CREATE VIRTUAL TABLE vectors USING vec0(embedding FLOAT[%d] distance_metric=cosine)`, opts.Dimension)
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return VecReport{}, err
	}

	insertStart := time.Now()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return VecReport{}, err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO vectors(rowid, embedding) VALUES(?, ?)`)
	if err != nil {
		_ = tx.Rollback()
		return VecReport{}, err
	}
	for i := 0; i < opts.Count; i++ {
		vector := syntheticVector(opts.Dimension, opts.Seed, i)
		if _, err := stmt.ExecContext(ctx, i+1, vectorBlob(vector)); err != nil {
			_ = stmt.Close()
			_ = tx.Rollback()
			return VecReport{}, err
		}
	}
	if err := stmt.Close(); err != nil {
		_ = tx.Rollback()
		return VecReport{}, err
	}
	if err := tx.Commit(); err != nil {
		return VecReport{}, err
	}
	insertDuration := time.Since(insertStart)

	queryDurations := make([]time.Duration, 0, opts.Queries)
	for i := 0; i < opts.Queries; i++ {
		query := syntheticVector(opts.Dimension, opts.Seed, i%opts.Count)
		started := time.Now()
		rows, err := db.QueryContext(ctx, `
SELECT rowid, distance
FROM vectors
WHERE embedding MATCH ?
ORDER BY distance
LIMIT ?`, vectorBlob(query), opts.Limit)
		if err != nil {
			return VecReport{}, err
		}
		var previous = -1.0
		var count int
		for rows.Next() {
			var rowID int64
			var distance float64
			if err := rows.Scan(&rowID, &distance); err != nil {
				rows.Close()
				return VecReport{}, err
			}
			if distance < previous {
				rows.Close()
				return VecReport{}, errors.New("sqlite-vec returned distances out of order")
			}
			previous = distance
			count++
		}
		if err := rows.Close(); err != nil {
			return VecReport{}, err
		}
		if count != min(opts.Limit, opts.Count) {
			return VecReport{}, fmt.Errorf("sqlite-vec returned %d rows, want %d", count, min(opts.Limit, opts.Count))
		}
		queryDurations = append(queryDurations, time.Since(started))
	}

	if _, err := db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		return VecReport{}, err
	}
	if err := db.Close(); err != nil {
		return VecReport{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return VecReport{}, err
	}
	sort.Slice(queryDurations, func(i, j int) bool { return queryDurations[i] < queryDurations[j] })
	report := VecReport{
		Schema:       VecReportSchema,
		VecVersion:   version,
		Count:        opts.Count,
		Dimension:    opts.Dimension,
		Queries:      opts.Queries,
		Limit:        opts.Limit,
		DatabaseSize: info.Size(),
		InsertMS:     milliseconds(insertDuration),
		QueryP50MS:   milliseconds(percentile(queryDurations, 0.50)),
		QueryP95MS:   milliseconds(percentile(queryDurations, 0.95)),
	}
	if !temporary {
		report.DatabasePath = path
	}
	return report, nil
}

func validateVecOptions(opts VecOptions) error {
	switch {
	case opts.Count <= 0 || opts.Count > 100_000:
		return errors.New("count must be between 1 and 100000")
	case opts.Dimension <= 0 || opts.Dimension > 8192:
		return errors.New("dimension must be between 1 and 8192")
	case opts.Queries <= 0 || opts.Queries > 1000:
		return errors.New("queries must be between 1 and 1000")
	case opts.Limit <= 0 || opts.Limit > 1000:
		return errors.New("limit must be between 1 and 1000")
	default:
		return nil
	}
}

func vecDatabasePath(path string) (string, bool, error) {
	if path != "" {
		absolute, err := filepath.Abs(path)
		if err != nil {
			return "", false, err
		}
		if _, err := os.Stat(absolute); err == nil {
			return "", false, fmt.Errorf("database path already exists: %s", absolute)
		} else if !os.IsNotExist(err) {
			return "", false, err
		}
		return absolute, false, nil
	}
	file, err := os.CreateTemp("", "worktrail-semantic-vec-*.sqlite")
	if err != nil {
		return "", false, err
	}
	path = file.Name()
	if err := file.Close(); err != nil {
		return "", false, err
	}
	if err := os.Remove(path); err != nil {
		return "", false, err
	}
	return path, true, nil
}

func syntheticVector(dimension int, seed uint64, index int) []float32 {
	state := seed ^ (uint64(index+1) * 0x9e3779b97f4a7c15)
	vector := make([]float32, dimension)
	var squared float64
	for i := range vector {
		state = state*6364136223846793005 + 1442695040888963407
		value := float64(int32(state>>32)) / float64(math.MaxInt32)
		vector[i] = float32(value)
		squared += value * value
	}
	scale := float32(1 / math.Sqrt(squared))
	for i := range vector {
		vector[i] *= scale
	}
	return vector
}

func vectorBlob(vector []float32) []byte {
	blob := make([]byte, len(vector)*4)
	for i, value := range vector {
		binary.LittleEndian.PutUint32(blob[i*4:], math.Float32bits(value))
	}
	return blob
}

func percentile(values []time.Duration, fraction float64) time.Duration {
	index := int(math.Ceil(float64(len(values))*fraction)) - 1
	if index < 0 {
		index = 0
	}
	return values[index]
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}
