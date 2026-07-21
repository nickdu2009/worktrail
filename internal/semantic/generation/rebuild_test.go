package generation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/policy"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestRebuildActivatesVerifiedCandidate(t *testing.T) {
	root := t.TempDir()
	semanticDir := filepath.Join(t.TempDir(), "semantic")
	writeRebuildSource(t, root, "  leading source body\n")

	request := rebuildRequest(root, semanticDir, "generation-001", &rebuildEmbedder{})
	active, err := Rebuild(context.Background(), request)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	sources, err := policySources(root)
	if err != nil {
		t.Fatalf("FreshSources() error = %v", err)
	}
	if active.GenerationID != request.GenerationID ||
		active.RecallProfileID != request.Metadata.Profile ||
		active.BundleID != request.BundleID ||
		active.SnapshotHash != sources {
		t.Fatalf("active pointer = %#v, does not match rebuild request", active)
	}
	stored, err := ReadActive(semanticDir)
	if err != nil {
		t.Fatalf("ReadActive() error = %v", err)
	}
	if stored != active {
		t.Fatalf("stored pointer = %#v, want %#v", stored, active)
	}
	candidatePath, err := generationPath(semanticDir, request.GenerationID, ".sqlite")
	if err != nil {
		t.Fatalf("generationPath() error = %v", err)
	}
	metadata := request.Metadata
	metadata.Generation = request.GenerationID
	metadata.Snapshot = sources
	sealed, err := OpenSealed(candidatePath, metadata)
	if err != nil {
		t.Fatalf("OpenSealed() error = %v", err)
	}
	if err := sealed.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	info, err := os.Stat(semanticDir)
	if err != nil {
		t.Fatalf("stat semantic directory: %v", err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("semantic directory mode = %o, want 700", info.Mode().Perm())
	}
}

func TestRebuildCatchUpConvergesAfterSourceChange(t *testing.T) {
	root := t.TempDir()
	semanticDir := t.TempDir()
	writeRebuildSource(t, root, "first source body\n")
	previous := testPointer("previous-generation", "")
	if err := writePointer(semanticDir, previous); err != nil {
		t.Fatalf("write previous pointer: %v", err)
	}
	request := rebuildRequest(root, semanticDir, "generation-002", &rebuildEmbedder{
		onFirstEmbed: func() {
			writeRebuildSource(t, root, "changed source body\n")
		},
	})

	active, err := Rebuild(context.Background(), request)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	sources, err := policySources(root)
	if err != nil {
		t.Fatalf("FreshSources() error = %v", err)
	}
	if active.SnapshotHash != sources {
		t.Fatalf("active snapshot = %q, want %q", active.SnapshotHash, sources)
	}
	if active.GenerationID == previous.GenerationID {
		t.Fatal("active pointer was not replaced")
	}
}

func TestRebuildCatchUpAddChangeDeleteConverges(t *testing.T) {
	root := t.TempDir()
	semanticDir := t.TempDir()
	writeRebuildSource(t, root, "base body\n")
	writeRebuildSourceAt(t, root, "rules/extra.md", "extra-001", "extra body\n")

	var scans atomic.Int32
	request := rebuildRequest(root, semanticDir, "generation-catchup", &rebuildEmbedder{})
	request.ScanSources = func(scanRoot, scope string) (index.SourceSnapshot, error) {
		n := scans.Add(1)
		if n == 2 {
			writeRebuildSource(t, root, "changed base body\n")
			_ = os.Remove(filepath.Join(root, "rules", "extra.md"))
			writeRebuildSourceAt(t, root, "rules/added.md", "added-001", "added body\n")
		}
		return policy.FreshSources(scanRoot, scope)
	}

	active, err := Rebuild(context.Background(), request)
	if err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	final, err := policy.FreshSources(root, "project")
	if err != nil {
		t.Fatalf("FreshSources() error = %v", err)
	}
	if active.SnapshotHash != final.SemanticSnapshotHash {
		t.Fatalf("snapshot = %q, want %q", active.SnapshotHash, final.SemanticSnapshotHash)
	}
	if scans.Load() < 3 {
		t.Fatalf("scans = %d, want at least 3", scans.Load())
	}
}

func TestRebuildCatchUpPassAndBudgetBoundariesDoNotActivate(t *testing.T) {
	root := t.TempDir()
	semanticDir := t.TempDir()
	writeRebuildSource(t, root, "body\n")
	previous := testPointer("previous-generation", "")
	if err := writePointer(semanticDir, previous); err != nil {
		t.Fatalf("write previous pointer: %v", err)
	}

	t.Run("pass limit", func(t *testing.T) {
		var scans atomic.Int32
		request := rebuildRequest(root, semanticDir, "generation-pass-limit", &rebuildEmbedder{})
		request.MaxCatchUpPasses = 3
		request.ScanSources = func(scanRoot, scope string) (index.SourceSnapshot, error) {
			n := scans.Add(1)
			writeRebuildSource(t, root, "body "+strings.Repeat("x", int(n))+"\n")
			return policy.FreshSources(scanRoot, scope)
		}
		_, err := Rebuild(context.Background(), request)
		if !errors.Is(err, ErrSourcesChanged) {
			t.Fatalf("Rebuild() error = %v, want ErrSourcesChanged", err)
		}
		active, err := ReadActive(semanticDir)
		if err != nil {
			t.Fatalf("ReadActive() error = %v", err)
		}
		if active != previous {
			t.Fatalf("active pointer = %#v, want unchanged", active)
		}
	})

	t.Run("budget limit", func(t *testing.T) {
		start := time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)
		now := start
		request := rebuildRequest(root, semanticDir, "generation-budget-limit", &rebuildEmbedder{})
		request.CatchUpBudget = 30 * time.Second
		request.Now = func() time.Time { return now }
		request.ScanSources = func(scanRoot, scope string) (index.SourceSnapshot, error) {
			now = now.Add(31 * time.Second)
			writeRebuildSource(t, root, "body "+now.Format(time.RFC3339Nano)+"\n")
			return policy.FreshSources(scanRoot, scope)
		}
		_, err := Rebuild(context.Background(), request)
		if !errors.Is(err, ErrSourcesChanged) {
			t.Fatalf("Rebuild() error = %v, want ErrSourcesChanged", err)
		}
	})
}

func TestActivateFinalCheckFailureLeavesPointerUnchanged(t *testing.T) {
	semanticDir := t.TempDir()
	previous := testPointer("previous-generation", "")
	if err := writePointer(semanticDir, previous); err != nil {
		t.Fatalf("write previous pointer: %v", err)
	}
	_, err := Activate(context.Background(), semanticDir, func(context.Context) (ActivationCandidate, error) {
		return ActivationCandidate{
			Scope:           previous.Scope,
			GenerationID:    "candidate-generation",
			RecallProfileID: previous.RecallProfileID,
			BundleID:        previous.BundleID,
			SnapshotHash:    "sealed-snapshot",
		}, nil
	}, ActivateOptions{FinalSnapshot: func(context.Context) (string, error) {
		return "live-different-snapshot", nil
	}})
	if !errors.Is(err, ErrSourcesChanged) {
		t.Fatalf("Activate() error = %v, want ErrSourcesChanged", err)
	}
	active, err := ReadActive(semanticDir)
	if err != nil {
		t.Fatalf("ReadActive() error = %v", err)
	}
	if active != previous {
		t.Fatalf("active pointer = %#v, want unchanged", active)
	}
}

func TestRebuildRejectsInvalidPolicyBinding(t *testing.T) {
	root := t.TempDir()
	writeRebuildSource(t, root, "body\n")
	request := rebuildRequest(root, t.TempDir(), "generation-policy", &rebuildEmbedder{})
	request.Versions.ChunkerVersion = "chunker-v2-eval-deadbeef"
	if _, err := Rebuild(context.Background(), request); err == nil || !strings.Contains(err.Error(), "chunker version mismatch") {
		t.Fatalf("Rebuild() error = %v, want chunker version mismatch", err)
	}

	request = rebuildRequest(root, t.TempDir(), "generation-policy-2", &rebuildEmbedder{})
	budget := chunk.DefaultBudget()
	budget.Target = 384
	request.Policy = chunk.ChunkingPolicy{
		Version:    chunk.Version,
		Budget:     budget,
		ConfigHash: chunk.ConfigHash(budget),
	}
	request.Versions.ChunkerVersion = chunk.Version
	if _, err := Rebuild(context.Background(), request); err == nil || !strings.Contains(err.Error(), "DefaultPolicy") {
		t.Fatalf("Rebuild() error = %v, want DefaultPolicy rejection", err)
	}
}

func TestRebuildFailuresLeaveActivePointerUnchanged(t *testing.T) {
	for _, test := range []struct {
		name      string
		counter   rebuildCounter
		embedder  *rebuildEmbedder
		wantError string
	}{
		{
			name:      "token counter",
			counter:   rebuildCounter{err: errors.New("token counter failed")},
			embedder:  &rebuildEmbedder{},
			wantError: "token counter failed",
		},
		{
			name:      "embedder",
			counter:   rebuildCounter{},
			embedder:  &rebuildEmbedder{err: errors.New("embedder failed")},
			wantError: "embedder failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			semanticDir := t.TempDir()
			writeRebuildSource(t, root, "source body\n")
			previous := testPointer("previous-generation", "")
			if err := writePointer(semanticDir, previous); err != nil {
				t.Fatalf("write previous pointer: %v", err)
			}
			request := rebuildRequest(root, semanticDir, "generation-"+strings.ReplaceAll(test.name, " ", "-"), test.embedder)
			request.TokenCounter = test.counter

			_, err := Rebuild(context.Background(), request)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("Rebuild() error = %v, want substring %q", err, test.wantError)
			}
			active, err := ReadActive(semanticDir)
			if err != nil {
				t.Fatalf("ReadActive() error = %v", err)
			}
			if active != previous {
				t.Fatalf("active pointer = %#v, want unchanged %#v", active, previous)
			}
		})
	}
}

func TestRebuildPreservesLeadingSourceWhitespaceInChunkBody(t *testing.T) {
	root := t.TempDir()
	writeRebuildSource(t, root, "  leading source body\n")
	embedder := &rebuildEmbedder{}
	request := rebuildRequest(root, filepath.Join(t.TempDir(), "semantic"), "generation-003", embedder)

	if _, err := Rebuild(context.Background(), request); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	if len(embedder.inputs) != 1 {
		t.Fatalf("Embed() inputs = %d, want 1", len(embedder.inputs))
	}
	if !strings.HasSuffix(embedder.inputs[0], "\n\n  leading source body\n") {
		t.Fatalf("embedding input = %q, want original leading whitespace in body", embedder.inputs[0])
	}
}

func TestRebuildUsesAbsoluteSourceRanges(t *testing.T) {
	root := t.TempDir()
	body := "absolute range body\n"
	writeRebuildSource(t, root, body)
	request := rebuildRequest(root, filepath.Join(t.TempDir(), "semantic"), "generation-ranges", &rebuildEmbedder{})
	if _, err := Rebuild(context.Background(), request); err != nil {
		t.Fatalf("Rebuild() error = %v", err)
	}
	sources, err := policy.FreshSources(root, "project")
	if err != nil {
		t.Fatalf("FreshSources() error = %v", err)
	}
	if len(sources.Records) != 1 || sources.Records[0].BodyStartByte <= 0 {
		t.Fatalf("source record = %#v", sources.Records)
	}
	raw, err := os.ReadFile(filepath.Join(root, "rules", "current.md"))
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	path, err := generationPath(request.SemanticDir, request.GenerationID, ".sqlite")
	if err != nil {
		t.Fatal(err)
	}
	metadata := request.Metadata
	metadata.Generation = request.GenerationID
	metadata.Snapshot = sources.SemanticSnapshotHash
	sealed, err := OpenSealed(path, metadata)
	if err != nil {
		t.Fatalf("OpenSealed() error = %v", err)
	}
	defer sealed.Close()
	var start, end int
	var storedBody string
	if err := sealed.db.QueryRow(`SELECT source_start, source_end, body FROM chunks`).Scan(&start, &end, &storedBody); err != nil {
		t.Fatalf("query chunk: %v", err)
	}
	if storedBody != body {
		t.Fatalf("stored body = %q, want %q", storedBody, body)
	}
	if string(raw[start:end]) != body {
		t.Fatalf("absolute slice = %q, want %q", raw[start:end], body)
	}
}

func TestRebuildRejectsInvalidGenerationIDAndSemanticPath(t *testing.T) {
	for _, test := range []struct {
		name        string
		generation  string
		semanticDir string
	}{
		{name: "path traversal generation", generation: "../escape", semanticDir: t.TempDir()},
		{name: "empty semantic directory", generation: "generation-004", semanticDir: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := rebuildRequest(t.TempDir(), test.semanticDir, test.generation, &rebuildEmbedder{})
			if _, err := Rebuild(context.Background(), request); err == nil {
				t.Fatal("Rebuild() error = nil")
			}
		})
	}
}

func rebuildRequest(root, semanticDir, generationID string, embedder *rebuildEmbedder) RebuildRequest {
	versions := SubsystemVersions{
		ChunkerVersion:   chunk.Version,
		IndexingVersion:  policy.Version,
		LexicalVersion:   "worktrail-fts5-gse-v2",
		SQLiteVecVersion: "sqlite-vec-v0.1.9",
	}
	return RebuildRequest{
		Scope:        "project",
		Root:         root,
		SemanticDir:  semanticDir,
		GenerationID: generationID,
		BundleID:     "bundle-test-001",
		Metadata: Metadata{
			Schema:     databaseSchema,
			Profile:    "profile-test-001",
			ModelSpace: "model-space-test-001",
			SQLiteVec:  "sqlite-vec-test",
			Dimension:  2,
		},
		Policy:       chunk.DefaultPolicy(),
		Versions:     versions,
		TokenCounter: rebuildCounter{},
		Embedder:     embedder,
		Tokenizer:    index.NewTokenizer(),
	}
}

func writeRebuildSource(t *testing.T, root, body string) {
	t.Helper()
	writeRebuildSourceAt(t, root, "rules/current.md", "rule-001", body)
}

func writeRebuildSourceAt(t *testing.T, root, relPath, id, body string) {
	t.Helper()
	metadata, err := json.Marshal(map[string]string{
		"id":    id,
		"scope": "project",
		"type":  "rule",
		"title": "Rule",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create source directory: %v", err)
	}
	data := []byte(store.Marker + "\n" + string(metadata) + "\n---\n\n" + body)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
}

func policySources(root string) (string, error) {
	sources, err := policy.FreshSources(root, "project")
	if err != nil {
		return "", err
	}
	return sources.SemanticSnapshotHash, nil
}

type rebuildCounter struct {
	err error
}

func (c rebuildCounter) CountTokens(_ context.Context, value string) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return len(strings.Fields(value)), nil
}

type rebuildEmbedder struct {
	err          error
	inputs       []string
	onFirstEmbed func()
}

func (e *rebuildEmbedder) Embed(_ context.Context, input string) ([]float32, error) {
	e.inputs = append(e.inputs, input)
	if len(e.inputs) == 1 && e.onFirstEmbed != nil {
		e.onFirstEmbed()
	}
	if e.err != nil {
		return nil, e.err
	}
	return []float32{1, 0}, nil
}
