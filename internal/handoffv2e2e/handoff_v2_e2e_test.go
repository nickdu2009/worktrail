package handoffv2e2e_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/app"
	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/recovery"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/store"
)

func TestLocalCreateAndTeamPublishRespectGitBoundary(t *testing.T) {
	env := newGitProject(t)
	configureAppEnv(t, env)
	local := createLocal(t, env, "task-clean", "clean handoff")

	if status := gitOutput(t, env.ProjectRoot, "status", "--porcelain", "--untracked-files=all"); status != "" {
		t.Fatalf("local create must remain ignored, status=%q", status)
	}
	assertGitSuccess(t, env.ProjectRoot, "check-ignore", "--quiet", filepath.ToSlash(filepath.Join(".worktrail", local.RelPath)))

	indexBefore := gitOutput(t, env.ProjectRoot, "write-tree")
	var team handoff.Record
	if out := runApp(t, []string{"handoff", "publish", local.Meta.ID, "--format", "json"}); json.Unmarshal(out, &team) != nil {
		t.Fatalf("decode CLI-published team handoff: %s", out)
	}
	if team.Meta.Visibility != model.VisibilityTeam || team.Meta.Worktree.CodeAvailability != model.CodeAvailabilityAvailable {
		t.Fatalf("published team metadata = %+v", team.Meta)
	}
	if indexAfter := gitOutput(t, env.ProjectRoot, "write-tree"); indexAfter != indexBefore {
		t.Fatalf("publish changed Git index tree: before=%s after=%s", indexBefore, indexAfter)
	}
	teamGitPath := filepath.ToSlash(filepath.Join(".worktrail", team.RelPath))
	status := gitOutput(t, env.ProjectRoot, "status", "--porcelain", "--untracked-files=all")
	if !strings.Contains(status, "?? "+teamGitPath) || strings.Contains(status, local.RelPath) {
		t.Fatalf("team handoff must be trackable while local remains ignored, status=%q", status)
	}

	assertGitSuccess(t, env.ProjectRoot, "add", teamGitPath)
	assertGitSuccess(t, env.ProjectRoot, "commit", "-m", "track team handoff")
	dirtyLocal := createLocal(t, env, "task-dirty", "dirty handoff")
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runAppError([]string{"handoff", "publish", dirtyLocal.Meta.ID}); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty publish error = %v, want default rejection", err)
	}
}

func TestCheckpointCloseResumeRoundsStayBoundedTaskScopedAndRelocatable(t *testing.T) {
	env := newProject(t)
	configureAppEnv(t, env)

	active, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-a",
		Title:  "Shared title",
		Body:   "# State Capsule: Shared title\n\n" + strings.Repeat("initial context ", 80),
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := wtstate.Checkpoint(env, wtstate.CheckpointOptions{
		Scope: "project",
		ID:    active.State.ID,
		Note:  "before handoff",
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.State.ID == active.State.ID || checkpoint.Checkpoint != checkpoint.State.ID {
		t.Fatalf("checkpoint must have an independent id: active=%s checkpoint=%+v", active.State.ID, checkpoint)
	}

	seenHandoffs := map[string]bool{}
	minBody, maxBody := handoff.LocalBodyMax, 0
	oldRoot := ""
	for round := 0; round < 10; round++ {
		runApp(t, []string{
			"state", "close", "--id", active.State.ID, "--to", "handoff",
			"--next-step", "continue task A", fmt.Sprintf("round %d complete", round),
		})
		current, err := handoff.Latest(env, "project")
		if err != nil {
			t.Fatal(err)
		}
		if seenHandoffs[current.Meta.ID] {
			t.Fatalf("handoff id repeated across rounds: %s", current.Meta.ID)
		}
		seenHandoffs[current.Meta.ID] = true
		if current.Meta.SourceState == nil || current.Meta.SourceState.ID != active.State.ID {
			t.Fatalf("handoff source state = %+v, want %s", current.Meta.SourceState, active.State.ID)
		}
		bodySize := len([]byte(current.Body))
		if bodySize > handoff.LocalBodyMax {
			t.Fatalf("round %d handoff body = %d bytes, limit=%d", round, bodySize, handoff.LocalBodyMax)
		}
		if bodySize < minBody {
			minBody = bodySize
		}
		if bodySize > maxBody {
			maxBody = bodySize
		}

		if round == 2 {
			oldRoot = env.ProjectRoot
			movedRoot := filepath.Join(filepath.Dir(oldRoot), "relocated-project")
			if err := os.Rename(oldRoot, movedRoot); err != nil {
				t.Fatal(err)
			}
			env.ProjectRoot = movedRoot
			env.ProjectWT = filepath.Join(movedRoot, ".worktrail")
			t.Setenv("WORKTRAIL_PROJECT_ROOT", movedRoot)
		}

		resumeArgs := []string{"resume", "--task-id", "task-a", "--format", "json"}
		expectedSource := recovery.SourceLocalHandoff
		expectedSourceID := current.Meta.ID
		if round == 0 {
			resumeArgs = []string{"resume", "--ref", "checkpoint:" + checkpoint.State.ID, "--format", "json"}
			expectedSource = recovery.SourceExplicitCheckpoint
			expectedSourceID = checkpoint.State.ID
		}
		out := runApp(t, resumeArgs)
		var resumed resumeOutput
		if err := json.Unmarshal(out, &resumed); err != nil {
			t.Fatalf("decode resume output %q: %v", out, err)
		}
		if resumed.TaskID != "task-a" || resumed.RecoverySource != expectedSource || resumed.Source.ID != expectedSourceID {
			t.Fatalf("round %d resume = %+v", round, resumed)
		}
		if filepath.IsAbs(filepath.FromSlash(resumed.Source.RelPath)) || filepath.IsAbs(filepath.FromSlash(resumed.State.RelPath)) {
			t.Fatalf("round %d resume refs are absolute: %+v", round, resumed)
		}
		if oldRoot != "" && bytes.Contains(out, []byte(oldRoot)) {
			t.Fatalf("relocated resume leaked old root %q: %s", oldRoot, out)
		}
		active, err = wtstate.Show(env, wtstate.ShowOptions{
			Scope:     "project",
			ID:        resumed.State.ID,
			Directory: wtstate.DirActive,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len([]byte(active.Body)) > handoff.LocalBodyMax {
			t.Fatalf("round %d resumed state body grew beyond handoff bound: %d", round, len([]byte(active.Body)))
		}
	}
	if growth := maxBody - minBody; growth > 512 {
		t.Fatalf("handoff bodies grew across resume rounds: min=%d max=%d", minBody, maxBody)
	}
	if _, err := wtstate.Show(env, wtstate.ShowOptions{
		Scope: "project", ID: checkpoint.State.ID, Directory: wtstate.DirCheckpoints,
	}); err != nil {
		t.Fatalf("independent checkpoint disappeared after close/resume rounds: %v", err)
	}

	beta, err := wtstate.Start(env, wtstate.StartOptions{
		Scope:  "project",
		TaskID: "task-b",
		Title:  "Shared title",
		Body:   "beta-only-marker",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = recovery.Select(env, "project")
	var ambiguity *recovery.AmbiguousTaskError
	if !errors.As(err, &ambiguity) || len(ambiguity.Candidates) != 2 {
		t.Fatalf("task omission error = %v, ambiguity=%+v", err, ambiguity)
	}

	brokenPath := filepath.Join(env.ProjectWT, "handoffs", "local", "broken.md")
	if err := os.WriteFile(brokenPath, []byte("---worktrail\n{broken\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	listed, err := handoff.ListWithDiagnostics(env, handoff.ListOptions{
		Scope: "project", Visibility: model.VisibilityLocal,
	})
	if err != nil || len(listed.Diagnostics) != 1 {
		t.Fatalf("fail-soft list = %+v, err=%v", listed, err)
	}
	selected, err := recovery.NewTaskScopedResolver(env).Resolve("project", recovery.TaskSelector{TaskID: "task-a"})
	if err != nil {
		t.Fatalf("resolve task A with malformed neighbor: %v", err)
	}
	if selected.TaskID != "task-a" || selected.Handoff == nil || strings.Contains(selected.Handoff.Body, "beta-only-marker") {
		t.Fatalf("task A selection crossed task boundary: %+v", selected)
	}
	for _, ref := range selected.SupportingRefs {
		if ref.ID == beta.State.ID {
			t.Fatalf("task B ref leaked into task A selection: %+v", selected.SupportingRefs)
		}
		if filepath.IsAbs(filepath.FromSlash(ref.RelPath)) || oldRoot != "" && strings.Contains(ref.RelPath, oldRoot) {
			t.Fatalf("non-relocatable supporting ref: %+v", ref)
		}
	}
}

func TestOperationalCLIMigrationPruneDoctorAndRepair(t *testing.T) {
	env := newGitProject(t)
	configureAppEnv(t, env)

	legacyPath := filepath.Join(env.ProjectWT, "handoffs", "legacy-e2e.md")
	if err := os.WriteFile(legacyPath, []byte("# Legacy E2E\n\nMigration body.\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var migrationPlan struct {
		DryRun  bool `json:"dry_run"`
		Summary struct {
			Planned int `json:"planned"`
		} `json:"summary"`
	}
	out := runApp(t, []string{"migrate", "handoff-v2", "--format", "json"})
	if err := json.Unmarshal(out, &migrationPlan); err != nil || !migrationPlan.DryRun || migrationPlan.Summary.Planned != 1 {
		t.Fatalf("migration plan = %+v err=%v output=%s", migrationPlan, err, out)
	}
	backup := filepath.Join(filepath.Dir(env.ProjectWT), "migration-backup")
	pendingMarker := "runtime/migrations/e2e-explicit-repair.marker"
	coordinator := ops.New(env.ProjectWT)
	coordinator.Failpoint = func(phase string, _ int, _ ops.Action) error {
		if phase == "commit" {
			return errors.New("leave migration prerequisite pending")
		}
		return nil
	}
	operation, err := coordinator.Begin(ops.Spec{
		ID: "handoff-v2-migration-e2e-pending",
		Writes: []ops.Write{{
			Path: pendingMarker,
			Data: []byte("replayed by explicit doctor repair"),
			Mode: 0o600,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("pending migration prerequisite unexpectedly committed")
	}
	if err := runAppError([]string{"migrate", "handoff-v2", "--apply", "--confirm", "--backup-dir", backup}); !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("migration with pending operation error = %v", err)
	}
	statusOutput := runApp(t, []string{"doctor", "ops", "status", "--format", "json"})
	if !bytes.Contains(statusOutput, []byte("handoff-v2-migration-e2e-pending")) {
		t.Fatalf("ops status omitted pending migration prerequisite: %s", statusOutput)
	}
	runApp(t, []string{"doctor", "ops", "repair", "--confirm", "--format", "json"})
	if data, err := os.ReadFile(filepath.Join(env.ProjectWT, filepath.FromSlash(pendingMarker))); err != nil ||
		string(data) != "replayed by explicit doctor repair" {
		t.Fatalf("explicit ops repair marker: data=%q err=%v", data, err)
	}
	runApp(t, []string{"migrate", "handoff-v2", "--apply", "--confirm", "--backup-dir", backup, "--format", "json"})
	if _, err := os.Stat(legacyPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy handoff remained after CLI migration: %v", err)
	}
	if _, err := handoff.Read(filepath.Join(env.ProjectWT, "handoffs", "local", "legacy-e2e.md")); err != nil {
		t.Fatalf("CLI migration target: %v", err)
	}

	expiredRuntime := writeExpiredRuntime(t, env.ProjectWT)
	var prunePlan struct {
		Applied bool `json:"applied"`
		Plan    struct {
			Items []json.RawMessage `json:"items"`
		} `json:"plan"`
	}
	out = runApp(t, []string{"runtime", "prune", "--format", "json"})
	if err := json.Unmarshal(out, &prunePlan); err != nil || prunePlan.Applied || len(prunePlan.Plan.Items) != 1 {
		t.Fatalf("runtime prune plan = %+v err=%v output=%s", prunePlan, err, out)
	}
	runApp(t, []string{"runtime", "prune", "--apply", "--confirm", "--format", "json"})
	if _, err := os.Stat(expiredRuntime); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("runtime prune did not delete expired record: %v", err)
	}

	var opsStatus struct {
		OK     bool `json:"ok"`
		Repair bool `json:"repair"`
	}
	out = runApp(t, []string{"doctor", "ops", "status", "--format", "json"})
	if err := json.Unmarshal(out, &opsStatus); err != nil || !opsStatus.OK || opsStatus.Repair {
		t.Fatalf("ops doctor status = %+v err=%v output=%s", opsStatus, err, out)
	}

	malformedState := filepath.Join(env.ProjectWT, "state", "active", "broken-e2e.md")
	malformedRuntime := filepath.Join(env.ProjectWT, "runtime", "sessions", "broken-e2e.md")
	for _, path := range []string{malformedState, malformedRuntime} {
		if err := os.WriteFile(path, []byte("---worktrail\n{\"id\":\n---\n# Broken E2E\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var recoveryPlan struct {
		OK       bool     `json:"ok"`
		Apply    bool     `json:"apply"`
		Coverage []string `json:"coverage"`
		State    struct {
			Applied     bool              `json:"applied"`
			Diagnostics []json.RawMessage `json:"diagnostics"`
			Actions     []string          `json:"actions"`
		} `json:"state"`
		RuntimePlan struct {
			Items []json.RawMessage `json:"items"`
		} `json:"runtime_plan"`
		RuntimeResult struct {
			Quarantined int `json:"quarantined"`
		} `json:"runtime_result"`
	}
	out = runApp(t, []string{"doctor", "recovery", "--format", "json"})
	if err := json.Unmarshal(out, &recoveryPlan); err != nil || recoveryPlan.OK || recoveryPlan.Apply ||
		len(recoveryPlan.Coverage) != 2 || len(recoveryPlan.State.Diagnostics) != 1 ||
		len(recoveryPlan.State.Actions) != 1 || len(recoveryPlan.RuntimePlan.Items) != 1 {
		t.Fatalf("recovery quarantine plan = %+v err=%v output=%s", recoveryPlan, err, out)
	}
	if err := runAppError([]string{"doctor", "recovery", "--apply"}); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("recovery apply without confirm error = %v", err)
	}
	if err := runAppError([]string{"doctor", "recovery", "--repair", "--confirm"}); err == nil || !strings.Contains(err.Error(), "unknown doctor recovery flag --repair") {
		t.Fatalf("obsolete recovery repair flag error = %v", err)
	}
	out = runApp(t, []string{"doctor", "recovery", "--apply", "--confirm", "--format", "json"})
	if err := json.Unmarshal(out, &recoveryPlan); err != nil || !recoveryPlan.OK || !recoveryPlan.Apply ||
		!recoveryPlan.State.Applied || recoveryPlan.RuntimeResult.Quarantined != 1 {
		t.Fatalf("applied recovery quarantine = %+v err=%v output=%s", recoveryPlan, err, out)
	}
	for _, path := range []string{malformedState, malformedRuntime} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("malformed recovery source remains at %s: %v", path, err)
		}
	}
	for _, pattern := range []string{
		filepath.Join(env.ProjectWT, "runtime", "quarantine", "state", "*-broken-e2e.md"),
		filepath.Join(env.ProjectWT, "runtime", "quarantine", "sessions", "broken-e2e.md"),
	} {
		matches, err := filepath.Glob(pattern)
		if err != nil || len(matches) != 1 {
			t.Fatalf("recovery quarantine pattern %s = %v, err=%v", pattern, matches, err)
		}
	}

	repairable := createLocal(t, env, "task-repair", "repair local mode")
	if err := os.Chmod(repairable.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	var repairPlan struct {
		Applied bool     `json:"applied"`
		Actions []string `json:"actions"`
	}
	out = runApp(t, []string{"handoff", "repair", "--format", "json"})
	if err := json.Unmarshal(out, &repairPlan); err != nil || repairPlan.Applied || len(repairPlan.Actions) == 0 {
		t.Fatalf("handoff repair plan = %+v err=%v output=%s", repairPlan, err, out)
	}
	runApp(t, []string{"handoff", "repair", "--apply", "--confirm", "--format", "json"})
	info, err := os.Stat(repairable.Path)
	if err != nil {
		t.Fatalf("stat repaired handoff: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("handoff repair mode = %v", info.Mode().Perm())
	}
}

type resumeOutput struct {
	TaskID         string    `json:"task_id"`
	State          model.Ref `json:"state"`
	Source         model.Ref `json:"source"`
	RecoverySource string    `json:"recovery_source_kind"`
}

func newProject(t *testing.T) paths.Env {
	t.Helper()
	base := t.TempDir()
	project := filepath.Join(base, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	env := paths.Env{
		Home:        filepath.Join(base, "home"),
		UserRoot:    filepath.Join(base, "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
	if err := store.InitProject(env); err != nil {
		t.Fatalf("init project: %v", err)
	}
	return env
}

func newGitProject(t *testing.T) paths.Env {
	t.Helper()
	env := newProject(t)
	assertGitSuccess(t, env.ProjectRoot, "init")
	assertGitSuccess(t, env.ProjectRoot, "config", "user.email", "test@example.com")
	assertGitSuccess(t, env.ProjectRoot, "config", "user.name", "Worktrail Test")
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, "README.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assertGitSuccess(t, env.ProjectRoot, "add", "-A")
	assertGitSuccess(t, env.ProjectRoot, "commit", "-m", "fixture")
	return env
}

func configureAppEnv(t *testing.T, env paths.Env) {
	t.Helper()
	t.Setenv("HOME", env.Home)
	t.Setenv("WORKTRAIL_HOME", env.UserRoot)
	t.Setenv("WORKTRAIL_PROJECT_ROOT", env.ProjectRoot)
}

func createLocal(t *testing.T, env paths.Env, taskID, summary string) handoff.Record {
	t.Helper()
	record, err := handoff.CreateLocal(context.Background(), env, handoff.CreateRequest{
		Scope:     "project",
		TaskID:    taskID,
		Title:     taskID,
		Summary:   summary,
		NextSteps: []model.NextStep{{Action: "continue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func runApp(t *testing.T, args []string) []byte {
	t.Helper()
	var out, errOut bytes.Buffer
	if err := app.Run(context.Background(), args, strings.NewReader(""), &out, &errOut); err != nil {
		t.Fatalf("worktrail %s: %v\nstderr: %s", strings.Join(args, " "), err, errOut.String())
	}
	return out.Bytes()
}

func runAppError(args []string) error {
	var out, errOut bytes.Buffer
	return app.Run(context.Background(), args, strings.NewReader(""), &out, &errOut)
}

func writeExpiredRuntime(t *testing.T, root string) string {
	t.Helper()
	now := time.Now().UTC()
	data, err := store.RenderMarkdown(map[string]any{
		"schema":           model.SchemaRuntimeV2,
		"id":               "runtime-e2e-expired",
		"object_kind":      model.ObjectKindRuntime,
		"scope":            "project",
		"runtime_type":     model.RuntimeTypeSessionState,
		"title":            "Expired E2E runtime",
		"durability":       model.DurabilityEphemeral,
		"lifecycle_status": model.LifecycleActive,
		"project_id":       "project_test",
		"task_id":          "task-runtime",
		"created_at":       now.Add(-48 * time.Hour),
		"updated_at":       now.Add(-48 * time.Hour),
		"expires_at":       now.Add(-time.Hour),
	}, "expired runtime")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "runtime", "sessions", "expired-e2e.md")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func assertGitSuccess(t *testing.T, dir string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}
