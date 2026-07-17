//go:build darwin || linux

package bundle

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

func acquireBundleLock(ctx context.Context, parent, bundleID string) (func(), error) {
	lockPath := filepath.Join(parent, "."+bundleID+".lock")
	lockFile, err := os.OpenFile(lockPath, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() {
				_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
				_ = lockFile.Close()
			}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			lockFile.Close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			lockFile.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
