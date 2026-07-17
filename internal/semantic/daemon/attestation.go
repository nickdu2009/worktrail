package daemon

import (
	"errors"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

const (
	trustedBundleRequiredMessage = "trusted bundle required"
	experimentalSupportWarning   = "experimental runtime variant; verified by the mandatory installation self-check"
)

// RuntimeSupportWarning describes support caveats without changing the
// lifecycle reason-code contract.
func RuntimeSupportWarning(supportLevel string) string {
	if supportLevel == "experimental" {
		return experimentalSupportWarning
	}
	return ""
}

func trustedBundleRequiredError() *Error {
	return &Error{
		Code:    contracts.ReasonRuntimeUnavailable,
		Message: trustedBundleRequiredMessage,
	}
}

func runtimeVerificationError(err error) *Error {
	var daemonErr *Error
	if errors.As(err, &daemonErr) && daemonErr != nil && daemonErr.Code != "" {
		return &Error{
			Code:    daemonErr.Code,
			Message: trustedBundleRequiredMessage,
		}
	}
	return trustedBundleRequiredError()
}
