//go:build darwin

package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var errSocketActive = errors.New("semantic service socket is already active")

func listenOwnedSocket(path string, uid int) (net.Listener, error) {
	listener, err := net.Listen("unix", path)
	if err == nil {
		if err := os.Chmod(path, 0o600); err != nil {
			listener.Close()
			return nil, err
		}
		return peerListener{Listener: listener, uid: uid}, nil
	}
	if !errors.Is(err, syscall.EADDRINUSE) {
		return nil, err
	}
	if staleErr := staleOwnedSocket(path, uid); staleErr != nil {
		return nil, staleErr
	}
	if err := os.Remove(path); err != nil {
		return nil, err
	}
	listener, err = net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		listener.Close()
		return nil, err
	}
	return peerListener{Listener: listener, uid: uid}, nil
}

func staleOwnedSocket(path string, uid int) error {
	if err := secureSocketDirectory(filepath.Dir(path), uid); err != nil {
		return err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return errors.New("semantic service socket path is not a socket")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return errors.New("semantic service socket is not owned by the current user")
	}
	connection, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		connection.Close()
		return errSocketActive
	}
	return nil
}

func secureSocketDirectory(path string, uid int) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm() != 0o700 {
		return errors.New("semantic service socket directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(stat.Uid) != uid {
		return errors.New("semantic service socket directory owner is unsafe")
	}
	return nil
}

func removeOwnedSocket(path string, uid int) error {
	_, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := staleOwnedSocket(path, uid); err != nil {
		return err
	}
	return os.Remove(path)
}

type peerListener struct {
	net.Listener
	uid int
}

func (l peerListener) Accept() (net.Conn, error) {
	for {
		connection, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if err := verifyPeerUID(connection, l.uid); err == nil {
			return connection, nil
		}
		_ = connection.Close()
	}
}

func verifyPeerUID(connection net.Conn, expected int) error {
	unixConnection, ok := connection.(*net.UnixConn)
	if !ok {
		return errors.New("semantic service peer is not a Unix connection")
	}
	raw, err := unixConnection.SyscallConn()
	if err != nil {
		return err
	}
	var uid uint32
	var controlErr error
	if err := raw.Control(func(fd uintptr) {
		var credential *unix.Xucred
		credential, controlErr = unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		if controlErr == nil {
			uid = credential.Uid
		}
	}); err != nil {
		return err
	}
	if controlErr != nil {
		return controlErr
	}
	if int(uid) != expected {
		return fmt.Errorf("semantic service peer uid %d does not match %d", uid, expected)
	}
	return nil
}

func withActivationLock(ctx context.Context, path string, fn func() error) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return err
	}
	if err := secureSocketDirectory(directory, os.Getuid()); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			defer unix.Flock(int(file.Fd()), unix.LOCK_UN)
			return fn()
		}
		if !errors.Is(err, unix.EWOULDBLOCK) && !errors.Is(err, unix.EAGAIN) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
