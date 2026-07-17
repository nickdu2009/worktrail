//go:build darwin

package daemon

import (
	"os"
	"syscall"
)

var (
	terminateSignal os.Signal = syscall.SIGTERM
	killSignal      os.Signal = syscall.SIGKILL
)
