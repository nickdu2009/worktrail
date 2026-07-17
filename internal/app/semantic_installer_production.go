package app

import (
	"context"
	"errors"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

// Immutable Hugging Face artifact URLs currently redirect to a delivery host.
// Allow one extra hop for CDN bridge hops observed in 2026-07 delivery paths.
const immutableArtifactMaxRedirects = 2

var (
	ErrSemanticInstallerUnavailable = errors.New("semantic installation is unavailable")
	ErrSemanticPlatformUnsupported  = errors.New("semantic installation requires a supported Apple silicon Mac")
	ErrSemanticInstallerFailed      = errors.New("semantic runtime installation failed")
)

// productionSemanticInstaller resolves machine-specific installation inputs
// only when --semantic is explicitly requested.
type productionSemanticInstaller struct {
	deps semanticInstallerProductionDependencies
}

type semanticInstallerProductionDependencies struct {
	discoverRoots   func() (paths.SemanticRoots, error)
	parseManifest   func([]byte) (bundle.Manifest, error)
	detectChip      func() (string, error)
	buildInstaller  func(paths.SemanticRoots) (bundle.Installer, error)
	install         func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error)
	installAndCheck func(context.Context, bundle.Installer, bundle.Manifest, string, bundle.InstallCheck) (bundle.InstallResult, error)
	selfCheck       func(context.Context, paths.SemanticRoots) error
}

func newProductionSemanticInstaller() SemanticInstaller {
	return productionSemanticInstaller{deps: productionSemanticInstallerDeps()}
}

func productionSemanticInstallerDeps() semanticInstallerProductionDependencies {
	return semanticInstallerProductionDependencies{
		discoverRoots: paths.DiscoverSemanticRoots,
		parseManifest: bundle.ParseTrustedManifest,
		detectChip:    bundle.DetectDarwinChip,
		buildInstaller: func(roots paths.SemanticRoots) (bundle.Installer, error) {
			return bundle.Installer{
				Roots: roots,
				Downloader: bundle.HTTPDownloader{
					// Immutable Hugging Face artifact URLs may redirect once to
					// their HTTPS delivery host. Further redirects are rejected.
					MaxRedirects: immutableArtifactMaxRedirects,
					// Transient CDN/gateway failures (e.g. 504) are retried.
					MaxAttempts: 4,
				},
			}, nil
		},
		install: func(ctx context.Context, installer bundle.Installer, manifest bundle.Manifest, chip string) (bundle.InstallResult, error) {
			return installer.Install(ctx, manifest, chip)
		},
		installAndCheck: func(ctx context.Context, installer bundle.Installer, manifest bundle.Manifest, chip string, check bundle.InstallCheck) (bundle.InstallResult, error) {
			return installer.InstallAndCheck(ctx, manifest, chip, check)
		},
		selfCheck: experimentalSemanticSelfCheck,
	}
}

func (i productionSemanticInstaller) Install(ctx context.Context, _ paths.Env) (SemanticInstallInfo, error) {
	roots, err := i.deps.discoverRoots()
	if err != nil {
		return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerUnavailable)
	}
	manifest, err := i.deps.parseManifest(bundle.EmbeddedTrustedManifestM1)
	if err != nil {
		return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerUnavailable)
	}
	chip, err := i.deps.detectChip()
	if err != nil {
		if errors.Is(err, bundle.ErrUnsupportedPlatform) || errors.Is(err, bundle.ErrUnsupportedChip) {
			return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonPlatformUnsupported, ErrSemanticPlatformUnsupported)
		}
		return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerUnavailable)
	}
	installer, err := i.deps.buildInstaller(roots)
	if err != nil {
		return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerUnavailable)
	}
	supportLevel := runtimeSupportLevel(manifest, chip)
	if supportLevel == "experimental" {
		if i.deps.selfCheck == nil || i.deps.installAndCheck == nil {
			return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerFailed)
		}
		if _, err := i.deps.installAndCheck(ctx, installer, manifest, chip, func(checkCtx context.Context, _ bundle.InstallResult) error {
			return i.deps.selfCheck(checkCtx, roots)
		}); err != nil {
			return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerFailed)
		}
		return semanticInstallInfo(chip, supportLevel), nil
	}
	if _, err := i.deps.install(ctx, installer, manifest, chip); err != nil {
		return SemanticInstallInfo{}, semanticInstallerError(contracts.ReasonRuntimeUnavailable, ErrSemanticInstallerFailed)
	}
	return semanticInstallInfo(chip, supportLevel), nil
}

func semanticInstallerError(code contracts.ReasonCode, cause error) error {
	return &daemon.Error{Code: code, Message: string(code), Err: cause}
}

func runtimeSupportLevel(manifest bundle.Manifest, chip string) string {
	for _, variant := range manifest.Runtime.Variants {
		if variant.Chip == chip {
			return variant.SupportLevel
		}
	}
	return ""
}

func semanticInstallInfo(chip, supportLevel string) SemanticInstallInfo {
	return SemanticInstallInfo{
		SupportLevel: supportLevel,
		Chip:         chip,
		Warning:      daemon.RuntimeSupportWarning(supportLevel),
	}
}

// experimentalSemanticSelfCheck starts the just-installed experimental runtime and
// exercises the production authenticated loopback protocol. A fully passing
// check preserves an existing healthy daemon; a failed check cleans a daemon
// only after Start has authenticated and verified it.
func experimentalSemanticSelfCheck(ctx context.Context, roots paths.SemanticRoots) (err error) {
	composed, err := composition.Build(composition.Input{
		Roots:    roots,
		Versions: composition.DefaultSubsystemVersions(),
	})
	if err != nil {
		return err
	}

	created := false
	managed := false
	passed := false
	defer func() {
		if cleanupErr := cleanupExperimentalSelfCheck(context.WithoutCancel(ctx), created || (managed && !passed), composed.Controller, composed.Store.Remove); cleanupErr != nil {
			err = bundle.RetainBundle(cleanupErr)
		}
	}()

	report, err := composed.Controller.Start(ctx)
	if err != nil {
		return err
	}
	created = report.Started
	managed = true
	descriptor, err := composed.Store.Load()
	if err != nil {
		return err
	}
	key, err := composed.Store.APIKey()
	if err != nil {
		return err
	}
	identity, err := composed.Client.Readiness(ctx, descriptor.Endpoint, key)
	if err != nil {
		return err
	}
	if identity.Alias != composed.Runtime.BundleID || identity.Dimension != bundle.BGEM3EmbeddingDimension {
		return errors.New("semantic runtime self-check identity mismatch")
	}
	tokens, err := composed.Client.CountTokens(ctx, descriptor.Endpoint, key, "worktrail experimental runtime self-check")
	if err != nil || tokens < 1 {
		return errors.New("semantic runtime self-check tokenization failed")
	}
	embeddings, err := composed.Client.Embed(ctx, descriptor.Endpoint, key, composed.Runtime.BundleID, []string{"worktrail experimental runtime self-check"})
	if err != nil || len(embeddings) != 1 {
		return errors.New("semantic runtime self-check embedding failed")
	}
	passed = true
	return nil
}

func cleanupExperimentalSelfCheck(
	ctx context.Context,
	cleanup bool,
	controller daemon.Controller,
	remove func() error,
) error {
	if !cleanup {
		return nil
	}
	if _, err := controller.Stop(ctx); err != nil {
		return err
	}
	return remove()
}
