package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
	default:
		return fmt.Errorf("unsupported migrate target %q", args[0])
	}
}

func runMigrateKDD(env paths.Env, ioctx IO, args []string) error {
	if wantsHelp(args) {
		printMigrateKDDHelp(ioctx.Out)
		return nil
	}
	flags, _ := splitFlags(args)
	root := flagValue(flags, "root", "")
	format := flagValue(flags, "format", "text")
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
}

func printMigrateKDDHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail migrate kdd [--root path] [--write-candidates] [--write-pack file] [--update-gitignore] [--format text|json]")
	fmt.Fprintln(out, "       worktrail migrate kdd --cleanup-legacy --confirm [--root path] [--archive-path path] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Default mode is a dry-run. Candidate writes stay pending and never promote, merge, or discard knowledge.")
}

func runMigrateStoragePlan(env paths.Env, ioctx IO, args []string) error {
	flags, _ := splitFlags(args)
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
	flags, _ := splitFlags(args)
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
