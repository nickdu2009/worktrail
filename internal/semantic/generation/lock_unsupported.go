//go:build !darwin && !linux

package generation

import (
	"context"
	"os"
)

func acquireLockedFile(_ context.Context, _ string, _ LeaseMode) (*os.File, error) {
	return nil, ErrLeaseUnsupported
}

func releaseLockedFile(_ *os.File) error {
	return ErrLeaseUnsupported
}
