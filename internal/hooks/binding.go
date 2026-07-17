package hooks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	bindingIdleTTL  = 24 * time.Hour
	bindingFileMode = 0o600
)

type taskBinding struct {
	Host                 string    `json:"host"`
	SessionHash          string    `json:"session_hash"`
	StateID              string    `json:"state_id"`
	TaskID               string    `json:"task_id"`
	StateRevision        string    `json:"state_revision"`
	LastInjectedRevision string    `json:"last_injected_revision,omitempty"`
	LastSeenAt           time.Time `json:"last_seen_at"`
	NeedsContextRefresh  bool      `json:"needs_context_refresh,omitempty"`
}

type uniqueTask struct {
	Capsule  wtstate.Capsule
	Revision string
	TaskID   string
}

func resolveUniqueExplicitTask(env paths.Env) (*uniqueTask, error) {
	items, err := wtstate.ListExplicitActiveWithTask(env, "project")
	if err != nil {
		return nil, err
	}
	if len(items) != 1 {
		return nil, nil
	}
	cap := items[0]
	taskID := strings.TrimSpace(wtstate.TaskID(cap))
	if taskID == "" {
		return nil, nil
	}
	return &uniqueTask{
		Capsule:  cap,
		Revision: wtstate.StateRevision(cap),
		TaskID:   taskID,
	}, nil
}

func bindingPathFromHash(root, host, sessionHash string) (string, error) {
	dir, err := runtime.EnsurePrivateDir(root, filepath.Join("runtime", "hooks", "bindings"))
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, host+"-"+sessionHash+".json"), nil
}

func loadBinding(root, host, sessionID string) (*taskBinding, error) {
	return loadBindingByHash(root, host, shortHash(sessionID))
}

func loadBindingByHash(root, host, sessionHash string) (*taskBinding, error) {
	path, err := bindingPathFromHash(root, host, sessionHash)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var binding taskBinding
	if err := json.Unmarshal(data, &binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func saveBinding(root string, binding taskBinding) error {
	path, err := bindingPathFromHash(root, binding.Host, binding.SessionHash)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(binding, "", "  ")
	if err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), bindingFileMode)
}

func deleteBinding(root, host, sessionID string) error {
	path, err := bindingPathFromHash(root, host, shortHash(sessionID))
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func refreshBinding(env paths.Env, host, sessionID string, task *uniqueTask, markRefresh bool) (*taskBinding, error) {
	if task == nil || strings.TrimSpace(sessionID) == "" {
		if strings.TrimSpace(sessionID) != "" {
			_ = deleteBinding(env.ProjectWT, host, sessionID)
		}
		return nil, nil
	}
	sessionHash := shortHash(sessionID)
	binding := taskBinding{
		Host:          host,
		SessionHash:   sessionHash,
		StateID:       task.Capsule.State.ID,
		TaskID:        task.TaskID,
		StateRevision: task.Revision,
		LastSeenAt:    time.Now().UTC(),
	}
	if existing, err := loadBindingByHash(env.ProjectWT, host, sessionHash); err == nil && existing != nil {
		binding.LastInjectedRevision = existing.LastInjectedRevision
		binding.NeedsContextRefresh = existing.NeedsContextRefresh
	}
	if markRefresh {
		binding.NeedsContextRefresh = true
		binding.LastInjectedRevision = ""
	}
	if err := saveBinding(env.ProjectWT, binding); err != nil {
		return nil, err
	}
	return &binding, nil
}

func markBindingInjected(root string, binding *taskBinding, revision string) error {
	if binding == nil {
		return nil
	}
	binding.LastInjectedRevision = revision
	binding.NeedsContextRefresh = false
	binding.LastSeenAt = time.Now().UTC()
	return saveBinding(root, *binding)
}

func pruneIdleBindings(root string, now time.Time) error {
	dir := filepath.Join(root, "runtime", "hooks", "bindings")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var binding taskBinding
		if err := json.Unmarshal(data, &binding); err != nil {
			continue
		}
		if now.Sub(binding.LastSeenAt) > bindingIdleTTL {
			_ = os.Remove(path)
		}
	}
	return nil
}

func shortHash(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return hex.EncodeToString(sum[:])[:12]
}
