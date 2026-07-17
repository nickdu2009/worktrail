package bundle

import (
	"context"
	"errors"
	"os"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

const (
	expectedEmbeddingDimension = 1024
	runtimeVerificationMessage = "semantic runtime verification failed"
)

// RuntimeVerifier authorizes daemon lifecycle only when the supplied runtime
// spec still matches the locally installed artifacts pinned by the embedded
// immutable manifest. It performs filesystem reads only.
type RuntimeVerifier struct {
	Roots paths.SemanticRoots

	loadInstalled    func(paths.SemanticRoots, string, string) (InstalledBundle, error)
	detectDarwinChip func() (string, error)
}

var _ daemon.RuntimeVerifier = (*RuntimeVerifier)(nil)

// NewRuntimeVerifier creates the production local verifier. The embedded
// manifest is the trust root; no network attestation is required.
func NewRuntimeVerifier(roots paths.SemanticRoots) *RuntimeVerifier {
	return &RuntimeVerifier{
		Roots:            roots,
		loadInstalled:    LoadInstalledForChip,
		detectDarwinChip: DetectDarwinChip,
	}
}

// VerifyRuntime re-verifies the installed bundle and selected chip variant for
// every daemon lifecycle request. It never downloads, starts, or modifies the
// runtime.
func (v *RuntimeVerifier) VerifyRuntime(_ context.Context, runtime daemon.RuntimeSpec) error {
	if v == nil {
		return verificationFailure(contracts.ReasonRuntimeUnavailable)
	}
	loader := v.loadInstalled
	if loader == nil {
		loader = LoadInstalledForChip
	}
	detectChip := v.detectDarwinChip
	if detectChip == nil {
		detectChip = DetectDarwinChip
	}

	chip, err := detectChip()
	if err != nil {
		if errors.Is(err, ErrUnsupportedPlatform) || errors.Is(err, ErrUnsupportedChip) {
			return verificationFailure(contracts.ReasonPlatformUnsupported)
		}
		return verificationFailure(contracts.ReasonRuntimeUnavailable)
	}
	installed, err := loader(v.Roots, runtime.BundleID, chip)
	if err != nil {
		return v.loadFailure(runtime.BundleID, err)
	}
	resolved, err := installed.ResolveRuntime(chip)
	if err != nil {
		if errors.Is(err, ErrUnsupportedChip) || errors.Is(err, ErrRuntimeVariantUnavailable) {
			return verificationFailure(contracts.ReasonPlatformUnsupported)
		}
		return verificationFailure(contracts.ReasonRuntimeUnavailable)
	}
	if !runtimeMatchesResolved(runtime, installed, resolved) {
		return verificationFailure(contracts.ReasonRuntimeUnavailable)
	}
	return nil
}

func (v *RuntimeVerifier) loadFailure(bundleID string, err error) error {
	if errors.Is(err, ErrUnsupportedChip) || errors.Is(err, ErrRuntimeVariantUnavailable) {
		return verificationFailure(contracts.ReasonPlatformUnsupported)
	}
	if errors.Is(err, ErrUntrustedBundleID) || bundleRootMissing(v.Roots, bundleID) {
		return verificationFailure(contracts.ReasonBundleMissing)
	}
	return verificationFailure(contracts.ReasonRuntimeUnavailable)
}

func runtimeMatchesResolved(runtime daemon.RuntimeSpec, installed InstalledBundle, resolved ResolvedRuntime) bool {
	return runtime.BundleID == resolved.BundleID &&
		runtime.RuntimePath == resolved.RuntimePath &&
		runtime.ModelPath == resolved.ModelPath &&
		runtime.WorkingDir == installed.BundleRoot &&
		runtime.Alias == resolved.BundleID &&
		runtime.Dimension == expectedEmbeddingDimension &&
		runtime.LlamaAppVersion == resolved.RuntimeVersion &&
		runtime.RuntimeSHA256 == resolved.RuntimeSHA256 &&
		runtime.ChipVariant == resolved.Chip &&
		runtime.ModelSHA256 == resolved.ModelSHA256 &&
		runtime.SupportLevel == resolved.SupportLevel
}

func bundleRootMissing(roots paths.SemanticRoots, bundleID string) bool {
	root, err := roots.Bundle(bundleID)
	if err != nil {
		return false
	}
	_, err = os.Lstat(root)
	return errors.Is(err, os.ErrNotExist)
}

func verificationFailure(code contracts.ReasonCode) *daemon.Error {
	return &daemon.Error{
		Code:    code,
		Message: runtimeVerificationMessage,
	}
}
