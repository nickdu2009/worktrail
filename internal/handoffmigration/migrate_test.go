package handoffmigration

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestDryRunAndApplyMigrateV1AndRetiredCandidate(t *testing.T) {
	root := migrationRoot(t)
	missingState := filepath.Join(root, "state", "active", "missing-state.md")
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "legacy.md"), map[string]any{
		"schema":          model.SchemaHandoff,
		"id":              "legacy-id",
		"scope":           "project",
		"title":           "Legacy",
		"summary":         "Legacy summary",
		"task_id":         "task-old",
		"source_state_id": missingState,
		"status":          "current",
		"created_at":      "2026-07-01T01:02:03Z",
		"updated_at":      "2026-07-01T01:02:04Z",
	}, "# Legacy\n\nLegacy body.")
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "plain.md"), nil, "# Plain Legacy\n\nPlain body.")
	writeLegacyDocument(t, filepath.Join(root, "candidates", "project", "candidate-id.md"), map[string]any{
		"schema":         model.SchemaCandidate,
		"id":             "candidate-id",
		"scope":          "project",
		"candidate_type": model.CandidateTypeHandoff,
		"target_path":    "handoffs/candidate-id.md",
		"title":          "Candidate Handoff",
		"summary":        "Candidate summary",
		"operation":      "replace",
		"status":         "pending",
		"created_at":     "2026-07-02T01:02:03Z",
	}, "# Candidate\n\nCandidate body.")

	now := time.Date(2026, 7, 15, 1, 2, 3, 4, time.UTC)
	dry, err := Run(Options{Root: root, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if !dry.DryRun || !dry.OK || dry.InventoryFileCount != 3 ||
		dry.Summary.LegacyHandoffs != 2 || dry.Summary.HandoffCandidates != 1 ||
		dry.Summary.Planned != 3 || dry.Summary.Unresolved != 1 {
		t.Fatalf("dry-run report = %+v", dry)
	}
	if _, err := os.Stat(dry.BackupDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("dry-run created backup directory: %v", err)
	}
	legacyItem := itemBySource(t, dry.Items, "handoffs/legacy.md")
	if !strings.HasPrefix(legacyItem.TaskID, "task_legacy_") {
		t.Fatalf("missing-state task id = %q, want stable independent legacy task id", legacyItem.TaskID)
	}
	if !hasDiagnostic(legacyItem.Diagnostics, "unresolved_absolute_source_reference") {
		t.Fatalf("missing state did not produce unresolved diagnostic: %+v", legacyItem.Diagnostics)
	}
	plainTask := itemBySource(t, dry.Items, "handoffs/plain.md").TaskID
	if !strings.HasPrefix(plainTask, "task_legacy_") {
		t.Fatalf("plain task id = %q, want stable legacy-derived id", plainTask)
	}
	dryAgain, err := Run(Options{Root: root, Now: func() time.Time { return now.Add(time.Hour) }})
	if err != nil {
		t.Fatal(err)
	}
	if itemBySource(t, dryAgain.Items, "handoffs/plain.md").TaskID != plainTask {
		t.Fatal("legacy-derived task id changed between runs")
	}

	backup := filepath.Join(filepath.Dir(root), "handoff-v2-backup")
	applied, err := Run(Options{
		Root: root, BackupDir: backup, Apply: true, Confirm: true,
		Now: func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || !applied.OK || applied.Summary.Migrated != 3 || applied.Summary.SourceFilesRemoved != 3 {
		t.Fatalf("apply report = %+v", applied)
	}
	if applied.ManifestPath != filepath.Join(backup, "manifest.json") {
		t.Fatalf("manifest path = %q", applied.ManifestPath)
	}
	for _, item := range applied.Items {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(item.SourcePath))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("source still exists %s: %v", item.SourcePath, err)
		}
		record, err := handoff.Read(filepath.Join(root, filepath.FromSlash(item.TargetPath)))
		if err != nil {
			t.Fatalf("read migrated %s: %v", item.TargetPath, err)
		}
		if record.Meta.Visibility != model.VisibilityLocal || record.Meta.FormatVersion != 2 {
			t.Fatalf("migrated metadata = %+v", record.Meta)
		}
		if record.Meta.MigratedFrom != item.SourcePath {
			t.Fatalf("migrated_from = %q, want %q", record.Meta.MigratedFrom, item.SourcePath)
		}
		if item.SourceKind == "handoff_candidate" && record.Meta.LifecycleStatus != model.LifecycleCurrent {
			t.Fatalf("candidate migrated with lifecycle %q, want current", record.Meta.LifecycleStatus)
		}
		info, err := os.Stat(record.Path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("local handoff mode = %04o", info.Mode().Perm())
		}
	}
	targetData, err := os.ReadFile(filepath.Join(root, "handoffs", "local", "legacy-id.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(targetData), missingState) {
		t.Fatalf("migrated handoff retained absolute source path:\n%s", targetData)
	}
	backupData, err := os.ReadFile(filepath.Join(backup, "files", "handoffs", "legacy.md"))
	if err != nil || !strings.Contains(string(backupData), "Legacy body.") {
		t.Fatalf("backup is not restorable: err=%v body=%q", err, backupData)
	}

	rerunBackup := filepath.Join(filepath.Dir(root), "rerun-backup")
	rerun, err := Run(Options{Root: root, BackupDir: rerunBackup, Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rerun.OK || rerun.InventoryFileCount != 0 || len(rerun.Operations) != 0 {
		t.Fatalf("idempotent rerun = %+v", rerun)
	}
	if _, err := os.Stat(rerunBackup); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("no-op rerun created backup: %v", err)
	}
}

func TestBackupMustRemainOutsideWorktrailRoot(t *testing.T) {
	root := migrationRoot(t)
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "legacy.md"), nil, "# Legacy\n\nBody.")
	_, err := Run(Options{
		Root: root, BackupDir: filepath.Join(root, "backup"), Apply: true, Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("inside-root backup error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "backup")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid backup path was created: %v", err)
	}
	link := filepath.Join(filepath.Dir(root), "backup-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{
		Root: root, BackupDir: filepath.Join(link, "backup"), Apply: true, Confirm: true,
	}); err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("symlinked inside-root backup error = %v", err)
	}
}

func TestConflictingTargetIsNeverOverwritten(t *testing.T) {
	root := migrationRoot(t)
	source := filepath.Join(root, "handoffs", "same-id.md")
	target := filepath.Join(root, "handoffs", "local", "same-id.md")
	writeLegacyDocument(t, source, nil, "# Legacy\n\nBody.")
	writeFile(t, target, []byte("different target"))

	dry, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if dry.Summary.Conflicts != 1 || dry.Items[0].Status != "conflict" {
		t.Fatalf("conflict dry-run = %+v", dry)
	}
	_, err = Run(Options{
		Root: root, BackupDir: filepath.Join(filepath.Dir(root), "conflict-backup"),
		Apply: true, Confirm: true,
	})
	if err == nil || !strings.Contains(err.Error(), "blocked") {
		t.Fatalf("conflict apply error = %v", err)
	}
	if data, _ := os.ReadFile(target); string(data) != "different target" {
		t.Fatalf("conflicting target was overwritten: %q", data)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("conflicting source was removed: %v", err)
	}
}

func TestLegacySourcesMappingToSameTargetConflictInsteadOfOverwrite(t *testing.T) {
	root := migrationRoot(t)
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "same.md"), map[string]any{
		"schema": model.SchemaHandoff, "id": "same", "scope": "project",
	}, "# Root Handoff\n\nRoot body.")
	writeLegacyDocument(t, filepath.Join(root, "candidates", "project", "same.md"), map[string]any{
		"schema": model.SchemaCandidate, "id": "same", "scope": "project",
		"candidate_type": model.CandidateTypeHandoff, "status": "pending",
	}, "# Candidate Handoff\n\nDifferent body.")
	report, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.Conflicts != 2 {
		t.Fatalf("source collision report = %+v", report)
	}
	for _, item := range report.Items {
		if item.Status != "conflict" || !hasDiagnostic(item.Diagnostics, "migration_target_collision") {
			t.Fatalf("collision item = %+v", item)
		}
	}
}

func TestFailedWriteOperationRequiresExplicitRepairBeforeRetry(t *testing.T) {
	root := migrationRoot(t)
	source := filepath.Join(root, "handoffs", "replay.md")
	writeLegacyDocument(t, source, nil, "# Replay\n\nBody.")
	injected := false
	failpoint := func(phase string, _ int, _ ops.Action) error {
		if phase == "commit" && !injected {
			injected = true
			return errors.New("injected crash before source cleanup")
		}
		return nil
	}
	first, err := Run(Options{
		Root: root, BackupDir: filepath.Join(filepath.Dir(root), "first-backup"),
		Apply: true, Confirm: true, Failpoint: failpoint,
	})
	if err == nil || !strings.Contains(err.Error(), "injected crash") {
		t.Fatalf("first migration error = %v report=%+v", err, first)
	}
	if _, err := os.Stat(source); err != nil {
		t.Fatalf("source removed before write verification: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "handoffs", "local", "replay.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("target should not be written before replay: %v", err)
	}

	_, err = Run(Options{
		Root: root, BackupDir: filepath.Join(filepath.Dir(root), "first-backup"),
		Apply: true, Confirm: true,
	})
	if err == nil || !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("retry error = %v, want pending operation", err)
	}
	if _, err := ops.New(root).ReplayPending(); err != nil {
		t.Fatalf("explicit replay: %v", err)
	}
	second, err := Run(Options{
		Root: root, BackupDir: filepath.Join(filepath.Dir(root), "first-backup"),
		Apply: true, Confirm: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Applied {
		t.Fatalf("post-repair report = %+v", second)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source not removed after explicit replay: %v", err)
	}
}

func TestMigrationRejectsSymlinkLegacySource(t *testing.T) {
	root := migrationRoot(t)
	target := filepath.Join(filepath.Dir(root), "outside.md")
	writeFile(t, target, []byte("# Outside\n"))
	source := filepath.Join(root, "handoffs", "linked.md")
	if err := os.MkdirAll(filepath.Dir(source), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, source); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(Options{Root: root}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("symlink source error = %v", err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "# Outside\n" {
		t.Fatalf("symlink target changed: err=%v data=%q", err, data)
	}
}

func TestMigrationDetectsConcurrentSourceAndTargetMutationBeforeDelete(t *testing.T) {
	t.Run("source", func(t *testing.T) {
		root := migrationRoot(t)
		source := filepath.Join(root, "handoffs", "mutated-source.md")
		writeLegacyDocument(t, source, nil, "# Source\n\nOriginal.")
		mutated := false
		_, err := Run(Options{
			Root: root, BackupDir: filepath.Join(filepath.Dir(root), "source-backup"),
			Apply: true, Confirm: true,
			Failpoint: func(phase string, index int, _ ops.Action) error {
				if phase == "commit" && index == 0 && !mutated {
					mutated = true
					return os.WriteFile(source, []byte("# Source\n\nChanged concurrently."), 0o644)
				}
				return nil
			},
		})
		if err == nil || !strings.Contains(err.Error(), "legacy source changed before cleanup") {
			t.Fatalf("concurrent source mutation error = %v", err)
		}
		if data, readErr := os.ReadFile(source); readErr != nil || !strings.Contains(string(data), "Changed concurrently") {
			t.Fatalf("mutated source was removed: err=%v data=%q", readErr, data)
		}
	})

	t.Run("target", func(t *testing.T) {
		root := migrationRoot(t)
		source := filepath.Join(root, "handoffs", "mutated-target.md")
		target := filepath.Join(root, "handoffs", "local", "mutated-target.md")
		writeLegacyDocument(t, source, nil, "# Target\n\nOriginal.")
		mutated := false
		_, err := Run(Options{
			Root: root, BackupDir: filepath.Join(filepath.Dir(root), "target-backup"),
			Apply: true, Confirm: true,
			Failpoint: func(phase string, index int, _ ops.Action) error {
				if phase == "commit" && index == 0 && !mutated {
					mutated = true
					writeFile(t, target, []byte("concurrent target"))
				}
				return nil
			},
		})
		if err == nil || !strings.Contains(err.Error(), "target hash changed") {
			t.Fatalf("concurrent target mutation error = %v", err)
		}
		if _, statErr := os.Stat(source); statErr != nil {
			t.Fatalf("source removed after target hash mismatch: %v", statErr)
		}
		if data, readErr := os.ReadFile(target); readErr != nil || string(data) != "concurrent target" {
			t.Fatalf("concurrent target overwritten: err=%v data=%q", readErr, data)
		}
	})
}

func TestTerminalHandoffCandidatesMigrateWithTerminalLifecycleAndLeaveCandidateSurface(t *testing.T) {
	for _, status := range []string{"discarded", "archived"} {
		t.Run(status, func(t *testing.T) {
			root := migrationRoot(t)
			source := filepath.Join(root, "candidates", "project", status+".md")
			writeLegacyDocument(t, source, map[string]any{
				"schema": model.SchemaCandidate, "id": status, "scope": "project",
				"candidate_type": model.CandidateTypeHandoff, "status": status,
			}, "# Terminal\n\nMust stay terminal.")
			report, err := Run(Options{Root: root, Apply: true, Confirm: true})
			if err != nil {
				t.Fatal(err)
			}
			if report.Summary.Migrated != 1 || report.Items[0].Status != "migrated" ||
				!hasDiagnostic(report.Items[0].Diagnostics, "terminal_handoff_candidate_preserved") {
				t.Fatalf("terminal candidate report = %+v", report)
			}
			if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("terminal candidate remained on candidate surface: %v", err)
			}
			record, err := handoff.Read(filepath.Join(root, "handoffs", "local", status+".md"))
			if err != nil {
				t.Fatalf("read terminal V2 handoff: %v", err)
			}
			if record.Meta.LifecycleStatus != status {
				t.Fatalf("terminal lifecycle = %q, want %q", record.Meta.LifecycleStatus, status)
			}
			if record.Meta.LifecycleStatus == model.LifecycleCurrent {
				t.Fatalf("terminal candidate revived as current: %+v", record.Meta)
			}
			backupPath := filepath.Join(report.BackupDir, "files", "candidates", "project", status+".md")
			if data, err := os.ReadFile(backupPath); err != nil || !strings.Contains(string(data), "Must stay terminal.") {
				t.Fatalf("terminal candidate backup missing: err=%v data=%q", err, data)
			}
		})
	}
}

func TestDryRunRejectsUnsafeSourceStateBeforeReadingIt(t *testing.T) {
	t.Run("outside root", func(t *testing.T) {
		root := migrationRoot(t)
		outside := filepath.Join(filepath.Dir(root), "outside-state.md")
		writeLegacyDocument(t, outside, map[string]any{
			"schema": model.SchemaState, "id": "outside-state", "task_id": "outside-task",
		}, "# Outside")
		writeLegacyDocument(t, filepath.Join(root, "handoffs", "outside-ref.md"), map[string]any{
			"schema": model.SchemaHandoff, "id": "outside-ref", "scope": "project",
			"source_state": outside,
		}, "# Outside Ref\n\nBody.")

		report, err := Run(Options{Root: root})
		if err != nil {
			t.Fatal(err)
		}
		item := itemBySource(t, report.Items, "handoffs/outside-ref.md")
		if item.Status != "invalid" || !hasDiagnostic(item.Diagnostics, "legacy_source_invalid") {
			t.Fatalf("outside source_state plan item = %+v", item)
		}
		if report.OK || report.Summary.Invalid != 1 {
			t.Fatalf("outside source_state report = %+v", report)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		root := migrationRoot(t)
		outside := filepath.Join(filepath.Dir(root), "symlink-target.md")
		writeLegacyDocument(t, outside, map[string]any{
			"schema": model.SchemaState, "id": "linked-state", "task_id": "linked-task",
		}, "# Linked")
		link := filepath.Join(root, "state", "active", "linked-state.md")
		if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, link); err != nil {
			t.Fatal(err)
		}
		writeLegacyDocument(t, filepath.Join(root, "handoffs", "linked-ref.md"), map[string]any{
			"schema": model.SchemaHandoff, "id": "linked-ref", "scope": "project",
			"source_state": "state/active/linked-state.md",
		}, "# Linked Ref\n\nBody.")

		report, err := Run(Options{Root: root})
		if err != nil {
			t.Fatal(err)
		}
		item := itemBySource(t, report.Items, "handoffs/linked-ref.md")
		if item.Status != "invalid" || !strings.Contains(item.Diagnostics[0].Message, "symbolic link") {
			t.Fatalf("symlink source_state plan item = %+v", item)
		}
	})
}

func TestDryRunPrevalidatesGeneratedV2AndApplyWritesNothingInvalid(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta map[string]any
	}{
		{
			name: "invalid id",
			meta: map[string]any{
				"schema": model.SchemaHandoff, "id": "bad id", "scope": "project",
			},
		},
		{
			name: "invalid source tool",
			meta: map[string]any{
				"schema": model.SchemaHandoff, "id": "bad-source-tool", "scope": "project",
				"source_tool": "/Users/alice/bin/tool",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := migrationRoot(t)
			source := filepath.Join(root, "handoffs", "invalid.md")
			writeLegacyDocument(t, source, tc.meta, "# Invalid\n\nBody.")

			dry, err := Run(Options{Root: root})
			if err != nil {
				t.Fatal(err)
			}
			if dry.OK || dry.Summary.Invalid != 1 || len(dry.Items) != 1 || dry.Items[0].Status != "invalid" {
				t.Fatalf("invalid generated target was not visible in plan: %+v", dry)
			}

			backup := filepath.Join(filepath.Dir(root), "invalid-backup")
			applied, err := Run(Options{Root: root, BackupDir: backup, Apply: true, Confirm: true})
			if err == nil || !strings.Contains(err.Error(), "blocked") {
				t.Fatalf("invalid apply error = %v report=%+v", err, applied)
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatalf("invalid source changed: %v", err)
			}
			if _, err := os.Stat(backup); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid plan created backup before blocking: %v", err)
			}
			if target := dry.Items[0].TargetPath; target != "" {
				if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(target))); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("invalid target was written: %v", err)
				}
			}
		})
	}
}

func TestMigrationSanitizesBodyBoundsSizeAndMarksUncertainTask(t *testing.T) {
	root := migrationRoot(t)
	source := filepath.Join(root, "handoffs", "unsafe.md")
	body := "# Unsafe\n\nPaths /Users/alice/private/repo and /tmp/private/snapshot.\n\n" +
		"## State Snapshot\n\n## Raw Transcript\n\n- user: secret request\n- assistant: recursive answer\n\n" +
		"## Continuation\n\n- user: another request\n- assistant: another answer\n\n" +
		strings.Repeat("x", handoff.LocalBodyMax+1024)
	writeLegacyDocument(t, source, map[string]any{
		"schema": model.SchemaHandoff, "id": "unsafe", "scope": "project",
		"task_id": "untrusted-shared-task",
	}, body)
	backup := filepath.Join(filepath.Dir(root), "sanitize-backup")
	report, err := Run(Options{Root: root, BackupDir: backup, Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	item := itemBySource(t, report.Items, "handoffs/unsafe.md")
	record, err := handoff.Read(filepath.Join(root, filepath.FromSlash(item.TargetPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !record.Meta.TaskIdentityUncertain || !strings.HasPrefix(record.Meta.TaskID, "task_legacy_") {
		t.Fatalf("uncertain task metadata = %+v", record.Meta)
	}
	if record.Meta.MigratedFrom != "handoffs/unsafe.md" {
		t.Fatalf("migrated_from = %q", record.Meta.MigratedFrom)
	}
	if len([]byte(record.Body)) > handoff.LocalBodyMax {
		t.Fatalf("migrated body = %d bytes, max=%d", len([]byte(record.Body)), handoff.LocalBodyMax)
	}
	for _, forbidden := range []string{"/Users/alice", "/tmp/private", "State Snapshot", "Raw Transcript", "- user:", "- assistant:"} {
		if strings.Contains(record.Body, forbidden) {
			t.Fatalf("migrated body retained %q:\n%s", forbidden, record.Body)
		}
	}
	if !hasDiagnostic(item.Diagnostics, "legacy_recursive_snapshot_removed") ||
		!hasDiagnostic(item.Diagnostics, "legacy_transcript_turns_removed") ||
		!hasDiagnostic(item.Diagnostics, "legacy_body_truncated") {
		t.Fatalf("sanitize diagnostics = %+v", item.Diagnostics)
	}
}

func TestMigrationAcceptsTimestampedSourcePathAndPreservesMigratedFrom(t *testing.T) {
	root := migrationRoot(t)
	const sourcePath = "handoffs/20260514-002407-cross-machine-handoff.md"
	writeLegacyDocument(t, filepath.Join(root, filepath.FromSlash(sourcePath)), nil,
		"# Timestamped\n\nContact owner@example.com, call +1 (415) 555-2671, or inspect /Users/alice/private/repo.")

	report, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Summary.Planned != 1 || report.Summary.Invalid != 0 {
		t.Fatalf("timestamped source dry-run = %+v", report)
	}
	item := itemBySource(t, report.Items, sourcePath)
	doc, err := store.ParseMarkdown(item.targetData)
	if err != nil {
		t.Fatal(err)
	}
	if got := stringValue(doc.Meta["migrated_from"]); got != sourcePath {
		t.Fatalf("migrated_from = %q, want %q", got, sourcePath)
	}
	for _, forbidden := range []string{"owner@example.com", "+1 (415) 555-2671", "/Users/alice/private/repo"} {
		if strings.Contains(doc.Body, forbidden) {
			t.Fatalf("timestamped migration retained %q:\n%s", forbidden, doc.Body)
		}
	}
}

func TestMigrationRemovesNestedStateCapsuleUntilPreviousHandoff(t *testing.T) {
	root := migrationRoot(t)
	const sourcePath = "handoffs/nested-state-snapshot.md"
	body := `# Legacy Plan

Retained introduction.

## State Snapshot

# State Capsule: captured session

## Original Intent

Nested intent must be removed.

## Evidence

conversation_id: nested-runtime-id

## Next Step

Nested next step must be removed.

## Previous Handoff

- Handoff ID: prior-handoff

## Next Step

Retained outer next step.
`
	writeLegacyDocument(t, filepath.Join(root, filepath.FromSlash(sourcePath)), nil, body)

	report, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if !report.OK || report.Summary.Planned != 1 {
		t.Fatalf("nested snapshot dry-run = %+v", report)
	}
	item := itemBySource(t, report.Items, sourcePath)
	doc, err := store.ParseMarkdown(item.targetData)
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"State Capsule", "Original Intent", "nested-runtime-id", "Nested next step"} {
		if strings.Contains(doc.Body, forbidden) {
			t.Fatalf("nested snapshot retained %q:\n%s", forbidden, doc.Body)
		}
	}
	for _, retained := range []string{"Retained introduction.", "## Previous Handoff", "prior-handoff", "Retained outer next step."} {
		if !strings.Contains(doc.Body, retained) {
			t.Fatalf("nested snapshot dropped %q:\n%s", retained, doc.Body)
		}
	}
	if count := strings.Count(doc.Body, "## Next Step"); count != 1 {
		t.Fatalf("next-step heading count = %d, want 1:\n%s", count, doc.Body)
	}
	if !hasDiagnostic(item.Diagnostics, "legacy_recursive_snapshot_removed") {
		t.Fatalf("nested snapshot diagnostics = %+v", item.Diagnostics)
	}
}

func TestDefaultBackupIsExternalIgnoredAndManifestVerified(t *testing.T) {
	root := migrationRoot(t)
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "backup.md"), nil, "# Backup\n\nBody.")
	report, err := Run(Options{Root: root, Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if rel, err := filepath.Rel(root, report.BackupDir); err != nil || rel == "." || !strings.HasPrefix(rel, "..") {
		t.Fatalf("backup directory is not external: rel=%q err=%v", rel, err)
	}
	gitignore, err := os.ReadFile(filepath.Join(filepath.Dir(root), ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gitignore), "/.worktrail-handoff-v2-backups/") {
		t.Fatalf("default backup is not precisely ignored:\n%s", gitignore)
	}
	if report.ManifestFileCount != 1 || report.ManifestHash == "" {
		t.Fatalf("manifest metadata = %+v", report)
	}
	actualHash, err := hashFile(report.ManifestPath)
	if err != nil || actualHash != report.ManifestHash {
		t.Fatalf("manifest hash = %q, actual=%q err=%v", report.ManifestHash, actualHash, err)
	}
	data, err := os.ReadFile(report.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var decoded manifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.FileCount != 1 || len(decoded.Files) != 1 || decoded.InventoryHash != report.InventoryHash {
		t.Fatalf("manifest = %+v", decoded)
	}
}

func TestExistingMismatchedBackupDirectoryIsAConflict(t *testing.T) {
	root := migrationRoot(t)
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "backup-conflict.md"), nil, "# Backup\n\nBody.")
	backup := filepath.Join(filepath.Dir(root), "existing-backup")
	writeFile(t, filepath.Join(backup, "manifest.json"), []byte(`{"schema":"other"}`))
	_, err := Run(Options{Root: root, BackupDir: backup, Apply: true, Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "backup directory conflict") {
		t.Fatalf("backup conflict error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "handoffs", "backup-conflict.md")); statErr != nil {
		t.Fatalf("source removed despite backup conflict: %v", statErr)
	}
}

func TestMigrationDoesNotReplayForeignPendingOperations(t *testing.T) {
	root := migrationRoot(t)
	foreign := ops.New(root)
	foreign.Failpoint = func(phase string, _ int, _ ops.Action) error {
		if phase == "commit" {
			return errors.New("leave foreign operation pending")
		}
		return nil
	}
	operation, err := foreign.Begin(ops.Spec{
		ID:     "handoff-v2-migration-foreign",
		Writes: []ops.Write{{Path: "foreign.txt", Data: []byte("foreign"), Mode: 0o600}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("foreign operation unexpectedly committed")
	}
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "own.md"), nil, "# Own\n\nBody.")
	_, err = Run(Options{
		Root: root, BackupDir: filepath.Join(filepath.Dir(root), "own-backup"),
		Apply: true, Confirm: true,
	})
	if err == nil || !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("migration error = %v, want pending operation", err)
	}
	if _, err := os.Stat(filepath.Join(root, "foreign.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("foreign operation was replayed: %v", err)
	}
}

func TestExistingSourceStateTaskIDWins(t *testing.T) {
	root := migrationRoot(t)
	statePath := filepath.Join(root, "state", "archived", "state-1.md")
	writeLegacyDocument(t, statePath, map[string]any{
		"schema": model.SchemaState, "id": "state-1", "task_id": "task-from-state",
	}, "# State")
	writeLegacyDocument(t, filepath.Join(root, "handoffs", "stateful.md"), map[string]any{
		"schema": model.SchemaHandoff, "id": "stateful", "scope": "project",
		"task_id": "task-old", "source_state_id": statePath,
	}, "# Stateful\n\nBody.")
	report, err := Run(Options{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	item := itemBySource(t, report.Items, "handoffs/stateful.md")
	if item.TaskID != "task-from-state" {
		t.Fatalf("task id = %q, want source state task", item.TaskID)
	}
}

func migrationRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "project", ".worktrail")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "config.json"), []byte(`{"project_id":"project_test"}`))
	return root
}

func writeLegacyDocument(t *testing.T, path string, meta map[string]any, body string) {
	t.Helper()
	data := []byte(body)
	var err error
	if meta != nil {
		data, err = store.RenderMarkdown(meta, body)
		if err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, path, data)
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func itemBySource(t *testing.T, items []Item, source string) Item {
	t.Helper()
	for _, item := range items {
		if item.SourcePath == source {
			return item
		}
	}
	t.Fatalf("missing migration item %s: %+v", source, items)
	return Item{}
}

func hasDiagnostic(diagnostics []Diagnostic, code string) bool {
	for _, diagnostic := range diagnostics {
		if diagnostic.Code == code {
			return true
		}
	}
	return false
}
