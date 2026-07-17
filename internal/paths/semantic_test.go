package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverSemanticRootsSeparatesKnowledgeRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(home, "formal-knowledge"))
	env, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	roots, err := DiscoverSemanticRoots()
	if err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{roots.Cache, roots.Runtime, roots.Logs} {
		if root == env.UserRoot {
			t.Fatalf("semantic root overlaps WORKTRAIL_HOME: %s", root)
		}
	}
}

func TestSemanticRootPathsRejectEscape(t *testing.T) {
	roots := SemanticRoots{
		Cache:   filepath.Join(t.TempDir(), "cache"),
		Runtime: filepath.Join(t.TempDir(), "runtime"),
		Logs:    filepath.Join(t.TempDir(), "logs"),
	}
	for _, resolver := range []func(string) (string, error){
		roots.Bundle,
		roots.RuntimeState,
		roots.Log,
	} {
		if _, err := resolver("../escape"); err == nil {
			t.Fatal("expected escape rejection")
		}
	}
	if _, err := os.Stat(roots.Cache); !os.IsNotExist(err) {
		t.Fatal("path resolution must not create directories")
	}
}
