package textsafety

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/nickdu2009/worktrail/internal/redact"
)

type Profile string

const (
	ProfileLocal Profile = "local"
	ProfileTeam  Profile = "team"
)

type Finding struct {
	Kind     string `json:"kind"`
	Blocked  bool   `json:"blocked"`
	Redacted bool   `json:"redacted,omitempty"`
	Message  string `json:"message"`
}

type Result struct {
	Text     string    `json:"text"`
	Findings []Finding `json:"findings,omitempty"`
	Redacted bool      `json:"redacted"`
}

type rule struct {
	kind        string
	pattern     *regexp.Regexp
	replacement string
	blocked     bool
}

var processRules = []rule{
	{kind: "private_key", pattern: regexp.MustCompile(`-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----`), blocked: true},
	{kind: "email", pattern: regexp.MustCompile(`(?i)\b[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}\b`), replacement: "[REDACTED_EMAIL]"},
	{kind: "phone", pattern: regexp.MustCompile(`(^|[^A-Za-z0-9_.-])(?:\+?\d[\d .()-]{8,}\d)`), replacement: "${1}[REDACTED_PHONE]"},
	{kind: "absolute_path", pattern: localPathRE, replacement: "[REDACTED_ABSOLUTE_PATH]"},
	{kind: "raw_transcript", pattern: regexp.MustCompile(`(?s)(?:<user_query>.*?</user_query>|"role"\s*:\s*"(?:user|assistant|tool)".{0,400}"(?:message|content)"\s*:)`), replacement: "[REDACTED_RAW_TRANSCRIPT]"},
	{kind: "diff", pattern: regexp.MustCompile(`(?m)^(?:diff --git |\+\+\+ [ab]/|--- [ab]/|@@ .+ @@)`), replacement: "[REDACTED_DIFF]"},
}

var (
	ErrBlockedContent    = errors.New("blocked content")
	ErrTeamUnsafeContent = errors.New("team-unsafe content")
)

func Process(text string, profile Profile) (Result, error) {
	if profile != ProfileLocal && profile != ProfileTeam {
		return Result{}, fmt.Errorf("unknown text safety profile %q", profile)
	}
	result := Result{Text: normalizeNewlines(text)}
	scan := redact.Scan(result.Text)
	switch scan.Status {
	case redact.StatusBlocked:
		result.Findings = append(result.Findings, Finding{Kind: scan.Findings[0].Type, Blocked: true, Message: "blocked secret-like content detected"})
		return result, fmt.Errorf("%w: %s", ErrBlockedContent, scan.Findings[0].Type)
	case redact.StatusRedacted:
		if profile == ProfileTeam {
			result.Findings = append(result.Findings, Finding{Kind: scan.Findings[0].Type, Blocked: true, Message: "team handoffs reject redactable secret or PII content"})
			return result, fmt.Errorf("%w: %s", ErrTeamUnsafeContent, scan.Findings[0].Type)
		}
		result.Text = scan.Text
		result.Redacted = true
		for _, finding := range scan.Findings {
			result.Findings = append(result.Findings, Finding{Kind: finding.Type, Redacted: true, Message: "content was redacted for local storage"})
		}
	}
	for _, candidate := range processRules {
		if !candidate.pattern.MatchString(result.Text) {
			continue
		}
		if candidate.blocked {
			result.Findings = append(result.Findings, Finding{Kind: candidate.kind, Blocked: true, Message: "blocked secret-like content detected"})
			return result, fmt.Errorf("%w: %s", ErrBlockedContent, candidate.kind)
		}
		if profile == ProfileTeam {
			result.Findings = append(result.Findings, Finding{Kind: candidate.kind, Blocked: true, Message: "team handoffs reject this content class"})
			return result, fmt.Errorf("%w: %s", ErrTeamUnsafeContent, candidate.kind)
		}
		result.Text = candidate.pattern.ReplaceAllString(result.Text, candidate.replacement)
		result.Redacted = true
		result.Findings = append(result.Findings, Finding{Kind: candidate.kind, Redacted: true, Message: "content was redacted for local storage"})
	}
	return result, nil
}

// The APIs below are shared by semantic candidate validation and retained as
// the lower-level issue-reporting surface.
type Options struct {
	CheckBlocked    bool
	CheckTranscript bool
}

type Issue struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ValidationError struct {
	Cause  error
	Issues []Issue
}

func (e *ValidationError) Error() string {
	if e.Cause != nil {
		return e.Cause.Error()
	}
	return strings.Join(Messages(e.Issues), "; ")
}

func (e *ValidationError) Codes() []string { return Codes(e.Issues) }
func (e *ValidationError) Unwrap() error   { return e.Cause }

func Codes(issues []Issue) []string {
	var out []string
	seen := map[string]bool{}
	for _, issue := range issues {
		code := strings.TrimSpace(issue.Code)
		if code != "" && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

func Messages(issues []Issue) []string {
	var out []string
	for _, issue := range issues {
		if issue.Message != "" {
			out = append(out, issue.Message)
		}
	}
	return out
}

func NewValidationError(issues []Issue) error { return WrapValidationError(nil, issues) }

func WrapValidationError(cause error, issues []Issue) error {
	if cause != nil && len(issues) == 0 {
		return cause
	}
	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Cause: cause, Issues: append([]Issue(nil), issues...)}
}

func SemanticFieldIssues(field, text string, opts Options) []Issue {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var errs []Issue
	switch redact.Scan(text).Status {
	case redact.StatusBlocked:
		if opts.CheckBlocked {
			errs = append(errs, issue(field, "blocked_sensitive_material", field+" contains blocked sensitive material"))
		}
	case redact.StatusRedacted:
		errs = append(errs, issue(field, "redactable_secret_or_pii", field+" contains redactable secret or PII pattern"))
	}
	if ContainsLocalAbsolutePath(text) {
		errs = append(errs, issue(field, "local_absolute_path", field+" contains local absolute path"))
	}
	if opts.CheckTranscript && ContainsTranscriptStyleConversation(text) {
		errs = append(errs, issue(field, "raw_transcript_style_conversation", field+" contains raw transcript-style conversation"))
	}
	return errs
}

func ContainsTranscriptStyleConversation(text string) bool {
	userSeen, assistantSeen, turns := false, false, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		switch {
		case strings.HasPrefix(line, "- user:") || strings.HasPrefix(line, "user:"):
			userSeen, turns = true, turns+1
		case strings.HasPrefix(line, "- assistant:") || strings.HasPrefix(line, "assistant:"):
			assistantSeen, turns = true, turns+1
		}
	}
	return turns >= 2 && userSeen && assistantSeen
}

func ContainsLocalAbsolutePath(text string) bool { return localPathRE.MatchString(text) }
func RedactLocalAbsolutePaths(text string) string {
	return localPathRE.ReplaceAllString(text, "[REDACTED:local-path]")
}

var localPathRE = regexp.MustCompile(`(?i)(?:(?:/Users|/home|/private|/Volumes)/[^\s` + "`" + `]+|[A-Z]:[\\/]Users[\\/][^\s` + "`" + `]+)`)

func FieldCode(field, suffix string) string { return issue(field, suffix, "").Code }
func RequiredFieldIssue(field string) Issue {
	return issue(field, "required", field+" is required")
}

func issue(field, suffix, message string) Issue {
	field = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(field, " ", "_")))
	if field == "" {
		return Issue{Code: suffix, Message: message}
	}
	return Issue{Field: field, Code: field + "_" + suffix, Message: message}
}

func normalizeNewlines(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	return strings.ReplaceAll(value, "\r", "\n")
}
