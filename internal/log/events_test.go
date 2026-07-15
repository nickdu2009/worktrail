package log

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
)

func TestAppendConcurrentWritesCompleteJSONLines(t *testing.T) {
	root := t.TempDir()
	const writers = 12
	const perWriter = 8
	var wg sync.WaitGroup
	errs := make(chan error, writers)
	for writer := 0; writer < writers; writer++ {
		wg.Add(1)
		go func(writer int) {
			defer wg.Done()
			for item := 0; item < perWriter; item++ {
				id := fmt.Sprintf("%d-%d", writer, item)
				if err := Append(root, "test.concurrent", id, "test", map[string]any{"id": id}); err != nil {
					errs <- err
					return
				}
			}
		}(writer)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}

	file, err := os.Open(filepath.Join(root, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	seen := make(map[string]bool, writers*perWriter)
	scanner := bufio.NewScanner(file)
	lines := 0
	for scanner.Scan() {
		lines++
		var event model.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("line %d is not complete JSON: %v: %q", lines, err, scanner.Bytes())
		}
		if event.Event != "test.concurrent" || event.ID == "" {
			t.Fatalf("unexpected event on line %d: %+v", lines, event)
		}
		if seen[event.ID] {
			t.Fatalf("duplicate event id %q", event.ID)
		}
		seen[event.ID] = true
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if want := writers * perWriter; lines != want {
		t.Fatalf("lines = %d, want %d", lines, want)
	}
}

func TestAppendBlocksPendingWholeFileEventUntilExplicitReplay(t *testing.T) {
	root := t.TempDir()
	coordinator := ops.New(root)
	coordinator.Failpoint = func(phase string, index int, _ ops.Action) error {
		if phase == "commit" && index == 0 {
			return fmt.Errorf("simulated crash before event write")
		}
		return nil
	}
	operation, err := coordinator.Begin(ops.Spec{
		ID: "op_state_checkpoint_pending",
		Writes: []ops.Write{{
			Path: "logs/events.jsonl",
			Data: []byte(`{"event":"state.checkpoint","id":"pending"}` + "\n"),
			Mode: 0o644,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := operation.Commit(); err == nil {
		t.Fatal("pending operation unexpectedly committed")
	}
	err = Append(root, "state.update", "after", "test", nil)
	if !errors.Is(err, ops.ErrPendingOperation) {
		t.Fatalf("Append error = %v, want ErrPendingOperation", err)
	}
	var pendingErr *ops.PendingOperationError
	if !errors.As(err, &pendingErr) || len(pendingErr.IDs) != 1 || pendingErr.IDs[0] != "op_state_checkpoint_pending" {
		t.Fatalf("pending error = %#v, err=%v", pendingErr, err)
	}
	if _, err := os.Stat(filepath.Join(root, "logs", "events.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked append modified event log: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "ops", "journal", "op_state_checkpoint_pending.commit.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("blocked append committed pending operation: %v", err)
	}

	if replayed, err := ops.New(root).ReplayPending(); err != nil || len(replayed) != 1 {
		t.Fatalf("explicit replay = %v, err=%v", replayed, err)
	}
	if err := Append(root, "state.update", "after", "test", nil); err != nil {
		t.Fatal(err)
	}
	events := readEvents(t, root)
	if len(events) != 2 || events[0].Event != "state.checkpoint" || events[1].Event != "state.update" {
		t.Fatalf("events after replay = %+v", events)
	}
}

func TestAppendStaleOwnerWithoutIntentRequiresDoctorRepair(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "ops", "lock")
	if err := os.MkdirAll(lockPath, 0o700); err != nil {
		t.Fatal(err)
	}
	host, err := os.Hostname()
	if err != nil {
		t.Fatal(err)
	}
	owner := ops.LockOwner{
		OperationID: "op_log_append_crashed",
		PID:         1 << 30,
		Host:        host,
		AcquiredAt:  time.Now().UTC().Add(-time.Hour),
	}
	data, err := json.Marshal(owner)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lockPath, "owner.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	err = Append(root, "test.stale", "blocked", "test", nil)
	if !errors.Is(err, ops.ErrLocked) || !strings.Contains(err.Error(), "doctor ops --repair --confirm") {
		t.Fatalf("Append stale lock error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "logs", "events.jsonl")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale owner append modified log: %v", err)
	}
	if entries, err := os.ReadDir(filepath.Join(root, "ops", "journal")); err == nil && len(entries) != 0 {
		t.Fatalf("stale owner append created journal intents: %v", entries)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}

	if err := ops.RemoveStaleLock(root); err != nil {
		t.Fatal(err)
	}
	if replayed, err := ops.New(root).ReplayPending(); err != nil || len(replayed) != 0 {
		t.Fatalf("explicit repair replay = %v, err=%v", replayed, err)
	}
	if err := Append(root, "test.stale", "recovered", "test", nil); err != nil {
		t.Fatal(err)
	}
}

func TestAppendCrossProcessWritesCompleteJSONLines(t *testing.T) {
	root := t.TempDir()
	const processes = 6
	const perProcess = 8
	commands := make([]*exec.Cmd, 0, processes)
	outputs := make([]*bytes.Buffer, 0, processes)
	for process := 0; process < processes; process++ {
		command := exec.Command(os.Args[0], "-test.run=^TestAppendProcessHelper$")
		command.Env = append(os.Environ(),
			"WORKTRAIL_LOG_HELPER_ROOT="+root,
			"WORKTRAIL_LOG_HELPER_PROCESS="+strconv.Itoa(process),
			"WORKTRAIL_LOG_HELPER_COUNT="+strconv.Itoa(perProcess),
		)
		output := &bytes.Buffer{}
		command.Stdout = output
		command.Stderr = output
		if err := command.Start(); err != nil {
			t.Fatal(err)
		}
		commands = append(commands, command)
		outputs = append(outputs, output)
	}
	var failures []string
	for index, command := range commands {
		if err := command.Wait(); err != nil {
			failures = append(failures, fmt.Sprintf("helper %d: %v\n%s", index, err, outputs[index].String()))
		}
	}
	if len(failures) > 0 {
		t.Fatal(strings.Join(failures, "\n"))
	}
	events := readEvents(t, root)
	if len(events) != processes*perProcess {
		t.Fatalf("events = %d, want %d", len(events), processes*perProcess)
	}
	seen := map[string]bool{}
	for _, event := range events {
		if seen[event.ID] {
			t.Fatalf("duplicate event id %q", event.ID)
		}
		seen[event.ID] = true
	}
}

func TestAppendProcessHelper(t *testing.T) {
	root := os.Getenv("WORKTRAIL_LOG_HELPER_ROOT")
	if root == "" {
		return
	}
	process, err := strconv.Atoi(os.Getenv("WORKTRAIL_LOG_HELPER_PROCESS"))
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(os.Getenv("WORKTRAIL_LOG_HELPER_COUNT"))
	if err != nil {
		t.Fatal(err)
	}
	for item := 0; item < count; item++ {
		id := fmt.Sprintf("%d-%d", process, item)
		if err := Append(root, "test.process", id, "test", nil); err != nil {
			t.Fatal(err)
		}
	}
}

func readEvents(t *testing.T, root string) []model.Event {
	t.Helper()
	file, err := os.Open(filepath.Join(root, "logs", "events.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var events []model.Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event model.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("invalid event line: %v: %q", err, scanner.Bytes())
		}
		events = append(events, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return events
}
