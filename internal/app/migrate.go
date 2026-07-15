package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/handoffmigration"
	kddmigration "github.com/nickdu2009/worktrail/internal/migration/kdd"
	storagemigration "github.com/nickdu2009/worktrail/internal/migration/storage"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
)

func runMigrate(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsHelp(args) {
		printMigrateHelp(ioctx.Out)
		if len(args) == 0 {
			return fmt.Errorf("migrate target required")
		}
		return nil
	}
	switch args[0] {
	case "kdd":
		return runMigrateKDD(env, ioctx, args[1:])
	case "storage-plan":
		return runMigrateStoragePlan(env, ioctx, args[1:])
	case "storage-apply":
		return runMigrateStorageApply(env, ioctx, args[1:])
	case "handoff-v2":
		return runMigrateHandoffV2(env, ioctx, args[1:])
	default:
		return fmt.Errorf("unsupported migrate target %q", args[0])
	}
}

func runMigrateKDD(env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printMigrateKDDHelp(ioctx.Out)
		return nil
	}
	flags, err := parseMigrateFlags(args,
		map[string]bool{"write-candidates": true, "update-gitignore": true, "cleanup-legacy": true, "confirm": true},
		map[string]bool{"root": true, "format": true, "write-pack": true, "archive-path": true},
	)
	if err != nil {
		return fmt.Errorf("worktrail migrate kdd: %w", err)
	}
	root := flagValue(flags, "root", "")
	format := flagValue(flags, "format", "text")
	if err := validateMigrateFormat(format); err != nil {
		return fmt.Errorf("worktrail migrate kdd: %w", err)
	}
	if flags["update-gitignore"] == "true" {
		if err := store.EnsureProjectGitignore(env); err != nil {
			return err
		}
	}
	if flags["cleanup-legacy"] == "true" {
		report, err := cleanupLegacyKDD(env, root, flagValue(flags, "archive-path", ""), flags["confirm"] == "true")
		if err != nil {
			if format == "json" {
				_ = json.NewEncoder(ioctx.Out).Encode(report)
			}
			return err
		}
		if format == "json" {
			return json.NewEncoder(ioctx.Out).Encode(report)
		}
		fmt.Fprintf(ioctx.Out, "cleanup: %s\nlegacy_root: %s\n", report.Action, report.LegacyRoot)
		if report.ArchivePath != "" {
			fmt.Fprintf(ioctx.Out, "archive_path: %s\n", report.ArchivePath)
		}
		return nil
	}
	report, err := kddmigration.Run(env, kddmigration.Options{
		Root:            root,
		WriteCandidates: flags["write-candidates"] == "true",
		WritePack:       flagValue(flags, "write-pack", ""),
	})
	if err != nil {
		return err
	}
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	return printMigrateKDDReport(ioctx.Out, report)
}

func printMigrateKDDReport(out io.Writer, report kddmigration.Report) error {
	fmt.Fprintf(out, "source: %s\nproject: %s\nroot: %s\n", report.Source, report.Project, report.Root)
	fmt.Fprintf(out, "dry_run: %t\nmatched: %d\ncreated: %d\nskipped: %d\nblocked: %d\nproject_items: %d\nlocal_items: %d\n", report.DryRun, report.Matched, report.Created, report.Skipped, report.Blocked, report.ProjectItems, report.LocalItems)
	if report.WritePack != "" {
		fmt.Fprintf(out, "write_pack: %s\n", report.WritePack)
	}
	for _, item := range report.Items {
		if item.SkipReason != "" {
			fmt.Fprintf(out, "skipped: %s\t%s\n", item.SourcePath, item.SkipReason)
			continue
		}
		fmt.Fprintf(out, "%s\t%s\t%s\t%s\t%s\t%s\n", item.SourcePath, item.Scope, item.CandidateID, item.CandidateType, item.Operation, item.TargetPath)
		if len(item.Warnings) > 0 {
			fmt.Fprintf(out, "warnings: %s\t%s\n", item.SourcePath, strings.Join(item.Warnings, ","))
		}
	}
	for _, id := range report.Candidates {
		fmt.Fprintf(out, "candidate: %s\n", id)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "next steps:")
	for _, step := range report.NextSteps {
		fmt.Fprintf(out, "- %s\n", step)
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "git guidance:")
	for _, item := range report.GitGuidance {
		fmt.Fprintf(out, "- %s\n", item)
	}
	return nil
}

type legacyCleanupReport struct {
	Schema      string `json:"schema"`
	LegacyRoot  string `json:"legacy_root"`
	ArchivePath string `json:"archive_path,omitempty"`
	Action      string `json:"action"`
	OK          bool   `json:"ok"`
}

func cleanupLegacyKDD(env paths.Env, rootFlag, archivePath string, confirmed bool) (legacyCleanupReport, error) {
	root := kddmigration.LegacyRoot(env, rootFlag)
	report := legacyCleanupReport{Schema: "worktrail.migration.cleanup.v1", LegacyRoot: root}
	if !confirmed {
		return report, fmt.Errorf("worktrail migrate kdd --cleanup-legacy requires --confirm")
	}
	doctor := buildMigrationDoctorReport(env, migrationDoctorOptions{Root: root, CleanupMode: true})
	if doctor.Summary.Errors > 0 {
		return report, fmt.Errorf("legacy cleanup blocked by migration doctor errors")
	}
	if strings.TrimSpace(archivePath) != "" {
		if !filepath.IsAbs(archivePath) {
			archivePath = filepath.Join(env.ProjectRoot, archivePath)
		}
		archivePath, _ = filepath.Abs(archivePath)
		if archivePath == root || strings.HasPrefix(archivePath, root+string(os.PathSeparator)) {
			return report, fmt.Errorf("archive path must be outside legacy KDD root")
		}
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			return report, err
		}
		if err := os.Rename(root, archivePath); err != nil {
			return report, err
		}
		report.ArchivePath = archivePath
		report.Action = "archived"
	} else {
		if err := os.RemoveAll(root); err != nil {
			return report, err
		}
		report.Action = "deleted"
	}
	if _, err := os.Stat(root); err == nil {
		return report, fmt.Errorf("legacy KDD root still exists after cleanup: %s", root)
	} else if !os.IsNotExist(err) {
		return report, err
	}
	report.OK = true
	return report, nil
}

func printMigrateHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail migrate kdd [--root path] [--write-candidates] [--format text|json]")
	fmt.Fprintln(out, "       worktrail migrate storage-plan [--format text|json]")
	fmt.Fprintln(out, "       worktrail migrate storage-apply --confirm [--format text|json]")
	fmt.Fprintln(out, "       worktrail migrate handoff-v2 [--apply --confirm] [--backup-dir path] [--format text|json]")
}

func printMigrateKDDHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail migrate kdd [--root path] [--write-candidates] [--write-pack file] [--update-gitignore] [--format text|json]")
	fmt.Fprintln(out, "       worktrail migrate kdd --cleanup-legacy --confirm [--root path] [--archive-path path] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Default mode is a dry-run. Candidate writes stay pending and never promote, merge, or discard knowledge.")
}

func runMigrateStoragePlan(env paths.Env, ioctx IO, args []string) error {
	flags, err := parseMigrateFlags(args, nil, map[string]bool{"format": true})
	if err != nil {
		return fmt.Errorf("worktrail migrate storage-plan: %w", err)
	}
	if err := validateMigrateFormat(flagValue(flags, "format", "text")); err != nil {
		return fmt.Errorf("worktrail migrate storage-plan: %w", err)
	}
	plan, err := storagemigration.PlanRoot(env.ProjectWT)
	if err != nil {
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(plan)
	}
	fmt.Fprintf(ioctx.Out, "schema: %s\nroot: %s\nitems: %d\nwarnings: %d\n", plan.Schema, plan.Root, len(plan.Items), len(plan.Warnings))
	for _, item := range plan.Items {
		if item.TargetPath != "" {
			fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", item.Action, item.SourcePath, item.TargetPath)
			continue
		}
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", item.Action, item.SourcePath, item.Reason)
	}
	return nil
}

func runMigrateStorageApply(env paths.Env, ioctx IO, args []string) error {
	flags, err := parseMigrateFlags(args, map[string]bool{"confirm": true}, map[string]bool{"format": true})
	if err != nil {
		return fmt.Errorf("worktrail migrate storage-apply: %w", err)
	}
	if err := validateMigrateFormat(flagValue(flags, "format", "text")); err != nil {
		return fmt.Errorf("worktrail migrate storage-apply: %w", err)
	}
	report, err := storagemigration.ApplyRoot(env.ProjectWT, flags["confirm"] == "true")
	if err != nil {
		if flagValue(flags, "format", "text") == "json" {
			_ = json.NewEncoder(ioctx.Out).Encode(report)
		}
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "schema: %s\nroot: %s\nmanifest: %s\ncreated: %d\nskipped: %d\nwarnings: %d\n", report.Schema, report.Root, report.Manifest, report.Created, report.Skipped, len(report.Warnings))
	return nil
}

func runMigrateHandoffV2(env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		fmt.Fprintln(ioctx.Out, "usage: worktrail migrate handoff-v2 [--apply --confirm] [--backup-dir path] [--format text|json]")
		fmt.Fprintln(ioctx.Out)
		fmt.Fprintln(ioctx.Out, "Inventories root-level V1 handoffs and retired handoff candidates. Default mode is read-only.")
		return nil
	}
	flags, err := parseMigrateFlags(args,
		map[string]bool{"apply": true, "confirm": true},
		map[string]bool{"backup-dir": true, "format": true},
	)
	if err != nil {
		return fmt.Errorf("worktrail migrate handoff-v2: %w", err)
	}
	format := flagValue(flags, "format", "text")
	if err := validateMigrateFormat(format); err != nil {
		return fmt.Errorf("worktrail migrate handoff-v2: %w", err)
	}
	report, err := handoffmigration.Run(handoffmigration.Options{
		Root:      env.ProjectWT,
		BackupDir: flagValue(flags, "backup-dir", ""),
		Apply:     flags["apply"] == "true",
		Confirm:   flags["confirm"] == "true",
	})
	if err == nil && report.Applied {
		indexResult := rebuildIndexForScope(env, "project")
		report.IndexRebuild = &handoffmigration.IndexRebuild{
			Scope: indexResult.Scope, Entries: indexResult.Entries,
			IndexPath: indexResult.IndexPath, Error: indexResult.Error,
		}
		if indexResult.Error != "" {
			report.OK = false
			err = fmt.Errorf("handoff-v2 migration applied but required index rebuild failed: %s", indexResult.Error)
		}
	}
	if format == "json" {
		if encodeErr := json.NewEncoder(ioctx.Out).Encode(report); encodeErr != nil {
			return encodeErr
		}
	} else {
		fmt.Fprintf(ioctx.Out, "schema: %s\nroot: %s\ndry_run: %t\nok: %t\ninventory_hash: %s\nfiles: %d\nlegacy_handoffs: %d\nhandoff_candidates: %d\nplanned: %d\nmigrated: %d\nnoop: %d\nconflicts: %d\ninvalid: %d\nunresolved: %d\nbackup_dir: %s\n",
			report.Schema, report.Root, report.DryRun, report.OK, report.InventoryHash, report.InventoryFileCount,
			report.Summary.LegacyHandoffs, report.Summary.HandoffCandidates, report.Summary.Planned,
			report.Summary.Migrated, report.Summary.Noop, report.Summary.Conflicts,
			report.Summary.Invalid, report.Summary.Unresolved, report.BackupDir)
		for _, item := range report.Items {
			fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\t%s\n", item.Status, item.SourcePath, item.TargetPath, item.SourceHash)
		}
		if report.IndexRebuild != nil {
			fmt.Fprintf(ioctx.Out, "index_rebuilt: %s\t%d\t%s\n", report.IndexRebuild.Scope, report.IndexRebuild.Entries, report.IndexRebuild.IndexPath)
		}
	}
	return err
}

func parseMigrateFlags(args []string, booleanFlags, valueFlags map[string]bool) (map[string]string, error) {
	flags := map[string]string{}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") || arg == "--" {
			return nil, fmt.Errorf("positional arguments are not accepted: %q", arg)
		}
		raw := strings.TrimPrefix(arg, "--")
		key, value, hasValue := raw, "", false
		if strings.Contains(raw, "=") {
			key, value, hasValue = strings.Cut(raw, "=")
		}
		if key == "" || !booleanFlags[key] && !valueFlags[key] {
			return nil, fmt.Errorf("unknown flag --%s", key)
		}
		if _, duplicate := flags[key]; duplicate {
			return nil, fmt.Errorf("flag --%s was provided more than once", key)
		}
		if booleanFlags[key] {
			if !hasValue {
				flags[key] = "true"
				continue
			}
			if value != "true" && value != "false" {
				return nil, fmt.Errorf("flag --%s requires true or false", key)
			}
			flags[key] = value
			continue
		}
		if !hasValue {
			if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
				return nil, fmt.Errorf("flag --%s requires a value", key)
			}
			index++
			value = args[index]
		}
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("flag --%s requires a value", key)
		}
		flags[key] = value
	}
	return flags, nil
}

func validateMigrateFormat(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("--format must be text or json, got %q", format)
	}
	return nil
}
