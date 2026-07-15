package worktreesnap

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestCaptureUsesOnlyGitIdentityAndPorcelain(t *testing.T) {
	var commands []string
	runner := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		commands = append(commands, strings.Join(args, " "))
		switch args[0] {
		case "rev-parse":
			if len(args) > 1 && args[1] == "--show-toplevel" {
				return []byte("/repo\n"), nil
			}
			return []byte("0123456789abcdef\n"), nil
		case "branch":
			return []byte("feat/handoff-v2\n"), nil
		case "status":
			return []byte(" M internal/a.go\x00?? internal/b.go\x00"), nil
		default:
			return nil, fmt.Errorf("unexpected git command %v", args)
		}
	}
	snapshot, err := capture(context.Background(), "/repo", runner)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Dirty || snapshot.ChangedPathCount != 2 || len(snapshot.ChangedPaths) != 2 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if snapshot.ChangedPaths[0].Path != "internal/a.go" || snapshot.ChangedPaths[0].Status != " M" {
		t.Fatalf("first changed path = %+v", snapshot.ChangedPaths[0])
	}
	for _, command := range commands {
		if strings.Contains(command, "diff") {
			t.Fatalf("capture must not run git diff: %s", command)
		}
	}
}

func TestParsePorcelainCapsPathsAndCountsAllChanges(t *testing.T) {
	var status strings.Builder
	for index := 0; index < MaxChangedPaths+7; index++ {
		fmt.Fprintf(&status, " M file-%02d.go\x00", index)
	}
	paths, count, err := parsePorcelainZ([]byte(status.String()))
	if err != nil {
		t.Fatal(err)
	}
	if count != MaxChangedPaths+7 {
		t.Fatalf("count = %d", count)
	}
	if len(paths) != MaxChangedPaths {
		t.Fatalf("recorded paths = %d", len(paths))
	}
}

func TestParsePorcelainRejectsEscapingPath(t *testing.T) {
	if _, _, err := parsePorcelainZ([]byte(" M ../secret\x00")); err == nil {
		t.Fatal("expected unsafe path rejection")
	}
}
