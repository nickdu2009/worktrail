package app

import (
	"github.com/nickdu2009/worktrail/internal/textsafety"
)

func validateSemanticDraftText(title, summary, body string) error {
	var errs []textsafety.Issue
	errs = append(errs, textsafety.SemanticFieldIssues("title", title, textsafety.Options{CheckBlocked: true})...)
	errs = append(errs, textsafety.SemanticFieldIssues("summary", summary, textsafety.Options{CheckBlocked: true, CheckTranscript: true})...)
	errs = append(errs, textsafety.SemanticFieldIssues("body", body, textsafety.Options{CheckBlocked: true, CheckTranscript: true})...)
	return textsafety.NewValidationError(errs)
}
