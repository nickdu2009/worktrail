package generation

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
)

func TestBuildCandidateWritesChunksFTSVectorsAndEmptyTags(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	embedder := &fakeEmbedder{vectors: map[string][]float32{
		"input alpha": {1, 0},
		"input beta":  {0, 1},
	}}
	chunks := []chunk.Chunk{
		builderChunk("chunk-alpha", "input alpha", "alpha body"),
		builderChunk("chunk-beta", "input beta", "beta body"),
	}

	if err := BuildCandidate(context.Background(), candidate, chunks, embedder); err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	if got, want := embedder.calls, []string{"input alpha", "input beta"}; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Embed() calls = %v, want %v", got, want)
	}
	assertTableCount(t, candidate, "chunks", 2)
	assertTableCount(t, candidate, "chunk_fts", 2)
	assertTableCount(t, candidate, "chunk_vec", 2)
	assertTableCount(t, candidate, "chunk_tags", 0)

	var ftsID string
	if err := candidate.db.QueryRow(`SELECT chunk_id FROM chunk_fts WHERE chunk_fts MATCH ?`, "alpha").Scan(&ftsID); err != nil {
		t.Fatalf("FTS query error = %v", err)
	}
	if ftsID != "chunk-alpha" {
		t.Fatalf("FTS chunk ID = %q, want %q", ftsID, "chunk-alpha")
	}

	var knnID string
	if err := candidate.db.QueryRow(`
		SELECT chunks.chunk_id
		FROM (
			SELECT rowid
			FROM chunk_vec
			WHERE embedding MATCH ? AND k = 1
		) AS nearest
		JOIN chunks ON chunks.rowid = nearest.rowid
	`,
		encodeVectorBlob([]float32{1, 0}),
	).Scan(&knnID); err != nil {
		t.Fatalf("KNN query error = %v", err)
	}
	if knnID != "chunk-alpha" {
		t.Fatalf("KNN chunk ID = %q, want %q", knnID, "chunk-alpha")
	}
}

func TestBuildCandidateBatchesInputsInStableOrder(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	chunks := make([]chunk.Chunk, embeddingBatchSize+1)
	vectors := make(map[string][]float32, len(chunks))
	wantBatches := make([][]string, 0, 2)
	for index := range chunks {
		input := fmt.Sprintf("input %02d", index)
		chunks[index] = builderChunk(fmt.Sprintf("chunk-%02d", index), input, "body")
		if index%2 == 0 {
			vectors[input] = []float32{1, 0}
		} else {
			vectors[input] = []float32{0, 1}
		}
	}
	wantBatches = append(wantBatches, embeddingInputs(chunks[:embeddingBatchSize]))
	wantBatches = append(wantBatches, embeddingInputs(chunks[embeddingBatchSize:]))
	embedder := &fakeBatchEmbedder{
		vectors:   vectors,
		singleErr: errors.New("single embedding must not be used"),
	}

	if err := BuildCandidate(context.Background(), candidate, chunks, embedder); err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	if got, want := len(embedder.batchCalls), len(wantBatches); got != want {
		t.Fatalf("EmbedBatch() calls = %d, want %d", got, want)
	}
	for index, want := range wantBatches {
		got := embedder.batchCalls[index]
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("EmbedBatch() inputs at call %d = %v, want %v", index, got, want)
		}
	}
	if len(embedder.singleCalls) != 0 {
		t.Fatalf("Embed() calls = %v, want none", embedder.singleCalls)
	}
	assertTableCount(t, candidate, "chunks", len(chunks))
	assertTableCount(t, candidate, "chunk_fts", len(chunks))
	assertTableCount(t, candidate, "chunk_vec", len(chunks))
	for _, source := range chunks {
		assertChunkVector(t, candidate, source.ChunkID, vectors[source.EmbeddingInput])
	}
}

func TestBuildCandidateRejectsMismatchedBatchResponseWithoutWrites(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	embedder := &fakeBatchEmbedder{
		batchVectors: [][]float32{{1, 0}},
	}

	err := BuildCandidate(context.Background(), candidate, []chunk.Chunk{
		builderChunk("chunk-alpha", "input alpha", "alpha body"),
		builderChunk("chunk-beta", "input beta", "beta body"),
	}, embedder)
	if err == nil || !strings.Contains(err.Error(), "response count") {
		t.Fatalf("BuildCandidate() error = %v, want response count error", err)
	}
	if got, want := len(embedder.batchCalls), 1; got != want {
		t.Fatalf("EmbedBatch() calls = %d, want %d", got, want)
	}
	assertNoCandidateRows(t, candidate)
}

func TestBuildCandidateRejectsInvalidBatchVectorsWithoutWrites(t *testing.T) {
	cases := []struct {
		name    string
		vectors [][]float32
		want    string
	}{
		{"dimension", [][]float32{{1}, {0, 1}}, "dimension"},
		{"norm", [][]float32{{2, 0}, {0, 1}}, "L2 norm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := newBuilderCandidate(t, 2)
			embedder := &fakeBatchEmbedder{batchVectors: tc.vectors}

			err := BuildCandidate(context.Background(), candidate, []chunk.Chunk{
				builderChunk("chunk-alpha", "input alpha", "alpha body"),
				builderChunk("chunk-beta", "input beta", "beta body"),
			}, embedder)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildCandidate() error = %v, want %s error", err, tc.want)
			}
			assertNoCandidateRows(t, candidate)
		})
	}
}

func TestBuildCandidateBatchErrorDoesNotWrite(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	chunks := make([]chunk.Chunk, embeddingBatchSize+1)
	vectors := make(map[string][]float32, len(chunks))
	for index := range chunks {
		input := fmt.Sprintf("input %02d", index)
		chunks[index] = builderChunk(fmt.Sprintf("chunk-%02d", index), input, "body")
		vectors[input] = []float32{1, 0}
	}
	embedder := &fakeBatchEmbedder{
		vectors:       vectors,
		batchErr:      errors.New("daemon failed"),
		failBatchCall: 2,
	}

	err := BuildCandidate(context.Background(), candidate, chunks, embedder)
	if err == nil || !strings.Contains(err.Error(), "daemon failed") {
		t.Fatalf("BuildCandidate() error = %v, want daemon failure", err)
	}
	if got, want := len(embedder.batchCalls), 2; got != want {
		t.Fatalf("EmbedBatch() calls = %d, want %d", got, want)
	}
	assertNoCandidateRows(t, candidate)
}

func TestBuildCandidateRejectsInvalidEmbeddingsWithoutWrites(t *testing.T) {
	cases := []struct {
		name   string
		vector []float32
		err    error
		want   string
	}{
		{"dimension", []float32{1}, nil, "dimension"},
		{"non-finite", []float32{float32(math.NaN()), 0}, nil, "non-finite"},
		{"norm", []float32{2, 0}, nil, "L2 norm"},
		{"embedder error", nil, errors.New("daemon failed"), "daemon failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			candidate := newBuilderCandidate(t, 2)
			embedder := &fakeEmbedder{
				vectors: map[string][]float32{"input alpha": tc.vector},
				err:     tc.err,
			}

			err := BuildCandidate(
				context.Background(),
				candidate,
				[]chunk.Chunk{builderChunk("chunk-alpha", "input alpha", "alpha body")},
				embedder,
			)
			if err == nil {
				t.Fatal("BuildCandidate() error = nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("BuildCandidate() error = %q, want substring %q", err, tc.want)
			}
			assertNoCandidateRows(t, candidate)
		})
	}
}

func TestBuildCandidateRollsBackWholeBatch(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	embedder := &fakeEmbedder{vectors: map[string][]float32{
		"input alpha": {1, 0},
		"input beta":  {1},
	}}

	err := BuildCandidate(context.Background(), candidate, []chunk.Chunk{
		builderChunk("chunk-alpha", "input alpha", "alpha body"),
		builderChunk("chunk-beta", "input beta", "beta body"),
	}, embedder)
	if err == nil || !strings.Contains(err.Error(), "dimension") {
		t.Fatalf("BuildCandidate() error = %v, want dimension error", err)
	}
	if got, want := len(embedder.calls), 2; got != want {
		t.Fatalf("Embed() calls = %d, want %d", got, want)
	}
	assertNoCandidateRows(t, candidate)
}

func TestBuildCandidateRejectsDuplicateChunkIDs(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	embedder := &fakeEmbedder{vectors: map[string][]float32{"input alpha": {1, 0}}}

	err := BuildCandidate(context.Background(), candidate, []chunk.Chunk{
		builderChunk("chunk-alpha", "input alpha", "alpha body"),
		builderChunk("chunk-alpha", "input alpha", "alpha body"),
	}, embedder)
	if err == nil || !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("BuildCandidate() error = %v, want duplicate chunk ID error", err)
	}
	if len(embedder.calls) != 0 {
		t.Fatalf("Embed() calls = %v, want none", embedder.calls)
	}
	assertNoCandidateRows(t, candidate)
}

func TestBuildCandidateDoesNotPartiallyWriteExistingDuplicate(t *testing.T) {
	candidate := newBuilderCandidate(t, 2)
	embedder := &fakeEmbedder{vectors: map[string][]float32{
		"input alpha": {1, 0},
		"input beta":  {0, 1},
	}}
	if err := BuildCandidate(
		context.Background(),
		candidate,
		[]chunk.Chunk{builderChunk("chunk-alpha", "input alpha", "alpha body")},
		embedder,
	); err != nil {
		t.Fatalf("initial BuildCandidate() error = %v", err)
	}

	err := BuildCandidate(context.Background(), candidate, []chunk.Chunk{
		builderChunk("chunk-beta", "input beta", "beta body"),
		builderChunk("chunk-alpha", "input alpha", "alpha body"),
	}, embedder)
	if err == nil || !strings.Contains(err.Error(), "write chunk") {
		t.Fatalf("BuildCandidate() error = %v, want duplicate write error", err)
	}
	assertTableCount(t, candidate, "chunks", 1)
	assertTableCount(t, candidate, "chunk_fts", 1)
	assertTableCount(t, candidate, "chunk_vec", 1)
}

func newBuilderCandidate(t *testing.T, dimension int) *Candidate {
	t.Helper()
	metadata := testMetadata()
	metadata.Dimension = dimension
	candidate, err := CreateCandidate(t.TempDir()+"/candidate.sqlite", metadata)
	if err != nil {
		t.Fatalf("CreateCandidate() error = %v", err)
	}
	t.Cleanup(func() {
		_ = candidate.db.Close()
	})
	return candidate
}

func builderChunk(id, input, body string) chunk.Chunk {
	return chunk.Chunk{
		ChunkID:           id,
		Scope:             "project",
		DocumentID:        "document-1",
		Path:              "docs/example.md",
		Order:             0,
		HeadingBreadcrumb: []string{"Example"},
		SourceStart:       0,
		SourceEnd:         len(body),
		Body:              body,
		EmbeddingInput:    input,
		TokenCount:        2,
		EmbeddingHash:     "hash-" + id,
		ChunkerVersion:    chunk.Version,
	}
}

func assertNoCandidateRows(t *testing.T, candidate *Candidate) {
	t.Helper()
	for _, table := range []string{"chunks", "chunk_fts", "chunk_vec", "chunk_tags"} {
		assertTableCount(t, candidate, table, 0)
	}
}

func assertTableCount(t *testing.T, candidate *Candidate, table string, want int) {
	t.Helper()
	var got int
	if err := candidate.db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&got); err != nil {
		t.Fatalf("count %s error = %v", table, err)
	}
	if got != want {
		t.Fatalf("%s row count = %d, want %d", table, got, want)
	}
}

func assertChunkVector(t *testing.T, candidate *Candidate, chunkID string, want []float32) {
	t.Helper()
	var got []byte
	if err := candidate.db.QueryRow(`
		SELECT chunk_vec.embedding
		FROM chunks
		JOIN chunk_vec ON chunk_vec.rowid = chunks.rowid
		WHERE chunks.chunk_id = ?
	`, chunkID).Scan(&got); err != nil {
		t.Fatalf("read vector for %q error = %v", chunkID, err)
	}
	if string(got) != string(encodeVectorBlob(want)) {
		t.Fatalf("vector for %q = %v, want %v", chunkID, got, encodeVectorBlob(want))
	}
}

type fakeEmbedder struct {
	vectors map[string][]float32
	err     error
	calls   []string
}

func (f *fakeEmbedder) Embed(_ context.Context, input string) ([]float32, error) {
	f.calls = append(f.calls, input)
	if f.err != nil {
		return nil, f.err
	}
	return append([]float32(nil), f.vectors[input]...), nil
}

type fakeBatchEmbedder struct {
	vectors       map[string][]float32
	batchVectors  [][]float32
	batchErr      error
	failBatchCall int
	singleErr     error
	batchCalls    [][]string
	singleCalls   []string
}

func (f *fakeBatchEmbedder) Embed(_ context.Context, input string) ([]float32, error) {
	f.singleCalls = append(f.singleCalls, input)
	if f.singleErr != nil {
		return nil, f.singleErr
	}
	return append([]float32(nil), f.vectors[input]...), nil
}

func (f *fakeBatchEmbedder) EmbedBatch(_ context.Context, inputs []string) ([][]float32, error) {
	f.batchCalls = append(f.batchCalls, append([]string(nil), inputs...))
	if f.batchErr != nil && len(f.batchCalls) == f.failBatchCall {
		return nil, f.batchErr
	}
	if f.batchVectors != nil {
		return cloneVectors(f.batchVectors), nil
	}

	vectors := make([][]float32, len(inputs))
	for index, input := range inputs {
		vectors[index] = append([]float32(nil), f.vectors[input]...)
	}
	return vectors, nil
}

func embeddingInputs(chunks []chunk.Chunk) []string {
	inputs := make([]string, len(chunks))
	for index, source := range chunks {
		inputs[index] = source.EmbeddingInput
	}
	return inputs
}

func cloneVectors(vectors [][]float32) [][]float32 {
	cloned := make([][]float32, len(vectors))
	for index, vector := range vectors {
		cloned[index] = append([]float32(nil), vector...)
	}
	return cloned
}
