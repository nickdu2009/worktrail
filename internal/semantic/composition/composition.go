// Package composition constructs trusted local semantic-runtime dependencies.
package composition

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/policy"
	"github.com/nickdu2009/worktrail/internal/semantic/profile"
	"github.com/nickdu2009/worktrail/internal/semantic/retrieve"
	"github.com/nickdu2009/worktrail/internal/semantic/service"
)

const embeddingDimension = 1024

// DefaultSubsystemVersions returns a fresh, validated set of versioned semantic
// subsystem identity inputs for the table-aware recall profile. Changing any
// value creates a new recall profile and requires an explicit rebuild.
func DefaultSubsystemVersions() profile.SubsystemVersions {
	return profile.SubsystemVersions{
		ChunkerVersion:   chunk.Version,
		IndexingVersion:  policy.Version,
		LexicalVersion:   "worktrail-fts5-gse-v2",
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
	Bundle        bundle.InstalledBundle
	Runtime       bundle.ResolvedRuntime
	Identity      profile.Identity
	Controller    daemon.Controller
	TokenCounter  contracts.TokenCounter
	Embedder      generation.Embedder
	QueryEmbedder retrieve.QueryEmbedder
}

// HostResult contains worker-only dependencies. It is used exclusively by the
// launchd Host entrypoint; ordinary callers receive Result and cannot access
// worker credentials or the authenticated loopback client.
type HostResult struct {
	Bundle   bundle.InstalledBundle
	Runtime  bundle.ResolvedRuntime
	Identity profile.Identity
	Host     *service.Host
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
	newServiceClient     func(paths.SemanticRoots, string, string, string, string) (*service.Client, error)
}

// Build constructs the production semantic-runtime dependency graph from the
// installed bundle selected by the embedded immutable trusted manifest.
func Build(input Input) (Result, error) {
	return buildClient(input, productionDependencies())
}

// BuildHost constructs the launchd-owned worker graph. It does not start the
// Host or worker.
func BuildHost(input Input) (HostResult, error) {
	return buildHost(input, productionDependencies())
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
		newServiceClient: service.NewClient,
	}
}

type resolved struct {
	bundle      bundle.InstalledBundle
	runtime     bundle.ResolvedRuntime
	identity    profile.Identity
	fingerprint string
}

func buildClient(input Input, deps dependencies) (Result, error) {
	resolved, err := resolve(input, deps)
	if err != nil {
		return Result{}, err
	}
	client, err := deps.newServiceClient(
		input.Roots,
		resolved.runtime.BundleID,
		resolved.fingerprint,
		resolved.runtime.SupportLevel,
		resolved.runtime.Chip,
	)
	if err != nil {
		return Result{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	return Result{
		Bundle:        resolved.bundle,
		Runtime:       resolved.runtime,
		Identity:      resolved.identity,
		Controller:    client,
		TokenCounter:  client,
		Embedder:      client,
		QueryEmbedder: client,
	}, nil
}

func buildHost(input Input, deps dependencies) (HostResult, error) {
	resolved, err := resolve(input, deps)
	if err != nil {
		return HostResult{}, err
	}
	store, err := deps.newStore(input.Roots, resolved.runtime.BundleID)
	if err != nil {
		return HostResult{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	workerClient, err := deps.newHTTPClient(resolved.runtime.BundleID)
	if err != nil {
		return HostResult{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	locker, err := deps.newStartLocker(input.Roots.Runtime)
	if err != nil {
		return HostResult{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	runtimeSpec := daemon.RuntimeSpec{
		BundleID:        resolved.runtime.BundleID,
		RuntimePath:     resolved.runtime.RuntimePath,
		ModelPath:       resolved.runtime.ModelPath,
		WorkingDir:      resolved.bundle.BundleRoot,
		Alias:           resolved.runtime.BundleID,
		Dimension:       embeddingDimension,
		LlamaAppVersion: resolved.runtime.RuntimeVersion,
		RuntimeSHA256:   resolved.runtime.RuntimeSHA256,
		ChipVariant:     resolved.runtime.Chip,
		ModelSHA256:     resolved.runtime.ModelSHA256,
		SupportLevel:    resolved.runtime.SupportLevel,
	}
	controller, err := deps.newSupervisor(daemon.SupervisorConfig{
		Store:     store,
		Runtime:   runtimeSpec,
		Verifier:  deps.newRuntimeVerifier(input.Roots),
		Client:    workerClient,
		Allocator: deps.newEndpointAllocator(),
		Locker:    locker,
		Factory:   deps.newFactory(),
	})
	if err != nil {
		return HostResult{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	_, idleTimeout, err := service.LoadConfig(input.Roots)
	if err != nil {
		return HostResult{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	host := &service.Host{
		Roots:              input.Roots,
		BundleID:           resolved.runtime.BundleID,
		RuntimeFingerprint: resolved.fingerprint,
		Controller:         controller,
		Store:              store,
		WorkerClient:       workerClient,
		IdleTimeout:        idleTimeout,
	}
	return HostResult{Bundle: resolved.bundle, Runtime: resolved.runtime, Identity: resolved.identity, Host: host}, nil
}

func resolve(input Input, deps dependencies) (resolved, error) {
	bundleID, err := deps.trustedBundleID()
	if err != nil {
		return resolved{}, constructionFailure(contracts.ReasonBundleMissing)
	}
	chip, err := deps.detectChip()
	if err != nil {
		if errors.Is(err, bundle.ErrUnsupportedPlatform) || errors.Is(err, bundle.ErrUnsupportedChip) {
			return resolved{}, constructionFailure(contracts.ReasonPlatformUnsupported)
		}
		return resolved{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	installed, err := deps.loadInstalled(input.Roots, bundleID, chip)
	if err != nil {
		return resolved{}, constructionFailure(contracts.ReasonBundleMissing)
	}
	runtime, err := deps.resolveRuntime(installed, chip)
	if err != nil {
		if errors.Is(err, bundle.ErrUnsupportedPlatform) || errors.Is(err, bundle.ErrUnsupportedChip) || errors.Is(err, bundle.ErrRuntimeVariantUnavailable) {
			return resolved{}, constructionFailure(contracts.ReasonPlatformUnsupported)
		}
		return resolved{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	identity, err := deps.deriveIdentity(installed.Manifest, runtime, input.Versions)
	if err != nil {
		return resolved{}, constructionFailure(contracts.ReasonRuntimeUnavailable)
	}
	return resolved{bundle: installed, runtime: runtime, identity: identity, fingerprint: runtimeFingerprint(runtime)}, nil
}

func runtimeFingerprint(runtime bundle.ResolvedRuntime) string {
	digest := sha256.Sum256([]byte(runtime.BundleID + "\x00" + runtime.RuntimeVersion + "\x00" + runtime.RuntimeSHA256 + "\x00" + runtime.ModelSHA256 + "\x00" + runtime.Chip))
	return hex.EncodeToString(digest[:])
}

func constructionFailure(code contracts.ReasonCode) error {
	return &Error{Code: code}
}
