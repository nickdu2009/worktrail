//go:build darwin && launchdgate

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestLaunchdHostGate(t *testing.T) {
	if os.Getenv("WORKTRAIL_LAUNCHD_GATE") != "1" {
		t.Skip("focused launchd gate is not enabled")
	}
	home := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_HOME")
	binary := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_BINARY")
	label := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_LABEL")
	bundleID := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_BUNDLE_ID")
	roots := paths.SemanticRoots{
		Cache:   filepath.Join(home, "Library", "Caches", "worktrail", "semantic"),
		Runtime: filepath.Join(home, "Library", "Application Support", "worktrail", "semantic"),
		Logs:    filepath.Join(home, "Library", "Logs", "worktrail", "semantic"),
	}
	manager := Manager{
		Roots: roots, Label: label, UID: os.Getuid(),
		PlistPath:   filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		Environment: map[string]string{"HOME": home},
	}
	defer func() { _ = manager.Remove(context.Background()) }()

	if err := manager.Install(context.Background(), binary); err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if err := manager.Install(context.Background(), binary); err != nil {
		t.Fatalf("idempotent Install() error = %v", err)
	}
	client, err := NewClient(roots, bundleID, "", "verified", "m1")
	if err != nil {
		t.Fatal(err)
	}
	client.Manager = manager
	cold, err := client.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cold.HostState != "stopped" || cold.WorkerState != "idle_stopped" {
		t.Fatalf("cold status = %#v", cold)
	}
	if _, err := os.Stat(roots.ServiceSocket()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("status activated Host: %v", err)
	}

	const clients = 20
	var wait sync.WaitGroup
	errorsFound := make(chan error, clients)
	for index := 0; index < clients; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := client.Start(context.Background()); err != nil {
				errorsFound <- err
			}
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Fatalf("concurrent Start() error = %v", err)
	}
	ready, err := client.Status(context.Background())
	if err != nil || ready.WorkerPID <= 0 || ready.HostState != "ready" {
		t.Fatalf("ready status = %#v, err=%v", ready, err)
	}
	hostGroup, hostGroupErr := syscall.Getpgid(ready.HostPID)
	workerGroup, workerGroupErr := syscall.Getpgid(ready.WorkerPID)
	if ready.HostPID <= 0 || hostGroupErr != nil || workerGroupErr != nil || hostGroup != workerGroup {
		t.Fatalf("Host/worker process groups = host(pid=%d pgid=%d err=%v) worker(pid=%d pgid=%d err=%v)", ready.HostPID, hostGroup, hostGroupErr, ready.WorkerPID, workerGroup, workerGroupErr)
	}
	firstWorker := ready.WorkerPID

	command := exec.Command(binary, "semantic", "status", "--format=json")
	command.Env = append(os.Environ(), "HOME="+home)
	output, err := command.Output()
	if err != nil {
		t.Fatalf("second CLI status error = %v", err)
	}
	var cliStatus map[string]any
	if err := json.Unmarshal(output, &cliStatus); err != nil || cliStatus["worker_pid"] != float64(firstWorker) {
		t.Fatalf("second CLI did not reuse worker: %s, err=%v", output, err)
	}

	process, err := os.FindProcess(firstWorker)
	if err != nil || process.Signal(syscall.SIGKILL) != nil {
		t.Fatalf("kill worker error = %v", err)
	}
	if _, err := client.CountTokens(context.Background(), "worker crash recovery"); err != nil {
		t.Fatalf("single worker recovery error = %v", err)
	}
	recovered, err := client.Status(context.Background())
	if err != nil || recovered.WorkerPID <= 0 || recovered.WorkerPID == firstWorker {
		t.Fatalf("recovered status = %#v, err=%v", recovered, err)
	}

	canonicalRuntime, err := filepath.EvalSymlinks(filepath.Dir(roots.ServiceSocket()))
	if err != nil {
		t.Fatal(err)
	}
	canonicalSocket := filepath.Join(canonicalRuntime, filepath.Base(roots.ServiceSocket()))
	sandboxProfile := fmt.Sprintf(
		"(version 1) (allow default) (deny network-outbound) (allow network-outbound (remote unix-socket (path-literal %q)))",
		canonicalSocket,
	)
	blocked := exec.Command("/usr/bin/sandbox-exec", "-p", sandboxProfile, os.Args[0], "-test.run", "^TestLaunchdHostGateBlockedOutboundClient$")
	blocked.Env = append(os.Environ(), "WORKTRAIL_LAUNCHD_GATE_CLIENT_ONLY=1")
	if output, err := blocked.CombinedOutput(); err != nil {
		t.Fatalf("blocked-outbound client failed: %v\n%s", err, output)
	}

	waitDuration := 75 * time.Second
	if configured := os.Getenv("WORKTRAIL_LAUNCHD_GATE_IDLE_WAIT"); configured != "" {
		parsed, err := time.ParseDuration(configured)
		if err != nil {
			t.Fatal(err)
		}
		waitDuration = parsed
	}
	deadline := time.Now().Add(waitDuration)
	for time.Now().Before(deadline) && (socketExists(roots.ServiceSocket()) || processAlive(recovered.WorkerPID)) {
		time.Sleep(250 * time.Millisecond)
	}
	if socketExists(roots.ServiceSocket()) || processAlive(recovered.WorkerPID) {
		t.Fatalf("idle timeout retained Host or worker: socket=%t worker=%t", socketExists(roots.ServiceSocket()), processAlive(recovered.WorkerPID))
	}
	if _, err := client.CountTokens(context.Background(), "idle cold restart"); err != nil {
		t.Fatalf("idle cold restart error = %v", err)
	}
	beforeHostKill, _ := client.Status(context.Background())
	if err := exec.Command("/bin/launchctl", "kill", "SIGKILL", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run(); err != nil {
		t.Fatalf("kill Host error = %v", err)
	}
	// launchd applies the plist ExitTimeOut (10s) before it guarantees cleanup
	// of remaining processes in the dead job's process group. Keep a bounded
	// scheduling margin while still requiring the old worker to disappear. A
	// prior kickstart demand may cause launchd to replace the Host immediately,
	// so socket presence is not itself an orphan signal.
	hostKillDeadline := time.Now().Add(25 * time.Second)
	for time.Now().Before(hostKillDeadline) && processAlive(beforeHostKill.WorkerPID) {
		time.Sleep(100 * time.Millisecond)
	}
	if processAlive(beforeHostKill.WorkerPID) {
		t.Fatalf("Host SIGKILL cleanup retained old worker PID %d", beforeHostKill.WorkerPID)
	}
	if _, err := client.CountTokens(context.Background(), "Host crash recovery"); err != nil {
		t.Fatalf("Host crash recovery error = %v", err)
	}
	if err := manager.Remove(context.Background()); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if err := manager.Remove(context.Background()); err != nil {
		t.Fatalf("idempotent Remove() error = %v", err)
	}
}

func TestLaunchdHostGateBlockedOutboundClient(t *testing.T) {
	if os.Getenv("WORKTRAIL_LAUNCHD_GATE_CLIENT_ONLY") != "1" {
		t.Skip("client-only launchd probe is not enabled")
	}
	home := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_HOME")
	bundleID := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_BUNDLE_ID")
	roots := paths.SemanticRoots{
		Cache:   filepath.Join(home, "Library", "Caches", "worktrail", "semantic"),
		Runtime: filepath.Join(home, "Library", "Application Support", "worktrail", "semantic"),
		Logs:    filepath.Join(home, "Library", "Logs", "worktrail", "semantic"),
	}
	client, err := NewClient(roots, bundleID, "", "verified", "m1")
	if err != nil {
		t.Fatal(err)
	}
	label := requiredGateEnv(t, "WORKTRAIL_LAUNCHD_GATE_LABEL")
	client.Manager = Manager{
		Roots: roots, Label: label, UID: os.Getuid(),
		PlistPath:   filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		Environment: map[string]string{"HOME": home},
	}
	if _, err := client.CountTokens(context.Background(), "blocked outbound"); err != nil {
		t.Fatal(err)
	}
}

func requiredGateEnv(t *testing.T, key string) string {
	t.Helper()
	value := os.Getenv(key)
	if value == "" {
		t.Fatalf("%s is required", key)
	}
	return value
}

func socketExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func processAlive(pid int) bool {
	return pid > 0 && syscall.Kill(pid, 0) == nil
}

func waitUntil(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("timed out waiting for launchd gate condition after " + strconv.FormatInt(timeout.Milliseconds(), 10) + "ms")
}
