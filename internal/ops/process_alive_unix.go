//go:build !windows

package ops

import (
	"errors"
	"syscall"
)

func processAlive(pid int) (alive bool, known bool) {
	if pid <= 0 {
		return false, true
	}
	err := syscall.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return true, true
	case errors.Is(err, syscall.ESRCH):
		return false, true
	default:
		return false, false
	}
}
