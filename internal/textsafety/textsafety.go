package textsafety

import (
	"regexp"
	"strings"

	"github.com/nickdu2009/worktrail/internal/redact"
)

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

func (e *ValidationError) Codes() []string {
	return Codes(e.Issues)
}

func (e *ValidationError) Unwrap() error {
	return e.Cause
}

func Codes(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	seen := map[string]bool{}
	for _, issue := range issues {
		code := strings.TrimSpace(issue.Code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		out = append(out, code)
	}
	return out
}

func Messages(issues []Issue) []string {
	out := make([]string, 0, len(issues))
	for _, issue := range issues {
		if issue.Message != "" {
			out = append(out, issue.Message)
		}
	}
	return out
}

func NewValidationError(issues []Issue) error {
	return WrapValidationError(nil, issues)
}

func WrapValidationError(cause error, issues []Issue) error {
	if cause != nil && len(issues) == 0 {
		return cause
	}
	if len(issues) == 0 {
		return nil
	}
	cloned := append([]Issue(nil), issues...)
	return &ValidationError{Cause: cause, Issues: cloned}
}

func SemanticFieldIssues(field, text string, opts Options) []Issue {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var errs []Issue
	scan := redact.Scan(text)
	switch scan.Status {
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
	userSeen := false
	assistantSeen := false
	turns := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		switch {
		case strings.HasPrefix(line, "- user:") || strings.HasPrefix(line, "user:"):
			userSeen = true
			turns++
		case strings.HasPrefix(line, "- assistant:") || strings.HasPrefix(line, "assistant:"):
			assistantSeen = true
			turns++
		}
	}
	return turns >= 2 && userSeen && assistantSeen
}

func ContainsLocalAbsolutePath(text string) bool {
	return localPathRE.MatchString(text)
}

func RedactLocalAbsolutePaths(text string) string {
	return localPathRE.ReplaceAllString(text, "[REDACTED:local-path]")
}

var localPathRE = regexp.MustCompile(`(?:/Users/[^\s` + "`" + `]+|/home/[^\s` + "`" + `]+|[A-Za-z]:\\Users\\[^\s` + "`" + `]+)`)

func FieldCode(field, suffix string) string {
	return issue(field, suffix, "").Code
}

func RequiredFieldIssue(field string) Issue {
	return issue(field, "required", field+" is required")
}

func issue(field, suffix, message string) Issue {
	field = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(field, " ", "_")))
	if field == "" {
		return Issue{Code: suffix, Message: message}
	}
	return Issue{
		Field:   field,
		Code:    field + "_" + suffix,
		Message: message,
	}
}
