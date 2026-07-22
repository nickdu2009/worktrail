//go:build darwin

package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"syscall"
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
		// kern.proc.pid reports EIO for a reaped PID on current Darwin. Treat
		// the identity as absent only when a separate signal-0 probe also
		// proves the PID does not exist; every uncertain result remains a
		// fail-closed identity mismatch.
		probeErr := unix.Kill(identity.PID, 0)
		if errors.Is(err, unix.ESRCH) || errors.Is(err, unix.ENOENT) || errors.Is(probeErr, unix.ESRCH) {
			return nil, fmt.Errorf("%w: %v", ErrProcessNotFound, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrProcessIdentityMismatch, err)
	}
	if !actual.StartedAt.Equal(identity.StartedAt) {
		return nil, ErrProcessIdentityMismatch
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
	done     <-chan struct{}
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

	// The launchd-owned Host, not an individual request, owns the worker. Keep
	// the child in the Host process group while preventing request cancellation
	// from killing it.
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
		_ = command.Wait()
		p.process = nil
		return err
	}
	p.identity = identity
	if err := startWorkerWatch(identity, p.command.Dir); err != nil {
		_ = command.Process.Kill()
		_ = command.Wait()
		p.process = nil
		return err
	}
	done := make(chan struct{})
	p.done = done
	go func() {
		_ = command.Wait()
		close(done)
	}()
	return nil
}

func startWorkerWatch(worker Identity, directory string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve semantic worker watchdog executable: %w", err)
	}
	host, err := darwinIdentity(os.Getpid())
	if err != nil {
		return err
	}
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	defer readyReader.Close()
	watch := exec.Command(executable, "semantic", "worker-watch", "--launchd")
	watch.Dir = directory
	watch.Env = workerWatchEnvironment(os.Environ(), host, worker)
	watch.ExtraFiles = []*os.File{readyWriter}
	// Keep the watchdog outside the launchd Host process group so a targeted
	// Host SIGKILL cannot remove the only process able to clean the llama
	// worker. The llama worker itself remains in the Host process group.
	watch.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := watch.Start(); err != nil {
		readyWriter.Close()
		return fmt.Errorf("start semantic worker watchdog: %w", err)
	}
	_ = readyWriter.Close()
	_ = readyReader.SetReadDeadline(time.Now().Add(5 * time.Second))
	buffer := []byte{0}
	if _, err := readyReader.Read(buffer); err != nil || buffer[0] != 1 {
		_ = watch.Process.Kill()
		_ = watch.Wait()
		return errors.New("semantic worker watchdog did not become ready")
	}
	go func() {
		_ = watch.Wait()
		// A watchdog that exits while both original identities are still
		// present would leave the worker unprotected. Fail closed by stopping
		// that exact worker; the next request may recover it once.
		if processIdentityMatches(host) && processIdentityMatches(worker) {
			_ = stopWatchedWorker(worker)
		}
	}()
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
	done := p.done
	p.mu.Unlock()
	if identity.PID <= 0 || identity.StartedAt.IsZero() {
		return errors.New("semantic process has not started")
	}
	if done != nil {
		select {
		case <-done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
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
	owned := p.done != nil
	p.process = nil
	p.mu.Unlock()
	if process == nil {
		return nil
	}
	if owned {
		// command.Wait owns process reaping for children created by this Host.
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
