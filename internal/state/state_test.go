package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
)

func testEnv(t *testing.T) paths.Env {
	t.Helper()
	project := filepath.Join(t.TempDir(), "project")
	return paths.Env{
		Home:        filepath.Join(t.TempDir(), "home"),
		UserRoot:    filepath.Join(t.TempDir(), "home", ".worktrail"),
		ProjectRoot: project,
		ProjectWT:   filepath.Join(project, ".worktrail"),
	}
}

func TestLatestExplicitIgnoresHookSourceToolStates(t *testing.T) {
	env := testEnv(t)
	if _, err := Start(env, StartOptions{
		Scope:      "project",
		Title:      "Hook state",
		Body:       "hook",
		SourceTool: "cursor",
		Actor:      "hook:cursor-stop",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := Start(env, StartOptions{
		Scope: "project",
		Title: "Explicit state",
		Body:  "explicit",
		Actor: "cli:state-start",
	}); err != nil {
		t.Fatal(err)
	}
	cap, err := LatestExplicit(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if cap.State.Title != "Explicit state" {
		t.Fatalf("LatestExplicit = %q, want Explicit state", cap.State.Title)
	}
}

func TestLatestExplicitReturnsNotExistWhenOnlyHookStates(t *testing.T) {
	env := testEnv(t)
	if _, err := Start(env, StartOptions{
		Scope:      "project",
		Title:      "Hook only",
		Body:       "hook",
		SourceTool: "codex",
		Actor:      "hook:codex-stop",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := LatestExplicit(env, "project"); err != os.ErrNotExist {
		t.Fatalf("LatestExplicit err = %v, want ErrNotExist", err)
	}
}

func TestCheckpointHasIndependentIDAndIndexesWithoutCollision(t *testing.T) {
	env := testEnv(t)
	parent, err := Start(env, StartOptions{
		Scope: "project",
		Title: "Checkpoint identity",
		Body:  "state body",
	})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint, err := Checkpoint(env, CheckpointOptions{
		Scope: "project",
		ID:    parent.State.ID,
		Note:  "before risky work",
	})
	if err != nil {
		t.Fatal(err)
	}
	if checkpoint.State.ID == parent.State.ID {
		t.Fatalf("checkpoint id = parent id %q", parent.State.ID)
	}
	if !strings.HasPrefix(checkpoint.State.ID, "cp_") {
		t.Fatalf("checkpoint id = %q, want cp_ prefix", checkpoint.State.ID)
	}
	if checkpoint.Checkpoint != checkpoint.State.ID {
		t.Fatalf("checkpoint_id = %q, frontmatter id = %q", checkpoint.Checkpoint, checkpoint.State.ID)
	}
	if got := checkpoint.Metadata["checkpoint_of"]; got != parent.State.ID {
		t.Fatalf("checkpoint_of = %#v, want %q", got, parent.State.ID)
	}
	if base := strings.TrimSuffix(filepath.Base(checkpoint.Path), ".md"); base != checkpoint.State.ID {
		t.Fatalf("checkpoint filename id = %q, want %q", base, checkpoint.State.ID)
	}
	if _, err := index.Rebuild(env.ProjectWT, index.RebuildOptions{}); err != nil {
		t.Fatalf("index rebuild after checkpoint: %v", err)
	}
	if _, err := Close(env, CloseOptions{Scope: "project", ID: parent.State.ID}); err != nil {
		t.Fatal(err)
	}
	shown, err := Show(env, ShowOptions{
		Scope:     "project",
		ID:        checkpoint.State.ID,
		Directory: DirCheckpoints,
	})
	if err != nil {
		t.Fatal(err)
	}
	if shown.State.ID != checkpoint.State.ID || shown.Body != checkpoint.Body {
		t.Fatalf("shown checkpoint = %+v, want %+v", shown, checkpoint)
	}
}

func TestLatestReferenceIsRelativeWhileAliasRemainsCompatible(t *testing.T) {
	env := testEnv(t)
	started, err := Start(env, StartOptions{
		Scope: "project",
		Title: "Latest reference",
		Body:  "body",
	})
	if err != nil {
		t.Fatal(err)
	}
	ref, err := LatestReference(env, "project")
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != started.State.ID {
		t.Fatalf("reference id = %q, want %q", ref.ID, started.State.ID)
	}
	if filepath.IsAbs(ref.RelPath) || ref.RelPath != "state/active/"+started.State.ID+".md" {
		t.Fatalf("reference rel_path = %q", ref.RelPath)
	}
	latest, err := Show(env, ShowOptions{Scope: "project", ID: "latest", Directory: DirActive})
	if err != nil {
		t.Fatalf("legacy latest alias is not readable: %v", err)
	}
	if latest.State.ID != started.State.ID || latest.Body != started.Body {
		t.Fatalf("legacy latest alias = %+v, want active capsule", latest)
	}
}

func TestListSkipsMalformedStateAndReturnsDiagnostics(t *testing.T) {
	env := testEnv(t)
	valid, err := Start(env, StartOptions{Scope: "project", TaskID: "task-valid", Title: "Valid"})
	if err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(env.ProjectWT, "state", DirActive, "broken.md")
	if err := os.WriteFile(badPath, []byte("---worktrail\n{broken\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result, err := ListWithDiagnostics(env, ListOptions{Scope: "project", Directory: DirActive})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Capsules) != 1 || result.Capsules[0].State.ID != valid.State.ID {
		t.Fatalf("capsules = %+v", result.Capsules)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "state/active/broken.md" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
	latest, err := LatestActive(env, "project")
	if err != nil || latest.State.ID != valid.State.ID {
		t.Fatalf("LatestActive = %+v, err=%v", latest, err)
	}
}

func TestQuarantineMalformedStateIsConfirmedAndTransactional(t *testing.T) {
	env := testEnv(t)
	valid, err := Start(env, StartOptions{Scope: "project", TaskID: "task-valid", Title: "Valid"})
	if err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(env.ProjectWT, "state", DirCheckpoints, "broken.md")
	if err := os.MkdirAll(filepath.Dir(badPath), 0o700); err != nil {
		t.Fatal(err)
	}
	badData := []byte("---worktrail\n{broken\n---\n")
	if err := os.WriteFile(badPath, badData, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := QuarantineMalformed(env, QuarantineRequest{Scope: "project"})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Actions) != 1 || len(report.Diagnostics) != 1 {
		t.Fatalf("dry-run quarantine = %+v", report)
	}
	if _, err := os.Stat(badPath); err != nil {
		t.Fatalf("dry-run moved state: %v", err)
	}
	if _, err := QuarantineMalformed(env, QuarantineRequest{Scope: "project", Apply: true}); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("apply without confirmation error = %v", err)
	}
	report, err = QuarantineMalformed(env, QuarantineRequest{Scope: "project", Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("quarantine not applied: %+v", report)
	}
	if _, err := os.Lstat(badPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed state remains: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "quarantine", "state", "*-broken.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("quarantine matches = %v, err=%v", matches, err)
	}
	got, err := os.ReadFile(matches[0])
	if err != nil || string(got) != string(badData) {
		t.Fatalf("quarantine data = %q, err=%v", got, err)
	}
	if _, err := Show(env, ShowOptions{Scope: "project", ID: valid.State.ID, Directory: DirActive}); err != nil {
		t.Fatalf("valid state was affected: %v", err)
	}
}

func TestQuarantineDoesNotReplayPendingRepair(t *testing.T) {
	env := testEnv(t)
	badPath := filepath.Join(env.ProjectWT, "state", DirCheckpoints, "broken.md")
	if err := os.MkdirAll(filepath.Dir(badPath), 0o700); err != nil {
		t.Fatal(err)
	}
	badData := []byte("---worktrail\n{broken\n---\n")
	if err := os.WriteFile(badPath, badData, 0o600); err != nil {
		t.Fatal(err)
	}
	coordinator := ops.New(env.ProjectWT)
	operation, err := coordinator.Begin(ops.Spec{
		ID: "op_repair_pending",
		Writes: []ops.Write{{
			Path: "repair-result.txt", Data: []byte("repaired"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
		if phase == "commit" && index == 0 {
			return errors.New("leave repair pending")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("repair operation unexpectedly committed")
	}

	_, err = QuarantineMalformed(env, QuarantineRequest{Scope: "project", Apply: true, Confirm: true})
	if !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("QuarantineMalformed error = %v, want ErrPendingOperation", err)
	}
	got, err := os.ReadFile(badPath)
	if err != nil || string(got) != string(badData) {
		t.Fatalf("blocked quarantine changed malformed state: %q, err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "repair-result.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("quarantine replayed pending repair: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "quarantine", "state", "*-broken.md"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("blocked quarantine outputs = %v, err=%v", matches, err)
	}
}

func TestStateSymlinkDiagnosticIsNotRepairable(t *testing.T) {
	env := testEnv(t)
	dir := filepath.Join(env.ProjectWT, "state", DirArchived)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("---worktrail\n{broken\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "linked.md")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	report, err := QuarantineMalformed(env, QuarantineRequest{Scope: "project", Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Diagnostics) != 1 || report.Diagnostics[0].Repairable {
		t.Fatalf("symlink quarantine report = %+v", report)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("symlink was moved: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("symlink target was affected: %v", err)
	}
}

func TestCheckpointAndEventReplayAsOneOperation(t *testing.T) {
	env := testEnv(t)
	parent, err := Start(env, StartOptions{Scope: "project", TaskID: "task-checkpoint-op", Title: "Checkpoint op"})
	if err != nil {
		t.Fatal(err)
	}
	original := newCoordinator
	failed := false
	newCoordinator = func(root string) *ops.Coordinator {
		coordinator := ops.New(root)
		coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
			if phase == "commit" && index == 1 && !failed {
				failed = true
				return errors.New("checkpoint crash")
			}
			return nil
		}
		return coordinator
	}
	t.Cleanup(func() { newCoordinator = original })
	if _, err := Checkpoint(env, CheckpointOptions{Scope: "project", ID: parent.State.ID}); err == nil || !strings.Contains(err.Error(), "checkpoint crash") {
		t.Fatalf("Checkpoint error = %v", err)
	}
	newCoordinator = original
	replayed, err := ops.New(env.ProjectWT).ReplayPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 {
		t.Fatalf("replayed = %v", replayed)
	}
	checkpoints, err := List(env, ListOptions{Scope: "project", Directory: DirCheckpoints})
	if err != nil || len(checkpoints) != 1 {
		t.Fatalf("checkpoints = %+v, err=%v", checkpoints, err)
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil || !strings.Contains(string(events), `"event":"state.checkpoint"`) {
		t.Fatalf("events = %s, err=%v", events, err)
	}
}

func TestStateUpdateBlocksPendingCheckpointWithoutModifyingState(t *testing.T) {
	env := testEnv(t)
	parent, err := Start(env, StartOptions{Scope: "project", TaskID: "task-log-replay", Title: "Log replay"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(parent.Path)
	if err != nil {
		t.Fatal(err)
	}
	original := newCoordinator
	failed := false
	newCoordinator = func(root string) *ops.Coordinator {
		coordinator := ops.New(root)
		coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
			if phase == "commit" && index == 1 && !failed {
				failed = true
				return errors.New("checkpoint interrupted before event write")
			}
			return nil
		}
		return coordinator
	}
	t.Cleanup(func() { newCoordinator = original })
	if _, err := Checkpoint(env, CheckpointOptions{Scope: "project", ID: parent.State.ID}); err == nil {
		t.Fatal("checkpoint unexpectedly completed")
	}
	newCoordinator = original
	_, err = Update(env, UpdateOptions{
		Scope: "project", ID: parent.State.ID, AppendBody: "continued", Actor: "test",
	})
	if !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("Update error = %v, want ErrPendingOperation", err)
	}
	after, readErr := os.ReadFile(parent.Path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != string(before) {
		t.Fatalf("blocked update modified active state:\n%s", after)
	}
	var pendingErr *ops.PendingOperationError
	if !errors.As(err, &pendingErr) || len(pendingErr.IDs) != 1 {
		t.Fatalf("pending error = %#v, err=%v", pendingErr, err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "ops", "journal", pendingErr.IDs[0]+".commit.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked update committed pending checkpoint: %v", err)
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(events), `"event":"state.update"`) {
		t.Fatalf("blocked update appended event:\n%s", events)
	}

	if replayed, err := ops.New(env.ProjectWT).ReplayPending(); err != nil || len(replayed) != 1 {
		t.Fatalf("explicit replay = %v, err=%v", replayed, err)
	}
	if _, err := Update(env, UpdateOptions{
		Scope: "project", ID: parent.State.ID, AppendBody: "continued", Actor: "test",
	}); err != nil {
		t.Fatal(err)
	}
	events, err = os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"event":"state.checkpoint"`, `"event":"state.update"`} {
		if !strings.Contains(string(events), want) {
			t.Fatalf("events missing %s:\n%s", want, events)
		}
	}
}

func TestResumeCoreWriteSetReplaysAfterFailpoint(t *testing.T) {
	env := testEnv(t)
	source, err := Start(env, StartOptions{Scope: "project", TaskID: "task-resume-op", Title: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	original := newCoordinator
	failed := false
	newCoordinator = func(root string) *ops.Coordinator {
		coordinator := ops.New(root)
		coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
			if phase == "commit" && index == 1 && !failed {
				failed = true
				return errors.New("resume crash")
			}
			return nil
		}
		return coordinator
	}
	t.Cleanup(func() { newCoordinator = original })
	if _, err := Resume(env, ResumeOptions{
		StartOptions:   StartOptions{Scope: "project", TaskID: "task-resume-op", Title: "Resumed"},
		SourceActiveID: source.State.ID,
	}); err == nil || !strings.Contains(err.Error(), "resume crash") {
		t.Fatalf("Resume error = %v", err)
	}
	newCoordinator = original
	if _, err := ops.New(env.ProjectWT).ReplayPending(); err != nil {
		t.Fatal(err)
	}
	active, err := List(env, ListOptions{Scope: "project", Directory: DirActive})
	if err != nil || len(active) != 1 || active[0].State.Title != "Resumed" {
		t.Fatalf("active = %+v, err=%v", active, err)
	}
	if _, err := Show(env, ShowOptions{Scope: "project", ID: source.State.ID, Directory: DirArchived}); err != nil {
		t.Fatalf("source was not archived: %v", err)
	}
	alias, err := Show(env, ShowOptions{Scope: "project", ID: "latest", Directory: DirActive})
	if err != nil || alias.State.ID != active[0].State.ID {
		t.Fatalf("latest alias = %+v, err=%v", alias, err)
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil || !strings.Contains(string(events), `"event":"state.start"`) || !strings.Contains(string(events), `"event":"state.close"`) {
		t.Fatalf("events = %s, err=%v", events, err)
	}
}
