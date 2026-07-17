package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestRunInitSkipsSemanticInstallerWithoutSemanticFlag(t *testing.T) {
	for _, args := range [][]string{nil, {"--no-semantic"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			env := initTestEnv(t)
			var out bytes.Buffer
			installer := semanticInstallerFunc(func(context.Context, paths.Env) (SemanticInstallInfo, error) {
				t.Fatal("semantic installer was called")
				return SemanticInstallInfo{}, nil
			})

			if err := runInitWithInstaller(context.Background(), env, IO{Out: &out}, args, installer); err != nil {
				t.Fatalf("runInitWithInstaller(%v): %v", args, err)
			}
			assertCoreInit(t, env)
			if strings.Contains(out.String(), "semantic rebuild") {
				t.Fatalf("unexpected semantic guidance: %s", out.String())
			}
		})
	}
}

func TestRunInitSemanticInstallerRunsAfterCoreInit(t *testing.T) {
	env := initTestEnv(t)
	var out bytes.Buffer
	called := false
	installer := semanticInstallerFunc(func(_ context.Context, got paths.Env) (SemanticInstallInfo, error) {
		called = true
		if got.UserRoot != env.UserRoot || got.ProjectRoot != env.ProjectRoot || got.ProjectWT != env.ProjectWT {
			t.Fatalf("installer env = %#v, want %#v", got, env)
		}
		assertCoreInit(t, env)
		return SemanticInstallInfo{
			SupportLevel: "experimental",
			Chip:         "m3",
			Warning:      "experimental warning",
		}, nil
	})

	if err := runInitWithInstaller(context.Background(), env, IO{Out: &out}, []string{"--semantic"}, installer); err != nil {
		t.Fatalf("runInitWithInstaller(--semantic): %v", err)
	}
	if !called {
		t.Fatal("semantic installer was not called")
	}
	if !strings.Contains(out.String(), "next: worktrail semantic rebuild --scope all") {
		t.Fatalf("missing semantic guidance: %s", out.String())
	}
	for _, want := range []string{"support_level\texperimental", "chip\tm3", "warning\texperimental warning"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("missing semantic installation detail %q: %s", want, out.String())
		}
	}
}

func TestRunInitSemanticFailurePreservesCoreInit(t *testing.T) {
	env := initTestEnv(t)
	want := errors.New("semantic runtime unavailable")
	installer := semanticInstallerFunc(func(context.Context, paths.Env) (SemanticInstallInfo, error) {
		return SemanticInstallInfo{}, want
	})

	err := runInitWithInstaller(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--semantic"}, installer)
	if !errors.Is(err, want) {
		t.Fatalf("runInitWithInstaller(--semantic) error = %v, want %v", err, want)
	}
	assertCoreInit(t, env)
}

func TestRunInitRejectsInvalidArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "unknown flag", args: []string{"--unknown"}},
		{name: "positional argument", args: []string{"project"}},
		{name: "empty argument", args: []string{""}},
		{name: "conflicting flags", args: []string{"--semantic", "--no-semantic"}},
		{name: "repeated semantic flag", args: []string{"--semantic", "--semantic"}},
		{name: "repeated no-semantic flag", args: []string{"--no-semantic", "--no-semantic"}},
		{name: "semantic with positional argument", args: []string{"--semantic", "project"}},
		{name: "no-semantic with unknown flag", args: []string{"--no-semantic", "--unknown"}},
		{name: "multiple extra arguments", args: []string{"project", "extra", "arguments"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := initTestEnv(t)
			called := false
			installer := semanticInstallerFunc(func(context.Context, paths.Env) (SemanticInstallInfo, error) {
				called = true
				return SemanticInstallInfo{}, nil
			})

			if err := runInitWithInstaller(context.Background(), env, IO{Out: &bytes.Buffer{}}, tc.args, installer); err == nil {
				t.Fatalf("runInitWithInstaller(%v) unexpectedly succeeded", tc.args)
			}
			if called {
				t.Fatalf("semantic installer called for invalid args %v", tc.args)
			}
			assertNoCoreInit(t, env, tc.args)
		})
	}
}

func initTestEnv(t *testing.T) paths.Env {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	return paths.Env{
		UserRoot:    filepath.Join(t.TempDir(), "home"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func assertCoreInit(t *testing.T, env paths.Env) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(env.UserRoot, "config.json"),
		filepath.Join(env.ProjectWT, "config.json"),
	} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("core init missing %s: %v", path, err)
		}
	}
}

func assertNoCoreInit(t *testing.T, env paths.Env, args []string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(env.UserRoot, "config.json"),
		filepath.Join(env.ProjectWT, "config.json"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("core init ran for invalid args %v at %s: %v", args, path, err)
		}
	}
}
