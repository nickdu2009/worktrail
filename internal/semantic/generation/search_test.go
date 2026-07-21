package generation

import (
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
)

func TestActiveChunkFTSAndVectorKNNAreReadOnlyAndStable(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-active", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	path := createQueryableGeneration(t, directory, pointer, metadata, []queryChunk{
		{id: "chunk-b", entryID: "entry-b", path: "docs/b.md", body: "needle", input: "input b", vector: []float32{1, 0}},
		{id: "chunk-a", entryID: "entry-a", path: "docs/a.md", body: "needle", input: "input a", vector: []float32{1, 0}},
	})
	before, beforeInfo := readDatabaseState(t, path)

	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}

	fts, err := active.ChunkFTS("needle", 2)
	if err != nil {
		t.Fatalf("ChunkFTS() error = %v", err)
	}
	assertHitIDs(t, fts, "chunk-a", "chunk-b")
	if fts[0].EntryID != "entry-a" || fts[0].Path != "docs/a.md" || fts[0].Rank != 1 {
		t.Fatalf("first FTS hit = %#v, want entry, path, and rank", fts[0])
	}

	vector, err := active.VectorKNN([]float32{1, 0}, 2)
	if err != nil {
		t.Fatalf("VectorKNN() error = %v", err)
	}
	assertHitIDs(t, vector, "chunk-a", "chunk-b")
	if vector[0].Distance != 0 || vector[0].Rank != 1 {
		t.Fatalf("first KNN hit = %#v, want zero distance and rank 1", vector[0])
	}

	if err := active.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	assertDatabaseState(t, path, before, beforeInfo)
}

func TestActiveChunkFTSFilteredPushesTypeTopicAndTag(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-filtered-fts", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, []queryChunk{
		{id: "chunk-keep", entryID: "entry-keep", path: "docs/keep.md", docType: "rule", topic: "recall", tags: []string{"alpha"}, body: "needle keep", input: "input keep", vector: []float32{1, 0}},
		{id: "chunk-drop", entryID: "entry-drop", path: "docs/drop.md", docType: "note", topic: "other", tags: []string{"beta"}, body: "needle drop", input: "input drop", vector: []float32{0, 1}},
	})
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })

	hits, err := active.ChunkFTSFiltered("needle", ChunkFilters{Type: "rule", Tag: "alpha"}, 10)
	if err != nil {
		t.Fatalf("ChunkFTSFiltered() error = %v", err)
	}
	assertHitIDs(t, hits, "chunk-keep")
	if hits[0].DocumentType != "rule" || hits[0].DocumentTopic != "recall" {
		t.Fatalf("filtered hit metadata = %#v", hits[0])
	}
}

func TestActiveChunkFTSRejectsInvalidInput(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-invalid-fts", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, nil)
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })

	for _, test := range []struct {
		name  string
		query string
		limit int
	}{
		{name: "empty", query: "", limit: 1},
		{name: "whitespace", query: " \t\n", limit: 1},
		{name: "quotes only", query: `"""`, limit: 1},
		{name: "zero limit", query: "needle", limit: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := active.ChunkFTS(test.query, test.limit)
			requireQueryErrorKind(t, err, QueryErrorInvalidInput)
		})
	}
}

func TestActiveVectorKNNRejectsInvalidInput(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-invalid-vector", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, nil)
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })

	for _, test := range []struct {
		name      string
		embedding []float32
		limit     int
	}{
		{name: "dimension", embedding: []float32{1}, limit: 1},
		{name: "not finite", embedding: []float32{float32(math.NaN()), 0}, limit: 1},
		{name: "not normalized", embedding: []float32{2, 0}, limit: 1},
		{name: "zero limit", embedding: []float32{1, 0}, limit: 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := active.VectorKNN(test.embedding, test.limit)
			requireQueryErrorKind(t, err, QueryErrorInvalidInput)
		})
	}
}

func TestActiveVectorKNNReturnsNoHitsWithoutVectors(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-no-vectors", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, nil)
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })

	hits, err := active.VectorKNN([]float32{1, 0}, 1)
	if err != nil {
		t.Fatalf("VectorKNN() error = %v", err)
	}
	if len(hits) != 0 {
		t.Fatalf("VectorKNN() hits = %#v, want none", hits)
	}
}

func TestActiveVectorKNNUsesBoundedRequestedLimit(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-bounded-vector", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, []queryChunk{
		{id: "chunk-best", entryID: "entry-best", path: "docs/best.md", body: "best", input: "input best", vector: []float32{1, 0}},
		{id: "chunk-next", entryID: "entry-next", path: "docs/next.md", body: "next", input: "input next", vector: []float32{0.8, 0.6}},
		{id: "chunk-last", entryID: "entry-last", path: "docs/last.md", body: "last", input: "input last", vector: []float32{0, 1}},
	})
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })

	hits, err := active.VectorKNN([]float32{1, 0}, 1)
	if err != nil {
		t.Fatalf("VectorKNN() error = %v", err)
	}
	assertHitIDs(t, hits, "chunk-best")

	query, args := vectorKNNStatement([]float32{1, 0}, 1)
	if strings.Contains(strings.ToLower(query), "count(") {
		t.Fatalf("KNN query unexpectedly counts vectors: %s", query)
	}
	if len(args) != 3 {
		t.Fatalf("KNN query argument count = %d, want 3", len(args))
	}
	for _, position := range []int{1, 2} {
		got, ok := args[position].(int)
		if !ok || got != 1 {
			t.Fatalf("KNN query argument %d = %#v, want requested limit 1", position, args[position])
		}
	}
}

func TestActiveChunkFTSReturnsQueryError(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-fts-error", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	path, err := generationPath(directory, pointer.GenerationID, ".sqlite")
	if err != nil {
		t.Fatalf("generationPath() error = %v", err)
	}
	candidate, err := CreateCandidate(path, metadata)
	if err != nil {
		t.Fatalf("CreateCandidate() error = %v", err)
	}
	if _, err := candidate.db.Exec(`DROP TABLE chunk_fts`); err != nil {
		t.Fatalf("drop FTS table: %v", err)
	}
	if err := candidate.SealCandidate(); err != nil {
		t.Fatalf("SealCandidate() error = %v", err)
	}
	if err := writePointer(directory, pointer); err != nil {
		t.Fatalf("writePointer() error = %v", err)
	}

	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })
	_, err = active.ChunkFTS("needle", 1)
	requireQueryErrorKind(t, err, QueryErrorQuery)
}

func TestActiveChunksByIDsReturnsEvidenceMetadataAndNeighbors(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-evidence", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, []queryChunk{
		{
			id: "chunk-a", entryID: "entry", path: "docs/a.md", body: "row a", input: "input a", vector: []float32{1, 0},
			kind: chunk.KindTableRowGroup, groupID: "group-1", order: 1, breadcrumb: []string{"Architecture"},
			primaryStart: 10, primaryEnd: 20, contextStart: 0, contextEnd: 10, groupStart: 0, groupEnd: 40,
			prev: "", next: "chunk-b", sourceSize: 40,
		},
		{
			id: "chunk-b", entryID: "entry", path: "docs/a.md", body: "row b", input: "input b", vector: []float32{0, 1},
			kind: chunk.KindTableRowGroup, groupID: "group-1", order: 2, breadcrumb: []string{"Architecture"},
			primaryStart: 20, primaryEnd: 30, contextStart: 0, contextEnd: 10, groupStart: 0, groupEnd: 40,
			prev: "chunk-a", next: "", sourceSize: 40,
		},
	})
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	t.Cleanup(func() { _ = active.Close() })

	got, err := active.ChunksByIDs([]string{"chunk-b", "missing", "chunk-a", "chunk-b"})
	if err != nil {
		t.Fatalf("ChunksByIDs() error = %v", err)
	}
	if len(got) != 2 || got[0].ChunkID != "chunk-b" || got[1].ChunkID != "chunk-a" {
		t.Fatalf("ChunksByIDs() = %#v", got)
	}
	if got[1].ChunkKind != chunk.KindTableRowGroup || got[1].StructuralGroupID != "group-1" || got[1].NextChunkID != "chunk-b" {
		t.Fatalf("chunk-a evidence = %#v", got[1])
	}
	if got[1].ContextStart == nil || *got[1].ContextStart != 0 || got[1].ContextEnd == nil || *got[1].ContextEnd != 10 {
		t.Fatalf("context range = %#v", got[1])
	}
	if got[1].GroupStart == nil || *got[1].GroupStart != 0 || got[1].GroupEnd == nil || *got[1].GroupEnd != 40 {
		t.Fatalf("group range = %#v", got[1])
	}
	if len(got[1].HeadingBreadcrumb) != 1 || got[1].HeadingBreadcrumb[0] != "Architecture" {
		t.Fatalf("breadcrumb = %#v", got[1].HeadingBreadcrumb)
	}
}

func TestActiveClosedQueryReturnsMetadataError(t *testing.T) {
	directory := t.TempDir()
	pointer := testPointer("query-closed", "")
	metadata := metadataForPointer(pointer)
	metadata.Dimension = 2
	createQueryableGeneration(t, directory, pointer, metadata, nil)
	active, err := OpenActive(context.Background(), directory, metadata)
	if err != nil {
		t.Fatalf("OpenActive() error = %v", err)
	}
	if err := active.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, err = active.VectorKNN([]float32{1, 0}, 1)
	requireQueryErrorKind(t, err, QueryErrorMetadata)
}

type queryChunk struct {
	id           string
	entryID      string
	path         string
	docType      string
	topic        string
	tags         []string
	body         string
	input        string
	vector       []float32
	kind         string
	groupID      string
	order        int
	breadcrumb   []string
	primaryStart int
	primaryEnd   int
	contextStart int
	contextEnd   int
	groupStart   int
	groupEnd     int
	prev         string
	next         string
	sourceSize   int
	hasRanges    bool
}

func createQueryableGeneration(t *testing.T, directory string, pointer Pointer, metadata Metadata, chunks []queryChunk) string {
	t.Helper()
	path, err := generationPath(directory, pointer.GenerationID, ".sqlite")
	if err != nil {
		t.Fatalf("generationPath() error = %v", err)
	}
	candidate, err := CreateCandidate(path, metadata)
	if err != nil {
		t.Fatalf("CreateCandidate() error = %v", err)
	}
	inputs := make([]chunk.Chunk, 0, len(chunks))
	vectors := make(map[string][]float32, len(chunks))
	for _, source := range chunks {
		body := source.body
		if body == "" {
			body = source.input
		}
		end := len(body)
		if end == 0 {
			end = 1
		}
		docType := source.docType
		if docType == "" {
			docType = "rule"
		}
		kind := source.kind
		if kind == "" {
			kind = chunk.KindText
		}
		groupID := source.groupID
		if groupID == "" {
			groupID = "group-" + source.id
		}
		primaryStart, primaryEnd := 0, end
		sourceSize := end
		var contextRange, groupRange *chunk.ByteRange
		if source.hasRanges || source.primaryEnd > 0 || source.sourceSize > 0 {
			primaryStart = source.primaryStart
			primaryEnd = source.primaryEnd
			if primaryEnd <= primaryStart {
				primaryEnd = primaryStart + 1
			}
			sourceSize = source.sourceSize
			if sourceSize < primaryEnd {
				sourceSize = primaryEnd
			}
			if source.contextEnd > source.contextStart {
				contextRange = &chunk.ByteRange{Start: source.contextStart, End: source.contextEnd}
			}
			if source.groupEnd > source.groupStart {
				groupRange = &chunk.ByteRange{Start: source.groupStart, End: source.groupEnd}
			}
		}
		inputs = append(inputs, chunk.Chunk{
			ChunkID:           source.id,
			Scope:             "project",
			DocumentID:        source.entryID,
			Path:              source.path,
			Type:              docType,
			Topic:             source.topic,
			Tags:              append([]string{}, source.tags...),
			Order:             source.order,
			Kind:              kind,
			StructuralGroupID: groupID,
			HeadingBreadcrumb: append([]string(nil), source.breadcrumb...),
			Body:              body,
			MetadataTerms:     "path: " + source.path,
			ContextTerms:      "section",
			EmbeddingInput:    source.input,
			EmbeddingHash:     "hash-" + source.id,
			ChunkerVersion:    chunk.Version,
			SourceStart:       primaryStart,
			SourceEnd:         primaryEnd,
			ContextRange:      contextRange,
			GroupRange:        groupRange,
			PrevChunkID:       source.prev,
			NextChunkID:       source.next,
			SourceSizeByte:    sourceSize,
		})
		vectors[source.input] = source.vector
	}
	if err := BuildCandidate(context.Background(), candidate, inputs, &fakeEmbedder{vectors: vectors}, index.NewTokenizer()); err != nil {
		t.Fatalf("BuildCandidate() error = %v", err)
	}
	if err := candidate.SealCandidate(); err != nil {
		t.Fatalf("SealCandidate() error = %v", err)
	}
	if err := writePointer(directory, pointer); err != nil {
		t.Fatalf("writePointer() error = %v", err)
	}
	return path
}

func assertHitIDs(t *testing.T, hits []ChunkHit, want ...string) {
	t.Helper()
	if len(hits) != len(want) {
		t.Fatalf("hit count = %d, want %d: %#v", len(hits), len(want), hits)
	}
	for i, id := range want {
		if hits[i].ChunkID != id {
			t.Fatalf("hit %d ID = %q, want %q; all hits %#v", i, hits[i].ChunkID, id, hits)
		}
	}
}

func requireQueryErrorKind(t *testing.T, err error, want QueryErrorKind) {
	t.Helper()
	if err == nil {
		t.Fatal("query error = nil")
	}
	var queryErr *QueryError
	if !errors.As(err, &queryErr) {
		t.Fatalf("query error = %T %v, want *QueryError", err, err)
	}
	if queryErr.Kind != want {
		t.Fatalf("query error kind = %q, want %q", queryErr.Kind, want)
	}
}

func readDatabaseState(t *testing.T, path string) ([]byte, os.FileInfo) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read database: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	return data, info
}

func assertDatabaseState(t *testing.T, path string, wantBytes []byte, wantInfo os.FileInfo) {
	t.Helper()
	gotBytes, gotInfo := readDatabaseState(t, path)
	if string(gotBytes) != string(wantBytes) {
		t.Fatal("active generation query changed database bytes")
	}
	if !gotInfo.ModTime().Equal(wantInfo.ModTime()) {
		t.Fatalf("active generation query changed mtime: got %v, want %v", gotInfo.ModTime(), wantInfo.ModTime())
	}
	if gotInfo.Size() != wantInfo.Size() {
		t.Fatalf("active generation query changed size: got %d, want %d", gotInfo.Size(), wantInfo.Size())
	}
}
