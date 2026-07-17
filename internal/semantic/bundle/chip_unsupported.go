//go:build !darwin

package bundle

// DetectDarwinChip is unavailable on non-Darwin targets.
func DetectDarwinChip() (string, error) {
	return "", ErrUnsupportedPlatform
}
