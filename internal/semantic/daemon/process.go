package daemon

import (
	"context"
	"os"
	"time"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

// Command describes the executable a platform-specific process will start.
type Command struct {
	Path string
	Args []string
	Dir  string
	Env  []string
}

// Identity uniquely identifies a started operating-system process.
type Identity struct {
	PID       int
	StartedAt time.Time
}

// Factory creates independently controllable processes for daemon supervision.
// Platform-specific implementations own how Command is translated into a
// process; callers depend only on this interface.
type Factory interface {
	New(Command) Process
	// Open must reject an expected identity unless the operating system confirms
	// that PID currently has the same start time. A caller may signal a Process
	// only after Open succeeds.
	Open(Identity) (Process, error)
}

// Process is the injectable operating-system process boundary used by daemon
// supervision.
type Process interface {
	Start(context.Context) error
	Identity() (Identity, error)
	Signal(os.Signal) error
	// WaitExited blocks until the previously observed process identity is gone
	// or has been reused by a different process. Callers must supply a deadline
	// or cancelable context; a timeout must leave daemon state intact.
	WaitExited(context.Context) error
	Release() error
}

// ErrProcessUnsupported is returned when the current platform cannot supervise
// semantic runtime processes.
var ErrProcessUnsupported = &Error{
	Code:    contracts.ReasonPlatformUnsupported,
	Message: "semantic process supervision is unsupported on this platform",
}
