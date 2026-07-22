//go:build darwin

package service

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestManagerInstallWritesOwnedPlistAndRollsBack(t *testing.T) {
	roots := serviceTestRoots(t)
	base := t.TempDir()
	executable := filepath.Join(base, "worktrail")
	if err := os.WriteFile(executable, []byte("binary"), 0o700); err != nil {
		t.Fatal(err)
	}
	var commands [][]string
	manager := Manager{
		Roots: roots, Label: "com.example.worktrail.semantic.test", UID: os.Getuid(),
		PlistPath: filepath.Join(base, "LaunchAgents", "semantic.plist"),
		run: func(_ context.Context, args ...string) error {
			commands = append(commands, append([]string(nil), args...))
			return nil
		},
	}
	rollback, err := manager.InstallReversible(context.Background(), executable)
	if err != nil {
		t.Fatalf("InstallReversible() error = %v", err)
	}
	plist, err := os.ReadFile(manager.PlistPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"<key>RunAtLoad</key><false/>", "<key>KeepAlive</key><false/>",
		"<key>ProcessType</key><string>Standard</string>",
		"<key>ExitTimeOut</key><integer>10</integer>",
		"<key>AbandonProcessGroup</key><false/>",
		"<string>semantic</string><string>host</string><string>--launchd</string>",
	} {
		if !strings.Contains(string(plist), required) {
			t.Fatalf("plist does not contain %q: %s", required, plist)
		}
	}
	assertServiceMode(t, manager.PlistPath, 0o644)
	assertServiceMode(t, roots.ServiceMetadata(), 0o600)
	wantCommands := [][]string{
		{"print", manager.domain()},
		{"bootout", manager.target()},
		{"bootstrap", manager.domain(), manager.PlistPath},
	}
	if !reflect.DeepEqual(commands, wantCommands) {
		t.Fatalf("launchctl commands = %#v, want %#v", commands, wantCommands)
	}
	if err := rollback(context.Background()); err != nil {
		t.Fatalf("rollback() error = %v", err)
	}
	for _, path := range []string{manager.PlistPath, roots.ServiceMetadata()} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("rollback retained %s: %v", path, err)
		}
	}
}

func TestVerifyPeerUIDUsesDarwinPeerCredentials(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "worktrail-peer-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "peer.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, _ := listener.Accept()
		accepted <- connection
	}()
	client, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	server := <-accepted
	defer server.Close()
	if err := verifyPeerUID(server, os.Getuid()); err != nil {
		t.Fatalf("verifyPeerUID(current) error = %v", err)
	}
	if err := verifyPeerUID(server, os.Getuid()+1); err == nil {
		t.Fatal("verifyPeerUID(wrong) error = nil")
	}
}

func TestWaitRemoveOwnedSocketWaitsForActiveListenerShutdown(t *testing.T) {
	directory, err := os.MkdirTemp("/tmp", "worktrail-remove-socket-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	path := filepath.Join(directory, "semantic.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = listener.Close()
	}()
	if err := waitRemoveOwnedSocket(context.Background(), path, os.Getuid()); err != nil {
		t.Fatalf("waitRemoveOwnedSocket() error = %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("socket still exists after listener shutdown: %v", err)
	}
}
