//go:build !darwin && !linux

package bundle

import (
	"context"
	"errors"
)

func acquireBundleLock(_ context.Context, _ string, _ string) (func(), error) {
	return nil, errors.New("semantic bundle installation lock is unsupported on this platform")
}
