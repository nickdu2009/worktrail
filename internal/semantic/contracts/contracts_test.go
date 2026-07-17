package contracts

import "testing"

func TestParseMode(t *testing.T) {
	for _, want := range []Mode{ModeLexical, ModeAuto, ModeRequired} {
		got, err := ParseMode(string(want))
		if err != nil {
			t.Fatalf("ParseMode(%q): %v", want, err)
		}
		if got != want {
			t.Fatalf("ParseMode(%q) = %q, want %q", want, got, want)
		}
	}
	if _, err := ParseMode("required "); err == nil {
		t.Fatal("expected invalid mode error")
	}
}
