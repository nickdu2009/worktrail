package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/handoffmigration"
	"github.com/nickdu2009/worktrail/internal/index"
)

func TestMigrateHandoffV2CommandDefaultsToDryRunAndRequiresBothGates(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")
	source := filepath.Join(project, ".worktrail", "handoffs", "legacy.md")
	writeTextFile(t, source, "# Legacy\n\nBody.")

	out.Reset()
	if err := Run(context.Background(), []string{"migrate", "handoff-v2", "--format", "json"}, nil, &out, &errb); err != nil {
		t.Fatal(err)
	}
	var dry handoffmigration.Report
	if err := json.Unmarshal(out.Bytes(), &dry); err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || dry.InventoryFileCount != 1 {
		t.Fatalf("dry-run report = %+v", dry)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("dry-run removed source: %v", err)
	}

	out.Reset()
	err := Run(context.Background(), []string{"migrate", "handoff-v2", "--apply", "--format", "json"}, nil, &out, &errb)
	if err == nil || !strings.Contains(err.Error(), "both --apply and --confirm") {
		t.Fatalf("--apply without --confirm error = %v stdout=%s", err, out.String())
	}

	backup := filepath.Join(project, "handoff-v2-backup")
	out.Reset()
	if err := Run(context.Background(), []string{
		"migrate", "handoff-v2", "--apply", "--confirm", "--backup-dir", backup, "--format", "json",
	}, nil, &out, &errb); err != nil {
		t.Fatalf("apply migration: %v stdout=%s stderr=%s", err, out.String(), errb.String())
	}
	var applied handoffmigration.Report
	if err := json.Unmarshal(out.Bytes(), &applied); err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Summary.Migrated != 1 || applied.ManifestPath == "" {
		t.Fatalf("apply report = %+v", applied)
	}
	if applied.IndexRebuild == nil || applied.IndexRebuild.Error != "" || applied.IndexRebuild.Scope != "project" {
		t.Fatalf("required index rebuild = %+v", applied.IndexRebuild)
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "index", index.SQLiteFile)); err != nil {
		t.Fatalf("rebuilt index missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(project, ".worktrail", "handoffs", "local", "legacy.md")); err != nil {
		t.Fatalf("migrated target missing: %v", err)
	}
}

func TestMigrateCommandsRejectUnknownFlagsAndPositionalArguments(t *testing.T) {
	home := filepath.Join(t.TempDir(), "home")
	project := filepath.Join(t.TempDir(), "project")
	t.Setenv("WORKTRAIL_HOME", home)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	var out, errb bytes.Buffer
	runApp(t, &out, &errb, "init")

	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "handoff unknown flag", args: []string{"migrate", "handoff-v2", "--unknown"}, want: "unknown flag"},
		{name: "handoff positional", args: []string{"migrate", "handoff-v2", "extra"}, want: "positional"},
		{name: "kdd unknown flag", args: []string{"migrate", "kdd", "--unknown"}, want: "unknown flag"},
		{name: "storage plan positional", args: []string{"migrate", "storage-plan", "extra"}, want: "positional"},
		{name: "storage apply missing value", args: []string{"migrate", "storage-apply", "--format"}, want: "requires a value"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out.Reset()
			errb.Reset()
			err := Run(context.Background(), tc.args, nil, &out, &errb)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%v error = %v, want %q", tc.args, err, tc.want)
			}
		})
	}
}
