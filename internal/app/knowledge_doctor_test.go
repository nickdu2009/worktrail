package app

import "testing"

func TestRequirementHeadingSignalCount(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{
			name: "headings carry mvp scope and acceptance criteria",
			body: "# Title\n\n## MVP Constraints Preserved\n\nbody\n\n## Acceptance Criteria\n\nbody\n",
			want: 2,
		},
		{
			name: "single signal repeated across multiple headings",
			body: "# Title\n\n## 15. MVP 范围\n\nx\n\n### 15.1 MVP 必须做\n\ny\n\n### 15.2 MVP 可以不做\n\nz\n\n## MVP 边界\n\nw\n",
			want: 4,
		},
		{
			name: "signals only in body do not count",
			body: "# Title\n\nAll agents must preserve the MVP scope guard. The primary user is X. Acceptance criteria are documented elsewhere.\n\n## Implementation\n\nbody\n",
			want: 0,
		},
		{
			name: "persona substring should not match personal-accident or personality",
			body: "# Title\n\n## personal-accident product\n\nbody\n\n## personality-driven branding\n\nbody\n",
			want: 0,
		},
		{
			name: "persona as a real word in heading counts",
			body: "# Title\n\n## User personas\n\nbody\n",
			want: 1,
		},
		{
			name: "H1 title is ignored",
			body: "# Delivery Case As Primary User Entrypoint\n\nbody\n",
			want: 0,
		},
		{
			name: "H5 and deeper headings are ignored",
			body: "# Title\n\n##### MVP details\n\nbody\n",
			want: 0,
		},
		{
			name: "table column user goal in body does not count",
			body: "# Title\n\n## Workflow\n\n| Step | User goal | AI behavior |\n| --- | --- | --- |\n| 1 | win | ack |\n",
			want: 0,
		},
		{
			name: "out-of-scope and out of scope both detected",
			body: "# Title\n\n## Out of scope\n\nbody\n\n## Out-of-scope clarifications\n\nbody\n",
			want: 2,
		},
		{
			name: "heading containing both mvp and acceptance criteria counts once",
			body: "# Title\n\n## MVP acceptance criteria summary\n\nbody\n",
			want: 1,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := requirementHeadingSignalCount(tc.body)
			if got != tc.want {
				t.Fatalf("requirementHeadingSignalCount: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestRequirementHeadingSignalCountTolerantOfCaseAndWhitespace(t *testing.T) {
	body := "# Title\n\n##    MVP   Scope   \n\nbody\n\n##\tAcceptance Criteria\n\nbody\n"
	if got := requirementHeadingSignalCount(body); got != 2 {
		t.Fatalf("expected 2 heading signal hits, got %d", got)
	}
}
