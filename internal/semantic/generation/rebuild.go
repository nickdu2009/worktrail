package generation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/policy"
)

// ErrSourcesChanged reports that the selected source corpus changed while a
// candidate was being built. The sealed candidate is intentionally retained
// for explicit garbage collection, but is never activated.
var ErrSourcesChanged = errors.New("semantic rebuild sources changed")

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
	TokenCounter contracts.TokenCounter
	Embedder     Embedder
}

// Rebuild creates, seals, validates, and activates one immutable candidate.
// It does not start a daemon, perform retrieval, retry changed sources, retire
// prior generations, or remove unsuccessful candidates.
func Rebuild(ctx context.Context, request RebuildRequest) (Pointer, error) {
	candidatePath, err := generationPath(request.SemanticDir, request.GenerationID, ".sqlite")
	if err != nil {
		return Pointer{}, fmt.Errorf("resolve rebuild candidate path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(candidatePath), 0o700); err != nil {
		return Pointer{}, fmt.Errorf("create semantic generation directory: %w", err)
	}

	sources, err := policy.FreshSources(request.Root, request.Scope)
	if err != nil {
		return Pointer{}, fmt.Errorf("read rebuild sources: %w", err)
	}
	chunks, err := sourceChunks(ctx, sources.Records, request.TokenCounter)
	if err != nil {
		return Pointer{}, err
	}

	metadata := request.Metadata
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

	if err := BuildCandidate(ctx, candidate, chunks, request.Embedder); err != nil {
		return Pointer{}, fmt.Errorf("build rebuild candidate: %w", err)
	}
	if err := SealCandidate(candidate); err != nil {
		return Pointer{}, fmt.Errorf("seal rebuild candidate: %w", err)
	}

	currentSources, err := policy.FreshSources(request.Root, request.Scope)
	if err != nil {
		return Pointer{}, fmt.Errorf("recheck rebuild sources: %w", err)
	}
	if currentSources.SemanticSnapshotHash != sources.SemanticSnapshotHash {
		return Pointer{}, fmt.Errorf(
			"%w: built %q, current %q",
			ErrSourcesChanged,
			sources.SemanticSnapshotHash,
			currentSources.SemanticSnapshotHash,
		)
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
			SnapshotHash:    sources.SemanticSnapshotHash,
		}, nil
	})
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
		active.SnapshotHash != pointer.SnapshotHash {
		return Pointer{}, errors.New("activated semantic generation pointer does not match rebuild candidate")
	}
	return active, nil
}

func sourceChunks(ctx context.Context, records []index.SourceRecord, counter contracts.TokenCounter) ([]chunk.Chunk, error) {
	var chunks []chunk.Chunk
	for _, record := range records {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("chunk rebuild sources: %w", err)
		}
		document := chunk.Document{
			Scope: record.Entry.Scope,
			ID:    record.Entry.ID,
			Path:  record.Entry.Path,
			Title: record.Entry.Title,
			Type:  record.Entry.Type,
			Topic: record.Entry.Topic,
			Tags:  record.Entry.Tags,
			Body:  record.Body,
		}
		documentChunks, err := chunk.ChunkDocument(ctx, document, counter)
		if err != nil {
			return nil, fmt.Errorf("chunk source %q: %w", record.Entry.Path, err)
		}
		chunks = append(chunks, documentChunks...)
	}
	return chunks, nil
}
