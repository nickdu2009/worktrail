package daemon

import (
	"context"
	"errors"
	"os"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

func TestSupervisorStatusOnlyProbesRecordedDaemon(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(41)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41241")
	client := &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41241": {{identity: runtimeIdentity()}},
	}}
	verifier := &fakeRuntimeVerifier{}
	locker := &fakeLocker{}
	factory := &fakeFactory{}
	supervisor := testSupervisor(t, store, client, verifier, locker, factory, &fakeAllocator{})

	report, err := supervisor.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if report.State != StateReady {
		t.Fatalf("Status().State = %q, want %q", report.State, StateReady)
	}
	if report.SupportLevel != "verified" || report.Chip != testRuntime().ChipVariant || report.Warning != "" {
		t.Fatalf("Status() support details = %#v", report)
	}
	if verifier.calls != 0 || locker.calls != 0 || factory.newCalls != 0 {
		t.Fatalf("Status() caused side effects: verify=%d lock=%d start=%d", verifier.calls, locker.calls, factory.newCalls)
	}
	if len(client.calls) != 1 {
		t.Fatalf("readiness calls = %#v, want one authenticated probe", client.calls)
	}
}

func TestSupervisorStatusReportsExperimentalRuntimeWarning(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(42)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41242")
	client := &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41242": {{identity: runtimeIdentity()}},
	}}
	supervisor := testSupervisor(t, store, client, &fakeRuntimeVerifier{}, &fakeLocker{}, &fakeFactory{}, &fakeAllocator{})
	supervisor.config.Runtime.SupportLevel = "experimental"
	supervisor.config.Runtime.ChipVariant = "m4"
	descriptor, err := store.Load()
	if err != nil {
		t.Fatalf("Load() descriptor: %v", err)
	}
	descriptor.ChipVariant = "m4"
	if err := store.Save(descriptor); err != nil {
		t.Fatalf("Save() descriptor: %v", err)
	}

	report, err := supervisor.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if report.SupportLevel != "experimental" || report.Chip != "m4" || report.Warning != experimentalSupportWarning {
		t.Fatalf("Status() support details = %#v", report)
	}
}

func TestSupervisorStartVerificationFailurePreservesReasonCode(t *testing.T) {
	store := testStore(t)
	verifier := &fakeRuntimeVerifier{err: &Error{
		Code:    contracts.ReasonPlatformUnsupported,
		Message: "failed to verify /trusted/bundle/api-key",
	}}
	factory := &fakeFactory{}
	supervisor := testSupervisor(t, store, &scriptedClient{}, verifier, &fakeLocker{}, factory, &fakeAllocator{})

	report, err := supervisor.Start(context.Background())
	var daemonErr *Error
	if !errors.As(err, &daemonErr) {
		t.Fatalf("Start() error = %T %[1]v, want *Error", err)
	}
	if daemonErr.Code != contracts.ReasonPlatformUnsupported || report.Reason != contracts.ReasonPlatformUnsupported {
		t.Fatalf("Start() reason = %q/%q, want %q", daemonErr.Code, report.Reason, contracts.ReasonPlatformUnsupported)
	}
	if daemonErr.Error() != trustedBundleRequiredMessage {
		t.Fatalf("Start() message = %q, want %q", daemonErr.Error(), trustedBundleRequiredMessage)
	}
	if daemonErr.Err != nil {
		t.Fatalf("Start() exposed verifier error = %v", daemonErr.Err)
	}
	if factory.newCalls != 0 {
		t.Fatal("Start() created a process despite failed verification")
	}
}

func TestSupervisorStartRecoversFromEndpointBindRaceAndSavesAuthenticatedDescriptor(t *testing.T) {
	store := testStore(t)
	first := testIdentity(101)
	second := testIdentity(102)
	client := &scriptedClient{responses: map[string][]readinessResult{
		// First endpoint never becomes ready within ReadyWait; start retries once.
		"http://127.0.0.1:41001": {{err: &TransportError{Err: errors.New("connection refused after bind race")}}},
		"http://127.0.0.1:41002": {{identity: runtimeIdentity()}},
	}}
	factory := &fakeFactory{next: []Identity{first, second}}
	allocator := &fakeAllocator{addresses: []string{"127.0.0.1:41001", "127.0.0.1:41002"}}
	locker := &fakeLocker{}
	verifier := &fakeRuntimeVerifier{}
	supervisor := testSupervisor(t, store, client, verifier, locker, factory, allocator)

	report, err := supervisor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if report.State != StateReady {
		t.Fatalf("Start().State = %q, want %q", report.State, StateReady)
	}
	if !report.Started {
		t.Fatal("Start() did not report newly created daemon")
	}
	if locker.calls != 1 || allocator.calls != 2 || factory.newCalls != 2 {
		t.Fatalf("start retry = locks:%d allocs:%d processes:%d, want 1/2/2", locker.calls, allocator.calls, factory.newCalls)
	}
	if verifier.calls != 1 || verifier.runtime != testRuntime() {
		t.Fatalf("VerifyRuntime() = %d calls with %#v, want one call for %#v", verifier.calls, verifier.runtime, testRuntime())
	}
	if got := factory.processes[0].signals; len(got) != 1 || got[0] != killSignal {
		t.Fatalf("lost-race process signals = %#v, want kill signal", got)
	}
	if !factory.processes[0].released {
		t.Fatal("lost-race process was not released")
	}

	descriptor, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Start = %v", err)
	}
	if descriptor.PID != second.PID || !descriptor.StartTime.Equal(second.StartedAt) ||
		descriptor.Endpoint != "http://127.0.0.1:41002" || descriptor.Alias != testRuntime().Alias ||
		descriptor.Readiness != StateReady {
		t.Fatalf("saved descriptor = %#v", descriptor)
	}
	if _, err := store.APIKey(); err != nil {
		t.Fatalf("Start() did not persist an API key: %v", err)
	}
}

func TestSupervisorStartMarksHealthyExistingDaemonAsNotCreated(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(301)
	endpoint := "http://127.0.0.1:41301"
	saveDescriptor(t, store, identity, endpoint)
	factory := &fakeFactory{}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		endpoint: {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{})

	report, err := supervisor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if report.State != StateReady || report.Started {
		t.Fatalf("Start() report = %#v, want existing ready daemon", report)
	}
	if factory.newCalls != 0 {
		t.Fatal("Start() created a process despite an authenticated healthy daemon")
	}
}

func TestSupervisorStartWaitsForColdStartReadinessOnSameEndpoint(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(201)
	client := &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41201": {
			{err: &TransportError{Err: errors.New("connection refused while loading")}},
			{err: &TransportError{Err: errors.New("connection refused while loading")}},
			{identity: runtimeIdentity()},
		},
	}}
	factory := &fakeFactory{next: []Identity{identity}}
	allocator := &fakeAllocator{addresses: []string{"127.0.0.1:41201"}}
	supervisor := testSupervisor(t, store, client, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, allocator)

	report, err := supervisor.Start(context.Background())
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if report.State != StateReady {
		t.Fatalf("Start().State = %q, want %q", report.State, StateReady)
	}
	if factory.newCalls != 1 || allocator.calls != 1 {
		t.Fatalf("cold start = processes:%d allocs:%d, want 1/1", factory.newCalls, allocator.calls)
	}
	if len(client.calls) != 3 {
		t.Fatalf("readiness polls = %d, want 3", len(client.calls))
	}
	descriptor, err := store.Load()
	if err != nil {
		t.Fatalf("Load() after Start = %v", err)
	}
	if descriptor.PID != identity.PID || descriptor.Endpoint != "http://127.0.0.1:41201" {
		t.Fatalf("saved descriptor = %#v", descriptor)
	}
}

func TestSupervisorCommandUsesOwnedKeyFileAndFixedRuntimeFlags(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(111)
	factory := &fakeFactory{next: []Identity{identity}}
	address := "127.0.0.1:41111"
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://" + address: {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{addresses: []string{address}})

	if _, err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if len(factory.commands) != 1 {
		t.Fatalf("runtime commands = %d, want 1", len(factory.commands))
	}
	key, err := store.APIKey()
	if err != nil {
		t.Fatalf("APIKey() error = %v", err)
	}
	info, err := os.Stat(store.APIKeyPath())
	if err != nil {
		t.Fatalf("stat API key file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("API key file permissions = %o, want 600", got)
	}

	command := factory.commands[0]
	want := []string{
		"serve",
		"--host", "127.0.0.1",
		"--port", "41111",
		"--model", testRuntime().ModelPath,
		"--alias", testRuntime().Alias,
		"--api-key-file", store.APIKeyPath(),
		"--no-webui",
		"--log-disable",
		"--offline",
		"--embedding",
		"--pooling", "cls",
		"--embd-normalize", "2",
	}
	if !reflect.DeepEqual(command.Args, want) {
		t.Fatalf("runtime args = %#v, want fixed security/runtime flags", command.Args)
	}
	for _, argument := range command.Args {
		if argument == key {
			t.Fatal("runtime args contain the API key value")
		}
	}
}

func TestSupervisorStopRefusesMismatchedAuthenticatedRuntime(t *testing.T) {
	store := testStore(t)
	descriptorIdentity := testIdentity(61)
	saveDescriptor(t, store, descriptorIdentity, "http://127.0.0.1:41261")
	factory := &fakeFactory{}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41261": {{identity: RuntimeIdentity{Alias: "other-bundle", Dimension: expectedEmbeddingDimension}}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{})

	report, err := supervisor.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop() error = nil, want identity mismatch")
	}
	var daemonErr *Error
	if !errors.As(err, &daemonErr) || daemonErr.Code != contracts.ReasonRuntimeUnavailable {
		t.Fatalf("Stop() error = %T %[1]v, want runtime-unavailable *Error", err)
	}
	if report.Reason != contracts.ReasonRuntimeUnavailable {
		t.Fatalf("Stop().Reason = %q", report.Reason)
	}
	if factory.openCalls != 0 {
		t.Fatal("Stop() opened or signaled a process with mismatched runtime identity")
	}
	if _, err := store.Load(); err != nil {
		t.Fatalf("Stop() removed descriptor for unknown process: %v", err)
	}
}

func TestSupervisorStopRefusesFactoryIdentityMismatchWithoutSignal(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(66)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41266")
	process := &fakeProcess{identity: identity}
	factory := &fakeFactory{opened: process, openErr: errors.New("PID start time does not match")}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41266": {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{})

	_, err := supervisor.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop() error = nil, want OS identity mismatch")
	}
	if factory.openCalls != 1 {
		t.Fatalf("Factory.Open calls = %d, want 1", factory.openCalls)
	}
	if len(process.signals) != 0 || process.released {
		t.Fatalf("identity-mismatched process was touched: signals=%#v released=%v", process.signals, process.released)
	}
}

func TestSupervisorStopSignalsOnlyVerifiedProcess(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(71)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41271")
	process := &fakeProcess{identity: identity}
	factory := &fakeFactory{opened: process}
	verifier := &fakeRuntimeVerifier{}
	locker := &fakeLocker{}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41271": {{identity: runtimeIdentity()}},
	}}, verifier, locker, factory, &fakeAllocator{})

	report, err := supervisor.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if report.State != StateStopped {
		t.Fatalf("Stop().State = %q, want %q", report.State, StateStopped)
	}
	if verifier.calls != 1 || verifier.runtime != testRuntime() {
		t.Fatalf("VerifyRuntime() = %d calls with %#v, want one call for %#v", verifier.calls, verifier.runtime, testRuntime())
	}
	if locker.calls != 1 {
		t.Fatalf("Stop() locks = %d, want 1 per-bundle lock", locker.calls)
	}
	if len(process.signals) != 1 || process.signals[0] != terminateSignal || process.waitCalls != 1 || !process.released {
		t.Fatalf("verified process stop = signals:%#v wait:%d released:%v", process.signals, process.waitCalls, process.released)
	}
	if _, err := store.Load(); !errors.Is(err, ErrDescriptorNotFound) {
		t.Fatalf("Stop() retained descriptor: %v", err)
	}
	if _, err := os.Stat(store.APIKeyPath()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Stop() retained API key: %v", err)
	}
}

func TestSupervisorStopWaitsForDelayedExitBeforeRemovingDescriptor(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(72)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41272")
	process := &fakeProcess{identity: identity, exitAfter: 40 * time.Millisecond}
	factory := &fakeFactory{opened: process}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41272": {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{})
	supervisor.config.StopWait = 200 * time.Millisecond

	if _, err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if process.waitCalls != 1 {
		t.Fatalf("WaitExited calls = %d, want 1", process.waitCalls)
	}
	if _, err := store.Load(); !errors.Is(err, ErrDescriptorNotFound) {
		t.Fatalf("Stop() retained descriptor after delayed exit: %v", err)
	}
}

func TestSupervisorStopTreatsPIDReuseAsExit(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(73)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41273")
	process := &fakeProcess{identity: identity, reuseAfterSignal: true}
	factory := &fakeFactory{opened: process}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41273": {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{})

	if _, err := supervisor.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if _, err := store.Load(); !errors.Is(err, ErrDescriptorNotFound) {
		t.Fatalf("Stop() retained descriptor after PID reuse: %v", err)
	}
}

func TestSupervisorStopRetainsDescriptorWhenProcessNeverExits(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(74)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41274")
	process := &fakeProcess{identity: identity, neverExit: true}
	factory := &fakeFactory{opened: process}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41274": {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, &fakeLocker{}, factory, &fakeAllocator{})
	supervisor.config.StopWait = 30 * time.Millisecond

	_, err := supervisor.Stop(context.Background())
	if err == nil {
		t.Fatal("Stop() error = nil, want wait timeout")
	}
	if _, loadErr := store.Load(); loadErr != nil {
		t.Fatalf("Stop() removed descriptor despite wait failure: %v", loadErr)
	}
	if _, keyErr := os.Stat(store.APIKeyPath()); keyErr != nil {
		t.Fatalf("Stop() removed API key despite wait failure: %v", keyErr)
	}
}

func TestSupervisorStopHoldsBundleLockUntilWaitCompletes(t *testing.T) {
	store := testStore(t)
	identity := testIdentity(75)
	saveDescriptor(t, store, identity, "http://127.0.0.1:41275")
	process := &fakeProcess{identity: identity, exitAfter: 80 * time.Millisecond}
	locker := &holdLocker{}
	factory := &fakeFactory{opened: process}
	supervisor := testSupervisor(t, store, &scriptedClient{responses: map[string][]readinessResult{
		"http://127.0.0.1:41275": {{identity: runtimeIdentity()}},
	}}, &fakeRuntimeVerifier{}, locker, factory, &fakeAllocator{})
	supervisor.config.StopWait = 200 * time.Millisecond

	stopErr := make(chan error, 1)
	go func() {
		_, err := supervisor.Stop(context.Background())
		stopErr <- err
	}()
	time.Sleep(20 * time.Millisecond)
	if !locker.holding.Load() {
		t.Fatal("Stop() released bundle lock before WaitExited completed")
	}
	blocked, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if unlock, err := locker.Lock(blocked, testRuntime().BundleID); err == nil {
		unlock()
		t.Fatal("second Lock acquired while Stop still waiting for exit")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Lock error = %v, want deadline exceeded", err)
	}
	if err := <-stopErr; err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if locker.holding.Load() {
		t.Fatal("Stop() retained bundle lock after completion")
	}
}

func TestNewSupervisorRejectsUntrustedRuntimeShape(t *testing.T) {
	config := testConfig(testStore(t), &scriptedClient{}, &fakeRuntimeVerifier{}, &fakeLocker{}, &fakeFactory{}, &fakeAllocator{})
	config.Runtime.Alias = "not-the-bundle"
	if _, err := NewSupervisor(config); err == nil {
		t.Fatal("NewSupervisor() accepted an alias unrelated to its bundle")
	}
	config.Runtime = testRuntime()
	config.Runtime.Dimension = 768
	if _, err := NewSupervisor(config); err == nil {
		t.Fatal("NewSupervisor() accepted a non-1024 embedding dimension")
	}
	config.Runtime = testRuntime()
	config.Runtime.RuntimePath = "llama-server"
	if _, err := NewSupervisor(config); err == nil {
		t.Fatal("NewSupervisor() accepted a PATH-resolved runtime")
	}
}

func testSupervisor(t *testing.T, store Store, client ReadinessClient, verifier RuntimeVerifier, locker StartLocker, factory Factory, allocator EndpointAllocator) *Supervisor {
	t.Helper()
	supervisor, err := NewSupervisor(testConfig(store, client, verifier, locker, factory, allocator))
	if err != nil {
		t.Fatal(err)
	}
	return supervisor
}

func testConfig(store Store, client ReadinessClient, verifier RuntimeVerifier, locker StartLocker, factory Factory, allocator EndpointAllocator) SupervisorConfig {
	return SupervisorConfig{
		Store:             store,
		Runtime:           testRuntime(),
		Verifier:          verifier,
		Client:            client,
		Allocator:         allocator,
		Locker:            locker,
		Factory:           factory,
		Now:               func() time.Time { return time.Date(2026, time.July, 16, 4, 0, 0, 0, time.UTC) },
		StartTries:        2,
		ReadyWait:         40 * time.Millisecond,
		ReadyPollInterval: 5 * time.Millisecond,
	}
}

func testRuntime() RuntimeSpec {
	return RuntimeSpec{
		BundleID:        "bundle-a",
		RuntimePath:     "/trusted/bundle-a/llama-server",
		ModelPath:       "/trusted/bundle-a/model.gguf",
		WorkingDir:      "/trusted/bundle-a",
		Alias:           "bundle-a",
		Dimension:       expectedEmbeddingDimension,
		LlamaAppVersion: "b123",
		RuntimeSHA256:   "runtime-sha",
		ChipVariant:     "apple-silicon",
		ModelSHA256:     "model-sha",
		SupportLevel:    "verified",
	}
}

func testIdentity(pid int) Identity {
	return Identity{
		PID:       pid,
		StartedAt: time.Date(2026, time.July, 16, 3, 0, pid%60, 0, time.UTC),
	}
}

func runtimeIdentity() RuntimeIdentity {
	return RuntimeIdentity{
		Alias:     testRuntime().Alias,
		Dimension: expectedEmbeddingDimension,
	}
}

func saveDescriptor(t *testing.T, store Store, identity Identity, endpoint string) {
	t.Helper()
	if _, err := store.GenerateAPIKey(); err != nil {
		t.Fatal(err)
	}
	runtime := testRuntime()
	if err := store.Save(Descriptor{
		PID:             identity.PID,
		StartTime:       identity.StartedAt,
		Endpoint:        endpoint,
		LlamaAppVersion: runtime.LlamaAppVersion,
		RuntimeSHA256:   runtime.RuntimeSHA256,
		ChipVariant:     runtime.ChipVariant,
		ModelSHA256:     runtime.ModelSHA256,
		Alias:           runtime.Alias,
		LastSuccess:     identity.StartedAt,
		Readiness:       StateReady,
	}); err != nil {
		t.Fatal(err)
	}
}

type readinessResult struct {
	identity RuntimeIdentity
	err      error
}

type scriptedClient struct {
	calls     []string
	responses map[string][]readinessResult
}

func (c *scriptedClient) Readiness(_ context.Context, endpoint, _ string) (RuntimeIdentity, error) {
	c.calls = append(c.calls, endpoint)
	results := c.responses[endpoint]
	if len(results) == 0 {
		return RuntimeIdentity{}, &TransportError{Err: errors.New("no scripted readiness response")}
	}
	result := results[0]
	c.responses[endpoint] = results[1:]
	return result.identity, result.err
}

type fakeAllocator struct {
	addresses []string
	calls     int
	err       error
}

func (f *fakeAllocator) Allocate(context.Context) (string, error) {
	f.calls++
	if f.err != nil {
		return "", f.err
	}
	if len(f.addresses) == 0 {
		return "", errors.New("no endpoint available")
	}
	address := f.addresses[0]
	f.addresses = f.addresses[1:]
	return address, nil
}

type fakeLocker struct {
	calls int
	err   error
}

func (f *fakeLocker) Lock(context.Context, string) (func(), error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return func() {}, nil
}

type holdLocker struct {
	mu      sync.Mutex
	holding atomic.Bool
	calls   int
}

func (l *holdLocker) Lock(ctx context.Context, _ string) (func(), error) {
	acquired := make(chan struct{})
	go func() {
		l.mu.Lock()
		close(acquired)
	}()
	select {
	case <-ctx.Done():
		go func() {
			<-acquired
			l.mu.Unlock()
		}()
		return nil, ctx.Err()
	case <-acquired:
	}
	l.calls++
	l.holding.Store(true)
	return func() {
		l.holding.Store(false)
		l.mu.Unlock()
	}, nil
}

type fakeFactory struct {
	next      []Identity
	processes []*fakeProcess
	commands  []Command
	opened    *fakeProcess
	newCalls  int
	openCalls int
	openErr   error
}

func (f *fakeFactory) New(command Command) Process {
	f.newCalls++
	f.commands = append(f.commands, command)
	identity := testIdentity(100 + f.newCalls)
	if len(f.next) != 0 {
		identity = f.next[0]
		f.next = f.next[1:]
	}
	process := &fakeProcess{identity: identity}
	f.processes = append(f.processes, process)
	return process
}

func (f *fakeFactory) Open(identity Identity) (Process, error) {
	f.openCalls++
	if f.openErr != nil {
		return nil, f.openErr
	}
	if f.opened == nil {
		f.opened = &fakeProcess{identity: identity}
	}
	return f.opened, nil
}

type fakeProcess struct {
	identity         Identity
	started          bool
	released         bool
	signals          []os.Signal
	err              error
	waitCalls        int
	exitAfter        time.Duration
	neverExit        bool
	reuseAfterSignal bool
	signaledAt       time.Time
}

func (f *fakeProcess) Start(context.Context) error {
	f.started = true
	return f.err
}

func (f *fakeProcess) Identity() (Identity, error) {
	if !f.started && f.identity.PID == 0 {
		return Identity{}, errors.New("process has not started")
	}
	return f.identity, nil
}

func (f *fakeProcess) Signal(signal os.Signal) error {
	f.signals = append(f.signals, signal)
	f.signaledAt = time.Now()
	if f.reuseAfterSignal {
		f.identity.StartedAt = f.identity.StartedAt.Add(time.Second)
	}
	return nil
}

func (f *fakeProcess) WaitExited(ctx context.Context) error {
	f.waitCalls++
	if f.neverExit {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.reuseAfterSignal {
		return nil
	}
	if f.exitAfter <= 0 {
		return nil
	}
	timer := time.NewTimer(f.exitAfter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (f *fakeProcess) Release() error {
	f.released = true
	return nil
}
