//go:build !darwin

package daemon

import "context"

// NewStartLocker returns a no-side-effect unsupported locker on non-Darwin
// platforms. The root is still validated to preserve the construction contract.
func NewStartLocker(root string) (StartLocker, error) {
	if err := validateLockRoot(root); err != nil {
		return nil, err
	}
	return unsupportedStartLocker{}, nil
}

type unsupportedStartLocker struct{}

func (unsupportedStartLocker) Lock(context.Context, string) (func(), error) {
	return nil, ErrProcessUnsupported
}

// NewEndpointAllocator returns a no-side-effect unsupported allocator on
// platforms where semantic runtime supervision is unsupported.
func NewEndpointAllocator() EndpointAllocator {
	return unsupportedEndpointAllocator{}
}

type unsupportedEndpointAllocator struct{}

func (unsupportedEndpointAllocator) Allocate(context.Context) (string, error) {
	return "", ErrProcessUnsupported
}
