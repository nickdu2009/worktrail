package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

func TestRunSearchWithoutSemanticPreservesLexicalOutput(t *testing.T) {
	env := semanticSearchTestEnv(t)
	args := []string{"--format", "json", "needle"}
	var baselineOut, baselineErr, searchOut, searchErr bytes.Buffer

	if err := runSearch(context.Background(), env, IO{Out: &baselineOut, Err: &baselineErr}, args); err != nil {
		t.Fatalf("runSearch baseline: %v", err)
	}
	if err := runSearchWithSemantic(context.Background(), env, IO{Out: &searchOut, Err: &searchErr}, args, unavailableSemanticSearcher{}); err != nil {
		t.Fatalf("runSearchWithSemantic lexical: %v", err)
	}
	if searchOut.String() != baselineOut.String() || searchErr.String() != baselineErr.String() {
		t.Fatalf("lexical output changed:\nstdout got=%q\nstdout want=%q\nstderr got=%q\nstderr want=%q", searchOut.String(), baselineOut.String(), searchErr.String(), baselineErr.String())
	}
	var results []index.Result
	if err := json.Unmarshal(searchOut.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal lexical JSON: %v", err)
	}
	if len(results) != 1 || results[0].Entry.Title != "Needle Rule" {
		t.Fatalf("lexical results = %#v", results)
	}
}

func TestRunSearchAcceptsSemanticModesAndPassesRequest(t *testing.T) {
	for _, arg := range []string{"--semantic", "--semantic=auto", "--semantic=required"} {
		t.Run(arg, func(t *testing.T) {
			searcher := &semanticSearchStub{response: SemanticSearchResponse{Results: []index.Result{{Entry: index.Entry{Title: "Semantic hit"}, Score: 2}}}}
			var out, errw bytes.Buffer
			err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{Out: &out, Err: &errw}, []string{arg, "--scope", "user", "--type", "rule", "--topic", "topic", "--tag", "tag", "needle"}, searcher)
			if err != nil {
				t.Fatalf("runSearchWithSemantic(%s): %v", arg, err)
			}
			if searcher.calls != 1 {
				t.Fatalf("searcher calls = %d, want 1", searcher.calls)
			}
			wantMode := contracts.ModeAuto
			if arg == "--semantic=required" {
				wantMode = contracts.ModeRequired
			}
			if got := searcher.request; got.Query != "needle" || got.Scope != "user" || got.Type != "rule" || got.Topic != "topic" || got.Tag != "tag" || got.Mode != wantMode || got.Limit != searchResultLimit {
				t.Fatalf("request = %#v", got)
			}
			if !strings.Contains(out.String(), "Semantic hit") || errw.Len() != 0 {
				t.Fatalf("stdout=%q stderr=%q", out.String(), errw.String())
			}
		})
	}
}

func TestRunSearchRejectsAmbiguousInvalidAndRepeatedSemanticOptions(t *testing.T) {
	for _, args := range [][]string{
		{"--semantic", "auto", "needle"},
		{"--semantic", "required", "needle"},
		{"--semantic=lexical", "needle"},
		{"--semantic=invalid", "needle"},
		{"--semantic", "--semantic=auto", "needle"},
		{"--semantic=auto", "--semantic=required", "needle"},
		{"--explain", "needle"},
		{"--semantic", "--explain=true", "needle"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{}, args, &semanticSearchStub{})
			if err == nil || !strings.Contains(err.Error(), "invalid semantic arguments") {
				t.Fatalf("runSearchWithSemantic(%v) error = %v", args, err)
			}
		})
	}
}

func TestSemanticRepairNextStepRoutesBundleAndProfile(t *testing.T) {
	if got := semanticRepairNextStep(contracts.ReasonBundleMissing, "project"); got != "worktrail init --semantic" {
		t.Fatalf("bundle missing next step = %q", got)
	}
	if got := semanticRepairNextStep(contracts.ReasonProfileStale, "user"); got != "worktrail semantic rebuild --scope user" {
		t.Fatalf("profile stale next step = %q", got)
	}
}

func TestRunSearchAutoFallbackPreservesLexicalJSONAndReportsNextStep(t *testing.T) {
	env := semanticSearchTestEnv(t)
	var lexicalOut, fallbackOut, fallbackErr bytes.Buffer
	if err := runSearch(context.Background(), env, IO{Out: &lexicalOut}, []string{"--format=json", "needle"}); err != nil {
		t.Fatalf("run lexical search: %v", err)
	}
	searcher := &semanticSearchStub{response: SemanticSearchResponse{
		Lanes: []SemanticSearchLane{{Name: "vector_knn", Degraded: true, Reason: contracts.ReasonGenerationMissing}},
	}}
	if err := runSearchWithSemantic(context.Background(), env, IO{Out: &fallbackOut, Err: &fallbackErr}, []string{"--semantic", "--format=json", "needle"}, searcher); err != nil {
		t.Fatalf("run semantic fallback: %v", err)
	}
	if fallbackOut.String() != lexicalOut.String() {
		t.Fatalf("fallback stdout = %q, want lexical JSON %q", fallbackOut.String(), lexicalOut.String())
	}
	for _, want := range []string{"semantic_generation_missing", "worktrail semantic rebuild --scope project"} {
		if !strings.Contains(fallbackErr.String(), want) {
			t.Fatalf("fallback stderr missing %q:\n%s", want, fallbackErr.String())
		}
	}
}

func TestRunSearchRequiredReturnsTypedError(t *testing.T) {
	var out, errw bytes.Buffer
	err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{Out: &out, Err: &errw}, []string{"--semantic=required", "needle"}, unavailableSemanticSearcher{})
	var semanticErr *SemanticSearchError
	if !errors.As(err, &semanticErr) || semanticErr.Code != contracts.ReasonRuntimeUnavailable {
		t.Fatalf("required error = %v, want runtime unavailable SemanticSearchError", err)
	}
	if out.Len() != 0 || errw.Len() != 0 {
		t.Fatalf("required error wrote stdout=%q stderr=%q", out.String(), errw.String())
	}
}

func TestRunSearchJSONV2EnvelopeForSuccessAndDegradedFallback(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		searcher := &semanticSearchStub{response: SemanticSearchResponse{
			Results: []index.Result{{Entry: index.Entry{Scope: "project", Type: "rule", Title: "Semantic hit"}, Score: 3}},
			Policy:  "semantic-retrieve-v2",
			Profile: "bge-m3",
			Lanes:   []SemanticSearchLane{{Name: "chunk_fts"}, {Name: "vector_knn"}},
		}}
		var out, errw bytes.Buffer
		if err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{Out: &out, Err: &errw}, []string{"--semantic=required", "--format=json-v2", "needle"}, searcher); err != nil {
			t.Fatalf("run JSON v2 success: %v", err)
		}
		var report semanticSearchEnvelope
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("unmarshal JSON v2: %v stdout=%s", err, out.String())
		}
		if report.Schema != semanticSearchResultSchema || report.Policy != "semantic-retrieve-v2" || report.Profile != "bge-m3" || len(report.Results) != 1 || report.Results[0].Entry.Title != "Semantic hit" || len(report.Lanes) != 2 {
			t.Fatalf("JSON v2 report = %#v", report)
		}
		if errw.Len() != 0 {
			t.Fatalf("success wrote stderr: %q", errw.String())
		}
	})

	t.Run("degraded fallback", func(t *testing.T) {
		env := semanticSearchTestEnv(t)
		searcher := &semanticSearchStub{response: SemanticSearchResponse{
			Policy:          "semantic-retrieve-v2",
			Degraded:        true,
			DegradedReasons: []contracts.ReasonCode{contracts.ReasonProfileStale},
			NextSteps:       []string{"worktrail semantic rebuild --scope project"},
		}}
		var out, errw bytes.Buffer
		if err := runSearchWithSemantic(context.Background(), env, IO{Out: &out, Err: &errw}, []string{"--semantic", "--format=json-v2", "needle"}, searcher); err != nil {
			t.Fatalf("run JSON v2 fallback: %v", err)
		}
		var report semanticSearchEnvelope
		if err := json.Unmarshal(out.Bytes(), &report); err != nil {
			t.Fatalf("unmarshal JSON v2 fallback: %v stdout=%s", err, out.String())
		}
		if report.Schema != semanticSearchResultSchema || len(report.Results) != 1 || report.Results[0].Entry.Title != "Needle Rule" || len(report.DegradedReasons) != 1 || report.DegradedReasons[0] != contracts.ReasonProfileStale || len(report.NextSteps) != 1 {
			t.Fatalf("JSON v2 fallback report = %#v", report)
		}
	})
}

func TestRunSearchRequiredJSONV2WritesJSONErrorEnvelope(t *testing.T) {
	var out bytes.Buffer
	err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{Out: &out}, []string{"--semantic=required", "--format=json-v2", "needle"}, unavailableSemanticSearcher{})
	if err != nil {
		t.Fatalf("required JSON v2 error = %v", err)
	}
	assertCLIErrorEnvelope(t, out.String(), string(contracts.ReasonRuntimeUnavailable))
	if strings.Contains(out.String(), "semantic search required but unavailable") == false {
		t.Fatalf("JSON error missing stable message: %s", out.String())
	}
}

func TestRunSearchJSONV2ErrorsUseCLIEnvelope(t *testing.T) {
	project := t.TempDir()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", home)

	for _, test := range []struct {
		name string
		args []string
		code string
	}{
		{
			name: "semantic parse error with spaced format",
			args: []string{"search", "--format", "json-v2", "--semantic", "auto", "needle"},
			code: "cli_usage_error",
		},
		{
			name: "required unavailable with equals format",
			args: []string{"search", "--format=json-v2", "--semantic=required", "needle"},
			code: string(contracts.ReasonBundleMissing),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, errw bytes.Buffer
			if err := Run(context.Background(), test.args, nil, &out, &errw); err != nil {
				t.Fatalf("Run(%v): %v", test.args, err)
			}
			assertCLIErrorEnvelope(t, out.String(), test.code)
			if errw.Len() != 0 {
				t.Fatalf("stderr contains non-JSON text: %q", errw.String())
			}
		})
	}
}

func TestCLIErrorCodesFallbackForEmptySemanticSearchCode(t *testing.T) {
	codes := cliErrorCodes(&SemanticSearchError{})
	if len(codes) != 1 || codes[0] == "" {
		t.Fatalf("empty semantic search code = %#v", codes)
	}
}

func TestRunSearchExplainUsesOnlyStderr(t *testing.T) {
	lexical := 1
	semantic := 2
	searcher := &semanticSearchStub{response: SemanticSearchResponse{
		Results: []index.Result{{Entry: index.Entry{ID: "entry-1", Scope: "project", Title: "Semantic hit"}, Score: 1}},
		Details: []SemanticSearchResultDetail{{
			Ranks: SemanticSearchRanks{Lexical: &lexical, Semantic: &semantic, Final: 1},
			ChunkMatches: []SemanticChunkMatch{{
				ChunkID:           "chunk-1",
				ChunkKind:         "table_row_group",
				StructuralGroupID: "group-1",
				EvidenceRole:      "match",
				Lanes:             []string{"chunk_fts"},
				PrimarySourceRange: SemanticByteRange{StartByte: 10, EndByte: 20},
			}},
		}},
		Policy:  "semantic-retrieve-v2",
		Profile: "bge-m3",
		Lanes:   []SemanticSearchLane{{Name: "chunk_fts", RawHits: 3}, {Name: "vector_knn", RawHits: 2}},
	}}
	var out, errw bytes.Buffer
	if err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{Out: &out, Err: &errw}, []string{"--semantic", "--explain", "--format=json", "needle"}, searcher); err != nil {
		t.Fatalf("run explain search: %v", err)
	}
	var results []index.Result
	if err := json.Unmarshal(out.Bytes(), &results); err != nil {
		t.Fatalf("unmarshal JSON v1 explain output: %v stdout=%s", err, out.String())
	}
	if len(results) != 1 || results[0].Entry.Title != "Semantic hit" {
		t.Fatalf("JSON v1 explain results = %#v", results)
	}
	stderr := errw.String()
	for _, want := range []string{
		"policy: semantic-retrieve-v2",
		"profile: bge-m3",
		"lane: chunk_fts",
		"lane: vector_knn",
		"result: 1 scope=project entry_id=entry-1 final=1",
		"match chunk_id=chunk-1",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("explain stderr missing %q:\n%s", want, stderr)
		}
	}
	for _, forbidden := range []string{"needle", "embedding_input", "\"body\""} {
		if strings.Contains(stderr, forbidden) {
			t.Fatalf("explain stderr leaked raw content %q:\n%s", forbidden, stderr)
		}
	}
}

func TestRunSearchJSONV2TableGoldenAndEvidenceNotOnIndexResult(t *testing.T) {
	lexical := 1
	semantic := 2
	searcher := &semanticSearchStub{response: SemanticSearchResponse{
		Results: []index.Result{{
			Entry: index.Entry{
				Schema:    "worktrail.index.entry.v1",
				ID:        "table-entry",
				Scope:     "project",
				Type:      "architecture",
				Path:      "architecture/table.md",
				Title:     "Table Hardening",
				Lifecycle: "current",
				UpdatedAt: mustParseTime(t, "2026-07-20T00:00:00Z"),
			},
			Score: 0.032,
		}},
		Details: []SemanticSearchResultDetail{{
			Ranks: SemanticSearchRanks{Lexical: &lexical, Semantic: &semantic, Final: 1},
			ChunkMatches: []SemanticChunkMatch{
				{
					ChunkID:           "chunk-match",
					ChunkKind:         "table_row_group",
					StructuralGroupID: "table-1",
					HeadingBreadcrumb: []string{"Architecture", "Runtime"},
					EvidenceRole:      "match",
					Lanes:             []string{"chunk_fts", "vector_knn"},
					BestChunkRanks:    map[string]int{"chunk_fts": 3, "vector_knn": 7},
					PrimarySourceRange: SemanticByteRange{StartByte: 120, EndByte: 360},
					ContextSourceRange: &SemanticByteRange{StartByte: 40, EndByte: 119},
					StructuralGroupSourceRange: &SemanticByteRange{StartByte: 40, EndByte: 980},
				},
				{
					ChunkID:           "chunk-neighbor",
					ChunkKind:         "table_row_group",
					StructuralGroupID: "table-1",
					HeadingBreadcrumb: []string{"Architecture", "Runtime"},
					EvidenceRole:      "neighbor",
					PrimarySourceRange: SemanticByteRange{StartByte: 360, EndByte: 480},
				},
			},
		}},
		Policy:  "semantic-retrieve-v2",
		Profile: "bge-m3",
		Lanes: []SemanticSearchLane{{
			Scope: "project", Name: "chunk_fts", RawHits: 80, FilterRejections: 4,
			EligibleEntries: 10, RefillRounds: 2, HardCap: 200,
		}},
	}}
	var out bytes.Buffer
	if err := runSearchWithSemantic(context.Background(), paths.Env{}, IO{Out: &out}, []string{"--semantic=required", "--format=json-v2", "table"}, searcher); err != nil {
		t.Fatalf("run JSON v2 golden: %v", err)
	}
	goldenPath := filepath.Join("testdata", "search-json-v2-table.golden")
	if os.Getenv("WRITE_GOLDEN") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", goldenPath)
		return
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if out.String() != string(want) {
		t.Fatalf("JSON v2 golden mismatch\ngot:\n%s\nwant:\n%s", out.String(), want)
	}

	raw, _ := json.Marshal(searcher.response.Results[0])
	if strings.Contains(string(raw), "chunk_matches") || strings.Contains(string(raw), "chunk-match") {
		t.Fatalf("index.Result unexpectedly carries chunk evidence: %s", raw)
	}
	var roundTrip index.Result
	if err := json.Unmarshal(raw, &roundTrip); err != nil {
		t.Fatalf("round-trip index.Result: %v", err)
	}
	if roundTrip.Entry.ID != "table-entry" {
		t.Fatalf("round-trip entry = %#v", roundTrip)
	}
}

func TestProductionSearchJSONV1GoldenRemainsReadableBaseline(t *testing.T) {
	goldenPath := filepath.Join("..", "..", "scripts", "semantic", "fixtures", "production-e2e", "search-json-v1.golden")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read production JSON v1 golden: %v", err)
	}
	var results []index.Result
	if err := json.Unmarshal(want, &results); err != nil {
		t.Fatalf("production JSON v1 golden is not valid index.Result JSON: %v", err)
	}
	if len(results) != 1 || results[0].Entry.ID != "e2e-decision-hybrid-recall" {
		t.Fatalf("production JSON v1 golden unexpected: %#v", results)
	}
	if strings.Contains(string(want), "chunk_matches") || strings.Contains(string(want), "worktrail.search.results.v2") {
		t.Fatal("production JSON v1 golden was altered toward v2")
	}
}

func mustParseTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

type semanticSearchStub struct {
	response SemanticSearchResponse
	err      error
	request  SemanticSearchRequest
	calls    int
}

func (s *semanticSearchStub) Search(_ context.Context, request SemanticSearchRequest) (SemanticSearchResponse, error) {
	s.calls++
	s.request = request
	return s.response, s.err
}

func semanticSearchTestEnv(t *testing.T) paths.Env {
	t.Helper()
	project := t.TempDir()
	home := t.TempDir()
	root := filepath.Join(project, ".worktrail")
	if err := os.MkdirAll(filepath.Join(root, "rules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "rules", "needle.md"), []byte("# Needle Rule\n\nneedle body"), 0o644); err != nil {
		t.Fatal(err)
	}
	return paths.Env{
		Home:        home,
		UserRoot:    filepath.Join(home, ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   root,
	}
}
