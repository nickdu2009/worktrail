package chunk

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

var candidateBudgets = []Budget{
	{Target: 384, HardMax: 640, Overlap: 64, MinPayload: 80},
	{Target: 512, HardMax: 768, Overlap: 64, MinPayload: 80},
	{Target: 512, HardMax: 768, Overlap: 64, MinPayload: 128},
	{Target: 640, HardMax: 768, Overlap: 64, MinPayload: 80},
}

func TestCandidateBudgetsPreserveTableStructuralInvariants(t *testing.T) {
	body := readTestdata(t, "long-endpoint-matrix.md")
	doc := Document{
		Scope: "project",
		ID:    "endpoint-matrix",
		Path:  "testdata/long-endpoint-matrix.md",
		Title: "Endpoint Matrix",
		Type:  "architecture",
		Body:  body,
	}
	for _, budget := range candidateBudgets {
		budget := budget
		t.Run(budgetName(budget), func(t *testing.T) {
			policy := PolicyWithBudget(budget)
			chunks, err := ChunkDocumentWithPolicy(context.Background(), doc, wordCounter{}, policy)
			if err != nil {
				t.Fatalf("ChunkDocumentWithPolicy() error = %v", err)
			}
			assertTableStructuralInvariants(t, doc, chunks, budget)
		})
	}
}

func TestLongJourneyBranchesSplitsUnderDefaultPolicy(t *testing.T) {
	body := readTestdata(t, "long-journey-branches.md")
	doc := Document{
		Scope: "project",
		ID:    "journey-branches",
		Path:  "testdata/long-journey-branches.md",
		Title: "Journey Branches",
		Body:  body,
	}
	chunks, err := ChunkDocument(context.Background(), doc, wordCounter{})
	if err != nil {
		t.Fatalf("ChunkDocument() error = %v", err)
	}
	assertTableStructuralInvariants(t, doc, chunks, DefaultBudget())
	if len(chunks) < 2 {
		t.Fatalf("len(chunks) = %d, want oversized journey table to split", len(chunks))
	}
}

func TestTableChunksGolden(t *testing.T) {
	goldenPath := filepath.Join("testdata", "table-chunks.golden.json")
	raw, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	var golden tableGolden
	if err := json.Unmarshal(raw, &golden); err != nil {
		t.Fatalf("unmarshal golden: %v", err)
	}

	doc := Document{
		Scope:          golden.Document.Scope,
		ID:             golden.Document.ID,
		Path:           golden.Document.Path,
		Title:          golden.Document.Title,
		Type:           golden.Document.Type,
		Topic:          golden.Document.Topic,
		Tags:           append([]string(nil), golden.Document.Tags...),
		Body:           golden.Source,
		SourceBaseByte: golden.Document.SourceBaseByte,
		SourceSizeByte: golden.Document.SourceSizeByte,
	}
	budget := Budget{
		Target:     golden.Budget.Target,
		HardMax:    golden.Budget.HardMax,
		Overlap:    golden.Budget.Overlap,
		MinPayload: golden.Budget.MinPayload,
	}
	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	got := make([]goldenChunk, 0, len(chunks))
	for _, chunk := range chunks {
		got = append(got, toGoldenChunk(chunk))
	}
	if !reflect.DeepEqual(got, golden.Chunks) {
		gotJSON, _ := json.MarshalIndent(got, "", "  ")
		wantJSON, _ := json.MarshalIndent(golden.Chunks, "", "  ")
		t.Fatalf("golden mismatch\ngot:\n%s\nwant:\n%s", gotJSON, wantJSON)
	}

	// Absolute ranges must slice the original source file view.
	sourceView := make([]byte, doc.SourceBaseByte+len(doc.Body))
	copy(sourceView[doc.SourceBaseByte:], doc.Body)
	if doc.SourceSizeByte > 0 && len(sourceView) < doc.SourceSizeByte {
		padded := make([]byte, doc.SourceSizeByte)
		copy(padded, sourceView)
		sourceView = padded
	}
	for _, chunk := range chunks {
		switch {
		case chunk.Kind == KindTableCellFragment:
			// Synthetic cell fragments may cite one cell range multiple times.
			continue
		case chunk.Kind == KindTableRowGroup && chunk.ContextRange == nil && strings.Contains(chunk.Body, "="):
			// Synthetic column-group bodies are not raw source slices.
			continue
		default:
			if string(sourceView[chunk.SourceStart:chunk.SourceEnd]) != chunk.Body {
				t.Fatalf("primary slice mismatch for kind=%s order=%d", chunk.Kind, chunk.Order)
			}
		}
	}

	again, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("second chunkDocument() error = %v", err)
	}
	if !reflect.DeepEqual(chunks, again) {
		t.Fatal("golden document chunking is not deterministic")
	}
}

func TestIrreducibleTableErrorSupportsErrorsIs(t *testing.T) {
	// Oversized table whose reused header context leaves no MinPayload room.
	headerWords := strings.Repeat("header meta ", 20)
	rowWords := strings.Repeat("row payload ", 40)
	doc := Document{
		Path:  "docs/wide.md",
		Title: strings.Repeat("title ", 20),
		Body:  "| " + headerWords + " |\n|---|\n| " + rowWords + " |\n",
	}
	_, err := chunkDocument(context.Background(), doc, wordCounter{}, Budget{Target: 30, HardMax: 40, Overlap: 2, MinPayload: 25})
	if err == nil {
		t.Fatal("expected IrreducibleTableError")
	}
	if !errors.Is(err, ErrIrreducibleTable) {
		t.Fatalf("errors.Is(err, ErrIrreducibleTable) = false for %v", err)
	}
	var typed *IrreducibleTableError
	if !errors.As(err, &typed) {
		t.Fatalf("errors.As() failed for %v", err)
	}
	if typed.Path != doc.Path || typed.BlockKind != GroupKindTable || typed.HardMax != 40 || typed.Reason == "" {
		t.Fatalf("typed error incomplete: %+v", typed)
	}
	if typed.StartByte >= typed.EndByte {
		t.Fatalf("typed error range invalid: %+v", typed)
	}
	if typed.Reason == IrreducibleReason("semantic_profile_stale") {
		t.Fatal("irreducible table reason must not reuse semantic_profile_stale")
	}
}

func TestSourceBaseByteShiftsAbsoluteRangesAndIDs(t *testing.T) {
	body := "| A | B |\n|---|---|\n| café | pipe\\|ok |\n"
	base := Document{
		Scope: "project",
		ID:    "doc-1",
		Path:  "docs/table.md",
		Body:  body,
	}
	shifted := base
	shifted.SourceBaseByte = 40
	shifted.SourceSizeByte = 40 + len(body)

	left, err := ChunkDocument(context.Background(), base, wordCounter{})
	if err != nil {
		t.Fatalf("base ChunkDocument() error = %v", err)
	}
	right, err := ChunkDocument(context.Background(), shifted, wordCounter{})
	if err != nil {
		t.Fatalf("shifted ChunkDocument() error = %v", err)
	}
	if len(left) != 1 || len(right) != 1 {
		t.Fatalf("chunk counts = (%d,%d), want (1,1)", len(left), len(right))
	}
	if right[0].SourceStart != left[0].SourceStart+40 || right[0].SourceEnd != left[0].SourceEnd+40 {
		t.Fatalf("absolute ranges not shifted: base=%d:%d shifted=%d:%d", left[0].SourceStart, left[0].SourceEnd, right[0].SourceStart, right[0].SourceEnd)
	}
	if left[0].ChunkID == right[0].ChunkID || left[0].StructuralGroupID == right[0].StructuralGroupID {
		t.Fatal("source base must affect chunk and structural group IDs")
	}
}

func TestCellFragmentFallbackAllowsRepeatedCellRanges(t *testing.T) {
	words := strings.Repeat("alpha beta gamma delta ", 80)
	doc := Document{
		Path: "docs/cell.md",
		Body: "| Col |\n|---|\n| " + words + " |\n",
	}
	budget := Budget{Target: 40, HardMax: 55, Overlap: 4, MinPayload: 5}
	chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
	if err != nil {
		t.Fatalf("chunkDocument() error = %v", err)
	}
	var fragments []Chunk
	for _, chunk := range chunks {
		if chunk.Kind == KindTableCellFragment {
			fragments = append(fragments, chunk)
		}
	}
	if len(fragments) < 2 {
		t.Fatalf("len(fragments) = %d, want cell fallback fragments", len(fragments))
	}
	for i := 1; i < len(fragments); i++ {
		if fragments[i].SourceStart != fragments[0].SourceStart || fragments[i].SourceEnd != fragments[0].SourceEnd {
			t.Fatalf("fragment ranges diverged: %#v vs %#v", fragments[0], fragments[i])
		}
		if fragments[i].FragmentOrdinal <= fragments[i-1].FragmentOrdinal {
			t.Fatalf("fragment ordinals must advance: %d then %d", fragments[i-1].FragmentOrdinal, fragments[i].FragmentOrdinal)
		}
		if fragments[i].TokenCount > budget.HardMax {
			t.Fatalf("fragment exceeds HardMax: %d", fragments[i].TokenCount)
		}
	}
}

func FuzzChunkDocumentTableRanges(f *testing.F) {
	f.Add("| A | B |\n|---|---|\n| one | two |\n")
	f.Add("| A | B |\r\n|---|---|\r\n| café | x |\r\n")
	f.Add("# H\n\n| Col \\| A | B |\n| ---: | --- |\n| value | other |\n")
	journey := readTestdata(f, "long-journey-branches.md")
	f.Add(journey)
	if len(journey) > 1200 {
		// Keep a short well-formed prefix seed rather than a mid-row truncation.
		f.Add(journey[:1200])
	}

	f.Fuzz(func(t *testing.T, body string) {
		if !utf8.ValidString(body) {
			return
		}
		if len(body) > 8000 {
			body = body[:8000]
		}
		doc := Document{
			Scope: "project",
			ID:    "fuzz",
			Path:  "fuzz.md",
			Body:  body,
		}
		for _, budget := range []Budget{
			{Target: 32, HardMax: 48, Overlap: 4, MinPayload: 5},
			DefaultBudget(),
		} {
			chunks, err := chunkDocument(context.Background(), doc, wordCounter{}, budget)
			if err != nil {
				// Malformed fuzz inputs may fail atom collection; irreducible
				// tables are typed failures. Only successful emissions are
				// checked for range/neighbor/HardMax invariants.
				if errors.Is(err, ErrIrreducibleTable) {
					continue
				}
				msg := err.Error()
				if strings.Contains(msg, "cannot determine") ||
					strings.Contains(msg, "gfm table node not found") ||
					strings.Contains(msg, "has no header") ||
					strings.Contains(msg, "invalid table header range") ||
					strings.Contains(msg, "invalid absolute range") {
					continue
				}
				t.Fatalf("chunkDocument() error = %v", err)
			}
			limit := doc.SourceBaseByte + len(doc.Body)
			if doc.SourceSizeByte > 0 {
				limit = doc.SourceSizeByte
			}
			for i, chunk := range chunks {
				if chunk.SourceStart < 0 || chunk.SourceStart >= chunk.SourceEnd || chunk.SourceEnd > limit {
					t.Fatalf("out of range primary [%d,%d) limit=%d", chunk.SourceStart, chunk.SourceEnd, limit)
				}
				if chunk.TokenCount > budget.HardMax {
					t.Fatalf("HardMax exceeded: %d > %d", chunk.TokenCount, budget.HardMax)
				}
				if chunk.GroupRange != nil {
					if chunk.GroupRange.Start < 0 || chunk.GroupRange.Start >= chunk.GroupRange.End || chunk.GroupRange.End > limit {
						t.Fatalf("out of range group %#v", chunk.GroupRange)
					}
				}
				if i > 0 && chunk.PrevChunkID != "" {
					if chunks[i-1].StructuralGroupID != chunk.StructuralGroupID {
						t.Fatalf("cross-group neighbor at %d", i)
					}
				}
				if chunk.Kind == KindTableCellFragment && i > 0 {
					prev := chunks[i-1]
					if prev.Kind == KindTableCellFragment &&
						prev.SourceStart == chunk.SourceStart &&
						prev.SourceEnd == chunk.SourceEnd &&
						chunk.FragmentOrdinal <= prev.FragmentOrdinal &&
						prev.StructuralGroupID == chunk.StructuralGroupID {
						t.Fatalf("zero-progress cell fragment ordinals")
					}
				}
			}
		}
	})
}

type tableGolden struct {
	Source   string         `json:"source"`
	Document goldenDocument `json:"document"`
	Budget   goldenBudget   `json:"budget"`
	Chunks   []goldenChunk  `json:"chunks"`
}

type goldenDocument struct {
	Scope          string   `json:"scope"`
	ID             string   `json:"id"`
	Path           string   `json:"path"`
	Title          string   `json:"title"`
	Type           string   `json:"type"`
	Topic          string   `json:"topic"`
	Tags           []string `json:"tags"`
	SourceBaseByte int      `json:"source_base_byte"`
	SourceSizeByte int      `json:"source_size_byte"`
}

type goldenBudget struct {
	Target     int `json:"target"`
	HardMax    int `json:"hard_max"`
	Overlap    int `json:"overlap"`
	MinPayload int `json:"min_payload"`
}

type goldenChunk struct {
	Kind              string   `json:"kind"`
	Order             int      `json:"order"`
	FragmentOrdinal   int      `json:"fragment_ordinal"`
	StructuralGroupID string   `json:"structural_group_id"`
	ChunkID           string   `json:"chunk_id"`
	HeadingBreadcrumb []string `json:"heading_breadcrumb"`
	SourceStart       int      `json:"source_start"`
	SourceEnd         int      `json:"source_end"`
	ContextStart      *int     `json:"context_start"`
	ContextEnd        *int     `json:"context_end"`
	GroupStart        *int     `json:"group_start"`
	GroupEnd          *int     `json:"group_end"`
	Body              string   `json:"body"`
	ContextTerms      string   `json:"context_terms"`
	TokenCount        int      `json:"token_count"`
	PrevChunkID       string   `json:"prev_chunk_id"`
	NextChunkID       string   `json:"next_chunk_id"`
	ChunkerVersion    string   `json:"chunker_version"`
}

func toGoldenChunk(chunk Chunk) goldenChunk {
	got := goldenChunk{
		Kind:              chunk.Kind,
		Order:             chunk.Order,
		FragmentOrdinal:   chunk.FragmentOrdinal,
		StructuralGroupID: chunk.StructuralGroupID,
		ChunkID:           chunk.ChunkID,
		HeadingBreadcrumb: append([]string(nil), chunk.HeadingBreadcrumb...),
		SourceStart:       chunk.SourceStart,
		SourceEnd:         chunk.SourceEnd,
		Body:              chunk.Body,
		ContextTerms:      chunk.ContextTerms,
		TokenCount:        chunk.TokenCount,
		PrevChunkID:       chunk.PrevChunkID,
		NextChunkID:       chunk.NextChunkID,
		ChunkerVersion:    chunk.ChunkerVersion,
	}
	if chunk.ContextRange != nil {
		start := chunk.ContextRange.Start
		end := chunk.ContextRange.End
		got.ContextStart = &start
		got.ContextEnd = &end
	}
	if chunk.GroupRange != nil {
		start := chunk.GroupRange.Start
		end := chunk.GroupRange.End
		got.GroupStart = &start
		got.GroupEnd = &end
	}
	return got
}

func assertTableStructuralInvariants(t *testing.T, doc Document, chunks []Chunk, budget Budget) {
	t.Helper()
	if len(chunks) == 0 {
		t.Fatal("expected chunks")
	}
	tableStart, tableEnd, ok := firstTableRange(doc.Body)
	if !ok {
		t.Fatal("testdata missing GFM table")
	}
	absStart := doc.SourceBaseByte + tableStart
	absEnd := doc.SourceBaseByte + tableEnd

	var (
		groupID string
		covered []ByteRange
	)
	for i, chunk := range chunks {
		if chunk.TokenCount > budget.HardMax {
			t.Fatalf("chunk %d token count %d > HardMax %d", i, chunk.TokenCount, budget.HardMax)
		}
		if chunk.ChunkerVersion != Version {
			t.Fatalf("ChunkerVersion = %q, want %q", chunk.ChunkerVersion, Version)
		}
		if chunk.Kind != KindTableRowGroup && chunk.Kind != KindTableCellFragment && chunk.Kind != KindText {
			t.Fatalf("unexpected kind %q", chunk.Kind)
		}
		if chunk.GroupRange == nil {
			continue
		}
		if chunk.GroupRange.Start == absStart && chunk.GroupRange.End == absEnd {
			if groupID == "" {
				groupID = chunk.StructuralGroupID
			} else if chunk.StructuralGroupID != groupID {
				t.Fatalf("table chunks diverged structural group IDs")
			}
			if chunk.Kind == KindTableRowGroup && chunk.ContextRange != nil {
				covered = append(covered, ByteRange{Start: chunk.SourceStart, End: chunk.SourceEnd})
			}
		}
		if i > 0 && chunk.PrevChunkID != "" && chunks[i-1].StructuralGroupID != chunk.StructuralGroupID {
			t.Fatalf("cross-group neighbor between %d and %d", i-1, i)
		}
	}
	if groupID == "" {
		t.Fatal("expected table structural group")
	}
	if len(covered) > 0 {
		assertExactOrderedCoverage(t, covered)
	}
}

func assertExactOrderedCoverage(t *testing.T, ranges []ByteRange) {
	t.Helper()
	for i := range ranges {
		if ranges[i].Start >= ranges[i].End {
			t.Fatalf("empty/inverted range %#v", ranges[i])
		}
		if i == 0 {
			continue
		}
		if ranges[i].Start < ranges[i-1].End {
			t.Fatalf("overlapping row-group ranges %#v and %#v", ranges[i-1], ranges[i])
		}
		if ranges[i].Start != ranges[i-1].End {
			t.Fatalf("gap between row-group ranges %#v and %#v", ranges[i-1], ranges[i])
		}
	}
}

func firstTableRange(body string) (int, int, bool) {
	atoms, err := collectAtoms([]byte(body))
	if err != nil {
		return 0, 0, false
	}
	for _, atom := range atoms {
		if atom.table {
			return atom.start, atom.end, true
		}
	}
	return 0, 0, false
}

func budgetName(budget Budget) string {
	return fmt.Sprintf("%d_%d_%d", budget.Target, budget.HardMax, budget.MinPayload)
}

func readTestdata(t testing.TB, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read testdata %s: %v", name, err)
	}
	return string(raw)
}
