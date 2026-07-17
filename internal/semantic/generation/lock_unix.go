//go:build darwin || linux

package generation

import (
	"context"
	"os"
	"syscall"
	"time"
)

func acquireLockedFile(ctx context.Context, path string, mode LeaseMode) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	lockMode := syscall.LOCK_SH
	if mode == LeaseExclusive {
		lockMode = syscall.LOCK_EX
	}
	for {
		err = syscall.Flock(int(file.Fd()), lockMode|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = file.Close()
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func releaseLockedFile(file *os.File) error {
	unlockErr := syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	closeErr := file.Close()
	if unlockErr != nil {
		return unlockErr
	}
	return closeErr
}
