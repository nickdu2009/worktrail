package generation

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

func TestRebuildSourceChangeLeavesActivePointerUnchanged(t *testing.T) {
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

	_, err := Rebuild(context.Background(), request)
	if !errors.Is(err, ErrSourcesChanged) {
		t.Fatalf("Rebuild() error = %v, want ErrSourcesChanged", err)
	}
	active, err := ReadActive(semanticDir)
	if err != nil {
		t.Fatalf("ReadActive() error = %v", err)
	}
	if active != previous {
		t.Fatalf("active pointer = %#v, want unchanged %#v", active, previous)
	}
	candidatePath, err := generationPath(semanticDir, request.GenerationID, ".sqlite")
	if err != nil {
		t.Fatalf("generationPath() error = %v", err)
	}
	if _, err := os.Stat(candidatePath); err != nil {
		t.Fatalf("sealed source-changed candidate was not retained: %v", err)
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
		TokenCounter: rebuildCounter{},
		Embedder:     embedder,
	}
}

func writeRebuildSource(t *testing.T, root, body string) {
	t.Helper()
	metadata, err := json.Marshal(map[string]string{
		"id":    "rule-001",
		"scope": "project",
		"type":  "rule",
		"title": "Rule",
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "rules", "current.md")
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
