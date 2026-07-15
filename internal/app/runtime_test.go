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

	"github.com/nickdu2009/worktrail/internal/model"
	wtruntime "github.com/nickdu2009/worktrail/internal/runtime"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestRuntimePruneCLIDryRunAndConfirmationGate(t *testing.T) {
	project := filepath.Join(t.TempDir(), "project")
	root := filepath.Join(project, ".worktrail")
	if err := wtruntime.EnsureDirs(root); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
	path := writeExpiredRuntimeForCLI(t, root)

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"runtime", "prune", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("dry-run: %v stderr=%s", err, errOut.String())
	}
	var dry runtimePruneReport
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatalf("decode dry-run: %v\n%s", err, out.String())
	}
	if dry.Applied || len(dry.Plan.Items) != 1 {
		t.Fatalf("dry-run report = %+v", dry)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run deleted runtime: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"runtime", "prune"}, nil, &out, &errOut); err != nil {
		t.Fatalf("text dry-run: %v stderr=%s", err, errOut.String())
	}
	if !strings.Contains(out.String(), "mode=dry-run") || !strings.Contains(out.String(), "dry-run: no files deleted") {
		t.Fatalf("text dry-run output:\n%s", out.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("text dry-run deleted runtime: %v", err)
	}

	out.Reset()
	errOut.Reset()
	err := Run(context.Background(), []string{"runtime", "prune", "--apply"}, nil, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "both --apply and --confirm") {
		t.Fatalf("apply without confirm error = %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("failed gate deleted runtime: %v", err)
	}

	out.Reset()
	errOut.Reset()
	if err := Run(context.Background(), []string{"runtime", "prune", "--apply", "--confirm", "--format", "json"}, nil, &out, &errOut); err != nil {
		t.Fatalf("apply: %v stderr=%s", err, errOut.String())
	}
	var applied runtimePruneReport
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatalf("decode apply: %v\n%s", err, out.String())
	}
	if !applied.Applied || applied.Result.Deleted != 1 || applied.Result.OperationID == "" {
		t.Fatalf("applied report = %+v", applied)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("applied prune left runtime: %v", err)
	}
}

func TestRuntimeAndOpsCLIHelpRoutes(t *testing.T) {
	project := t.TempDir()
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", filepath.Join(t.TempDir(), "home"))
	for _, tc := range []struct {
		args []string
		want string
	}{
		{args: []string{"runtime", "--help"}, want: "worktrail runtime prune"},
		{args: []string{"runtime", "prune", "--help"}, want: "Deletion requires both --apply and --confirm"},
		{args: []string{"doctor", "ops", "--help"}, want: "worktrail doctor ops repair --confirm"},
	} {
		var out, errOut bytes.Buffer
		if err := Run(context.Background(), tc.args, nil, &out, &errOut); err != nil {
			t.Fatalf("Run %v: %v stderr=%s", tc.args, err, errOut.String())
		}
		if !strings.Contains(out.String(), tc.want) {
			t.Fatalf("Run %v help missing %q:\n%s", tc.args, tc.want, out.String())
		}
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), []string{"--help"}, nil, &out, &errOut); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"worktrail runtime prune", "worktrail doctor ops"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("top-level help missing %q:\n%s", want, out.String())
		}
	}
}

func writeExpiredRuntimeForCLI(t *testing.T, root string) string {
	t.Helper()
	now := time.Now().UTC()
	meta := map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "runtime-cli-expired",
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            "Expired CLI runtime",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"project_id":       "project-1",
		"task_id":          "task-1",
		"created_at":       now.Add(-48 * time.Hour),
		"updated_at":       now.Add(-48 * time.Hour),
		"expires_at":       now.Add(-time.Hour),
	}
	data, err := store.RenderMarkdown(meta, "expired runtime")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "runtime", "sessions", "expired.md")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
