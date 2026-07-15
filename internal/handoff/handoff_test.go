package handoff

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/textsafety"
	"github.com/nickdu2009/worktrail/internal/worktreesnap"
)

func TestCreateLocalV2UsesStableIDFilenameAndTaskScopedPrevious(t *testing.T) {
	env := handoffTestEnv(t)
	source, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "task-stable", Title: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	first, err := CreateLocal(context.Background(), env, CreateRequest{
		Title:     "任务交接",
		Summary:   "first",
		TaskID:    "task-stable",
		NextSteps: []model.NextStep{{Action: "continue implementation"}},
		SourceState: &model.Ref{
			Scope:   "project",
			Kind:    "state",
			ID:      source.State.ID,
			RelPath: "/Users/alice/project/.worktrail/state/active/" + source.State.ID + ".md",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.Meta.Schema != Schema || first.Meta.ProjectID != "project-stable" || first.Meta.TaskID != "task-stable" {
		t.Fatalf("metadata = %+v", first.Meta)
	}
	if got, want := first.RelPath, "handoffs/local/"+first.Meta.ID+".md"; got != want {
		t.Fatalf("rel path = %q, want %q", got, want)
	}
	if strings.Contains(first.Body, "/Users/") || strings.Contains(first.Body, "State Snapshot") {
		t.Fatalf("body contains non-portable snapshot data:\n%s", first.Body)
	}
	info, err := os.Stat(first.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("local mode = %04o", info.Mode().Perm())
	}

	second, err := CreateLocal(context.Background(), env, CreateRequest{
		Title:     "renamed task",
		Summary:   "second",
		TaskID:    "task-stable",
		NextSteps: []model.NextStep{{Action: "run tests"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Meta.PreviousHandoff == nil || second.Meta.PreviousHandoff.ID != first.Meta.ID {
		t.Fatalf("previous = %+v", second.Meta.PreviousHandoff)
	}
	updatedFirst, err := Show(env, ShowRequest{ID: first.Meta.ID, Visibility: model.VisibilityLocal})
	if err != nil {
		t.Fatal(err)
	}
	if updatedFirst.Meta.LifecycleStatus != model.LifecycleSuperseded {
		t.Fatalf("first lifecycle = %s", updatedFirst.Meta.LifecycleStatus)
	}
}

func TestCreateLocalValidationSafetyAndLimits(t *testing.T) {
	env := handoffTestEnv(t)
	if _, err := CreateLocal(context.Background(), env, CreateRequest{Summary: "missing next", TaskID: "task-1"}); err == nil || !strings.Contains(err.Error(), "next step") {
		t.Fatalf("expected next-step validation, got %v", err)
	}
	if _, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary:   "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----",
		TaskID:    "task-1",
		NextSteps: []model.NextStep{{Action: "continue"}},
	}); !errors.Is(err, textsafety.ErrBlockedContent) {
		t.Fatalf("expected blocked secret, got %v", err)
	}
	record, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary:   "contact nick@example.com",
		TaskID:    "task-1",
		NextSteps: []model.NextStep{{Action: "continue from /Users/nick/private"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.RedactionStatus != "redacted" || strings.Contains(record.Body, "nick@example.com") || strings.Contains(record.Body, "/Users/") {
		t.Fatalf("record was not redacted: %+v\n%s", record.Meta, record.Body)
	}
	if _, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary:  "oversized",
		TaskID:   "task-2",
		Complete: true,
		Body:     strings.Repeat("x", LocalBodyMax+1),
	}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected local body limit, got %v", err)
	}
}

func TestListWithDiagnosticsIsFailSoft(t *testing.T) {
	env := handoffTestEnv(t)
	valid, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary:   "valid",
		TaskID:    "task-list",
		NextSteps: []model.NextStep{{Action: "continue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	badPath := filepath.Join(env.ProjectWT, "handoffs", "local", "broken.md")
	if err := os.WriteFile(badPath, []byte("---worktrail\n{broken\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := ListWithDiagnostics(env, ListOptions{Visibility: model.VisibilityLocal})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Records) != 1 || result.Records[0].Meta.ID != valid.Meta.ID {
		t.Fatalf("valid records = %+v", result.Records)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Path != "handoffs/local/broken.md" {
		t.Fatalf("diagnostics = %+v", result.Diagnostics)
	}
}

func TestPublishRequiresGitCleanlinessAndDoesNotStage(t *testing.T) {
	env := gitHandoffTestEnv(t)
	local := mustCreateLocal(t, env, "task-publish")
	if got := gitOutput(t, env.ProjectRoot, "status", "--porcelain"); got != "" {
		t.Fatalf("create left visible Git changes: %q", got)
	}
	team, err := Publish(context.Background(), env, PublishRequest{ID: local.Meta.ID})
	if err != nil {
		t.Fatal(err)
	}
	if team.Meta.Visibility != model.VisibilityTeam || team.Meta.PublishedFrom == nil || team.Meta.PublishedFrom.ID != local.Meta.ID {
		t.Fatalf("team metadata = %+v", team.Meta)
	}
	if team.Meta.Worktree.CodeAvailability != model.CodeAvailabilityAvailable {
		t.Fatalf("code availability = %q", team.Meta.Worktree.CodeAvailability)
	}
	info, err := os.Stat(team.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("team mode = %04o", info.Mode().Perm())
	}
	if staged := gitOutput(t, env.ProjectRoot, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("publish staged files: %q", staged)
	}
	status := gitOutput(t, env.ProjectRoot, "status", "--porcelain")
	if !strings.Contains(status, team.RelPath) && !strings.Contains(status, ".worktrail/handoffs/") {
		t.Fatalf("team file should be visible but unstaged: %q", status)
	}
	before, err := os.ReadFile(team.Path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Close(env, CloseRequest{ID: team.Meta.ID}); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected immutable close rejection, got %v", err)
	}
	after, _ := os.ReadFile(team.Path)
	if string(before) != string(after) {
		t.Fatal("team file changed after rejected close")
	}
}

func TestPublishDirtyRequiresExplicitConfirmedException(t *testing.T) {
	env := gitHandoffTestEnv(t)
	local := mustCreateLocal(t, env, "task-dirty")
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(context.Background(), env, PublishRequest{ID: local.Meta.ID}); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("expected dirty rejection, got %v", err)
	}
	if _, err := Publish(context.Background(), env, PublishRequest{ID: local.Meta.ID, AllowDirty: true}); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("expected confirmation rejection, got %v", err)
	}
	team, err := Publish(context.Background(), env, PublishRequest{ID: local.Meta.ID, AllowDirty: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if team.Meta.Worktree.CodeAvailability != model.CodeAvailabilityUnavailable {
		t.Fatalf("dirty publish availability = %q", team.Meta.Worktree.CodeAvailability)
	}
}

func TestPublishRejectsUserScopeAndNonGitProjects(t *testing.T) {
	env := handoffTestEnv(t)
	local := mustCreateLocal(t, env, "task-no-git")
	if _, err := Publish(context.Background(), env, PublishRequest{Scope: "user", ID: local.Meta.ID}); err == nil || !strings.Contains(err.Error(), "project scope") {
		t.Fatalf("expected user-scope rejection, got %v", err)
	}
	if _, err := Publish(context.Background(), env, PublishRequest{ID: local.Meta.ID}); !errors.Is(err, worktreesnap.ErrNotGitRepository) {
		t.Fatalf("expected non-Git rejection, got %v", err)
	}
}

func TestPublishRejectsTeamBodyOverLimit(t *testing.T) {
	env := gitHandoffTestEnv(t)
	local, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary:  "large local body",
		TaskID:   "task-large-team",
		Complete: true,
		Body:     strings.Repeat("x", TeamBodyMax+1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Publish(context.Background(), env, PublishRequest{ID: local.Meta.ID}); err == nil || !strings.Contains(err.Error(), "team handoff body exceeds") {
		t.Fatalf("expected team body rejection, got %v", err)
	}
}

func TestDoctorReportsMultipleImmutableTeamHeads(t *testing.T) {
	env := handoffTestEnv(t)
	for _, id := range []string{"handoff_team_a", "handoff_team_b"} {
		meta := Metadata{HandoffMetaV2: model.HandoffMetaV2{
			BaseMetaV2: model.BaseMetaV2{
				Schema:     Schema,
				ID:         id,
				Scope:      "project",
				ObjectKind: model.ObjectKindRuntime,
				Title:      id,
				CreatedAt:  testTime,
				UpdatedAt:  testTime,
			},
			ProjectID:       "project-stable",
			TaskID:          "task-dag",
			RuntimeType:     model.RuntimeTypeHandoff,
			Summary:         id,
			Complete:        true,
			Visibility:      model.VisibilityTeam,
			StorageClass:    model.StorageClassTeam,
			Durability:      model.DurabilityDurable,
			LifecycleStatus: model.LifecyclePublished,
			ResumePriority:  model.ResumePriorityManualHandoff,
			FormatVersion:   2,
		}}
		if err := setContentHash(&meta, "# Handoff\n"); err != nil {
			t.Fatal(err)
		}
		data, err := renderRecord(meta, "# Handoff\n")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(env.ProjectWT, filepath.FromSlash(handoffRelPath(model.VisibilityTeam, id)))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Doctor(env, DoctorRequest{})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diagnostic := range report.Diagnostics {
		found = found || diagnostic.Code == "multiple_team_heads"
	}
	if !found {
		t.Fatalf("doctor diagnostics = %+v", report.Diagnostics)
	}
}

func TestTeamSupersedesRequiresExplicitReconciliationForMultipleHeads(t *testing.T) {
	records := []Record{
		{Meta: Metadata{HandoffMetaV2: model.HandoffMetaV2{BaseMetaV2: model.BaseMetaV2{ID: "team-a", Scope: "project"}, TaskID: "task-dag", Visibility: model.VisibilityTeam}}, RelPath: "handoffs/team/team-a.md"},
		{Meta: Metadata{HandoffMetaV2: model.HandoffMetaV2{BaseMetaV2: model.BaseMetaV2{ID: "team-b", Scope: "project"}, TaskID: "task-dag", Visibility: model.VisibilityTeam}}, RelPath: "handoffs/team/team-b.md"},
	}
	if _, err := resolveTeamSupersedes("project", "task-dag", nil, records, records); err == nil || !strings.Contains(err.Error(), "reconciliation") {
		t.Fatalf("expected multiple-head ambiguity, got %v", err)
	}
	refs, err := resolveTeamSupersedes("project", "task-dag", []string{"team-a", "team-b"}, records, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].RelPath == "" || refs[1].RelPath == "" {
		t.Fatalf("reconciliation refs = %+v", refs)
	}
	if _, err := resolveTeamSupersedes("project", "task-dag", []string{"team-a"}, records, records); err == nil || !strings.Contains(err.Error(), "every current head") {
		t.Fatalf("partial reconciliation error = %v", err)
	}
}

func TestRepairIsDryRunByDefaultAndRequiresConfirmation(t *testing.T) {
	env := handoffTestEnv(t)
	record := mustCreateLocal(t, env, "task-repair")
	if err := os.Chmod(record.Path, 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Repair(env, RepairRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Actions) == 0 {
		t.Fatalf("dry-run report = %+v", report)
	}
	if info, _ := os.Stat(record.Path); info.Mode().Perm() != 0o644 {
		t.Fatalf("dry-run changed mode to %04o", info.Mode().Perm())
	}
	if _, err := Repair(env, RepairRequest{Apply: true}); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("expected confirmation error, got %v", err)
	}
	report, err = Repair(env, RepairRequest{Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied {
		t.Fatalf("apply report = %+v", report)
	}
	if info, _ := os.Stat(record.Path); info.Mode().Perm() != 0o600 {
		t.Fatalf("repair mode = %04o", info.Mode().Perm())
	}
}

func TestCreateTransactionReplaysHandoffStateAliasAndEvents(t *testing.T) {
	env := handoffTestEnv(t)
	activeRel := "state/active/state-transaction.md"
	aliasRel := "state/active/latest.md"
	for _, rel := range []string{activeRel, aliasRel} {
		path := filepath.Join(env.ProjectWT, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("active\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	originalFactory := newCoordinator
	failed := false
	newCoordinator = func(root string) *ops.Coordinator {
		coordinator := ops.New(root)
		coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
			if phase == "commit" && index == 1 && !failed {
				failed = true
				return errors.New("injected commit failure")
			}
			return nil
		}
		return coordinator
	}
	t.Cleanup(func() { newCoordinator = originalFactory })
	_, err := CreateLocalWithMutation(context.Background(), env, CreateRequest{
		Summary:   "transaction",
		TaskID:    "task-transaction",
		NextSteps: []model.NextStep{{Action: "replay"}},
	}, Mutation{
		Writes:  []FileWrite{{Path: "state/archived/state-transaction.md", Data: []byte("archived\n"), Mode: 0o600}},
		Deletes: []string{activeRel, aliasRel},
		Events:  []Event{{Name: "state.close", ID: "state-transaction", Actor: "test"}},
	})
	if err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("expected injected failure, got %v", err)
	}
	newCoordinator = originalFactory
	replayed, err := ops.New(env.ProjectWT).ReplayPending()
	if err != nil {
		t.Fatal(err)
	}
	if len(replayed) != 1 {
		t.Fatalf("replayed operations = %v", replayed)
	}
	result, err := ListWithDiagnostics(env, ListOptions{Visibility: model.VisibilityLocal, TaskID: "task-transaction"})
	if err != nil || len(result.Records) != 1 {
		t.Fatalf("handoff after replay = %+v, err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "state", "archived", "state-transaction.md")); err != nil {
		t.Fatalf("archive missing after replay: %v", err)
	}
	for _, rel := range []string{activeRel, aliasRel} {
		if _, err := os.Stat(filepath.Join(env.ProjectWT, filepath.FromSlash(rel))); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists after replay: %v", rel, err)
		}
	}
	events, err := os.ReadFile(filepath.Join(env.ProjectWT, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(events), `"handoff.create"`) || !strings.Contains(string(events), `"state.close"`) {
		t.Fatalf("events after replay:\n%s", events)
	}
}

func TestNewHandoffBlocksPendingOperationUntilExplicitReplay(t *testing.T) {
	env := handoffTestEnv(t)
	originalFactory := newCoordinator
	failed := false
	newCoordinator = func(root string) *ops.Coordinator {
		coordinator := ops.New(root)
		coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
			if phase == "commit" && index == 0 && !failed {
				failed = true
				return errors.New("leave pending handoff")
			}
			return nil
		}
		return coordinator
	}
	t.Cleanup(func() { newCoordinator = originalFactory })
	if _, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary: "first pending", TaskID: "task-auto-replay", Complete: true,
	}); err == nil || !strings.Contains(err.Error(), "leave pending") {
		t.Fatalf("first CreateLocal error = %v", err)
	}
	newCoordinator = originalFactory
	_, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary: "second after recovery", TaskID: "task-auto-replay", Complete: true,
	})
	if !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("second CreateLocal error = %v, want ErrPendingOperation", err)
	}
	var pendingErr *ops.PendingOperationError
	if !errors.As(err, &pendingErr) || len(pendingErr.IDs) != 1 {
		t.Fatalf("pending error = %#v, err=%v", pendingErr, err)
	}
	listed, err := ListWithDiagnostics(env, ListOptions{Visibility: model.VisibilityLocal, TaskID: "task-auto-replay"})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Records) != 0 {
		t.Fatalf("blocked handoff create modified records: %+v", listed.Records)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "ops", "journal", pendingErr.IDs[0]+".commit.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked handoff create committed pending operation: %v", err)
	}

	if replayed, err := ops.New(env.ProjectWT).ReplayPending(); err != nil || len(replayed) != 1 {
		t.Fatalf("explicit replay = %v, err=%v", replayed, err)
	}
	if _, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary: "second after recovery", TaskID: "task-auto-replay", Complete: true,
	}); err != nil {
		t.Fatal(err)
	}
	listed, err = ListWithDiagnostics(env, ListOptions{Visibility: model.VisibilityLocal, TaskID: "task-auto-replay"})
	if err != nil {
		t.Fatal(err)
	}
	current := 0
	for _, record := range listed.Records {
		if record.Meta.LifecycleStatus == model.LifecycleCurrent {
			current++
		}
	}
	if len(listed.Records) != 2 || current != 1 {
		t.Fatalf("records = %+v", listed.Records)
	}
}

func TestCreateLocalDoesNotReplayPendingPrune(t *testing.T) {
	env := handoffTestEnv(t)
	coordinator := ops.New(env.ProjectWT)
	operation, err := coordinator.Begin(ops.Spec{
		ID: "op_prune_pending",
		Writes: []ops.Write{{
			Path: "prune-result.txt", Data: []byte("pruned"),
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
		if phase == "commit" && index == 0 {
			return errors.New("leave prune pending")
		}
		return nil
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("prune operation unexpectedly committed")
	}

	_, err = CreateLocal(context.Background(), env, CreateRequest{
		Summary: "must wait for doctor", TaskID: "task-pending-prune", Complete: true,
	})
	if !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("CreateLocal error = %v, want ErrPendingOperation", err)
	}
	if _, err := os.Stat(filepath.Join(env.ProjectWT, "prune-result.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("handoff create replayed prune: %v", err)
	}
	listed, err := ListWithDiagnostics(env, ListOptions{
		Visibility: model.VisibilityLocal, TaskID: "task-pending-prune",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.Records) != 0 {
		t.Fatalf("blocked handoff create wrote records: %+v", listed.Records)
	}
}

func TestConcurrentCreatesLeaveOneCurrentPerTask(t *testing.T) {
	env := handoffTestEnv(t)
	results := make(chan error, 2)
	for index := 0; index < 2; index++ {
		go func() {
			_, err := CreateLocal(context.Background(), env, CreateRequest{
				Summary:   "concurrent",
				TaskID:    "task-concurrent",
				NextSteps: []model.NextStep{{Action: "continue"}},
			})
			results <- err
		}()
	}
	for index := 0; index < 2; index++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	listed, err := ListWithDiagnostics(env, ListOptions{Visibility: model.VisibilityLocal, TaskID: "task-concurrent"})
	if err != nil {
		t.Fatal(err)
	}
	current := 0
	for _, record := range listed.Records {
		if record.Meta.LifecycleStatus == model.LifecycleCurrent {
			current++
		}
	}
	if len(listed.Records) != 2 || current != 1 {
		t.Fatalf("records=%d current=%d: %+v", len(listed.Records), current, listed.Records)
	}
}

func TestCreateRejectsInvalidIdentifiersAndMismatchedSourceState(t *testing.T) {
	env := handoffTestEnv(t)
	source, err := wtstate.Start(env, wtstate.StartOptions{Scope: "project", TaskID: "task-source", Title: "Source"})
	if err != nil {
		t.Fatal(err)
	}
	base := CreateRequest{
		Summary: "source validation", Complete: true, TaskID: "task-other",
		SourceState: &model.Ref{Scope: "project", Kind: "state", ID: source.State.ID},
	}
	if _, err := CreateLocal(context.Background(), env, base); err == nil || !strings.Contains(err.Error(), "does not belong") {
		t.Fatalf("mismatched source state error = %v", err)
	}
	base.TaskID = "task-source"
	base.SourceState.Kind = "checkpoint"
	if _, err := CreateLocal(context.Background(), env, base); err == nil || !strings.Contains(err.Error(), "kind") {
		t.Fatalf("wrong source kind error = %v", err)
	}
	base.SourceState = nil
	base.NextSteps = []model.NextStep{{ID: "bad/id", Action: "continue"}}
	base.Complete = false
	if _, err := CreateLocal(context.Background(), env, base); err == nil || !strings.Contains(err.Error(), "next step id") {
		t.Fatalf("invalid next-step id error = %v", err)
	}
}

func TestTeamFinalSafetyCoversAllUserControlledMetadata(t *testing.T) {
	base := Metadata{HandoffMetaV2: model.HandoffMetaV2{
		BaseMetaV2: model.BaseMetaV2{Title: "safe"},
		Summary:    "safe",
	}}
	cases := map[string]func(*Metadata){
		"handoff id":  func(meta *Metadata) { meta.ID = "AKIA1234567890ABCDEF" },
		"project id":  func(meta *Metadata) { meta.ProjectID = "AKIA1234567890ABCDEF" },
		"task id":     func(meta *Metadata) { meta.TaskID = "AKIA1234567890ABCDEF" },
		"tags":        func(meta *Metadata) { meta.Tags = []string{"/private/secret"} },
		"next id":     func(meta *Metadata) { meta.NextSteps = []model.NextStep{{ID: "AKIA1234567890ABCDEF", Action: "safe"}} },
		"next action": func(meta *Metadata) { meta.NextSteps = []model.NextStep{{Action: "/Volumes/private/action"}} },
		"next owner":  func(meta *Metadata) { meta.NextSteps = []model.NextStep{{Action: "safe", Owner: `C:\Users\alice`}} },
		"question":    func(meta *Metadata) { meta.OpenQuestions = []string{"/Users/alice/question"} },
		"risk":        func(meta *Metadata) { meta.Risks = []string{"nick@example.com"} },
		"validation": func(meta *Metadata) {
			meta.Validation = []model.ValidationEvidence{{Status: "passed", Summary: "/home/alice/result"}}
		},
		"source tool": func(meta *Metadata) { meta.SourceTool = `C:/Users/alice/tool` },
		"actor":       func(meta *Metadata) { meta.Actor = "/private/actor" },
		"branch":      func(meta *Metadata) { meta.Worktree.Branch = "/Volumes/private/branch" },
		"changed path": func(meta *Metadata) {
			meta.Worktree.ChangedPaths = []model.WorktreePathStatus{{Path: `C:\Users\alice\file`}}
		},
		"source ref path": func(meta *Metadata) { meta.SourceState = &model.Ref{RelPath: "/Users/alice/state"} },
		"source ref id":   func(meta *Metadata) { meta.SourceState = &model.Ref{ID: "AKIA1234567890ABCDEF"} },
		"previous ref id": func(meta *Metadata) { meta.PreviousHandoff = &model.Ref{ID: "AKIA1234567890ABCDEF"} },
		"published ref id": func(meta *Metadata) {
			meta.PublishedFrom = &model.Ref{ID: "AKIA1234567890ABCDEF"}
		},
		"supersedes ref id": func(meta *Metadata) {
			meta.Supersedes = []model.Ref{{ID: "AKIA1234567890ABCDEF"}}
		},
		"superseded-by ref id": func(meta *Metadata) {
			meta.SupersededBy = []model.Ref{{ID: "AKIA1234567890ABCDEF"}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			meta := base
			mutate(&meta)
			if err := validateFinalRecord(meta, "safe", textsafety.ProfileTeam); !errors.Is(err, textsafety.ErrTeamUnsafeContent) {
				t.Fatalf("validateFinalRecord error = %v", err)
			}
		})
	}
	if err := validateFinalRecord(base, `{"role":"user","content":"raw"}`, textsafety.ProfileTeam); !errors.Is(err, textsafety.ErrTeamUnsafeContent) {
		t.Fatalf("body JSONL bypass error = %v", err)
	}
}

func TestLegacyRootHandoffRequiresMigrationForNormalReads(t *testing.T) {
	env := handoffTestEnv(t)
	path := filepath.Join(env.ProjectWT, "handoffs", "legacy.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("# Legacy handoff\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := List(env, "project"); !errors.Is(err, ErrMigrationRequired) {
		t.Fatalf("List error = %v, want migration required", err)
	}
	if _, err := Doctor(env, DoctorRequest{}); err != nil {
		t.Fatalf("Doctor should retain explicit legacy inspection: %v", err)
	}
}

func TestMalformedLocalHandoffIsQuarantinedOnlyOnConfirmedApply(t *testing.T) {
	env := handoffTestEnv(t)
	path := filepath.Join(env.ProjectWT, "handoffs", "local", "broken.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---worktrail\n{broken\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := Doctor(env, DoctorRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Diagnostics) != 1 || !report.Diagnostics[0].Repairable {
		t.Fatalf("diagnostics = %+v", report.Diagnostics)
	}
	repair, err := Repair(env, RepairRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(repair.Actions) != 1 || repair.Applied {
		t.Fatalf("dry-run repair = %+v", repair)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("dry-run moved malformed handoff: %v", err)
	}
	if _, err := Repair(env, RepairRequest{Apply: true}); err == nil || !strings.Contains(err.Error(), "--confirm") {
		t.Fatalf("apply without confirmation error = %v", err)
	}
	repair, err = Repair(env, RepairRequest{Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !repair.Applied {
		t.Fatalf("repair was not applied: %+v", repair)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed local handoff remains: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(env.ProjectWT, "runtime", "quarantine", "handoff", "*-broken.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("quarantine matches = %v, err=%v", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil || !strings.Contains(string(data), "{broken") {
		t.Fatalf("quarantine data = %q, err=%v", data, err)
	}
}

func TestMalformedTeamHandoffIsNeverQuarantined(t *testing.T) {
	env := handoffTestEnv(t)
	path := filepath.Join(env.ProjectWT, "handoffs", "team", "broken.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---worktrail\n{broken\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Repair(env, RepairRequest{Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Actions) != 0 {
		t.Fatalf("team malformed repair = %+v", report)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("team malformed handoff was moved: %v", err)
	}
}

func TestHandoffDoctorQuarantinesMalformedStateOnConfirmedRepair(t *testing.T) {
	env := handoffTestEnv(t)
	path := filepath.Join(env.ProjectWT, "state", wtstate.DirActive, "broken.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---worktrail\n{broken\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	doctor, err := Doctor(env, DoctorRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if len(doctor.Diagnostics) != 1 || doctor.Diagnostics[0].Code != "invalid_state" || !doctor.Diagnostics[0].Repairable {
		t.Fatalf("doctor diagnostics = %+v", doctor.Diagnostics)
	}
	repair, err := Repair(env, RepairRequest{Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if !repair.Applied {
		t.Fatalf("state repair was not applied: %+v", repair)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("malformed state remains after handoff repair: %v", err)
	}
}

func TestMalformedLocalHandoffSymlinkIsNeverQuarantined(t *testing.T) {
	env := handoffTestEnv(t)
	dir := filepath.Join(env.ProjectWT, "handoffs", "local")
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
	report, err := Repair(env, RepairRequest{Apply: true, Confirm: true})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied || len(report.Actions) != 0 || len(report.Diagnostics) != 1 || report.Diagnostics[0].Repairable {
		t.Fatalf("symlink repair report = %+v", report)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("handoff symlink was moved: %v", err)
	}
	if _, err := os.Stat(outside); err != nil {
		t.Fatalf("handoff symlink target was affected: %v", err)
	}
}

var testTime = time.Unix(1, 0).UTC()

func handoffTestEnv(t *testing.T) paths.Env {
	t.Helper()
	projectRoot := filepath.Join(t.TempDir(), "project")
	projectWT := filepath.Join(projectRoot, ".worktrail")
	if err := os.MkdirAll(projectWT, 0o700); err != nil {
		t.Fatal(err)
	}
	writeJSON(t, filepath.Join(projectWT, "config.json"), map[string]any{"project_id": "project-stable"})
	return paths.Env{
		Home:        filepath.Join(t.TempDir(), "home"),
		UserRoot:    filepath.Join(t.TempDir(), "home", ".worktrail"),
		ProjectRoot: projectRoot,
		ProjectWT:   projectWT,
	}
}

func gitHandoffTestEnv(t *testing.T) paths.Env {
	t.Helper()
	env := handoffTestEnv(t)
	gitRun(t, env.ProjectRoot, "init")
	gitRun(t, env.ProjectRoot, "config", "user.email", "test@example.com")
	gitRun(t, env.ProjectRoot, "config", "user.name", "Worktrail Test")
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, "README.md"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ignore := ".worktrail/handoffs/local/\n.worktrail/ops/\n.worktrail/logs/\n"
	if err := os.WriteFile(filepath.Join(env.ProjectRoot, ".gitignore"), []byte(ignore), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, env.ProjectRoot, "add", ".gitignore", "README.md", ".worktrail/config.json")
	gitRun(t, env.ProjectRoot, "commit", "-m", "test fixture")
	return env
}

func mustCreateLocal(t *testing.T, env paths.Env, taskID string) Record {
	t.Helper()
	record, err := CreateLocal(context.Background(), env, CreateRequest{
		Summary:   "ready",
		TaskID:    taskID,
		NextSteps: []model.NextStep{{Action: "continue"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func writeJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
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
