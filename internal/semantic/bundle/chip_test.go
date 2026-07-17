package bundle

import (
	"errors"
	"runtime"
	"testing"
)

func TestNormalizeDarwinChip(t *testing.T) {
	for _, test := range []struct {
		value string
		want  string
	}{
		{value: "Apple M1 Pro", want: "m1"},
		{value: "Apple M2 Max", want: "m2"},
		{value: "Apple M3 Ultra", want: "m3"},
		{value: "Apple M4", want: "m4"},
		{value: "Apple M5 Pro", want: "m5"},
	} {
		t.Run(test.value, func(t *testing.T) {
			got, err := normalizeDarwinChip(test.value)
			if err != nil {
				t.Fatalf("normalizeDarwinChip() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("normalizeDarwinChip() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestNormalizeDarwinChipRejectsUnsupportedProcessor(t *testing.T) {
	for _, value := range []string{"Apple M6", "Apple A18 Pro", "Intel(R) Core(TM)", "M1"} {
		if _, err := normalizeDarwinChip(value); !errors.Is(err, ErrUnsupportedPlatform) {
			t.Fatalf("normalizeDarwinChip(%q) error = %v, want ErrUnsupportedPlatform", value, err)
		}
	}
}

func TestDetectDarwinChipUsesInjectedProbe(t *testing.T) {
	got, err := detectDarwinChip(func(name string) (string, error) {
		if name != "machdep.cpu.brand_string" {
			t.Fatalf("sysctl name = %q", name)
		}
		return "Apple M4 Max", nil
	})
	if err != nil {
		t.Fatalf("detectDarwinChip() error = %v", err)
	}
	if got != "m4" {
		t.Fatalf("detectDarwinChip() = %q, want m4", got)
	}
}

func TestDetectDarwinChipOnNonDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("non-Darwin behavior is covered by cross-compilation")
	}
	if _, err := DetectDarwinChip(); !errors.Is(err, ErrUnsupportedPlatform) {
		t.Fatalf("DetectDarwinChip() error = %v, want ErrUnsupportedPlatform", err)
	}
}
