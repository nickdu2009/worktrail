//go:build darwin

package daemon

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	workerWatchHostPID       = "WORKTRAIL_SEMANTIC_WATCH_HOST_PID"
	workerWatchHostStarted   = "WORKTRAIL_SEMANTIC_WATCH_HOST_STARTED"
	workerWatchWorkerPID     = "WORKTRAIL_SEMANTIC_WATCH_WORKER_PID"
	workerWatchWorkerStarted = "WORKTRAIL_SEMANTIC_WATCH_WORKER_STARTED"
	workerWatchReadyFD       = "WORKTRAIL_SEMANTIC_WATCH_READY_FD"
	workerWatchPollInterval  = 100 * time.Millisecond
)

// RunWorkerWatch is the hidden Darwin watchdog entrypoint. It receives only
// kernel process identities; runtime arguments and credentials never cross the
// watchdog boundary.
func RunWorkerWatch(ctx context.Context) error {
	readyFD, err := strconv.Atoi(os.Getenv(workerWatchReadyFD))
	if err != nil || readyFD < 3 {
		return errors.New("semantic worker watchdog readiness descriptor is invalid")
	}
	ready := os.NewFile(uintptr(readyFD), "semantic-worker-watch-ready")
	if ready == nil {
		return errors.New("semantic worker watchdog readiness descriptor is unavailable")
	}
	defer ready.Close()
	host, err := workerWatchIdentity(workerWatchHostPID, workerWatchHostStarted)
	if err != nil {
		return err
	}
	worker, err := workerWatchIdentity(workerWatchWorkerPID, workerWatchWorkerStarted)
	if err != nil {
		return err
	}
	if os.Getppid() != host.PID {
		return errors.New("semantic worker watchdog parent identity mismatch")
	}
	if !processIdentityMatches(host) || !processIdentityMatches(worker) {
		return errors.New("semantic worker watchdog initial identity mismatch")
	}
	if _, err := ready.Write([]byte{1}); err != nil {
		return errors.New("semantic worker watchdog readiness failed")
	}
	_ = ready.Close()
	return watchWorkerWithParentCheck(ctx, host, worker, func() bool { return os.Getppid() == host.PID })
}

func watchWorker(ctx context.Context, host, worker Identity) error {
	return watchWorkerWithParentCheck(ctx, host, worker, nil)
}

func watchWorkerWithParentCheck(ctx context.Context, host, worker Identity, parentMatches func() bool) error {
	ticker := time.NewTicker(workerWatchPollInterval)
	defer ticker.Stop()
	for {
		if !processIdentityMatches(worker) {
			return nil
		}
		if (parentMatches != nil && !parentMatches()) || !processIdentityMatches(host) {
			return stopWatchedWorker(worker)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func stopWatchedWorker(identity Identity) error {
	process, err := NewFactory().Open(identity)
	if errors.Is(err, ErrProcessNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	defer process.Release()
	if err := process.Signal(terminateSignal); err != nil {
		return err
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), defaultStopWait)
	err = process.WaitExited(waitCtx)
	cancel()
	if err == nil {
		return nil
	}
	killProcess, openErr := NewFactory().Open(identity)
	if errors.Is(openErr, ErrProcessNotFound) {
		return nil
	}
	if openErr != nil {
		return openErr
	}
	defer killProcess.Release()
	if err := killProcess.Signal(killSignal); err != nil {
		return err
	}
	killCtx, killCancel := context.WithTimeout(context.Background(), defaultStopWait)
	defer killCancel()
	return killProcess.WaitExited(killCtx)
}

func processIdentityMatches(identity Identity) bool {
	actual, err := darwinIdentity(identity.PID)
	return err == nil && actual.StartedAt.Equal(identity.StartedAt)
}

func workerWatchIdentity(pidKey, startedKey string) (Identity, error) {
	pid, err := strconv.Atoi(os.Getenv(pidKey))
	if err != nil || pid <= 0 {
		return Identity{}, errors.New("semantic worker watchdog PID is invalid")
	}
	started, err := time.Parse(time.RFC3339Nano, os.Getenv(startedKey))
	if err != nil || started.IsZero() {
		return Identity{}, errors.New("semantic worker watchdog start time is invalid")
	}
	return Identity{PID: pid, StartedAt: started.UTC()}, nil
}

func workerWatchEnvironment(base []string, host, worker Identity) []string {
	values := map[string]string{
		workerWatchHostPID:       strconv.Itoa(host.PID),
		workerWatchHostStarted:   host.StartedAt.Format(time.RFC3339Nano),
		workerWatchWorkerPID:     strconv.Itoa(worker.PID),
		workerWatchWorkerStarted: worker.StartedAt.Format(time.RFC3339Nano),
		workerWatchReadyFD:       "3",
	}
	result := make([]string, 0, len(base)+len(values))
	for _, entry := range base {
		key, _, found := strings.Cut(entry, "=")
		if found {
			if _, replace := values[key]; replace {
				continue
			}
		}
		result = append(result, entry)
	}
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}
