package chunk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

type wordCounter struct{}

func (wordCounter) CountTokens(_ context.Context, value string) (int, error) {
	return len(strings.Fields(value)), nil
}

type countingWordCounter struct {
	calls int
}

func (c *countingWordCounter) CountTokens(ctx context.Context, value string) (int, error) {
	c.calls++
	return wordCounter{}.CountTokens(ctx, value)
}

type failingCounter struct{}

func (failingCounter) CountTokens(context.Context, string) (int, error) {
	return 0, errors.New("counter unavailable")
}

func TestChunkDocumentBuildsNestedHeadingBreadcrumbs(t *testing.T) {
	doc := Document{
		Path: "docs/example.md",
		Body: "# Root\n\nRoot paragraph.\n\nChild\n-----\n\nChild paragraph.\n",
	}

	chunks, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("ChunkDocument() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	if got, want := chunks[0].HeadingBreadcrumb, []string{"Root"}; !reflect.DeepEqual(got, want) {
		t.Errorf("first breadcrumb = %q, want %q", got, want)
	}
	if got, want := chunks[1].HeadingBreadcrumb, []string{"Root", "Child"}; !reflect.DeepEqual(got, want) {
		t.Errorf("second breadcrumb = %q, want %q", got, want)
	}
	if !strings.HasPrefix(chunks[1].EmbeddingInput, "path: docs/example.md\n") {
		t.Errorf("embedding input does not start with stable metadata prefix: %q", chunks[1].EmbeddingInput)
	}
}

func TestChunkDocumentKeepsTopLevelListItemsIntact(t *testing.T) {
	doc := Document{
		Body: "# Tasks\n\n- first item stays together\n  - nested detail stays with its parent\n- second item stays together\n",
	}
	budget := Budget{Target: 12, HardMax: 30, Overlap: 2}

	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	if len(chunks) != 2 {
		t.Fatalf("len(chunks) = %d, want 2", len(chunks))
	}
	if !strings.Contains(chunks[0].Body, "- first item stays together") ||
		!strings.Contains(chunks[0].Body, "- nested detail stays with its parent") {
		t.Errorf("first list item was split: %q", chunks[0].Body)
	}
	if !strings.Contains(chunks[1].Body, "- second item stays together") {
		t.Errorf("second list item missing from its chunk: %q", chunks[1].Body)
	}
}

func TestChunkDocumentKeepsBoundedFencedCodeBlockIntact(t *testing.T) {
	doc := Document{
		Body: "# Example\n\nBefore code.\n\n```go\nfunc main() {\n\tprintln(\"hello\")\n}\n```\n\nAfter code.\n",
	}
	budget := Budget{Target: 12, HardMax: 30, Overlap: 2}

	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	var codeChunk *Chunk
	for i := range chunks {
		if strings.Contains(chunks[i].Body, "func main()") {
			codeChunk = &chunks[i]
			break
		}
	}
	if codeChunk == nil {
		t.Fatal("no chunk contains the fenced code block")
	}
	if !strings.Contains(codeChunk.Body, "```go") || !strings.Contains(codeChunk.Body, "```\n") {
		t.Errorf("fenced code boundaries were not retained: %q", codeChunk.Body)
	}
}

func TestChunkDocumentForcedSplitUsesOverlapAndHardMaximum(t *testing.T) {
	doc := Document{
		Body: "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen\n",
	}
	budget := Budget{Target: 10, HardMax: 15, Overlap: 2}

	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want at least 2 forced chunks", len(chunks))
	}
	for _, got := range chunks {
		if got.TokenCount > budget.HardMax {
			t.Errorf("chunk token count = %d, hard maximum = %d", got.TokenCount, budget.HardMax)
		}
	}
	if !strings.HasPrefix(chunks[1].Body, "seven eight ") {
		t.Errorf("second forced chunk = %q, want two-token overlap", chunks[1].Body)
	}
}

func TestForcedSplitUsesLogarithmicTokenProbes(t *testing.T) {
	counter := &countingWordCounter{}
	value := strings.Repeat("word ", 4096)
	c := chunker{
		source:  []byte(value),
		counter: counter,
		budget:  Budget{Target: 80, HardMax: 100, Overlap: 8},
	}

	end, tokens, err := c.forcedEnd(context.Background(), 0, len(c.source), nil, "")
	if err != nil {
		t.Fatalf("forcedEnd() error = %v", err)
	}
	if tokens > c.budget.HardMax {
		t.Fatalf("forcedEnd() tokens = %d, hard maximum = %d", tokens, c.budget.HardMax)
	}
	if counter.calls > 18 {
		t.Fatalf("forcedEnd() token calls = %d, want at most 18", counter.calls)
	}

	counter.calls = 0
	start, err := c.overlapStart(context.Background(), 0, end)
	if err != nil {
		t.Fatalf("overlapStart() error = %v", err)
	}
	if start <= 0 || start >= end {
		t.Fatalf("overlapStart() = %d for end %d", start, end)
	}
	if counter.calls > 10 {
		t.Fatalf("overlapStart() token calls = %d, want at most 10", counter.calls)
	}
	overlapTokens, err := wordCounter{}.CountTokens(context.Background(), string(c.source[start:end]))
	if err != nil {
		t.Fatal(err)
	}
	if overlapTokens > c.budget.Overlap {
		t.Fatalf("overlap tokens = %d, budget = %d", overlapTokens, c.budget.Overlap)
	}
}

func TestChunkDocumentSourceOffsetsSliceOriginalUTF8Body(t *testing.T) {
	doc := Document{
		Body: "# H\n\néclair paragraph.\n\nSecond paragraph.\n",
	}

	chunks, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("ChunkDocument() error = %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("len(chunks) = %d, want 1", len(chunks))
	}
	for _, got := range chunks {
		if source := doc.Body[got.SourceStart:got.SourceEnd]; source != got.Body {
			t.Errorf("body[%d:%d] = %q, chunk body = %q", got.SourceStart, got.SourceEnd, source, got.Body)
		}
	}
}

func TestChunkDocumentIsDeterministicAndLinksNeighbors(t *testing.T) {
	doc := Document{
		ID:    "entry-1",
		Path:  "docs/example.md",
		Title: "Example",
		Tags:  []string{"zeta", "alpha"},
		Body:  "# First\n\nOne.\n\n# Second\n\nTwo.\n",
	}

	first, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("first ChunkDocument() error = %v", err)
	}
	second, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("second ChunkDocument() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Errorf("ChunkDocument() is not deterministic:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	if first[0].StructuralGroupID == first[1].StructuralGroupID {
		t.Fatal("distinct text sections must not share a structural group")
	}
	if first[0].NextChunkID != "" || first[1].PrevChunkID != "" {
		t.Errorf("cross-group neighbor links = (%q, %q), want empty", first[0].NextChunkID, first[1].PrevChunkID)
	}
	if first[0].EmbeddingHash == "" || first[0].ChunkID == "" {
		t.Error("deterministic identities must not be empty")
	}
}

func TestChunkDocumentLinksNeighborsInsideForcedSplitGroup(t *testing.T) {
	doc := Document{
		Body: "one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen\n",
	}
	budget := Budget{Target: 10, HardMax: 15, Overlap: 2}

	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want at least 2", len(chunks))
	}
	if chunks[0].StructuralGroupID == "" || chunks[0].StructuralGroupID != chunks[1].StructuralGroupID {
		t.Fatalf("forced-split chunks must share a structural group: %q vs %q", chunks[0].StructuralGroupID, chunks[1].StructuralGroupID)
	}
	if chunks[0].NextChunkID != chunks[1].ChunkID || chunks[1].PrevChunkID != chunks[0].ChunkID {
		t.Errorf("same-group neighbor links = (%q, %q), want linked", chunks[0].NextChunkID, chunks[1].PrevChunkID)
	}
}

func TestChunkDocumentScopeSeparatesIdentityAndEmbeddingInput(t *testing.T) {
	doc := Document{
		ID:    "entry-1",
		Path:  "docs/example.md",
		Body:  "Same document body.\n",
		Scope: "project",
	}
	projectChunks, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("project ChunkDocument() error = %v", err)
	}

	doc.Scope = "user"
	userChunks, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("user ChunkDocument() error = %v", err)
	}
	if len(projectChunks) != 1 || len(userChunks) != 1 {
		t.Fatalf("chunk counts = (%d, %d), want (1, 1)", len(projectChunks), len(userChunks))
	}
	if projectChunks[0].Scope != "project" || userChunks[0].Scope != "user" {
		t.Errorf("chunk scopes = (%q, %q), want (project, user)", projectChunks[0].Scope, userChunks[0].Scope)
	}
	if projectChunks[0].ChunkID == userChunks[0].ChunkID {
		t.Error("same document ID and path in different scopes must not share a chunk ID")
	}
	if projectChunks[0].EmbeddingHash == userChunks[0].EmbeddingHash {
		t.Error("scope must affect the embedding input hash")
	}
	if !strings.Contains(projectChunks[0].EmbeddingInput, "scope: project\n") ||
		!strings.Contains(userChunks[0].EmbeddingInput, "scope: user\n") {
		t.Errorf("embedding inputs must contain scope: (%q, %q)", projectChunks[0].EmbeddingInput, userChunks[0].EmbeddingInput)
	}
}

func TestDefaultBudgetReturnsIndependentValues(t *testing.T) {
	first := DefaultBudget()
	first.Target = 1
	second := DefaultBudget()

	if second.Target != 640 || second.HardMax != 768 || second.Overlap != 64 || second.MinPayload != 80 {
		t.Errorf("DefaultBudget() = %+v, want 640/768/64/80", second)
	}
	policy := DefaultPolicy()
	if policy.Version != Version || policy.ConfigHash != ConfigHash(second) {
		t.Errorf("DefaultPolicy() = %+v, want version %q and matching config hash", policy, Version)
	}
}

func TestBudgetRejectsOverlapBeyondHardMaximum(t *testing.T) {
	_, err := chunkDocument(
		context.Background(),
		Document{Body: "paragraph"},
		wordCounter{},
		Budget{Target: 1, HardMax: 2, Overlap: 3},
	)
	if err == nil || !strings.Contains(err.Error(), "overlap must not exceed the hard maximum") {
		t.Fatalf("chunkDocument() error = %v, want overlap budget error", err)
	}
}

func TestChunkDocumentPropagatesCounterErrors(t *testing.T) {
	_, err := ChunkDocument(context.Background(), Document{Body: "paragraph"}, failingCounter{})
	if err == nil || !strings.Contains(err.Error(), "counter unavailable") {
		t.Fatalf("ChunkDocument() error = %v, want counter error", err)
	}
}

func TestChunkDocumentSplitsOversizedTablesIntoRowGroups(t *testing.T) {
	doc := Document{
		Path: "docs/table.md",
		Body: "| A | B |\n|---|---|\n| one two three four five | six seven eight nine ten |\n| eleven twelve thirteen | fourteen fifteen sixteen |\n",
	}
	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, Budget{Target: 24, HardMax: 30, Overlap: 2, MinPayload: 3})
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want split row groups", len(chunks))
	}
	for _, got := range chunks {
		if got.Kind != KindTableRowGroup {
			t.Errorf("chunk kind = %q, want %q", got.Kind, KindTableRowGroup)
		}
		if got.TokenCount > 30 {
			t.Errorf("token count %d exceeds hard max", got.TokenCount)
		}
		if got.StructuralGroupID == "" {
			t.Error("table chunks require structural group IDs")
		}
	}
	if chunks[0].StructuralGroupID != chunks[1].StructuralGroupID {
		t.Error("row groups from one table must share a structural group")
	}
	if chunks[0].NextChunkID != chunks[1].ChunkID {
		t.Errorf("same-table neighbors = %q, want %q", chunks[0].NextChunkID, chunks[1].ChunkID)
	}
}

func TestChunkPackageDoesNotImportDaemonTokenizer(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(file), "chunk.go"))
	if err != nil {
		t.Fatalf("read chunk.go: %v", err)
	}
	if strings.Contains(string(source), "internal/semantic/daemon") ||
		strings.Contains(string(source), "internal/index") {
		t.Error("chunk package must depend only on contracts for token counting")
	}
}
