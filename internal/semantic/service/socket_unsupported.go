//go:build !darwin

package service

import (
	"context"
	"net"
)

func listenOwnedSocket(string, int) (net.Listener, error) { return nil, unsupportedService() }
func removeOwnedSocket(string, int) error                 { return unsupportedService() }
func withActivationLock(_ context.Context, _ string, _ func() error) error {
	return unsupportedService()
}
