package textsafety

import (
	"errors"
	"strings"
	"testing"
)

func TestSemanticFieldIssuesBlockedAndTranscript(t *testing.T) {
	issues := SemanticFieldIssues("body", "-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----", Options{CheckBlocked: true})
	if len(issues) != 1 || issues[0].Code != "body_blocked_sensitive_material" {
		t.Fatalf("blocked issues = %#v", issues)
	}

	issues = SemanticFieldIssues("body", "# Draft\n\n- user: hi\n- assistant: hello", Options{CheckTranscript: true})
	if len(issues) != 1 || issues[0].Code != "body_raw_transcript_style_conversation" {
		t.Fatalf("transcript issues = %#v", issues)
	}
}

func TestSemanticFieldIssuesLocalPathAndPII(t *testing.T) {
	issues := SemanticFieldIssues("summary", "Contact nick@example.com at /Users/tester/private.txt", Options{CheckBlocked: true, CheckTranscript: true})
	codes := Codes(issues)
	for _, want := range []string{"summary_redactable_secret_or_pii", "summary_local_absolute_path"} {
		if !containsString(codes, want) {
			t.Fatalf("missing code %q in %#v", want, codes)
		}
	}
}

func TestValidationErrorCodesAndMessages(t *testing.T) {
	err := NewValidationError([]Issue{
		{Field: "reason", Code: "reason_required", Message: "reason is required"},
	})
	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	if got := strings.Join(validationErr.Codes(), ","); got != "reason_required" {
		t.Fatalf("codes = %q", got)
	}
	if got := strings.Join(Messages(validationErr.Issues), "; "); got != "reason is required" {
		t.Fatalf("messages = %q", got)
	}
}

func TestFieldCodeAndRequiredFieldIssue(t *testing.T) {
	if got := FieldCode("reason", "required"); got != "reason_required" {
		t.Fatalf("FieldCode = %q", got)
	}
	issue := RequiredFieldIssue("reason")
	if issue.Code != "reason_required" || issue.Field != "reason" {
		t.Fatalf("RequiredFieldIssue = %+v", issue)
	}
}

func TestProcessRejectsBlockedSecretsForBothProfiles(t *testing.T) {
	for _, profile := range []Profile{ProfileLocal, ProfileTeam} {
		if _, err := Process("-----BEGIN OPENSSH PRIVATE KEY-----\nsecret\n-----END OPENSSH PRIVATE KEY-----", profile); !errors.Is(err, ErrBlockedContent) {
			t.Fatalf("profile %s: expected blocked content, got %v", profile, err)
		}
	}
}

func TestProcessRedactsLocalButRejectsTeamContent(t *testing.T) {
	input := "Contact nick@example.com from /Users/nick/private.txt"
	local, err := Process(input, ProfileLocal)
	if err != nil {
		t.Fatal(err)
	}
	if !local.Redacted || strings.Contains(local.Text, "nick@example.com") || strings.Contains(local.Text, "/Users/") {
		t.Fatalf("local result = %+v", local)
	}
	if _, err := Process(input, ProfileTeam); !errors.Is(err, ErrTeamUnsafeContent) {
		t.Fatalf("expected team rejection, got %v", err)
	}
}

func TestProcessRejectsDiffAndRawTranscriptForTeam(t *testing.T) {
	for _, input := range []string{
		"diff --git a/a.go b/a.go\n@@ -1 +1 @@",
		`{"role":"user","message":"raw"}`,
		`{"role":"assistant","content":"raw"}`,
	} {
		if _, err := Process(input, ProfileTeam); !errors.Is(err, ErrTeamUnsafeContent) {
			t.Fatalf("expected team rejection for %q, got %v", input, err)
		}
	}
}

func TestProcessCoversPortableLocalPathVariants(t *testing.T) {
	for _, input := range []string{
		"/private/var/tmp/secret",
		"/Volumes/External/private",
		`C:\Users\alice\private`,
		"C:/Users/alice/private",
	} {
		local, err := Process(input, ProfileLocal)
		if err != nil {
			t.Fatal(err)
		}
		if !local.Redacted || strings.Contains(local.Text, input) {
			t.Fatalf("path %q was not redacted: %+v", input, local)
		}
		if _, err := Process(input, ProfileTeam); !errors.Is(err, ErrTeamUnsafeContent) {
			t.Fatalf("team path %q error = %v", input, err)
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
