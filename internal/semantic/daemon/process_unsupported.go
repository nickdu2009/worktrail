//go:build !darwin

package daemon

import (
	"context"
	"os"
)

// NewFactory returns the no-side-effect process factory for unsupported
// platforms.
func NewFactory() Factory {
	return unsupportedFactory{}
}

type unsupportedFactory struct{}

func (unsupportedFactory) New(Command) Process {
	return unsupportedProcess{}
}

func (unsupportedFactory) Open(Identity) (Process, error) {
	return unsupportedProcess{}, ErrProcessUnsupported
}

type unsupportedProcess struct{}

func (unsupportedProcess) Start(context.Context) error {
	return ErrProcessUnsupported
}

func (unsupportedProcess) Identity() (Identity, error) {
	return Identity{}, ErrProcessUnsupported
}

func (unsupportedProcess) Signal(os.Signal) error {
	return ErrProcessUnsupported
}

func (unsupportedProcess) Release() error {
	return nil
}
