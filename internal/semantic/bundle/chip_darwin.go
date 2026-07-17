//go:build darwin

package bundle

import "golang.org/x/sys/unix"

// DetectDarwinChip returns the canonical m1–m5 identifier for the local
// Apple-silicon processor. It only uses Darwin's sysctl API.
func DetectDarwinChip() (string, error) {
	return detectDarwinChip(unix.Sysctl)
}
