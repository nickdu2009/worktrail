package handoff

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

type DoctorRequest struct {
	Scope string `json:"scope,omitempty"`
}

type DoctorReport struct {
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type RepairRequest struct {
	Scope   string `json:"scope,omitempty"`
	Apply   bool   `json:"apply,omitempty"`
	Confirm bool   `json:"confirm,omitempty"`
	Actor   string `json:"actor,omitempty"`
}

type RepairReport struct {
	Applied     bool         `json:"applied"`
	Diagnostics []Diagnostic `json:"diagnostics"`
	Actions     []string     `json:"actions,omitempty"`
}

func Doctor(env paths.Env, request DoctorRequest) (DoctorReport, error) {
	scope := withDefault(request.Scope, "project")
	result, err := listWithDiagnostics(env, ListOptions{Scope: scope}, true)
	if err != nil {
		return DoctorReport{}, err
	}
	diagnostics := append([]Diagnostic(nil), result.Diagnostics...)
	states, err := wtstate.ListWithDiagnostics(env, wtstate.ListOptions{Scope: scope, Directory: "all"})
	if err != nil {
		return DoctorReport{}, err
	}
	for _, diagnostic := range states.Diagnostics {
		diagnostics = append(diagnostics, Diagnostic{
			Code:       "invalid_state",
			Path:       diagnostic.Path,
			Message:    diagnostic.Message,
			Repairable: diagnostic.Repairable,
		})
	}
	localCurrent := map[string][]Record{}
	teamByTask := map[string][]Record{}
	for _, record := range result.Records {
		switch record.Meta.Visibility {
		case model.VisibilityLocal:
			if record.Meta.LifecycleStatus == model.LifecycleCurrent {
				localCurrent[record.Meta.TaskID] = append(localCurrent[record.Meta.TaskID], record)
			}
			if info, statErr := os.Stat(record.Path); statErr == nil && info.Mode().Perm() != 0o600 {
				diagnostics = append(diagnostics, Diagnostic{
					Code:       "local_file_mode",
					Path:       record.RelPath,
					ID:         record.Meta.ID,
					Message:    fmt.Sprintf("local handoff mode is %04o, want 0600", info.Mode().Perm()),
					Repairable: true,
				})
			}
		case model.VisibilityTeam:
			teamByTask[record.Meta.TaskID] = append(teamByTask[record.Meta.TaskID], record)
			if info, statErr := os.Stat(record.Path); statErr == nil && info.Mode().Perm() != 0o644 {
				diagnostics = append(diagnostics, Diagnostic{
					Code:    "team_file_mode",
					Path:    record.RelPath,
					ID:      record.Meta.ID,
					Message: fmt.Sprintf("team handoff mode is %04o, want 0644; team files are immutable", info.Mode().Perm()),
				})
			}
			if scope == "project" {
				gitPath := filepath.ToSlash(filepath.Join(".worktrail", filepath.FromSlash(record.RelPath)))
				if err := exec.Command("git", "-C", env.ProjectRoot, "ls-files", "--error-unmatch", "--", gitPath).Run(); err != nil {
					diagnostics = append(diagnostics, Diagnostic{
						Code:    "team_untracked",
						Path:    record.RelPath,
						ID:      record.Meta.ID,
						Message: "team handoff is not tracked by Git; doctor does not stage or commit it",
					})
				}
			}
		}
	}
	for taskID, records := range localCurrent {
		if len(records) > 1 {
			diagnostics = append(diagnostics, Diagnostic{
				Code:       "multiple_local_current",
				ID:         taskID,
				Message:    fmt.Sprintf("task %q has %d current local handoffs", taskID, len(records)),
				Repairable: true,
			})
		}
	}
	for taskID, records := range teamByTask {
		heads := teamHeads(records)
		if len(heads) > 1 {
			ids := make([]string, 0, len(heads))
			for _, head := range heads {
				ids = append(ids, head.Meta.ID)
			}
			diagnostics = append(diagnostics, Diagnostic{
				Code:    "multiple_team_heads",
				ID:      taskID,
				Message: fmt.Sprintf("task %q has %d immutable team heads: %s", taskID, len(heads), strings.Join(ids, ", ")),
			})
		}
	}
	sort.Slice(diagnostics, func(left, right int) bool {
		if diagnostics[left].Code != diagnostics[right].Code {
			return diagnostics[left].Code < diagnostics[right].Code
		}
		if diagnostics[left].Path != diagnostics[right].Path {
			return diagnostics[left].Path < diagnostics[right].Path
		}
		return diagnostics[left].ID < diagnostics[right].ID
	})
	return DoctorReport{Diagnostics: diagnostics}, nil
}

func Repair(env paths.Env, request RepairRequest) (RepairReport, error) {
	mutationMu.Lock()
	defer mutationMu.Unlock()

	if request.Apply && !request.Confirm {
		return RepairReport{}, errors.New("handoff repair --apply requires --confirm")
	}
	root, scope, err := scopeRoot(env, request.Scope)
	if err != nil {
		return RepairReport{}, err
	}
	doctor, err := Doctor(env, DoctorRequest{Scope: scope})
	if err != nil {
		return RepairReport{}, err
	}
	report := RepairReport{Diagnostics: doctor.Diagnostics}
	for _, diagnostic := range doctor.Diagnostics {
		if diagnostic.Repairable {
			report.Actions = append(report.Actions, repairAction(diagnostic))
		}
	}
	if !request.Apply || len(report.Actions) == 0 {
		return report, nil
	}

	stateQuarantined := false
	for _, diagnostic := range doctor.Diagnostics {
		if diagnostic.Code == "invalid_state" && diagnostic.Repairable {
			stateReport, quarantineErr := wtstate.QuarantineMalformed(env, wtstate.QuarantineRequest{
				Scope: scope, Apply: true, Confirm: true, Actor: withDefault(request.Actor, "handoff-repair"),
			})
			if quarantineErr != nil {
				return RepairReport{}, quarantineErr
			}
			stateQuarantined = stateReport.Applied
			break
		}
	}
	quarantined := 0
	for _, diagnostic := range doctor.Diagnostics {
		if diagnostic.Code != "invalid_handoff" || !diagnostic.Repairable {
			continue
		}
		if err := quarantineMalformedLocal(root, diagnostic, withDefault(request.Actor, "handoff-repair")); err != nil {
			return RepairReport{}, err
		}
		quarantined++
	}
	listed, err := listWithDiagnostics(env, ListOptions{Scope: scope, Visibility: model.VisibilityLocal}, true)
	if err != nil {
		return RepairReport{}, err
	}
	now := time.Now().UTC()
	var writes []ops.Write
	var modePaths []string
	writePaths := map[string]bool{}
	byTask := map[string][]Record{}
	for _, record := range listed.Records {
		if record.Meta.LifecycleStatus == model.LifecycleCurrent {
			byTask[record.Meta.TaskID] = append(byTask[record.Meta.TaskID], record)
		}
	}
	for _, records := range byTask {
		if len(records) < 2 {
			continue
		}
		sort.Slice(records, func(left, right int) bool {
			if !records[left].Meta.UpdatedAt.Equal(records[right].Meta.UpdatedAt) {
				return records[left].Meta.UpdatedAt.After(records[right].Meta.UpdatedAt)
			}
			return records[left].Meta.ID < records[right].Meta.ID
		})
		for _, record := range records[1:] {
			record.Meta.LifecycleStatus = model.LifecycleSuperseded
			record.Meta.Status = model.LifecycleSuperseded
			record.Meta.UpdatedAt = now
			if err := setContentHash(&record.Meta, record.Body); err != nil {
				return RepairReport{}, err
			}
			data, err := renderRecord(record.Meta, record.Body)
			if err != nil {
				return RepairReport{}, err
			}
			writes = append(writes, ops.Write{Path: record.RelPath, Data: data, Mode: 0o600})
			writePaths[record.RelPath] = true
		}
	}
	for _, diagnostic := range doctor.Diagnostics {
		if !diagnostic.Repairable {
			continue
		}
		switch diagnostic.Code {
		case "local_file_mode":
			modePaths = append(modePaths, diagnostic.Path)
		case "content_hash_mismatch":
			if writePaths[diagnostic.Path] {
				continue
			}
			record, readErr := readRecord(filepath.Join(root, filepath.FromSlash(diagnostic.Path)), false)
			if readErr != nil {
				continue
			}
			if err := setContentHash(&record.Meta, record.Body); err != nil {
				return RepairReport{}, err
			}
			data, err := renderRecord(record.Meta, record.Body)
			if err != nil {
				return RepairReport{}, err
			}
			writes = append(writes, ops.Write{Path: diagnostic.Path, Data: data, Mode: 0o600})
			writePaths[diagnostic.Path] = true
		}
	}
	if len(writes) == 0 && len(modePaths) == 0 {
		report.Applied = quarantined > 0 || stateQuarantined
		return report, nil
	}
	events := []Event{{
		Name:  "handoff.repair",
		Actor: withDefault(request.Actor, "handoff-repair"),
		Data:  map[string]any{"actions": len(report.Actions)},
		Time:  now,
	}}
	opID, err := newOpaqueID("op_handoff_repair")
	if err != nil {
		return RepairReport{}, err
	}
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(opID, func() (ops.Spec, error) {
		eventWrites, buildErr := appendEventWrite(root, append([]ops.Write(nil), writes...), events)
		if buildErr != nil {
			return ops.Spec{}, buildErr
		}
		return ops.Spec{Writes: eventWrites}, nil
	})
	if err != nil {
		return RepairReport{}, err
	}
	if err := operation.Commit(); err != nil {
		return RepairReport{}, fmt.Errorf("commit handoff repair operation %s: %w", opID, err)
	}
	for _, relPath := range modePaths {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(relPath)), 0o600); err != nil {
			return RepairReport{}, fmt.Errorf("repair local handoff mode %q: %w", relPath, err)
		}
	}
	report.Applied = true
	return report, nil
}

func quarantineMalformedLocal(root string, diagnostic Diagnostic, actor string) error {
	relPath := filepath.ToSlash(filepath.Clean(filepath.FromSlash(diagnostic.Path)))
	if !strings.HasPrefix(relPath, "handoffs/local/") || filepath.Ext(relPath) != ".md" {
		return fmt.Errorf("refuse to quarantine non-local handoff path %q", diagnostic.Path)
	}
	opID, err := newOpaqueID("op_handoff_quarantine")
	if err != nil {
		return err
	}
	quarantineRel := filepath.ToSlash(filepath.Join(
		"runtime", "quarantine", "handoff", opID+"-"+filepath.Base(relPath),
	))
	coordinator := newCoordinator(root)
	operation, err := coordinator.BeginBuild(opID, func() (ops.Spec, error) {
		source := filepath.Join(root, filepath.FromSlash(relPath))
		info, statErr := os.Lstat(source)
		if statErr != nil {
			return ops.Spec{}, statErr
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return ops.Spec{}, fmt.Errorf("refuse to quarantine unsafe handoff path %q", relPath)
		}
		if _, readErr := readRecord(source, true); readErr == nil {
			return ops.Spec{}, fmt.Errorf("refuse to quarantine valid handoff %q", relPath)
		}
		data, readErr := os.ReadFile(source)
		if readErr != nil {
			return ops.Spec{}, readErr
		}
		target := filepath.Join(root, filepath.FromSlash(quarantineRel))
		if _, statErr := os.Lstat(target); statErr == nil {
			return ops.Spec{}, fmt.Errorf("quarantine target already exists: %s", quarantineRel)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return ops.Spec{}, statErr
		}
		writes, writeErr := appendEventWrite(root, []ops.Write{{
			Path: quarantineRel,
			Data: data,
			Mode: 0o600,
		}}, []Event{{
			Name:  "handoff.quarantine",
			Actor: actor,
			Data:  map[string]any{"source": relPath, "quarantine": quarantineRel},
			Time:  time.Now().UTC(),
		}})
		if writeErr != nil {
			return ops.Spec{}, writeErr
		}
		return ops.Spec{Writes: writes, Deletes: []string{relPath}}, nil
	})
	if err != nil {
		return err
	}
	if err := operation.Commit(); err != nil {
		return fmt.Errorf("commit handoff quarantine operation %s: %w", opID, err)
	}
	return nil
}

func repairAction(diagnostic Diagnostic) string {
	switch diagnostic.Code {
	case "multiple_local_current":
		return "supersede all but the newest current local handoff for task " + diagnostic.ID
	case "local_file_mode":
		return "set local handoff mode to 0600: " + diagnostic.Path
	case "content_hash_mismatch":
		return "recompute local content hash: " + diagnostic.Path
	case "invalid_handoff":
		return "quarantine malformed local handoff: " + diagnostic.Path
	case "invalid_state":
		return "quarantine malformed state: " + diagnostic.Path
	default:
		return "inspect local handoff: " + diagnostic.Path
	}
}
