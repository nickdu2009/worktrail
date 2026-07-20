package handoffmigration

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/textsafety"
)

const (
	reportSchema   = "worktrail.migration.handoff-v2.report.v1"
	manifestSchema = "worktrail.migration.handoff-v2.manifest.v1"
)

type Options struct {
	Root      string
	BackupDir string
	Apply     bool
	Confirm   bool
	Now       func() time.Time
	Failpoint ops.Failpoint
}

type Summary struct {
	LegacyHandoffs     int `json:"legacy_handoffs"`
	HandoffCandidates  int `json:"handoff_candidates"`
	Planned            int `json:"planned"`
	Migrated           int `json:"migrated"`
	Noop               int `json:"noop"`
	Conflicts          int `json:"conflicts"`
	Invalid            int `json:"invalid"`
	Unresolved         int `json:"unresolved"`
	SourceFilesRemoved int `json:"source_files_removed"`
}

type Diagnostic struct {
	Code     string `json:"code"`
	Path     string `json:"path,omitempty"`
	Message  string `json:"message"`
	Severity string `json:"severity"`
}

type IndexRebuild struct {
	Scope     string `json:"scope"`
	Entries   int    `json:"entries,omitempty"`
	IndexPath string `json:"index_path,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Item struct {
	SourceKind    string       `json:"source_kind"`
	SourcePath    string       `json:"source_path"`
	SourceHash    string       `json:"source_hash"`
	TargetPath    string       `json:"target_path"`
	TargetHash    string       `json:"target_hash"`
	HandoffID     string       `json:"handoff_id"`
	TaskID        string       `json:"task_id"`
	Status        string       `json:"status"`
	SourceRemoved bool         `json:"source_removed,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`

	sourceAbs  string
	sourceMode os.FileMode
	sourceData []byte
	targetData []byte
}

type Report struct {
	Schema             string        `json:"schema"`
	GeneratedAt        string        `json:"generated_at"`
	Root               string        `json:"root"`
	DryRun             bool          `json:"dry_run"`
	Applied            bool          `json:"applied"`
	OK                 bool          `json:"ok"`
	BackupDir          string        `json:"backup_dir"`
	ManifestPath       string        `json:"manifest_path,omitempty"`
	ManifestHash       string        `json:"manifest_hash,omitempty"`
	ManifestFileCount  int           `json:"manifest_file_count,omitempty"`
	InventoryHash      string        `json:"inventory_hash"`
	InventoryFileCount int           `json:"inventory_file_count"`
	Summary            Summary       `json:"summary"`
	Items              []Item        `json:"items"`
	Diagnostics        []Diagnostic  `json:"diagnostics,omitempty"`
	Operations         []string      `json:"operations,omitempty"`
	IndexRebuild       *IndexRebuild `json:"index_rebuild,omitempty"`
	Recovery           string        `json:"recovery,omitempty"`
}

type manifest struct {
	Schema        string         `json:"schema"`
	CreatedAt     string         `json:"created_at"`
	Root          string         `json:"root"`
	InventoryHash string         `json:"inventory_hash"`
	FileCount     int            `json:"file_count"`
	Files         []manifestFile `json:"files"`
}

type manifestFile struct {
	SourceKind string `json:"source_kind"`
	SourcePath string `json:"source_path"`
	SourceHash string `json:"source_hash"`
	BackupPath string `json:"backup_path"`
	Mode       uint32 `json:"mode"`
}

type legacySource struct {
	kind      string
	rel       string
	abs       string
	data      []byte
	mode      os.FileMode
	meta      map[string]any
	body      string
	modTime   time.Time
	candidate bool
}

func Run(options Options) (Report, error) {
	now := time.Now().UTC()
	if options.Now != nil {
		now = options.Now().UTC()
	}
	root, err := filepath.Abs(strings.TrimSpace(options.Root))
	if err != nil || strings.TrimSpace(options.Root) == "" {
		if err == nil {
			err = errors.New("worktrail root is required")
		}
		return Report{}, err
	}
	report := Report{
		Schema:      reportSchema,
		GeneratedAt: now.Format(time.RFC3339Nano),
		Root:        root,
		DryRun:      !options.Apply,
		Items:       []Item{},
	}
	if options.Apply != options.Confirm {
		return report, errors.New("handoff-v2 migration mutations require both --apply and --confirm")
	}
	report.BackupDir, err = resolveBackupDir(root, options.BackupDir, now)
	if err != nil {
		return report, err
	}

	sources, inventoryDiagnostics, err := inventory(root)
	if err != nil {
		return report, err
	}
	report.Diagnostics = append(report.Diagnostics, inventoryDiagnostics...)
	projectID := readProjectID(root)
	for _, source := range sources {
		item, itemErr := buildItem(root, projectID, source)
		if itemErr != nil {
			item = Item{
				SourceKind: source.kind,
				SourcePath: source.rel,
				SourceHash: hashBytes(source.data),
				Status:     "invalid",
				Diagnostics: []Diagnostic{{
					Code: "legacy_source_invalid", Path: source.rel, Severity: "error", Message: itemErr.Error(),
				}},
				sourceAbs: source.abs, sourceMode: source.mode, sourceData: source.data,
			}
		}
		report.Items = append(report.Items, item)
		if source.candidate {
			report.Summary.HandoffCandidates++
		} else {
			report.Summary.LegacyHandoffs++
		}
	}
	sort.Slice(report.Items, func(i, j int) bool { return report.Items[i].SourcePath < report.Items[j].SourcePath })
	report.InventoryHash = inventoryHash(report.Items)
	report.InventoryFileCount = len(report.Items)

	targetGroups := map[string][]int{}
	for index := range report.Items {
		if report.Items[index].Status != "invalid" {
			targetGroups[report.Items[index].TargetPath] = append(targetGroups[report.Items[index].TargetPath], index)
		}
	}
	for targetPath, indices := range targetGroups {
		if len(indices) < 2 {
			continue
		}
		expectedHash := report.Items[indices[0]].TargetHash
		for _, index := range indices[1:] {
			if report.Items[index].TargetHash == expectedHash {
				continue
			}
			for _, conflictIndex := range indices {
				item := &report.Items[conflictIndex]
				if item.Status == "conflict" {
					continue
				}
				item.Status = "conflict"
				item.Diagnostics = append(item.Diagnostics, Diagnostic{
					Code: "migration_target_collision", Path: targetPath, Severity: "error",
					Message: "multiple legacy sources map to the same V2 handoff with different hashes",
				})
				report.Summary.Conflicts++
			}
			break
		}
	}
	plannedTargets := map[string]bool{}
	for index := range report.Items {
		item := &report.Items[index]
		for _, diagnostic := range item.Diagnostics {
			if strings.Contains(diagnostic.Code, "unresolved") {
				report.Summary.Unresolved++
			}
		}
		if item.Status == "invalid" {
			report.Summary.Invalid++
			continue
		}
		if item.Status == "conflict" {
			continue
		}
		targetAbs := filepath.Join(root, filepath.FromSlash(item.TargetPath))
		existing, readErr := os.ReadFile(targetAbs)
		switch {
		case readErr == nil && hashBytes(existing) == item.TargetHash:
			item.Status = "noop"
			report.Summary.Noop++
		case readErr == nil:
			item.Status = "conflict"
			item.Diagnostics = append(item.Diagnostics, Diagnostic{
				Code:     "target_hash_conflict",
				Path:     item.TargetPath,
				Severity: "error",
				Message:  "target exists with a different hash; migration will not overwrite it",
			})
			report.Summary.Conflicts++
		case errors.Is(readErr, os.ErrNotExist):
			if plannedTargets[item.TargetPath] {
				item.Status = "coalesced"
			} else {
				item.Status = "planned"
				report.Summary.Planned++
				plannedTargets[item.TargetPath] = true
			}
		default:
			return report, readErr
		}
	}

	if !options.Apply {
		report.OK = report.Summary.Conflicts == 0 && report.Summary.Invalid == 0
		return report, nil
	}
	if report.Summary.Conflicts > 0 || report.Summary.Invalid > 0 {
		return report, fmt.Errorf("handoff-v2 migration blocked: conflicts=%d invalid=%d", report.Summary.Conflicts, report.Summary.Invalid)
	}
	if len(migratableItems(report.Items)) == 0 {
		report.Applied = true
		report.OK = true
		return report, nil
	}

	if strings.TrimSpace(options.BackupDir) == "" {
		if err := store.EnsureProjectGitignore(paths.Env{ProjectRoot: filepath.Dir(root)}); err != nil {
			return report, fmt.Errorf("ensure default handoff-v2 backup is gitignored: %w", err)
		}
	}
	report.ManifestPath, err = createBackup(report.BackupDir, report, now)
	if err != nil {
		return report, err
	}
	report.ManifestHash, err = hashFile(report.ManifestPath)
	if err != nil {
		return report, fmt.Errorf("hash backup manifest: %w", err)
	}
	report.ManifestFileCount = len(migratableItems(report.Items))
	report.Recovery = fmt.Sprintf("restore each source_path from %s according to %s; do not overwrite a changed destination", filepath.Join(report.BackupDir, "files"), report.ManifestPath)

	coordinator := ops.New(root)
	coordinator.Failpoint = options.Failpoint
	var writes []ops.Write
	writtenTargets := map[string]bool{}
	var deletes []string
	for _, item := range report.Items {
		if isMigratableStatus(item.Status) && !writtenTargets[item.TargetPath] {
			writes = append(writes, ops.Write{Path: item.TargetPath, Data: item.targetData, Mode: 0o600})
			writtenTargets[item.TargetPath] = true
		}
		if isMigratableStatus(item.Status) {
			deletes = append(deletes, item.SourcePath)
		}
	}
	writeOpID := operationID("handoff-v2-migration-write", report.InventoryHash)
	if !operationCommitted(root, writeOpID) {
		operation, beginErr := coordinator.Begin(ops.Spec{ID: writeOpID, Writes: writes})
		if beginErr != nil {
			return report, beginErr
		}
		report.Operations = append(report.Operations, writeOpID)
		if err := verifyMigrationIntent(operation.Intent(), report.Items, false); err != nil {
			return report, errors.Join(err, operation.Abort())
		}
		if err := operation.Commit(); err != nil {
			return report, fmt.Errorf("commit handoff-v2 write operation %s: %w", writeOpID, err)
		}
	}

	for _, item := range report.Items {
		if !isMigratableStatus(item.Status) {
			continue
		}
		targetAbs := filepath.Join(root, filepath.FromSlash(item.TargetPath))
		record, verifyErr := handoff.Read(targetAbs)
		if verifyErr != nil {
			return report, fmt.Errorf("verify migrated handoff %s: %w", item.TargetPath, verifyErr)
		}
		actualHash, verifyErr := hashFile(targetAbs)
		if verifyErr != nil {
			return report, fmt.Errorf("hash migrated handoff %s: %w", item.TargetPath, verifyErr)
		}
		if record.Meta.ID != item.HandoffID || actualHash != item.TargetHash {
			return report, fmt.Errorf("verify migrated handoff %s: identity or target hash mismatch", item.TargetPath)
		}
	}

	cleanupOpID := operationID("handoff-v2-migration-cleanup", report.InventoryHash)
	operation, err := coordinator.Begin(ops.Spec{ID: cleanupOpID, Writes: writes, Deletes: deletes})
	if err != nil {
		return report, err
	}
	report.Operations = append(report.Operations, cleanupOpID)
	if err := verifyMigrationIntent(operation.Intent(), report.Items, true); err != nil {
		return report, errors.Join(err, operation.Abort())
	}
	if err := operation.Commit(); err != nil {
		return report, fmt.Errorf("commit handoff-v2 cleanup operation %s: %w", cleanupOpID, err)
	}
	for index := range report.Items {
		item := &report.Items[index]
		if !isMigratableStatus(item.Status) {
			continue
		}
		if _, statErr := os.Lstat(item.sourceAbs); !errors.Is(statErr, os.ErrNotExist) {
			if statErr == nil {
				statErr = errors.New("source still exists")
			}
			return report, fmt.Errorf("verify legacy source removal %s: %w", item.SourcePath, statErr)
		}
		item.SourceRemoved = true
		item.Status = "migrated"
		report.Summary.Migrated++
		report.Summary.SourceFilesRemoved++
	}
	report.Applied = true
	report.OK = true
	if err := writeJSONAtomic(report.ManifestPath, manifestFor(report, now), 0o600); err != nil {
		return report, err
	}
	report.ManifestHash, err = hashFile(report.ManifestPath)
	if err != nil {
		return report, fmt.Errorf("hash final backup manifest: %w", err)
	}
	return report, nil
}

func inventory(root string) ([]legacySource, []Diagnostic, error) {
	var sources []legacySource
	var diagnostics []Diagnostic
	handoffsDir := filepath.Join(root, "handoffs")
	entries, err := os.ReadDir(handoffsDir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			continue
		}
		source, sourceErr := readLegacySource(root, filepath.Join(handoffsDir, entry.Name()), "handoff_v1")
		if sourceErr != nil {
			return nil, nil, sourceErr
		}
		schema := stringValue(source.meta["schema"])
		if schema == model.SchemaHandoffV2 {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "v2_handoff_in_legacy_root", Path: source.rel, Severity: "warning",
				Message: "V2 handoff is already encoded but remains in the legacy root; it was not inventoried as V1",
			})
			continue
		}
		if schema != "" && schema != model.SchemaHandoff {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "non_handoff_in_legacy_root", Path: source.rel, Severity: "warning",
				Message: fmt.Sprintf("root handoff file has unsupported schema %q and was skipped", schema),
			})
			continue
		}
		sources = append(sources, source)
	}

	candidatesDir := filepath.Join(root, "candidates")
	err = filepath.WalkDir(candidatesDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".md") {
			return nil
		}
		source, sourceErr := readLegacySource(root, path, "handoff_candidate")
		if sourceErr != nil {
			return sourceErr
		}
		if stringValue(source.meta["schema"]) != model.SchemaCandidate || stringValue(source.meta["candidate_type"]) != model.CandidateTypeHandoff {
			return nil
		}
		source.candidate = true
		sources = append(sources, source)
		return nil
	})
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].rel < sources[j].rel })
	return sources, diagnostics, nil
}

func operationCommitted(root, operationID string) bool {
	_, err := os.Stat(filepath.Join(root, "ops", "journal", operationID+".commit.json"))
	return err == nil
}

func readLegacySource(root, path, kind string) (legacySource, error) {
	if err := rejectSymlinkPath(root, path); err != nil {
		return legacySource{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return legacySource{}, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return legacySource{}, fmt.Errorf("legacy source %q is a symbolic link", path)
	}
	if !info.Mode().IsRegular() {
		return legacySource{}, fmt.Errorf("legacy source %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return legacySource{}, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return legacySource{}, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(info, after) {
		return legacySource{}, fmt.Errorf("legacy source %q changed while it was inventoried", path)
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return legacySource{}, err
	}
	source := legacySource{
		kind: kind, rel: filepath.ToSlash(rel), abs: path, data: data,
		mode: info.Mode().Perm(), body: string(data), meta: map[string]any{}, modTime: info.ModTime().UTC(),
	}
	if doc, parseErr := store.ParseMarkdown(data); parseErr == nil {
		source.meta = doc.Meta
		source.body = doc.Body
	}
	return source, nil
}

func rejectSymlinkPath(root, path string) error {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("legacy source %q escapes the worktrail root", path)
	}
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("worktrail root %q is a symbolic link", root)
	}
	current := root
	for _, part := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("legacy source path %q contains a symbolic link", current)
		}
	}
	return nil
}

func secureReferencePath(root, path string) (string, error) {
	rootAbs, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return "", err
	}
	if isAnyAbsolutePath(path) && !filepath.IsAbs(filepath.FromSlash(path)) {
		return "", fmt.Errorf("legacy reference %q is an absolute path outside the native Worktrail root", path)
	}
	path = filepath.Clean(path)
	if !filepath.IsAbs(path) {
		path = filepath.Join(rootAbs, path)
	}
	pathAbs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, pathAbs)
	if err != nil || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("legacy reference %q escapes the worktrail root", path)
	}
	rootInfo, err := os.Lstat(rootAbs)
	if err != nil {
		return "", err
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("worktrail root %q is a symbolic link", rootAbs)
	}
	current := rootAbs
	for _, part := range strings.Split(filepath.Clean(rel), string(filepath.Separator)) {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, statErr := os.Lstat(current)
		if errors.Is(statErr, os.ErrNotExist) {
			return pathAbs, nil
		}
		if statErr != nil {
			return "", statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("legacy reference path %q contains a symbolic link", current)
		}
	}
	return pathAbs, nil
}

func buildItem(root, projectID string, source legacySource) (Item, error) {
	id := strings.TrimSpace(stringValue(source.meta["id"]))
	if id == "" {
		id = strings.TrimSuffix(filepath.Base(source.rel), filepath.Ext(source.rel))
	}
	if err := validateOpaqueIdentity("legacy handoff id", id); err != nil {
		return Item{}, err
	}
	scope := withDefault(stringValue(source.meta["scope"]), "project")
	if scope != "project" {
		return Item{}, fmt.Errorf("legacy handoff scope %q is not supported under a project .worktrail root", scope)
	}
	diagnostics := []Diagnostic{}
	candidateStatus := ""
	if source.candidate {
		candidateStatus = strings.ToLower(strings.TrimSpace(stringValue(source.meta["status"])))
		if isTerminalCandidateStatus(candidateStatus) {
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "terminal_handoff_candidate_preserved",
				Path:     source.rel,
				Severity: "warning",
				Message:  fmt.Sprintf("legacy handoff candidate terminal status %s will be preserved as the V2 lifecycle while the candidate source is retired", candidateStatus),
			})
		}
	}
	sourceState, stateTaskID, err := resolveRef(root, scope, "state", firstValue(source.meta, "source_state", "source_state_id", "source_state_path"), source.rel, &diagnostics)
	if err != nil {
		return Item{}, fmt.Errorf("validate legacy source_state: %w", err)
	}
	previous, _, err := resolveRef(root, scope, "handoff", firstValue(source.meta, "previous_handoff", "previous_handoff_id", "previous_handoff_path"), source.rel, &diagnostics)
	if err != nil {
		return Item{}, fmt.Errorf("validate legacy previous_handoff: %w", err)
	}
	taskID := strings.TrimSpace(stateTaskID)
	taskIdentityUncertain := taskID == ""
	if taskID == "" {
		taskID = stableLegacyTaskID(source.kind + "\x00" + source.rel + "\x00" + id)
	}
	body, bodyDiagnostics, err := sanitizeLegacyBody(source.body, source.rel)
	if err != nil {
		return Item{}, err
	}
	diagnostics = append(diagnostics, bodyDiagnostics...)
	redactionStatus := withDefault(stringValue(source.meta["redaction_status"]), "clean")
	if len(bodyDiagnostics) > 0 {
		redactionStatus = "redacted"
	}
	title, err := sanitizeLegacyField(withDefault(stringValue(source.meta["title"]), inferTitle(body, id)))
	if err != nil {
		return Item{}, fmt.Errorf("sanitize legacy title: %w", err)
	}
	summary, err := sanitizeLegacyField(withDefault(stringValue(source.meta["summary"]), firstSummary(body, title)))
	if err != nil {
		return Item{}, fmt.Errorf("sanitize legacy summary: %w", err)
	}
	createdAt := timeValue(source.meta["created_at"], source.modTime)
	updatedAt := timeValue(source.meta["updated_at"], createdAt)
	lifecycle := withDefault(firstString(source.meta, "lifecycle_status", "status"), model.LifecycleCurrent)
	if source.candidate && isTerminalCandidateStatus(candidateStatus) {
		lifecycle = candidateStatus
	} else if lifecycle == "pending" || lifecycle == "" || source.candidate {
		lifecycle = model.LifecycleCurrent
	}
	meta := model.HandoffMetaV2{
		BaseMetaV2: model.BaseMetaV2{
			Schema: model.SchemaHandoffV2, ID: id, Scope: scope, ObjectKind: model.ObjectKindRuntime,
			Title: title, Tags: stringList(source.meta["tags"]), CreatedAt: createdAt, UpdatedAt: updatedAt,
		},
		ProjectID:             withDefault(projectID, "project_legacy_unbound"),
		TaskID:                taskID,
		RuntimeType:           model.RuntimeTypeHandoff,
		Summary:               summary,
		Complete:              true,
		Visibility:            model.VisibilityLocal,
		StorageClass:          model.StorageClassLocal,
		Durability:            model.DurabilityEphemeral,
		LifecycleStatus:       lifecycle,
		SourceState:           sourceState,
		PreviousHandoff:       previous,
		ResumePriority:        model.ResumePriorityManualHandoff,
		FormatVersion:         2,
		SchemaCompat:          []string{model.SchemaHandoffV2, model.SchemaHandoff},
		SourceTool:            withDefault(stringValue(source.meta["source_tool"]), "worktrail-migration"),
		Actor:                 "worktrail:migrate-handoff-v2",
		MigratedFrom:          source.rel,
		TaskIdentityUncertain: taskIdentityUncertain,
		RedactionStatus:       redactionStatus,
		Worktree: model.WorktreeSnapshot{
			CodeAvailability: model.CodeAvailabilityUnavailable,
			CapturedAt:       updatedAt,
		},
	}
	contentHash, err := handoffContentHash(meta, body)
	if err != nil {
		return Item{}, err
	}
	meta.ContentHash = contentHash
	targetData, err := store.RenderMarkdown(meta, body)
	if err != nil {
		return Item{}, err
	}
	if err := validateGeneratedV2(meta, body, targetData); err != nil {
		return Item{}, fmt.Errorf("prevalidate generated V2 handoff: %w", err)
	}
	targetPath := filepath.ToSlash(filepath.Join("handoffs", "local", id+".md"))
	return Item{
		SourceKind: source.kind, SourcePath: source.rel, SourceHash: hashBytes(source.data),
		TargetPath: targetPath, TargetHash: hashBytes(targetData), HandoffID: id, TaskID: taskID,
		Diagnostics: diagnostics, sourceAbs: source.abs, sourceMode: source.mode,
		sourceData: source.data, targetData: targetData,
	}, nil
}

func resolveRef(root, scope, kind string, raw any, sourcePath string, diagnostics *[]Diagnostic) (*model.Ref, string, error) {
	if raw == nil {
		return nil, "", nil
	}
	id, rel := "", ""
	switch value := raw.(type) {
	case string:
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, "", nil
		}
		if isAnyAbsolutePath(value) || strings.ContainsAny(value, `/\`) {
			refID, refRel, taskID, ok, inspectErr := inspectReference(root, value)
			if inspectErr != nil {
				return nil, "", inspectErr
			}
			if ok {
				if err := validateOpaqueIdentity(kind+" reference id", refID); err != nil {
					return nil, "", err
				}
				id, rel = refID, normalizeRefRel(kind, refID, refRel)
				return &model.Ref{Scope: scope, Kind: kind, ID: id, RelPath: rel}, taskID, nil
			}
			id = strings.TrimSuffix(filepath.Base(value), filepath.Ext(value))
			*diagnostics = append(*diagnostics, Diagnostic{
				Code: "unresolved_absolute_source_reference", Path: sourcePath, Severity: "warning",
				Message: fmt.Sprintf("%s reference %q could not be resolved; retained opaque id %q without an absolute path", kind, value, id),
			})
		} else {
			id = value
		}
	case map[string]any:
		id = stringValue(value["id"])
		rel = filepath.ToSlash(stringValue(value["rel_path"]))
	default:
		encoded, _ := json.Marshal(value)
		var object map[string]any
		if json.Unmarshal(encoded, &object) == nil {
			id = stringValue(object["id"])
			rel = filepath.ToSlash(stringValue(object["rel_path"]))
		}
	}
	if id == "" {
		*diagnostics = append(*diagnostics, Diagnostic{
			Code: "unresolved_source_reference", Path: sourcePath, Severity: "warning",
			Message: fmt.Sprintf("%s reference could not be converted to an id/ref", kind),
		})
		return nil, "", nil
	}
	if err := validateOpaqueIdentity(kind+" reference id", id); err != nil {
		return nil, "", err
	}
	rel = strings.ReplaceAll(rel, `\`, "/")
	if isAnyAbsolutePath(rel) || rel == ".." || strings.HasPrefix(rel, "../") {
		return nil, "", fmt.Errorf("%s reference rel_path must stay within the Worktrail root", kind)
	}
	taskID := ""
	if rel != "" {
		refID, refRel, foundTaskID, ok, inspectErr := inspectReference(root, filepath.FromSlash(rel))
		if inspectErr != nil {
			return nil, "", inspectErr
		}
		if ok {
			if err := validateOpaqueIdentity(kind+" reference id", refID); err != nil {
				return nil, "", err
			}
			id, rel, taskID = refID, normalizeRefRel(kind, refID, refRel), foundTaskID
			return &model.Ref{Scope: scope, Kind: kind, ID: id, RelPath: rel}, taskID, nil
		}
	}
	rel = normalizeRefRel(kind, id, rel)
	if kind == "state" {
		for _, candidate := range []string{
			filepath.Join(root, "state", "active", id+".md"),
			filepath.Join(root, "state", "archived", id+".md"),
			filepath.Join(root, "state", "checkpoints", id+".md"),
		} {
			refID, refRel, foundTaskID, ok, inspectErr := inspectReference(root, candidate)
			if inspectErr != nil {
				return nil, "", inspectErr
			}
			if ok {
				if err := validateOpaqueIdentity(kind+" reference id", refID); err != nil {
					return nil, "", err
				}
				id, rel, taskID = refID, normalizeRefRel(kind, refID, refRel), foundTaskID
				break
			}
		}
	}
	return &model.Ref{Scope: scope, Kind: kind, ID: id, RelPath: rel}, taskID, nil
}

func normalizeRefRel(kind, id, rel string) string {
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if kind == "handoff" && strings.HasPrefix(rel, "handoffs/") &&
		!strings.HasPrefix(rel, "handoffs/local/") && !strings.HasPrefix(rel, "handoffs/team/") {
		return filepath.ToSlash(filepath.Join("handoffs", "local", id+".md"))
	}
	return rel
}

func inspectReference(root, path string) (string, string, string, bool, error) {
	path, err := secureReferencePath(root, path)
	if err != nil {
		return "", "", "", false, err
	}
	before, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", "", "", false, nil
	}
	if err != nil {
		return "", "", "", false, err
	}
	if before.Mode()&os.ModeSymlink != 0 {
		return "", "", "", false, fmt.Errorf("legacy reference %q is a symbolic link", path)
	}
	if !before.Mode().IsRegular() {
		return "", "", "", false, fmt.Errorf("legacy reference %q is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", "", false, err
	}
	after, err := os.Lstat(path)
	if err != nil {
		return "", "", "", false, err
	}
	if after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return "", "", "", false, fmt.Errorf("legacy reference %q changed while it was inspected", path)
	}
	id := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	taskID := ""
	if doc, parseErr := store.ParseMarkdown(data); parseErr == nil {
		id = withDefault(stringValue(doc.Meta["id"]), id)
		taskID = stringValue(doc.Meta["task_id"])
	}
	rel := ""
	if candidate, relErr := filepath.Rel(root, path); relErr == nil && candidate != ".." && !strings.HasPrefix(candidate, ".."+string(filepath.Separator)) {
		rel = filepath.ToSlash(candidate)
	}
	return id, rel, taskID, true, nil
}

func validateGeneratedV2(expected model.HandoffMetaV2, expectedBody string, targetData []byte) error {
	doc, err := store.ParseMarkdown(targetData)
	if err != nil {
		return fmt.Errorf("parse rendered target: %w", err)
	}
	raw, err := json.Marshal(doc.Meta)
	if err != nil {
		return err
	}
	var meta model.HandoffMetaV2
	if err := json.Unmarshal(raw, &meta); err != nil {
		return fmt.Errorf("decode rendered target metadata: %w", err)
	}
	if !reflect.DeepEqual(meta, expected) {
		return errors.New("rendered target metadata does not round-trip to the prevalidated V2 metadata")
	}
	body := normalizeNewlines(doc.Body)
	if strings.TrimSpace(body) != strings.TrimSpace(normalizeNewlines(expectedBody)) {
		return errors.New("rendered target body does not match the prevalidated body")
	}
	if meta.Schema != model.SchemaHandoffV2 || expected.Schema != model.SchemaHandoffV2 {
		return fmt.Errorf("unexpected V2 handoff schema %q", meta.Schema)
	}
	for name, value := range map[string]string{
		"handoff id": meta.ID, "project_id": meta.ProjectID, "task_id": meta.TaskID,
		"source_tool": meta.SourceTool, "actor": meta.Actor,
	} {
		if err := validateOpaqueIdentity(name, value); err != nil {
			return err
		}
	}
	if meta.Scope != "project" || meta.ObjectKind != model.ObjectKindRuntime ||
		meta.RuntimeType != model.RuntimeTypeHandoff {
		return fmt.Errorf("invalid runtime identity scope=%q object_kind=%q runtime_type=%q", meta.Scope, meta.ObjectKind, meta.RuntimeType)
	}
	if strings.TrimSpace(meta.Title) == "" || strings.TrimSpace(meta.Summary) == "" {
		return errors.New("generated V2 handoff requires title and summary")
	}
	if meta.CreatedAt.IsZero() || meta.UpdatedAt.IsZero() || meta.Worktree.CapturedAt.IsZero() {
		return errors.New("generated V2 handoff timestamps must be populated")
	}
	if !meta.Complete || meta.Visibility != model.VisibilityLocal ||
		meta.StorageClass != model.StorageClassLocal || meta.Durability != model.DurabilityEphemeral ||
		meta.ResumePriority != model.ResumePriorityManualHandoff || meta.FormatVersion != 2 {
		return fmt.Errorf("invalid local V2 handoff storage metadata")
	}
	if !validMigratedLifecycle(meta.LifecycleStatus) {
		return fmt.Errorf("invalid migrated lifecycle_status %q", meta.LifecycleStatus)
	}
	if meta.RedactionStatus != "clean" && meta.RedactionStatus != "redacted" {
		return fmt.Errorf("invalid migrated redaction_status %q", meta.RedactionStatus)
	}
	if meta.Worktree.CodeAvailability != model.CodeAvailabilityUnavailable {
		return fmt.Errorf("invalid migrated code_availability %q", meta.Worktree.CodeAvailability)
	}
	if meta.MigratedFrom == "" || isAnyAbsolutePath(meta.MigratedFrom) ||
		meta.MigratedFrom == ".." || strings.HasPrefix(meta.MigratedFrom, "../") {
		return errors.New("generated V2 migrated_from must be Worktrail-root-relative")
	}
	if len(meta.SchemaCompat) != 2 || meta.SchemaCompat[0] != model.SchemaHandoffV2 ||
		meta.SchemaCompat[1] != model.SchemaHandoff {
		return errors.New("generated V2 schema_compat is incomplete")
	}
	for name, item := range rangeMigrationRefs(meta) {
		if err := validateGeneratedRef(name, meta.Scope, item.kind, item.ref); err != nil {
			return err
		}
	}
	if len([]byte(body)) > handoff.LocalBodyMax {
		return fmt.Errorf("generated V2 body exceeds %d bytes", handoff.LocalBodyMax)
	}
	if len(meta.ContentHash) != sha256.Size*2 {
		return errors.New("generated V2 content_hash must be a SHA-256 hex digest")
	}
	if _, err := hex.DecodeString(meta.ContentHash); err != nil {
		return fmt.Errorf("decode generated V2 content_hash: %w", err)
	}
	expectedHash, err := handoffContentHash(meta, body)
	if err != nil {
		return err
	}
	if meta.ContentHash != expectedHash {
		return fmt.Errorf("generated V2 content_hash mismatch: got %s want %s", meta.ContentHash, expectedHash)
	}
	safety, err := textsafety.Process(generatedSafetyText(meta, body), textsafety.ProfileLocal)
	if err != nil {
		return fmt.Errorf("generated V2 text safety: %w", err)
	}
	if safety.Redacted {
		return errors.New("generated V2 metadata or body still contains unprocessed redactable content")
	}
	return nil
}

func rangeMigrationRefs(meta model.HandoffMetaV2) map[string]struct {
	kind string
	ref  *model.Ref
} {
	return map[string]struct {
		kind string
		ref  *model.Ref
	}{
		"source_state":     {kind: "state", ref: meta.SourceState},
		"previous_handoff": {kind: "handoff", ref: meta.PreviousHandoff},
		"published_from":   {kind: "handoff", ref: meta.PublishedFrom},
	}
}

func validateGeneratedRef(name, scope, kind string, ref *model.Ref) error {
	if ref == nil {
		return nil
	}
	if ref.Scope != scope || ref.Kind != kind {
		return fmt.Errorf("%s scope/kind does not match generated handoff", name)
	}
	if err := validateOpaqueIdentity(name+" id", ref.ID); err != nil {
		return err
	}
	rel := strings.ReplaceAll(filepath.ToSlash(strings.TrimSpace(ref.RelPath)), `\`, "/")
	if rel != "" && (isAnyAbsolutePath(rel) || rel == ".." || strings.HasPrefix(rel, "../")) {
		return fmt.Errorf("%s rel_path must be Worktrail-root-relative", name)
	}
	return nil
}

func generatedSafetyText(meta model.HandoffMetaV2, body string) string {
	values := []string{
		meta.Title,
		meta.Summary,
		body,
		meta.SourceTool,
		meta.Actor,
		meta.Worktree.Branch,
		normalizeTimestampedLegacyPathForSafety(meta.MigratedFrom),
	}
	values = append(values, meta.Tags...)
	for _, ref := range []*model.Ref{meta.SourceState, meta.PreviousHandoff, meta.PublishedFrom} {
		if ref != nil {
			values = append(values, ref.RelPath)
		}
	}
	return strings.Join(values, "\n")
}

func normalizeTimestampedLegacyPathForSafety(value string) string {
	value = filepath.ToSlash(strings.TrimSpace(value))
	slash := strings.LastIndex(value, "/")
	dir, base := value[:slash+1], value[slash+1:]
	match := legacyTimestampedFilenamePattern.FindStringSubmatch(base)
	if len(match) != 4 {
		return value
	}
	if _, err := time.Parse("20060102-150405", match[1]+"-"+match[2]); err != nil {
		return value
	}
	return dir + "[LEGACY_TIMESTAMP]-" + match[3]
}

func validateOpaqueIdentity(name, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", name)
	}
	if !opaqueIdentifierPattern.MatchString(value) {
		return fmt.Errorf("%s must match %s", name, opaqueIdentifierPattern.String())
	}
	return nil
}

func isAnyAbsolutePath(path string) bool {
	path = strings.TrimSpace(path)
	return filepath.IsAbs(filepath.FromSlash(path)) ||
		len(path) >= 3 && path[1] == ':' && (path[2] == '/' || path[2] == '\\')
}

func isTerminalCandidateStatus(status string) bool {
	switch status {
	case model.LifecycleDiscarded, model.LifecycleArchived, model.LifecycleRetired,
		model.LifecyclePromoted, model.LifecycleMerged:
		return true
	default:
		return false
	}
}

func validMigratedLifecycle(status string) bool {
	switch status {
	case model.LifecycleCurrent, model.LifecycleDiscarded, model.LifecycleArchived,
		model.LifecycleRetired, model.LifecyclePromoted, model.LifecycleMerged,
		model.LifecycleClosed, model.LifecycleSuperseded, model.LifecyclePublished:
		return true
	default:
		return false
	}
}

func isMigratableStatus(status string) bool {
	switch status {
	case "planned", "noop", "coalesced", "migrated":
		return true
	default:
		return false
	}
}

func migratableItems(items []Item) []Item {
	result := make([]Item, 0, len(items))
	for _, item := range items {
		if isMigratableStatus(item.Status) {
			result = append(result, item)
		}
	}
	return result
}

func verifyMigrationIntent(intent ops.Intent, items []Item, cleanup bool) error {
	type expectedWrite struct {
		before string
		after  string
	}
	expectedWrites := map[string]expectedWrite{}
	expectedDeletes := map[string]string{}
	for _, item := range items {
		if !isMigratableStatus(item.Status) {
			continue
		}
		expected := expectedWrites[item.TargetPath]
		expected.after = strings.TrimPrefix(item.TargetHash, "sha256:")
		if cleanup || item.Status == "noop" {
			expected.before = expected.after
		} else if expected.before == "" {
			expected.before = ops.AbsentHash
		}
		expectedWrites[item.TargetPath] = expected
		if cleanup {
			expectedDeletes[item.SourcePath] = strings.TrimPrefix(item.SourceHash, "sha256:")
		}
	}
	seenWrites := map[string]bool{}
	seenDeletes := map[string]bool{}
	for _, action := range intent.Actions {
		switch action.Kind {
		case "write":
			expected, ok := expectedWrites[action.Path]
			if !ok || action.BeforeHash != expected.before || action.AfterHash != expected.after {
				return fmt.Errorf("migration intent contains unexpected or mismatched write %q", action.Path)
			}
			seenWrites[action.Path] = true
		case "delete":
			expected, ok := expectedDeletes[action.Path]
			if !ok {
				return fmt.Errorf("migration intent contains unexpected delete %q", action.Path)
			}
			if action.BeforeHash != expected {
				return fmt.Errorf("legacy source changed before cleanup: %s", action.Path)
			}
			seenDeletes[action.Path] = true
		default:
			return fmt.Errorf("migration intent contains unsupported action %q", action.Kind)
		}
	}
	if len(seenWrites) != len(expectedWrites) || len(seenDeletes) != len(expectedDeletes) {
		return errors.New("migration intent does not cover the complete verified source and target set")
	}
	return nil
}

func createBackup(backupDir string, report Report, now time.Time) (string, error) {
	if _, err := os.Stat(backupDir); err == nil {
		manifestPath := filepath.Join(backupDir, "manifest.json")
		if err := validateExistingBackup(manifestPath, backupDir, report); err != nil {
			return "", fmt.Errorf("backup directory conflict %s: %w", backupDir, err)
		}
		return manifestPath, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(backupDir, "files"), 0o700); err != nil {
		return "", err
	}
	for _, item := range migratableItems(report.Items) {
		target := filepath.Join(backupDir, "files", filepath.FromSlash(item.SourcePath))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return "", err
		}
		if err := os.WriteFile(target, item.sourceData, 0o600); err != nil {
			return "", err
		}
		if hash, err := hashFile(target); err != nil || hash != item.SourceHash {
			if err == nil {
				err = errors.New("backup hash mismatch")
			}
			return "", fmt.Errorf("verify backup %s: %w", item.SourcePath, err)
		}
	}
	manifestPath := filepath.Join(backupDir, "manifest.json")
	if err := writeJSONAtomic(manifestPath, manifestFor(report, now), 0o600); err != nil {
		return "", err
	}
	return manifestPath, nil
}

func validateExistingBackup(manifestPath, backupDir string, report Report) error {
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var existing manifest
	if err := json.Unmarshal(data, &existing); err != nil {
		return err
	}
	items := migratableItems(report.Items)
	if existing.Schema != manifestSchema || existing.Root != report.Root || existing.InventoryHash != report.InventoryHash ||
		existing.FileCount != len(items) || len(existing.Files) != len(items) {
		return errors.New("existing manifest does not match this migration inventory")
	}
	bySource := map[string]manifestFile{}
	for _, file := range existing.Files {
		bySource[file.SourcePath] = file
	}
	for _, item := range items {
		file, ok := bySource[item.SourcePath]
		expectedBackupPath := filepath.ToSlash(filepath.Join("files", item.SourcePath))
		if !ok || file.SourceHash != item.SourceHash || file.SourceKind != item.SourceKind ||
			file.BackupPath != expectedBackupPath || file.Mode != uint32(item.sourceMode.Perm()) {
			return fmt.Errorf("existing manifest does not match source %s", item.SourcePath)
		}
		backupPath := filepath.Join(backupDir, filepath.FromSlash(file.BackupPath))
		info, err := os.Lstat(backupPath)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("backup file %s is not a regular file", file.BackupPath)
		}
		actualHash, err := hashFile(backupPath)
		if err != nil || actualHash != item.SourceHash {
			if err == nil {
				err = errors.New("backup hash mismatch")
			}
			return fmt.Errorf("verify existing backup %s: %w", file.BackupPath, err)
		}
	}
	return nil
}

func manifestFor(report Report, now time.Time) manifest {
	items := migratableItems(report.Items)
	result := manifest{
		Schema: manifestSchema, CreatedAt: now.Format(time.RFC3339Nano), Root: report.Root,
		InventoryHash: report.InventoryHash, FileCount: len(items),
		Files: make([]manifestFile, 0, len(items)),
	}
	for _, item := range items {
		result.Files = append(result.Files, manifestFile{
			SourceKind: item.SourceKind, SourcePath: item.SourcePath, SourceHash: item.SourceHash,
			BackupPath: filepath.ToSlash(filepath.Join("files", item.SourcePath)), Mode: uint32(item.sourceMode.Perm()),
		})
	}
	return result
}

func resolveBackupDir(root, requested string, now time.Time) (string, error) {
	backupDir := strings.TrimSpace(requested)
	if backupDir == "" {
		backupDir = filepath.Join(filepath.Dir(root), ".worktrail-handoff-v2-backups", now.Format("20060102T150405.000000000Z"))
	}
	backupDir, err := filepath.Abs(backupDir)
	if err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	resolvedBackup, err := resolveProspectivePath(backupDir)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedBackup)
	if err != nil {
		return "", err
	}
	if rel == "." || rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("backup directory must be outside the migrated .worktrail root")
	}
	return backupDir, nil
}

func resolveProspectivePath(path string) (string, error) {
	path = filepath.Clean(path)
	current := path
	var suffix []string
	for {
		if _, err := os.Lstat(current); err == nil {
			resolved, err := filepath.EvalSymlinks(current)
			if err != nil {
				return "", err
			}
			for index := len(suffix) - 1; index >= 0; index-- {
				resolved = filepath.Join(resolved, suffix[index])
			}
			return resolved, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path, nil
		}
		suffix = append(suffix, filepath.Base(current))
		current = parent
	}
}

func readProjectID(root string) string {
	data, err := os.ReadFile(filepath.Join(root, "config.json"))
	if err != nil {
		return ""
	}
	var config struct {
		ProjectID string `json:"project_id"`
	}
	if json.Unmarshal(data, &config) != nil {
		return ""
	}
	return strings.TrimSpace(config.ProjectID)
}

func handoffContentHash(meta model.HandoffMetaV2, body string) (string, error) {
	raw, err := json.Marshal(meta)
	if err != nil {
		return "", err
	}
	canonical := map[string]any{}
	if err := json.Unmarshal(raw, &canonical); err != nil {
		return "", err
	}
	delete(canonical, "content_hash")
	canonicalJSON, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	canonicalBody := strings.TrimSpace(normalizeNewlines(body))
	sum := sha256.Sum256(append(append(canonicalJSON, '\n'), []byte(canonicalBody)...))
	return hex.EncodeToString(sum[:]), nil
}

func inventoryHash(items []Item) string {
	type entry struct {
		Path string `json:"path"`
		Hash string `json:"hash"`
	}
	entries := make([]entry, 0, len(items))
	for _, item := range items {
		entries = append(entries, entry{Path: item.SourcePath, Hash: item.SourceHash})
	}
	data, _ := json.Marshal(entries)
	return hashBytes(data)
}

func operationID(prefix, inventoryHash string) string {
	hash := strings.TrimPrefix(inventoryHash, "sha256:")
	if len(hash) > 20 {
		hash = hash[:20]
	}
	return prefix + "-" + hash
}

func stableLegacyTaskID(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "task_legacy_" + hex.EncodeToString(sum[:12])
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return hashBytes(data), nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func writeJSONAtomic(path string, value any, mode os.FileMode) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

func firstValue(meta map[string]any, keys ...string) any {
	for _, key := range keys {
		if value, ok := meta[key]; ok {
			return value
		}
	}
	return nil
}

func firstString(meta map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := stringValue(meta[key]); value != "" {
			return value
		}
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return strings.TrimSpace(text)
}

func stringList(value any) []string {
	var values []string
	switch typed := value.(type) {
	case []any:
		for _, item := range typed {
			if text := stringValue(item); text != "" {
				values = append(values, text)
			}
		}
	case []string:
		for _, item := range typed {
			if item = strings.TrimSpace(item); item != "" {
				values = append(values, item)
			}
		}
	}
	return values
}

func timeValue(value any, fallback time.Time) time.Time {
	text := stringValue(value)
	if text == "" {
		return fallback.UTC()
	}
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return fallback.UTC()
	}
	return parsed.UTC()
}

var (
	markdownHeadingPattern           = regexp.MustCompile(`^(#{1,6})[ \t]+(.+?)\s*$`)
	legacyAbsolutePathPattern        = regexp.MustCompile(`(?m)(^|[[:space:]\(\[\{=:])(/[[:alnum:]_.~@%+,\-]+(?:/[[:alnum:]_.~@%+,\-]+)+)`)
	legacyTimestampedFilenamePattern = regexp.MustCompile(`^(\d{8})-(\d{6})-(.+)$`)
	opaqueIdentifierPattern          = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,255}$`)
)

func sanitizeLegacyField(value string) (string, error) {
	value = legacyAbsolutePathPattern.ReplaceAllString(normalizeNewlines(value), `${1}[REDACTED_ABSOLUTE_PATH]`)
	result, err := textsafety.Process(value, textsafety.ProfileLocal)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(result.Text), nil
}

func sanitizeLegacyBody(value, sourcePath string) (string, []Diagnostic, error) {
	value = normalizeNewlines(value)
	pathSanitized := legacyAbsolutePathPattern.ReplaceAllString(value, `${1}[REDACTED_ABSOLUTE_PATH]`)
	result, err := textsafety.Process(pathSanitized, textsafety.ProfileLocal)
	if err != nil {
		return "", nil, err
	}
	body := result.Text
	var diagnostics []Diagnostic
	if result.Redacted || pathSanitized != value {
		diagnostics = append(diagnostics, Diagnostic{
			Code: "legacy_body_redacted", Path: sourcePath, Severity: "warning",
			Message: "legacy body contained local-only or sensitive material that was redacted during migration",
		})
	}
	for {
		next, removed := stripSnapshotAndTranscriptSections(body)
		body = next
		if !removed {
			break
		}
		alreadyReported := false
		for _, diagnostic := range diagnostics {
			if diagnostic.Code == "legacy_recursive_snapshot_removed" {
				alreadyReported = true
				break
			}
		}
		if !alreadyReported {
			diagnostics = append(diagnostics, Diagnostic{
				Code: "legacy_recursive_snapshot_removed", Path: sourcePath, Severity: "warning",
				Message: "recursive snapshot or raw transcript sections were removed from the migrated body",
			})
		}
	}
	if textsafety.ContainsTranscriptStyleConversation(body) {
		var kept []string
		for _, line := range strings.Split(body, "\n") {
			lower := strings.ToLower(strings.TrimSpace(line))
			if strings.HasPrefix(lower, "user:") || strings.HasPrefix(lower, "- user:") ||
				strings.HasPrefix(lower, "assistant:") || strings.HasPrefix(lower, "- assistant:") {
				continue
			}
			kept = append(kept, line)
		}
		body = strings.Join(kept, "\n")
		diagnostics = append(diagnostics, Diagnostic{
			Code: "legacy_transcript_turns_removed", Path: sourcePath, Severity: "warning",
			Message: "raw user/assistant transcript turns were removed from the migrated body",
		})
	}
	body = strings.TrimSpace(body)
	const renderedBodyTerminatorBytes = 1
	bodyLimit := handoff.LocalBodyMax - renderedBodyTerminatorBytes
	if len([]byte(body)) > bodyLimit {
		const suffix = "\n\n[legacy body truncated during migration]"
		limit := bodyLimit - len(suffix)
		raw := []byte(body)
		raw = raw[:limit]
		for len(raw) > 0 && !utf8.Valid(raw) {
			raw = raw[:len(raw)-1]
		}
		body = strings.TrimRight(string(raw), " \t\r\n") + suffix
		diagnostics = append(diagnostics, Diagnostic{
			Code: "legacy_body_truncated", Path: sourcePath, Severity: "warning",
			Message: fmt.Sprintf("legacy body was truncated to the %d-byte local handoff limit", handoff.LocalBodyMax),
		})
	}
	return body, diagnostics, nil
}

func stripSnapshotAndTranscriptSections(body string) (string, bool) {
	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	skipLevel := 0
	skipNestedStateCapsule := false
	removed := false
	for _, line := range lines {
		match := markdownHeadingPattern.FindStringSubmatch(strings.TrimSpace(line))
		if skipLevel > 0 {
			level, title := 0, ""
			if len(match) == 3 {
				level = len(match[1])
				title = strings.ToLower(strings.TrimSpace(match[2]))
			}
			if skipNestedStateCapsule {
				if level == skipLevel && title == "previous handoff" {
					skipLevel = 0
					skipNestedStateCapsule = false
				} else {
					removed = true
					continue
				}
			} else if level > 0 && level < skipLevel && strings.HasPrefix(title, "state capsule") {
				skipNestedStateCapsule = true
				removed = true
				continue
			} else if level > 0 && level <= skipLevel {
				skipLevel = 0
			} else {
				removed = true
				continue
			}
		}
		if len(match) == 3 {
			title := strings.ToLower(strings.TrimSpace(match[2]))
			if strings.Contains(title, "snapshot") || strings.Contains(title, "raw transcript") || strings.Contains(title, "full transcript") {
				skipLevel = len(match[1])
				removed = true
				continue
			}
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n"), removed
}

func inferTitle(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			if title := strings.TrimSpace(strings.TrimLeft(line, "#")); title != "" {
				return title
			}
		}
	}
	return fallback
}

func firstSummary(body, fallback string) string {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "---") {
			return line
		}
	}
	return fallback
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}

func withDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
