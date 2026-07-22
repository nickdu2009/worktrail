//go:build !darwin

package service

import (
	"context"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

func (Manager) Install(context.Context, string) error { return unsupportedService() }
func (Manager) InstallReversible(context.Context, string) (func(context.Context) error, error) {
	return nil, unsupportedService()
}
func (Manager) Activate(context.Context) error { return unsupportedService() }
func (Manager) Restart(context.Context) error  { return unsupportedService() }
func (Manager) Remove(context.Context) error   { return nil }
func (m Manager) Inspect(context.Context) (Inspection, error) {
	return Inspection{Domain: m.domain()}, unsupportedService()
}

func runLaunchctl(context.Context, ...string) error { return unsupportedService() }

func unsupportedService() error {
	return serviceError(contracts.ReasonPlatformUnsupported, "semantic runtime is unsupported on this platform", nil)
}
