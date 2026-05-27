package knowledge

import "testing"

func TestNormalizeLifecycle(t *testing.T) {
	tests := []struct {
		name      string
		lifecycle string
		stage     string
		status    string
		want      string
	}{
		{name: "default current", want: LifecycleCurrent},
		{name: "explicit lifecycle", lifecycle: LifecycleHistorical, want: LifecycleHistorical},
		{name: "status fallback", status: LifecycleRetired, want: LifecycleRetired},
		{name: "stage fallback", stage: LifecycleHistorical, want: LifecycleHistorical},
	}
	for _, tc := range tests {
		if got := NormalizeLifecycle(tc.lifecycle, tc.stage, tc.status); got != tc.want {
			t.Fatalf("%s: NormalizeLifecycle() = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestParseLifecycleListAndIncludesLifecycle(t *testing.T) {
	got, err := ParseLifecycleList("historical, retired, historical")
	if err != nil {
		t.Fatalf("ParseLifecycleList() error = %v", err)
	}
	if len(got) != 2 || got[0] != LifecycleHistorical || got[1] != LifecycleRetired {
		t.Fatalf("ParseLifecycleList() = %+v", got)
	}
	if !IncludesLifecycle(nil, LifecycleCurrent) || IncludesLifecycle(nil, LifecycleHistorical) {
		t.Fatalf("default lifecycle visibility should include only current")
	}
	if !IncludesLifecycle(got, LifecycleHistorical) || IncludesLifecycle(got, LifecycleCurrent) {
		t.Fatalf("IncludesLifecycle() mismatch for %+v", got)
	}
}

func TestParseLifecycleListRejectsInvalidValues(t *testing.T) {
	if _, err := ParseLifecycleList("historical, historcal"); err == nil {
		t.Fatal("ParseLifecycleList() expected invalid lifecycle error")
	}
}

func TestHasMarkdownLinkAndPathText(t *testing.T) {
	body := "See [Target](rules/example.md) and plain text rules/example.md.\n"
	if !HasMarkdownLink(body, "project.md", "rules/example.md") {
		t.Fatalf("expected markdown link match")
	}
	if !HasPathText(body, "rules/example.md") {
		t.Fatalf("expected plain text match")
	}
	if HasMarkdownLink(body, "project.md", "rules/missing.md") {
		t.Fatalf("unexpected markdown link match")
	}
}
