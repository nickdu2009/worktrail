package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscoverEnvOverrides(t *testing.T) {
	realHome := filepath.Join(t.TempDir(), "real-home")
	if err := os.MkdirAll(realHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", realHome)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "worktrail-home"))
	t.Setenv("WORKTRAIL_PROJECT_ROOT", filepath.Join(t.TempDir(), "project"))
	env, err := Discover()
	if err != nil {
		t.Fatal(err)
	}
	if env.Home != realHome {
		t.Fatalf("home = %s, want %s", env.Home, realHome)
	}
	if env.UserRoot != os.Getenv("WORKTRAIL_HOME") {
		t.Fatalf("user root = %s", env.UserRoot)
	}
	if env.ProjectWT != filepath.Join(os.Getenv("WORKTRAIL_PROJECT_ROOT"), ".worktrail") {
		t.Fatalf("project wt = %s", env.ProjectWT)
	}
}

func TestSafeJoinRejectsEscape(t *testing.T) {
	root := t.TempDir()
	if _, err := SafeJoin(root, "..", "outside"); err == nil {
		t.Fatal("expected escape error")
	}
	if _, err := SafeJoin(root, "inside", "file.md"); err != nil {
		t.Fatal(err)
	}
}
