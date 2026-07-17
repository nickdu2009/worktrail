package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSemanticIndexPathsUseScopedRoots(t *testing.T) {
	home := t.TempDir()
	userRoot := filepath.Join(t.TempDir(), "user-worktrail")
	projectRoot := filepath.Join(t.TempDir(), "project")
	t.Setenv("HOME", home)
	t.Setenv("WORKTRAIL_HOME", userRoot)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", projectRoot)

	env, err := Discover()
	if err != nil {
		t.Fatal(err)
	}

	userIndex, err := env.SemanticIndexRoot("user")
	if err != nil {
		t.Fatal(err)
	}
	projectIndex, err := env.SemanticIndexRoot("project")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(userRoot, "index", "semantic"); userIndex != want {
		t.Fatalf("user index = %q, want %q", userIndex, want)
	}
	if want := filepath.Join(projectRoot, ".worktrail", "index", "semantic"); projectIndex != want {
		t.Fatalf("project index = %q, want %q", projectIndex, want)
	}
	if userIndex == projectIndex {
		t.Fatal("user and project semantic indexes must be distinct")
	}

	tests := []struct {
		name string
		got  func() (string, error)
		want string
	}{
		{
			name: "active pointer",
			got:  func() (string, error) { return env.ActivePointerPath("user") },
			want: filepath.Join(userIndex, "active.json"),
		},
		{
			name: "generation database",
			got:  func() (string, error) { return env.GenerationDBPath("project", "gen-42") },
			want: filepath.Join(projectIndex, "gen-42.sqlite"),
		},
		{
			name: "coordination lock",
			got:  func() (string, error) { return env.CoordinationLockPath("project") },
			want: filepath.Join(projectIndex, "coordination.lock"),
		},
		{
			name: "generation lease",
			got:  func() (string, error) { return env.GenerationLeasePath("user", "gen-42") },
			want: filepath.Join(userIndex, "gen-42.lease"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.got()
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("path = %q, want %q", got, tt.want)
			}
		})
	}

	for _, root := range []string{userRoot, projectRoot} {
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Fatalf("path resolution must not create directories beneath %q", root)
		}
	}
}

func TestSemanticIndexPathsRejectEscapingGenerationIDs(t *testing.T) {
	env := Env{
		UserRoot:    filepath.Join(t.TempDir(), "user"),
		ProjectWT:   filepath.Join(t.TempDir(), "project", ".worktrail"),
		ProjectRoot: filepath.Join(t.TempDir(), "project"),
	}
	for _, generationID := range []string{"", ".", "..", "../escape", "nested/id", `nested\id`, "/absolute", " spaced "} {
		t.Run(generationID, func(t *testing.T) {
			if _, err := env.GenerationDBPath("project", generationID); err == nil {
				t.Fatal("expected generation database path rejection")
			}
			if _, err := env.GenerationLeasePath("user", generationID); err == nil {
				t.Fatal("expected generation lease path rejection")
			}
		})
	}
	if _, err := env.SemanticIndexRoot("other"); err == nil {
		t.Fatal("expected invalid scope rejection")
	}
}
