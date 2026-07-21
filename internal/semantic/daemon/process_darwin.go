//go:build darwin

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// NewFactory returns the Darwin process factory. It starts exactly the path in
// Command and deliberately never searches PATH for a runtime executable.
func NewFactory() Factory {
	return darwinFactory{}
}

type darwinFactory struct{}

func (darwinFactory) New(command Command) Process {
	return &darwinProcess{command: command}
}

func (darwinFactory) Open(identity Identity) (Process, error) {
	if identity.PID <= 0 || identity.StartedAt.IsZero() {
		return nil, errors.New("semantic process identity is incomplete")
	}
	actual, err := darwinIdentity(identity.PID)
	if err != nil {
		return nil, err
	}
	if !actual.StartedAt.Equal(identity.StartedAt) {
		return nil, errors.New("semantic process PID has a different start time")
	}
	process, err := os.FindProcess(identity.PID)
	if err != nil {
		return nil, fmt.Errorf("open semantic process %d: %w", identity.PID, err)
	}
	return &darwinProcess{process: process, identity: identity}, nil
}

type darwinProcess struct {
	command  Command
	process  *os.Process
	identity Identity
	mu       sync.Mutex
}

func (p *darwinProcess) Start(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if p.command.Path == "" {
		return errors.New("semantic process command path is required")
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.process != nil {
		return errors.New("semantic process has already started")
	}

	// WithoutCancel keeps the detached daemon alive after the command that
	// started it returns. CommandContext is still used to retain its standard
	// exec setup while the caller context is checked above.
	command := exec.CommandContext(context.WithoutCancel(ctx), p.command.Path, p.command.Args...)
	command.Dir = p.command.Dir
	if p.command.Env != nil {
		command.Env = p.command.Env
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start semantic process %q: %w", p.command.Path, err)
	}
	p.process = command.Process
	identity, err := darwinIdentity(command.Process.Pid)
	if err != nil {
		_ = command.Process.Kill()
		_ = command.Process.Release()
		p.process = nil
		return err
	}
	p.identity = identity
	return nil
}

func (p *darwinProcess) Identity() (Identity, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.process == nil || p.identity.PID <= 0 || p.identity.StartedAt.IsZero() {
		return Identity{}, errors.New("semantic process has not started")
	}
	return p.identity, nil
}

func (p *darwinProcess) Signal(signal os.Signal) error {
	p.mu.Lock()
	process := p.process
	p.mu.Unlock()
	if process == nil {
		return errors.New("semantic process has not started")
	}
	return process.Signal(signal)
}

func (p *darwinProcess) WaitExited(ctx context.Context) error {
	p.mu.Lock()
	identity := p.identity
	p.mu.Unlock()
	if identity.PID <= 0 || identity.StartedAt.IsZero() {
		return errors.New("semantic process has not started")
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		actual, err := darwinIdentity(identity.PID)
		if err != nil {
			// PID is gone; the original process has exited.
			return nil
		}
		if !actual.StartedAt.Equal(identity.StartedAt) {
			// PID was reused by a different process.
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (p *darwinProcess) Release() error {
	p.mu.Lock()
	process := p.process
	p.process = nil
	p.mu.Unlock()
	if process == nil {
		return nil
	}
	return process.Release()
}

// darwinIdentity reads the kernel's process birth timestamp. The timestamp has
// microsecond precision and is used with the PID to detect PID reuse before
// any signal is sent.
func darwinIdentity(pid int) (Identity, error) {
	info, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return Identity{}, fmt.Errorf("inspect semantic process %d: %w", pid, err)
	}
	if int(info.Proc.P_pid) != pid {
		return Identity{}, fmt.Errorf("kernel returned unexpected semantic process PID %d", info.Proc.P_pid)
	}
	started := info.Proc.P_starttime
	if started.Sec == 0 && started.Usec == 0 {
		return Identity{}, errors.New("semantic process start time is unavailable")
	}
	return Identity{
		PID:       pid,
		StartedAt: time.Unix(int64(started.Sec), int64(started.Usec)*int64(time.Microsecond)).UTC(),
	}, nil
}
