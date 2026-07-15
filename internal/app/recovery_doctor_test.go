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

	wtruntime "github.com/nickdu2009/worktrail/internal/runtime"
)

func TestDoctorRecoveryDryRunAndConfirmedQuarantine(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, ".worktrail")
	if err := wtruntime.EnsureDirs(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
	source := filepath.Join(root, "runtime", "sessions", "malformed.md")
	malformed := []byte("---worktrail\n{\"id\":\n---\n# Broken\n")
	if err := os.WriteFile(source, malformed, 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := filepath.Join(root, "state", "active")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	stateSource := filepath.Join(stateDir, "broken.md")
	malformedState := []byte("---worktrail\n{\"id\":\n---\n# Broken state\n")
	if err := os.WriteFile(stateSource, malformedState, 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"doctor", "recovery", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("dry-run: %v stderr=%s", err, errOut.String())
	}
	if !bytes.Contains(out.Bytes(), []byte(`"runtime_plan"`)) ||
		!bytes.Contains(out.Bytes(), []byte(`"runtime_result"`)) ||
		bytes.Contains(out.Bytes(), []byte(`"repair":`)) ||
		bytes.Contains(out.Bytes(), []byte(`"state_recovery"`)) {
		t.Fatalf("dry-run JSON contract = %s", out.String())
	}
	var dry recoveryDoctorReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if dry.Apply || dry.OK || len(dry.RuntimePlan.Items) != 1 || len(dry.State.Diagnostics) != 1 ||
		len(dry.State.Actions) != 1 || len(dry.Coverage) != 2 ||
		dry.Coverage[0] != "state" || dry.Coverage[1] != "runtime" {
		t.Fatalf("dry-run report = %+v", dry)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("dry-run moved malformed runtime: %v", err)
	}
	if _, err := os.Stat(stateSource); err != nil {
		t.Fatalf("dry-run moved malformed state: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"doctor", "recovery"}, nil, &out, &errOut); err != nil {
		t.Fatalf("text dry-run: %v stderr=%s", err, errOut.String())
	}
	for _, want := range []string{
		"mode=dry-run", "ok=false", "malformed_state=1", "malformed_runtime=1",
		"coverage=state,runtime", "state\tinvalid_state", "runtime\tmalformed",
		"use --apply --confirm",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("text dry-run missing %q:\n%s", want, out.String())
		}
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"doctor", "recovery", "--apply"}, nil, &out, &errOut); err == nil ||
		!strings.Contains(err.Error(), "both --apply and --confirm") {
		t.Fatalf("apply without confirmation error = %v", err)
	}
	if err := Run(context.Background(), []string{"doctor", "recovery", "--repair", "--confirm"}, nil, &out, &errOut); err == nil ||
		!strings.Contains(err.Error(), "unknown doctor recovery flag --repair") {
		t.Fatalf("obsolete repair flag error = %v", err)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("failed confirmation gate moved source: %v", err)
	}
	if _, err := os.Stat(stateSource); err != nil {
		t.Fatalf("failed confirmation gate moved state: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"doctor", "recovery", "--apply", "--confirm", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("apply: %v stderr=%s", err, errOut.String())
	}
	var applied recoveryDoctorReport
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.Apply || !applied.OK || !applied.State.Applied ||
		applied.RuntimeResult.Quarantined != 1 || applied.RuntimeResult.OperationID == "" {
		t.Fatalf("apply report = %+v", applied)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed runtime remains after apply: %v", err)
	}
	if _, err := os.Stat(stateSource); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed state remains after apply: %v", err)
	}
	quarantined := filepath.Join(root, "runtime", "quarantine", "sessions", "malformed.md")
	data, err := os.ReadFile(quarantined)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, malformed) {
		t.Fatalf("quarantined bytes changed:\n%s", data)
	}
	stateMatches, err := filepath.Glob(filepath.Join(root, "runtime", "quarantine", "state", "*-broken.md"))
	if err != nil || len(stateMatches) != 1 {
		t.Fatalf("state quarantine matches = %v, err=%v", stateMatches, err)
	}
	data, err = os.ReadFile(stateMatches[0])
	if err != nil || !bytes.Equal(data, malformedState) {
		t.Fatalf("quarantined state bytes = %q, err=%v", data, err)
	}
}

func TestDoctorRecoveryAndRuntimeRoutesAppearInHelp(t *testing.T) {
	project := t.TempDir()
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"doctor", "--help"}, want: "worktrail doctor recovery"},
		{args: []string{"doctor", "recovery", "--help"}, want: "--apply --confirm"},
		{args: []string{"doctor", "ops", "--help"}, want: "doctor ops repair --confirm"},
		{args: []string{"runtime", "prune", "--help"}, want: "runtime prune"},
	} {
		var out, errOut bytes.Buffer
		if err := Run(context.Background(), tc.args, nil, &out, &errOut); err != nil {
			t.Fatalf("Run %v: %v stderr=%s", tc.args, err, errOut.String())
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("Run %v help missing %q:\n%s", tc.args, tc.want, out.String())
		}
	}
	var topLevel, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, nil, &topLevel, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(topLevel.String(), "\n") {
		if strings.Contains(line, "worktrail handoff") &&
			!strings.Contains(line, "--next-step") &&
			!strings.Contains(line, "--complete") {
			t.Fatalf("top-level handoff example lacks completion intent: %q", line)
		}
	}
}
