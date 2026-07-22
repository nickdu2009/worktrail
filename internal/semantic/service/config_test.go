package service

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestConfigDefaultsAndPersistsSecurely(t *testing.T) {
	roots := serviceTestRoots(t)
	config, duration, err := LoadConfig(roots)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if config != DefaultConfig() || duration != 10*time.Minute {
		t.Fatalf("LoadConfig() = %#v, %s", config, duration)
	}
	if _, err := os.Stat(roots.ServiceConfig()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("LoadConfig() created config: %v", err)
	}
	if err := EnsureConfig(roots); err != nil {
		t.Fatalf("EnsureConfig() error = %v", err)
	}
	assertServiceMode(t, filepath.Dir(roots.ServiceConfig()), 0o700)
	assertServiceMode(t, roots.ServiceConfig(), 0o600)
}

func TestConfigRejectsInvalidSchemaDurationAndRange(t *testing.T) {
	tests := []string{
		`{"schema":"wrong","idle_timeout":"10m"}`,
		`{"schema":"worktrail.semantic.service-config.v1","idle_timeout":"later"}`,
		`{"schema":"worktrail.semantic.service-config.v1","idle_timeout":"30s"}`,
		`{"schema":"worktrail.semantic.service-config.v1","idle_timeout":"61m"}`,
	}
	for _, data := range tests {
		t.Run(data, func(t *testing.T) {
			roots := serviceTestRoots(t)
			if err := os.MkdirAll(filepath.Dir(roots.ServiceConfig()), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(roots.ServiceConfig(), []byte(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := LoadConfig(roots); err == nil {
				t.Fatal("LoadConfig() error = nil")
			}
		})
	}
}

func serviceTestRoots(t *testing.T) paths.SemanticRoots {
	t.Helper()
	base := t.TempDir()
	return paths.SemanticRoots{Cache: filepath.Join(base, "cache"), Runtime: filepath.Join(base, "runtime"), Logs: filepath.Join(base, "logs")}
}

func assertServiceMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("%s mode = %o, want %o", path, got, want)
	}
}
