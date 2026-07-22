//go:build !darwin

package daemon

import (
	"context"
	"errors"
)

func RunWorkerWatch(context.Context) error {
	return errors.New("semantic worker watchdog is unsupported on this platform")
}
