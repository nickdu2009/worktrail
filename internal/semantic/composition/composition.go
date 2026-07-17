// Package composition constructs trusted local semantic-runtime dependencies.
package composition

import (
	"errors"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/policy"
	"github.com/nickdu2009/worktrail/internal/semantic/profile"
)

const embeddingDimension = 1024

// DefaultSubsystemVersions returns a fresh, validated set of versioned semantic
// subsystem identity inputs for the v1 recall profile. Changing any value
// creates a new recall profile and requires an explicit rebuild.
func DefaultSubsystemVersions() profile.SubsystemVersions {
	return profile.SubsystemVersions{
		ChunkerVersion:   chunk.Version,
		IndexingVersion:  policy.Version,
		LexicalVersion:   "worktrail-fts5-gse-v1",
		SQLiteVecVersion: "sqlite-vec-v0.1.9",
	}
}

// Input identifies the immutable installed bundle and the versioned semantic
// subsystems that are not part of that bundle.
type Input struct {
	Roots    paths.SemanticRoots
	Versions profile.SubsystemVersions
}

// Result is the verified local semantic-runtime composition. Constructing it
// only validates static inputs and creates dependency values; it does not start
// a daemon, create state, generate credentials, or open a generation database.
type Result struct {
	Bundle       bundle.InstalledBundle
	Runtime      bundle.ResolvedRuntime
	Identity     profile.Identity
	Store        daemon.Store
	Client       *daemon.HTTPClient
	Controller   daemon.Controller
	TokenCounter contracts.TokenCounter
	Embedder     generation.Embedder
}

// Error is a sanitized construction failure. Its message deliberately omits
// host paths, credentials, and lower-level verification details.
type Error struct {
	Code contracts.ReasonCode
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	switch e.Code {
	case contracts.ReasonBundleMissing:
		return "semantic bundle is unavailable"
	case contracts.ReasonPlatformUnsupported:
		return "semantic runtime is unsupported on this platform"
	default:
		return "semantic runtime is unavailable"
	}
}

type dependencies struct {
	trustedBundleID      func() (string, error)
	loadInstalled        func(paths.SemanticRoots, string, string) (bundle.InstalledBundle, error)
	detectChip           func() (string, error)
	resolveRuntime       func(bundle.InstalledBundle, string) (bundle.ResolvedRuntime, error)
	deriveIdentity       func(bundle.Manifest, bundle.ResolvedRuntime, profile.SubsystemVersions) (profile.Identity, error)
	newStore             func(paths.SemanticRoots, string) (daemon.Store, error)
	newHTTPClient        func(string) (*daemon.HTTPClient, error)
	newRuntimeVerifier   func(paths.SemanticRoots) daemon.RuntimeVerifier
	newStartLocker       func(string) (daemon.StartLocker, error)
	newEndpointAllocator func() daemon.EndpointAllocator
	newFactory           func() daemon.Factory
	newSupervisor        func(daemon.SupervisorConfig) (daemon.Controller, error)
}

// Build constructs the production semantic-runtime dependency graph from the
// installed bundle selected by the embedded immutable trusted manifest.
func Build(input Input) (Result, error) {
	return build(input, productionDependencies())
}

func productionDependencies() dependencies {
	return dependencies{
		trustedBundleID: func() (string, error) {
			manifest, err := bundle.ParseTrustedManifest(bundle.EmbeddedTrustedManifestM1)
			if err != nil {
				return "", err
			}
			return manifest.BundleID, nil
		},
		loadInstalled: bundle.LoadInstalledForChip,
		detectChip:    bundle.DetectDarwinChip,
		resolveRuntime: func(installed bundle.InstalledBundle, chip string) (bundle.ResolvedRuntime, error) {
			return installed.ResolveRuntime(chip)
		},
		deriveIdentity:       profile.Derive,
		newStore:             daemon.NewStore,
		newHTTPClient:        daemon.NewHTTPClient,
		newRuntimeVerifier:   func(roots paths.SemanticRoots) daemon.RuntimeVerifier { return bundle.NewRuntimeVerifier(roots) },
		newStartLocker:       daemon.NewStartLocker,
		newEndpointAllocator: daemon.NewEndpointAllocator,
		newFactory:           daemon.NewFactory,
		newSupervisor: func(config daemon.SupervisorConfig) (daemon.Controller, error) {
			return daemon.NewSupervisor(config)
		},
	}
}

func build(input Input, deps dependencies) (Result, error) {
	bundleID, err := deps.trustedBundleID()
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonBundleMissing)
	}
	chip, err := deps.detectChip()
	if err != nil {
		if errors.Is(err, bundle.ErrUnsupportedPlatform) || errors.Is(err, bundle.ErrUnsupportedChip) {
			return Result{}, constructionFailure(contracts.ReasonPlatformUnsupported)
		}
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	installed, err := deps.loadInstalled(input.Roots, bundleID, chip)
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonBundleMissing)
	}
	runtime, err := deps.resolveRuntime(installed, chip)
	if err != nil {
		if errors.Is(err, bundle.ErrUnsupportedPlatform) || errors.Is(err, bundle.ErrUnsupportedChip) || errors.Is(err, bundle.ErrRuntimeVariantUnavailable) {
			return Result{}, constructionFailure(contracts.ReasonPlatformUnsupported)
		}
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	identity, err := deps.deriveIdentity(installed.Manifest, runtime, input.Versions)
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	store, err := deps.newStore(input.Roots, runtime.BundleID)
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	client, err := deps.newHTTPClient(runtime.BundleID)
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	locker, err := deps.newStartLocker(input.Roots.Runtime)
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}

	runtimeSpec := daemon.RuntimeSpec{
		BundleID:        runtime.BundleID,
		RuntimePath:     runtime.RuntimePath,
		ModelPath:       runtime.ModelPath,
		WorkingDir:      installed.BundleRoot,
		Alias:           runtime.BundleID,
		Dimension:       embeddingDimension,
		LlamaAppVersion: runtime.RuntimeVersion,
		RuntimeSHA256:   runtime.RuntimeSHA256,
		ChipVariant:     runtime.Chip,
		ModelSHA256:     runtime.ModelSHA256,
		SupportLevel:    runtime.SupportLevel,
	}
	verifier := deps.newRuntimeVerifier(input.Roots)
	controller, err := deps.newSupervisor(daemon.SupervisorConfig{
		Store:     store,
		Runtime:   runtimeSpec,
		Verifier:  verifier,
		Client:    client,
		Allocator: deps.newEndpointAllocator(),
		Locker:    locker,
		Factory:   deps.newFactory(),
	})
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}

	return Result{
		Bundle:     installed,
		Runtime:    runtime,
		Identity:   identity,
		Store:      store,
		Client:     client,
		Controller: controller,
		TokenCounter: daemon.DaemonTokenCounter{
			Counter:     client,
			Credentials: store,
		},
		Embedder: daemon.DaemonGenerationEmbedder{
			Embedder:    client,
			Credentials: store,
		},
	}, nil
}

func constructionFailure(code contracts.ReasonCode) error {
	return &Error{Code: code}
}
