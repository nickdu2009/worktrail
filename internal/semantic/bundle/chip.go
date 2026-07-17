package bundle

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

var (
	// ErrUnsupportedPlatform indicates that semantic runtime selection cannot
	// run on the current platform or detected processor.
	ErrUnsupportedPlatform = errors.New("semantic platform unsupported")
	// ErrUnsupportedChip indicates that a caller supplied a non-M1–M5 chip.
	ErrUnsupportedChip = errors.New("semantic chip unsupported")
)

var appleChipPattern = regexp.MustCompile(`(?i)\bapple\s+m([1-5])\b`)

// SysctlProbe reads a single sysctl value. It is injectable so chip
// normalization is testable without a shell, PATH lookup, or real hardware.
type SysctlProbe func(name string) (string, error)

func detectDarwinChip(probe SysctlProbe) (string, error) {
	if probe == nil {
		return "", fmt.Errorf("%w: sysctl probe is required", ErrUnsupportedPlatform)
	}
	value, err := probe("machdep.cpu.brand_string")
	if err != nil {
		return "", fmt.Errorf("%w: read CPU brand: %v", ErrUnsupportedPlatform, err)
	}
	return normalizeDarwinChip(value)
}

func normalizeDarwinChip(value string) (string, error) {
	match := appleChipPattern.FindStringSubmatch(strings.TrimSpace(value))
	if len(match) != 2 {
		return "", fmt.Errorf("%w: %q", ErrUnsupportedPlatform, value)
	}
	return "m" + match[1], nil
}

func isSupportedDarwinChip(chip string) bool {
	switch chip {
	case "m1", "m2", "m3", "m4", "m5":
		return true
	default:
		return false
	}
}
