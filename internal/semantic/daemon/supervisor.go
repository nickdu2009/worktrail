package daemon

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

const (
	StateReady = "ready"

	expectedEmbeddingDimension = 1024
	defaultStartAttempts       = 2
	// defaultReadyWait matches the M1 cold-readiness budget (model load before
	// llama.app binds the loopback listener). StartTries remains a full restart
	// budget after this wait is exhausted.
	defaultReadyWait         = 25 * time.Second
	defaultReadyPollInterval = 250 * time.Millisecond
	defaultStopWait          = 5 * time.Second
)

// RuntimeVerifier is the injected local integrity-verification boundary.
// Production composition must verify the installed bundle files against the
// embedded immutable manifest before Start or Stop can mutate daemon state.
//
// VerifyRuntime must return a *Error with an appropriate ReasonCode for a
// verifier-specific denial. Generic errors are reported without their text.
type RuntimeVerifier interface {
	VerifyRuntime(ctx context.Context, runtime RuntimeSpec) error
}

// ReadinessClient performs the authenticated runtime readiness request. A
// successful response must have been authenticated with the supplied API key.
type ReadinessClient interface {
	Readiness(ctx context.Context, endpoint, apiKey string) (RuntimeIdentity, error)
}

// EndpointAllocator reserves a candidate loopback address for one start
// attempt. The runtime owns the subsequent bind; a lost bind race is retried
// with a fresh allocation by the supervisor.
type EndpointAllocator interface {
	Allocate(context.Context) (string, error)
}

// StartLocker serializes starts for one immutable bundle ID.
type StartLocker interface {
	Lock(ctx context.Context, bundleID string) (unlock func(), err error)
}

// RuntimeSpec contains only the trusted, explicit bundle paths and immutable
// runtime identity used to construct llama serve arguments.
type RuntimeSpec struct {
	BundleID        string
	RuntimePath     string
	ModelPath       string
	WorkingDir      string
	Alias           string
	Dimension       int
	LlamaAppVersion string
	RuntimeSHA256   string
	ChipVariant     string
	ModelSHA256     string
	SupportLevel    string
}

// RuntimeIdentity is the model metadata an authenticated llama.app readiness
// response can actually prove. Operating-system process identity is verified
// exclusively by Factory.Open.
type RuntimeIdentity struct {
	Alias     string
	Dimension int
}

// TransportError explicitly marks a connection-level failure as safe for the
// one bounded API-first recovery sequence. Authentication and response-shape
// errors must not use this type.
type TransportError struct {
	Err error
}

func (e *TransportError) Error() string {
	if e == nil || e.Err == nil {
		return "semantic daemon transport failure"
	}
	return e.Err.Error()
}

func (e *TransportError) Unwrap() error { return e.Err }

func (*TransportError) RecoverableTransport() bool { return true }

// SupervisorConfig supplies every side-effecting dependency explicitly.
// Production wiring is intentionally deferred to the app and installer lanes.
type SupervisorConfig struct {
	Store      Store
	Runtime    RuntimeSpec
	Verifier   RuntimeVerifier
	Client     ReadinessClient
	Allocator  EndpointAllocator
	Locker     StartLocker
	Factory    Factory
	Now        func() time.Time
	StartTries int
	// ReadyWait is the per-attempt budget to poll authenticated readiness after
	// the process starts. Zero selects the default cold-start budget.
	ReadyWait time.Duration
	// ReadyPollInterval is the delay between readiness probes. Zero selects the
	// default poll interval.
	ReadyPollInterval time.Duration
	// StopWait is the budget to wait for process exit after SIGTERM before
	// removing descriptor state. Zero selects the default stop wait.
	StopWait time.Duration
}

// Supervisor is the trusted-bundle-gated implementation of Controller.
type Supervisor struct {
	config SupervisorConfig
}

var _ Controller = (*Supervisor)(nil)

// NewSupervisor validates the static runtime contract without starting a
// process, probing an endpoint, or writing daemon state.
func NewSupervisor(config SupervisorConfig) (*Supervisor, error) {
	if err := validateRuntime(config.Runtime); err != nil {
		return nil, err
	}
	if config.Client == nil {
		return nil, errors.New("semantic daemon readiness client is required")
	}
	if config.Verifier == nil {
		return nil, errors.New("semantic daemon runtime verifier is required")
	}
	if config.Allocator == nil {
		return nil, errors.New("semantic daemon endpoint allocator is required")
	}
	if config.Locker == nil {
		return nil, errors.New("semantic daemon start locker is required")
	}
	if config.Factory == nil {
		return nil, errors.New("semantic daemon process factory is required")
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if config.StartTries == 0 {
		config.StartTries = defaultStartAttempts
	}
	if config.StartTries < 1 {
		return nil, errors.New("semantic daemon start attempts must be positive")
	}
	if config.ReadyWait == 0 {
		config.ReadyWait = defaultReadyWait
	}
	if config.ReadyWait < 0 {
		return nil, errors.New("semantic daemon ready wait must be non-negative")
	}
	if config.ReadyPollInterval == 0 {
		config.ReadyPollInterval = defaultReadyPollInterval
	}
	if config.ReadyPollInterval < 0 {
		return nil, errors.New("semantic daemon ready poll interval must be non-negative")
	}
	if config.StopWait == 0 {
		config.StopWait = defaultStopWait
	}
	if config.StopWait < 0 {
		return nil, errors.New("semantic daemon stop wait must be non-negative")
	}
	return &Supervisor{config: config}, nil
}

// Status probes only an already-recorded daemon. It does not verify, lock,
// allocate an endpoint, generate credentials, start, or stop a process.
func (s *Supervisor) Status(ctx context.Context) (Report, error) {
	identity, descriptor, err := s.readiness(ctx)
	if err != nil {
		if errors.Is(err, ErrDescriptorNotFound) {
			return s.runtimeReport(stoppedReport("status")), nil
		}
		return s.runtimeReport(unavailableReport("status")), nil
	}
	if err := s.verify(descriptor, identity); err != nil {
		return s.runtimeReport(unavailableReport("status")), nil
	}
	return s.runtimeReport(readyReport("status")), nil
}

func (s *Supervisor) Start(ctx context.Context) (Report, error) {
	if err := s.verifyRuntime(ctx); err != nil {
		return operationFailure("start", err)
	}

	identity, descriptor, err := s.readiness(ctx)
	if err == nil {
		if err := s.verify(descriptor, identity); err != nil {
			return operationFailure("start", err)
		}
		return readyReport("start"), nil
	}
	if !isRecoverableTransport(err) {
		return operationFailure("start", err)
	}

	unlock, err := s.config.Locker.Lock(ctx, s.config.Runtime.BundleID)
	if err != nil {
		return operationFailure("start", err)
	}
	defer unlock()

	identity, descriptor, err = s.readiness(ctx)
	if err == nil {
		if err := s.verify(descriptor, identity); err != nil {
			return operationFailure("start", err)
		}
		return readyReport("start"), nil
	}
	if !isRecoverableTransport(err) {
		return operationFailure("start", err)
	}

	if _, err := s.start(ctx); err != nil {
		return operationFailure("start", err)
	}
	report := readyReport("start")
	report.Started = true
	return report, nil
}

func (s *Supervisor) Stop(ctx context.Context) (Report, error) {
	if err := s.verifyRuntime(ctx); err != nil {
		return operationFailure("stop", err)
	}

	unlock, err := s.config.Locker.Lock(ctx, s.config.Runtime.BundleID)
	if err != nil {
		return operationFailure("stop", err)
	}
	defer unlock()

	descriptor, err := s.config.Store.Load()
	if errors.Is(err, ErrDescriptorNotFound) {
		return stoppedReport("stop"), nil
	}
	if err != nil {
		return operationFailure("stop", err)
	}
	key, err := s.config.Store.APIKey()
	if err != nil {
		return operationFailure("stop", err)
	}
	identity, err := s.config.Client.Readiness(ctx, descriptor.Endpoint, key)
	if err != nil {
		return operationFailure("stop", err)
	}
	if err := s.verify(descriptor, identity); err != nil {
		return operationFailure("stop", err)
	}

	process, err := s.config.Factory.Open(Identity{PID: descriptor.PID, StartedAt: descriptor.StartTime})
	if err != nil {
		return operationFailure("stop", err)
	}
	defer process.Release()
	if err := process.Signal(terminateSignal); err != nil {
		return operationFailure("stop", err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, s.config.StopWait)
	defer cancel()
	if err := process.WaitExited(waitCtx); err != nil {
		return operationFailure("stop", fmt.Errorf("wait for semantic process exit: %w", err))
	}
	if err := s.config.Store.Remove(); err != nil {
		return operationFailure("stop", err)
	}
	return stoppedReport("stop"), nil
}

func (s *Supervisor) Restart(ctx context.Context) (Report, error) {
	if _, err := s.Stop(ctx); err != nil {
		return operationFailure("restart", err)
	}
	report, err := s.Start(ctx)
	report.Operation = "restart"
	return report, err
}

func (s *Supervisor) readiness(ctx context.Context) (RuntimeIdentity, Descriptor, error) {
	descriptor, err := s.config.Store.Load()
	if err != nil {
		if errors.Is(err, ErrDescriptorNotFound) {
			return RuntimeIdentity{}, Descriptor{}, &TransportError{Err: err}
		}
		return RuntimeIdentity{}, Descriptor{}, err
	}
	key, err := s.config.Store.APIKey()
	if err != nil {
		return RuntimeIdentity{}, Descriptor{}, err
	}
	identity, err := s.config.Client.Readiness(ctx, descriptor.Endpoint, key)
	if err != nil {
		return RuntimeIdentity{}, Descriptor{}, err
	}
	return identity, descriptor, nil
}

func (s *Supervisor) start(ctx context.Context) (Descriptor, error) {
	key, err := s.apiKey()
	if err != nil {
		return Descriptor{}, err
	}

	var lastErr error
	for attempt := 0; attempt < s.config.StartTries; attempt++ {
		address, err := s.config.Allocator.Allocate(ctx)
		if err != nil {
			lastErr = fmt.Errorf("allocate semantic daemon endpoint: %w", err)
			continue
		}
		endpoint, err := loopbackEndpoint(address)
		if err != nil {
			return Descriptor{}, err
		}

		process := s.config.Factory.New(s.command(address))
		if err := process.Start(ctx); err != nil {
			lastErr = err
			continue
		}
		processIdentity, err := process.Identity()
		if err != nil {
			s.releaseStarted(process)
			lastErr = err
			continue
		}
		identity, err := s.waitReady(ctx, endpoint, key)
		if err != nil {
			s.releaseStarted(process)
			if isRecoverableTransport(err) {
				lastErr = err
				continue
			}
			return Descriptor{}, err
		}
		if err := s.verifyAPIIdentity(identity); err != nil {
			s.releaseStarted(process)
			return Descriptor{}, err
		}

		descriptor := Descriptor{
			PID:             processIdentity.PID,
			StartTime:       processIdentity.StartedAt,
			Endpoint:        endpoint,
			LlamaAppVersion: s.config.Runtime.LlamaAppVersion,
			RuntimeSHA256:   s.config.Runtime.RuntimeSHA256,
			ChipVariant:     s.config.Runtime.ChipVariant,
			ModelSHA256:     s.config.Runtime.ModelSHA256,
			Alias:           s.config.Runtime.Alias,
			LastSuccess:     s.config.Now().UTC(),
			Readiness:       StateReady,
		}
		if err := s.config.Store.Save(descriptor); err != nil {
			s.releaseStarted(process)
			return Descriptor{}, err
		}
		if err := process.Release(); err != nil {
			return Descriptor{}, err
		}
		return descriptor, nil
	}
	if lastErr == nil {
		lastErr = errors.New("semantic daemon start attempts exhausted")
	}
	return Descriptor{}, fmt.Errorf("start semantic daemon after %d attempts: %w", s.config.StartTries, lastErr)
}

// waitReady polls authenticated readiness until success, a non-recoverable
// error, context cancellation, or the per-attempt ReadyWait budget expires.
// Recoverable transport failures (connection refused while the model loads)
// are retried on the same endpoint instead of immediately killing the process.
func (s *Supervisor) waitReady(ctx context.Context, endpoint, key string) (RuntimeIdentity, error) {
	// Use wall-clock time for the budget so frozen test clocks used for
	// descriptor timestamps cannot stall cold-start polling forever.
	deadline := time.Now().Add(s.config.ReadyWait)
	var lastErr error
	for {
		identity, err := s.config.Client.Readiness(ctx, endpoint, key)
		if err == nil {
			return identity, nil
		}
		lastErr = err
		if !isRecoverableTransport(err) {
			return RuntimeIdentity{}, err
		}
		if err := ctx.Err(); err != nil {
			return RuntimeIdentity{}, err
		}
		if !time.Now().Before(deadline) {
			return RuntimeIdentity{}, &TransportError{Err: fmt.Errorf("semantic daemon readiness timed out: %w", lastErr)}
		}
		timer := time.NewTimer(s.config.ReadyPollInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return RuntimeIdentity{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *Supervisor) command(address string) Command {
	host, port, _ := net.SplitHostPort(address)
	return Command{
		Path: s.config.Runtime.RuntimePath,
		Args: []string{
			"serve",
			"--host", host,
			"--port", port,
			"--model", s.config.Runtime.ModelPath,
			"--alias", s.config.Runtime.Alias,
			"--api-key-file", s.config.Store.APIKeyPath(),
			"--no-webui",
			"--log-disable",
			"--offline",
			"--embedding",
			"--pooling", "cls",
			"--embd-normalize", "2",
			// Physical ubatch must cover production HardMax (768). llama.app
			// defaults to 512 and rejects larger single embedding inputs with HTTP 500.
			"--ubatch-size", "1024",
		},
		Dir: s.config.Runtime.WorkingDir,
	}
}

func (s *Supervisor) apiKey() (string, error) {
	key, err := s.config.Store.APIKey()
	if errors.Is(err, fs.ErrNotExist) {
		key, err = s.config.Store.GenerateAPIKey()
	}
	if err != nil {
		return "", err
	}
	if err := validateAPIKey(key); err != nil {
		return "", err
	}
	return key, nil
}

func (s *Supervisor) verifyRuntime(ctx context.Context) error {
	if s.config.Verifier == nil {
		return trustedBundleRequiredError()
	}
	if err := s.config.Verifier.VerifyRuntime(ctx, s.config.Runtime); err != nil {
		return runtimeVerificationError(err)
	}
	return nil
}

func (s *Supervisor) verify(descriptor Descriptor, identity RuntimeIdentity) error {
	if descriptor.LlamaAppVersion != s.config.Runtime.LlamaAppVersion ||
		descriptor.RuntimeSHA256 != s.config.Runtime.RuntimeSHA256 ||
		descriptor.ChipVariant != s.config.Runtime.ChipVariant ||
		descriptor.ModelSHA256 != s.config.Runtime.ModelSHA256 {
		return errors.New("semantic daemon descriptor does not match trusted runtime fingerprint")
	}
	if descriptor.Alias != s.config.Runtime.Alias {
		return errors.New("semantic daemon descriptor alias does not match trusted bundle")
	}
	return s.verifyAPIIdentity(identity)
}

func (s *Supervisor) verifyAPIIdentity(identity RuntimeIdentity) error {
	if identity.Alias != s.config.Runtime.Alias {
		return errors.New("semantic daemon alias does not match trusted bundle")
	}
	if identity.Dimension != expectedEmbeddingDimension || identity.Dimension != s.config.Runtime.Dimension {
		return fmt.Errorf("semantic daemon embedding dimension = %d, want %d", identity.Dimension, expectedEmbeddingDimension)
	}
	return nil
}

func (s *Supervisor) releaseStarted(process Process) {
	_ = process.Signal(killSignal)
	_ = process.Release()
}

func validateRuntime(runtime RuntimeSpec) error {
	if strings.TrimSpace(runtime.BundleID) == "" {
		return errors.New("semantic runtime bundle ID is required")
	}
	if runtime.Alias != runtime.BundleID {
		return errors.New("semantic runtime alias must equal bundle ID")
	}
	if runtime.Dimension != expectedEmbeddingDimension {
		return fmt.Errorf("semantic runtime dimension = %d, want %d", runtime.Dimension, expectedEmbeddingDimension)
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"runtime path", runtime.RuntimePath},
		{"model path", runtime.ModelPath},
		{"llama.app version", runtime.LlamaAppVersion},
		{"runtime SHA-256", runtime.RuntimeSHA256},
		{"chip variant", runtime.ChipVariant},
		{"model SHA-256", runtime.ModelSHA256},
		{"support level", runtime.SupportLevel},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("semantic runtime %s is required", field.name)
		}
	}
	if runtime.SupportLevel != "verified" && runtime.SupportLevel != "experimental" {
		return fmt.Errorf("semantic runtime support level is invalid: %q", runtime.SupportLevel)
	}
	for _, path := range []string{runtime.RuntimePath, runtime.ModelPath} {
		if !filepath.IsAbs(path) || filepath.Clean(path) != path {
			return fmt.Errorf("semantic runtime path must be a clean absolute path: %q", path)
		}
	}
	if runtime.WorkingDir != "" && (!filepath.IsAbs(runtime.WorkingDir) || filepath.Clean(runtime.WorkingDir) != runtime.WorkingDir) {
		return fmt.Errorf("semantic runtime working directory must be a clean absolute path: %q", runtime.WorkingDir)
	}
	return nil
}

func validateAPIKey(key string) error {
	if key == "" || strings.TrimSpace(key) != key {
		return errors.New("semantic daemon API key is required and must not contain surrounding whitespace")
	}
	return nil
}

func loopbackEndpoint(address string) (string, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" {
		return "", fmt.Errorf("semantic daemon endpoint must be a 127.0.0.1 address: %q", address)
	}
	if value, err := strconv.Atoi(port); err != nil || value < 1 || value > 65535 {
		return "", fmt.Errorf("semantic daemon endpoint port is invalid: %q", address)
	}
	return "http://" + address, nil
}

func isRecoverableTransport(err error) bool {
	var recoverable interface{ RecoverableTransport() bool }
	return errors.As(err, &recoverable) && recoverable.RecoverableTransport()
}

func readyReport(operation string) Report {
	return Report{Schema: ReportSchema, Operation: operation, State: StateReady}
}

func stoppedReport(operation string) Report {
	return Report{Schema: ReportSchema, Operation: operation, State: StateStopped, Reason: contracts.ReasonRuntimeUnavailable}
}

func operationFailure(operation string, err error) (Report, error) {
	var semanticErr *Error
	code := contracts.ReasonRuntimeUnavailable
	message := fmt.Sprintf("local semantic runtime %s failed", operation)
	if errors.As(err, &semanticErr) && semanticErr.Code != "" {
		code = semanticErr.Code
		if semanticErr.Message == trustedBundleRequiredMessage {
			message = trustedBundleRequiredMessage
		}
	}
	typed := &Error{
		Code:    code,
		Message: message,
	}
	return Report{Schema: ReportSchema, Operation: operation, State: StateUnavailable, Reason: typed.Code}, typed
}

func (s *Supervisor) runtimeReport(report Report) Report {
	report.SupportLevel = s.config.Runtime.SupportLevel
	report.Chip = s.config.Runtime.ChipVariant
	report.Warning = RuntimeSupportWarning(s.config.Runtime.SupportLevel)
	return report
}
