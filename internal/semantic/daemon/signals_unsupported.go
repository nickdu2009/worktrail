//go:build !darwin

package daemon

import "os"

// Unsupported platforms never start a semantic process. These values keep the
// injected supervisor boundary compilable without claiming POSIX semantics.
var (
	terminateSignal os.Signal = os.Interrupt
	killSignal      os.Signal = os.Kill
)
