//go:build windows

package ops

import (
	"errors"

	"golang.org/x/sys/windows"
)

func processAlive(pid int) (alive bool, known bool) {
	if pid <= 0 {
		return false, true
	}
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err == nil {
		_ = windows.CloseHandle(handle)
		return true, true
	}
	switch {
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return false, true
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return true, true
	default:
		return false, false
	}
}
