package log

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/ops"
)

var appendLocks sync.Map

func Append(root string, event string, id string, actor string, data map[string]any) error {
	e := model.Event{Time: time.Now(), Event: event, ID: id, Actor: actor, Data: data}
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	path := filepath.Join(root, "logs", "events.jsonl")
	lock := appendLock(path)
	lock.Lock()
	defer lock.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for {
		err := appendUnderOpsLock(root, append(b, '\n'))
		if staleRecoverableLock(err) {
			return err
		}
		transient := errors.Is(err, ops.ErrLocked) || errors.Is(err, os.ErrNotExist)
		if !transient || time.Now().After(deadline) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func appendUnderOpsLock(root string, line []byte) error {
	coordinator := ops.New(root)
	operationID := fmt.Sprintf("op_log_append_%d_%d", time.Now().UTC().UnixNano(), os.Getpid())
	lock, err := coordinator.AcquireReadyLock(operationID)
	if err != nil {
		if staleRecoverableLock(err) {
			return fmt.Errorf("stale operation lock requires worktrail doctor ops --repair --confirm: %w", err)
		}
		return err
	}
	logsPath := filepath.Join(root, "logs")
	appendErr := appendLine(logsPath, filepath.Join(logsPath, "events.jsonl"), line)
	releaseErr := lock.Release()
	if err := errors.Join(appendErr, releaseErr); err != nil {
		return fmt.Errorf("append event under operation lock %s: %w", operationID, err)
	}
	return nil
}

func staleRecoverableLock(err error) bool {
	var lockErr *ops.LockError
	return errors.As(err, &lockErr) && lockErr.Status.Stale && lockErr.Status.Recoverable
}

func appendLine(logsPath, path string, line []byte) error {
	if err := os.MkdirAll(logsPath, 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(logsPath)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("event log directory %q is not a real directory", logsPath)
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("event log %q is not a regular non-symlink file", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	if stat, err := file.Stat(); err != nil {
		_ = file.Close()
		return err
	} else if stat.Size() > 0 {
		var last [1]byte
		if _, err := file.ReadAt(last[:], stat.Size()-1); err != nil {
			_ = file.Close()
			return err
		}
		if last[0] != '\n' {
			line = append([]byte{'\n'}, line...)
		}
	}
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func appendLock(path string) *sync.Mutex {
	value, _ := appendLocks.LoadOrStore(path, &sync.Mutex{})
	return value.(*sync.Mutex)
}
