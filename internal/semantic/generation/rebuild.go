package generation

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/policy"
)

// ErrSourcesChanged reports that the selected source corpus did not converge
// within the bounded catch-up policy. The sealed candidate is intentionally
// retained for explicit garbage collection, but is never activated.
var ErrSourcesChanged = errors.New("semantic rebuild sources changed")

const (
	defaultCatchUpPasses = 3
	defaultCatchUpBudget = 30 * time.Second
)

// SubsystemVersions mirrors profile.SubsystemVersions for rebuild identity
// binding without importing profile (avoids an import cycle through daemon).
type SubsystemVersions struct {
	ChunkerVersion   string
	IndexingVersion  string
	LexicalVersion   string
	SQLiteVecVersion string
}

// RebuildRequest defines the complete local-only input for rebuilding one
// semantic generation. Metadata supplies the static candidate identity;
// Generation and Snapshot are derived from the rebuild itself.
type RebuildRequest struct {
	Scope        string
	Root         string
	SemanticDir  string
	GenerationID string
	BundleID     string
	Metadata     Metadata
	Policy       chunk.ChunkingPolicy
	Versions     SubsystemVersions
	TokenCounter contracts.TokenCounter
	Embedder     Embedder
	Tokenizer    index.Tokenizer

	// Now and ScanSources are optional test hooks. Catch-up budget excludes the
	// initial S0 build.
	Now              func() time.Time
	ScanSources      func(root, scope string) (index.SourceSnapshot, error)
	MaxCatchUpPasses int
	CatchUpBudget    time.Duration
}

// Rebuild creates, seals, validates, and activates one immutable candidate.
// It does not start a daemon, perform retrieval, retire prior generations, or
// remove unsuccessful candidates.
func Rebuild(ctx context.Context, request RebuildRequest) (Pointer, error) {
	if err := validateRebuildRequest(request); err != nil {
		return Pointer{}, err
	}
	tokenizer := request.Tokenizer
	if tokenizer == nil {
		tokenizer = index.NewTokenizer()
	}
	scan := request.ScanSources
	if scan == nil {
		scan = policy.FreshSources
	}
	now := request.Now
	if now == nil {
		now = time.Now
	}
	maxPasses := request.MaxCatchUpPasses
	if maxPasses <= 0 {
		maxPasses = defaultCatchUpPasses
	}
	budget := request.CatchUpBudget
	if budget <= 0 {
		budget = defaultCatchUpBudget
	}

	candidatePath, err := generationPath(request.SemanticDir, request.GenerationID, ".sqlite")
	if err != nil {
		return Pointer{}, fmt.Errorf("resolve rebuild candidate path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(candidatePath), 0o700); err != nil {
		return Pointer{}, fmt.Errorf("create semantic generation directory: %w", err)
	}

	sources, err := scan(request.Root, request.Scope)
	if err != nil {
		return Pointer{}, fmt.Errorf("read rebuild sources: %w", err)
	}
	chunks, err := sourceChunks(ctx, sources.Records, request.TokenCounter, request.Policy)
	if err != nil {
		return Pointer{}, err
	}

	metadata := request.Metadata
	metadata.Schema = databaseSchema
	metadata.Generation = request.GenerationID
	metadata.Snapshot = sources.SemanticSnapshotHash
	metadata.BuildState = ""
	candidate, err := CreateCandidate(candidatePath, metadata)
	if err != nil {
		return Pointer{}, fmt.Errorf("create rebuild candidate: %w", err)
	}
	defer func() {
		if candidate.db != nil {
			_ = candidate.db.Close()
		}
	}()

	reuse, err := loadReusableVectors(ctx, request.SemanticDir, metadata.Profile)
	if err != nil {
		return Pointer{}, err
	}
	if err := BuildCandidateWithReuse(ctx, candidate, chunks, request.Embedder, tokenizer, reuse); err != nil {
		return Pointer{}, fmt.Errorf("build rebuild candidate: %w", err)
	}

	built := sources
	catchUpStarted := now()
	for pass := 0; pass < maxPasses; pass++ {
		if now().Sub(catchUpStarted) > budget {
			return Pointer{}, fmt.Errorf("%w: catch-up budget exceeded", ErrSourcesChanged)
		}
		current, err := scan(request.Root, request.Scope)
		if err != nil {
			return Pointer{}, fmt.Errorf("recheck rebuild sources: %w", err)
		}
		if current.SemanticSnapshotHash == built.SemanticSnapshotHash {
			built = current
			break
		}
		if err := applySourceCatchUp(ctx, candidate, built, current, request, tokenizer, reuse); err != nil {
			return Pointer{}, err
		}
		built = current
		if pass == maxPasses-1 {
			final, err := scan(request.Root, request.Scope)
			if err != nil {
				return Pointer{}, fmt.Errorf("recheck rebuild sources: %w", err)
			}
			if final.SemanticSnapshotHash != built.SemanticSnapshotHash {
				return Pointer{}, fmt.Errorf("%w: catch-up did not converge", ErrSourcesChanged)
			}
			built = final
		}
	}
	if now().Sub(catchUpStarted) > budget {
		return Pointer{}, fmt.Errorf("%w: catch-up budget exceeded", ErrSourcesChanged)
	}

	if err := candidate.UpdateSnapshot(ctx, built.SemanticSnapshotHash); err != nil {
		return Pointer{}, fmt.Errorf("update rebuild snapshot: %w", err)
	}
	metadata.Snapshot = built.SemanticSnapshotHash
	if err := SealCandidate(candidate); err != nil {
		return Pointer{}, fmt.Errorf("seal rebuild candidate: %w", err)
	}

	pointer, err := Activate(ctx, request.SemanticDir, func(context.Context) (ActivationCandidate, error) {
		sealed, err := OpenSealed(candidatePath, metadata)
		if err != nil {
			return ActivationCandidate{}, err
		}
		if err := sealed.Close(); err != nil {
			return ActivationCandidate{}, fmt.Errorf("close sealed rebuild candidate: %w", err)
		}
		return ActivationCandidate{
			Scope:           request.Scope,
			GenerationID:    request.GenerationID,
			RecallProfileID: metadata.Profile,
			BundleID:        request.BundleID,
			SnapshotHash:    built.SemanticSnapshotHash,
		}, nil
	}, ActivateOptions{FinalSnapshot: func(context.Context) (string, error) {
		current, err := scan(request.Root, request.Scope)
		if err != nil {
			return "", err
		}
		return current.SemanticSnapshotHash, nil
	}})
	if err != nil {
		return Pointer{}, fmt.Errorf("activate rebuild candidate: %w", err)
	}
	active, err := ReadActive(request.SemanticDir)
	if err != nil {
		return Pointer{}, fmt.Errorf("read activated semantic generation: %w", err)
	}
	if active.GenerationID != pointer.GenerationID ||
		active.RecallProfileID != pointer.RecallProfileID ||
		active.BundleID != pointer.BundleID ||
		active.SnapshotHash != pointer.SnapshotHash ||
		active.SnapshotHash != built.SemanticSnapshotHash {
		return Pointer{}, errors.New("activated semantic generation pointer does not match rebuild candidate")
	}
	return active, nil
}

func validateRebuildRequest(request RebuildRequest) error {
	if err := request.Versions.validate(); err != nil {
		return fmt.Errorf("rebuild subsystem versions: %w", err)
	}
	if err := request.Policy.Validate(); err != nil {
		return fmt.Errorf("rebuild chunking policy: %w", err)
	}
	if request.Versions.ChunkerVersion != request.Policy.Version {
		return fmt.Errorf(
			"rebuild chunker version mismatch: versions %q != policy %q",
			request.Versions.ChunkerVersion,
			request.Policy.Version,
		)
	}
	defaultPolicy := chunk.DefaultPolicy()
	switch {
	case request.Policy.Version == chunk.Version:
		if request.Policy.Budget != defaultPolicy.Budget || request.Policy.ConfigHash != defaultPolicy.ConfigHash {
			return errors.New("production chunker-v2 rebuild requires DefaultPolicy")
		}
	case strings.HasPrefix(request.Policy.Version, "chunker-v2-eval-"):
		want := "chunker-v2-eval-" + request.Policy.ConfigHash
		if request.Policy.Version != want {
			return fmt.Errorf("eval chunker version must equal %q", want)
		}
	default:
		return fmt.Errorf("unsupported rebuild chunker version %q", request.Policy.Version)
	}
	if request.TokenCounter == nil {
		return errors.New("rebuild token counter is required")
	}
	if request.Embedder == nil {
		return errors.New("rebuild embedder is required")
	}
	return nil
}

func (v SubsystemVersions) validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"chunker version", v.ChunkerVersion},
		{"indexing version", v.IndexingVersion},
		{"lexical version", v.LexicalVersion},
		{"sqlite-vec version", v.SQLiteVecVersion},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	return nil
}

func applySourceCatchUp(
	ctx context.Context,
	candidate *Candidate,
	previous, current index.SourceSnapshot,
	request RebuildRequest,
	tokenizer index.Tokenizer,
	reuse map[string][]float32,
) error {
	prevIndex := sourceRecordIndex(previous.Records)
	currIndex := sourceRecordIndex(current.Records)

	for key, prev := range prevIndex {
		if curr, ok := currIndex[key]; ok && sameSourceRecord(prev, curr) {
			continue
		}
		if err := candidate.DeleteDocumentChunks(ctx, prev.Entry.Scope, prev.Entry.ID, prev.Entry.Path); err != nil {
			return fmt.Errorf("delete changed source %q: %w", prev.Entry.Path, err)
		}
	}

	var changed []index.SourceRecord
	for key, curr := range currIndex {
		prev, ok := prevIndex[key]
		if ok && sameSourceRecord(prev, curr) {
			continue
		}
		changed = append(changed, curr)
	}
	if len(changed) == 0 {
		return nil
	}
	chunks, err := sourceChunks(ctx, changed, request.TokenCounter, request.Policy)
	if err != nil {
		return err
	}
	if err := BuildCandidateWithReuse(ctx, candidate, chunks, request.Embedder, tokenizer, reuse); err != nil {
		return fmt.Errorf("catch-up rebuild candidate: %w", err)
	}
	return nil
}

type sourceKey struct {
	scope string
	id    string
	path  string
}

func sourceRecordIndex(records []index.SourceRecord) map[sourceKey]index.SourceRecord {
	out := make(map[sourceKey]index.SourceRecord, len(records))
	for _, record := range records {
		out[sourceKey{scope: record.Entry.Scope, id: record.Entry.ID, path: record.Entry.Path}] = record
	}
	return out
}

func sameSourceRecord(left, right index.SourceRecord) bool {
	return left.Body == right.Body &&
		left.BodyStartByte == right.BodyStartByte &&
		left.SourceSizeByte == right.SourceSizeByte &&
		left.Entry.Type == right.Entry.Type &&
		left.Entry.Topic == right.Entry.Topic &&
		left.Entry.Status == right.Entry.Status &&
		left.Entry.Lifecycle == right.Entry.Lifecycle &&
		left.Entry.Active == right.Entry.Active &&
		strings.Join(left.Entry.Tags, "\x1f") == strings.Join(right.Entry.Tags, "\x1f")
}

func sourceChunks(ctx context.Context, records []index.SourceRecord, counter contracts.TokenCounter, policy chunk.ChunkingPolicy) ([]chunk.Chunk, error) {
	var chunks []chunk.Chunk
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("chunk rebuild sources: %w", err)
		}
		document := chunk.Document{
			Scope:          record.Entry.Scope,
			ID:             record.Entry.ID,
			Path:           record.Entry.Path,
			Title:          record.Entry.Title,
			Type:           record.Entry.Type,
			Topic:          record.Entry.Topic,
			Tags:           record.Entry.Tags,
			Body:           record.Body,
			SourceBaseByte: record.BodyStartByte,
			SourceSizeByte: record.SourceSizeByte,
		}
		documentChunks, err := chunk.ChunkDocumentWithPolicy(ctx, document, counter, policy)
		if err != nil {
			return nil, fmt.Errorf("chunk source %q: %w", record.Entry.Path, err)
		}
		chunks = append(chunks, documentChunks...)
	}
	return chunks, nil
}

func loadReusableVectors(ctx context.Context, semanticDir, profileID string) (map[string][]float32, error) {
	active, err := ReadActive(semanticDir)
	if errors.Is(err, ErrNoActivePointer) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read active generation for vector reuse: %w", err)
	}
	if active.RecallProfileID != profileID {
		return nil, nil
	}
	path, err := generationPath(semanticDir, active.GenerationID, ".sqlite")
	if err != nil {
		return nil, err
	}
	metadata := Metadata{
		Schema:     databaseSchema,
		Generation: active.GenerationID,
		Profile:    active.RecallProfileID,
		Snapshot:   active.SnapshotHash,
	}
	// OpenSealed requires full metadata; read vectors through a direct RO open
	// when the active generation matches the rebuild profile.
	db, err := sql.Open("sqlite", readOnlyURI(path))
	if err != nil {
		return nil, nil
	}
	defer db.Close()

	var schema, profile string
	if err := db.QueryRowContext(ctx, `SELECT schema, profile FROM meta`).Scan(&schema, &profile); err != nil {
		return nil, nil
	}
	if schema != databaseSchema || profile != profileID {
		return nil, nil
	}
	_ = metadata

	rows, err := db.QueryContext(ctx, `
		SELECT chunks.embedding_hash, chunk_vec.embedding
		FROM chunks
		JOIN chunk_vec ON chunk_vec.rowid = chunks.rowid`)
	if err != nil {
		return nil, nil
	}
	defer rows.Close()

	reuse := make(map[string][]float32)
	for rows.Next() {
		var hash string
		var blob []byte
		if err := rows.Scan(&hash, &blob); err != nil {
			return nil, fmt.Errorf("read reusable vector: %w", err)
		}
		vector, err := decodeVectorBlob(blob)
		if err != nil {
			return nil, err
		}
		reuse[hash] = vector
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reuse, nil
}

func decodeVectorBlob(blob []byte) ([]float32, error) {
	if len(blob)%4 != 0 {
		return nil, errors.New("vector blob length is invalid")
	}
	vector := make([]float32, len(blob)/4)
	for i := range vector {
		vector[i] = math.Float32frombits(binary.LittleEndian.Uint32(blob[i*4:]))
	}
	return vector, nil
}
