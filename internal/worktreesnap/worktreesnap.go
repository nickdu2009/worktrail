package worktreesnap

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
)

const MaxChangedPaths = 50

var ErrNotGitRepository = errors.New("not a Git repository")

type commandRunner func(context.Context, string, ...string) ([]byte, error)

func Capture(ctx context.Context, projectRoot string) (model.WorktreeSnapshot, error) {
	return capture(ctx, projectRoot, runGit)
}

func capture(ctx context.Context, projectRoot string, runner commandRunner) (model.WorktreeSnapshot, error) {
	projectRoot = strings.TrimSpace(projectRoot)
	if projectRoot == "" {
		return model.WorktreeSnapshot{}, errors.New("project root is required")
	}
	root, err := runner(ctx, projectRoot, "rev-parse", "--show-toplevel")
	if err != nil {
		return model.WorktreeSnapshot{}, fmt.Errorf("%w: %v", ErrNotGitRepository, err)
	}
	repoRoot := strings.TrimSpace(string(root))
	if repoRoot == "" {
		return model.WorktreeSnapshot{}, ErrNotGitRepository
	}
	branch, err := runner(ctx, repoRoot, "branch", "--show-current")
	if err != nil {
		return model.WorktreeSnapshot{}, fmt.Errorf("read Git branch: %w", err)
	}
	head, err := runner(ctx, repoRoot, "rev-parse", "HEAD")
	if err != nil {
		return model.WorktreeSnapshot{}, fmt.Errorf("read Git HEAD: %w", err)
	}
	status, err := runner(ctx, repoRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return model.WorktreeSnapshot{}, fmt.Errorf("read Git status: %w", err)
	}
	paths, count, err := parsePorcelainZ(status)
	if err != nil {
		return model.WorktreeSnapshot{}, err
	}
	return model.WorktreeSnapshot{
		Branch:           strings.TrimSpace(string(branch)),
		HeadCommit:       strings.TrimSpace(string(head)),
		Dirty:            count > 0,
		ChangedPathCount: count,
		ChangedPaths:     paths,
		CodeAvailability: model.CodeAvailabilityAvailable,
		CapturedAt:       time.Now().UTC(),
	}, nil
}

func runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = err.Error()
		}
		return nil, errors.New(message)
	}
	return stdout.Bytes(), nil
}

func parsePorcelainZ(data []byte) ([]model.WorktreePathStatus, int, error) {
	if len(data) == 0 {
		return nil, 0, nil
	}
	parts := bytes.Split(data, []byte{0})
	paths := make([]model.WorktreePathStatus, 0, min(len(parts), MaxChangedPaths))
	count := 0
	for index := 0; index < len(parts); index++ {
		entry := parts[index]
		if len(entry) == 0 {
			continue
		}
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, 0, fmt.Errorf("invalid Git porcelain entry %q", string(entry))
		}
		status := string(entry[:2])
		path := filepath.ToSlash(string(entry[3:]))
		if path == "" || filepath.IsAbs(filepath.FromSlash(path)) || path == ".." || strings.HasPrefix(path, "../") {
			return nil, 0, fmt.Errorf("Git status returned unsafe path %q", path)
		}
		count++
		if len(paths) < MaxChangedPaths {
			paths = append(paths, model.WorktreePathStatus{Path: path, Status: status})
		}
		if status[0] == 'R' || status[0] == 'C' || status[1] == 'R' || status[1] == 'C' {
			index++
			if index >= len(parts) || len(parts[index]) == 0 {
				return nil, 0, errors.New("Git rename status is missing its source path")
			}
		}
	}
	return paths, count, nil
}

func min(left, right int) int {
	if left < right {
		return left
	}
	return right
}
