package composition

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/policy"
	"github.com/nickdu2009/worktrail/internal/semantic/profile"
	"github.com/nickdu2009/worktrail/internal/semantic/service"
)

func TestBuildHostMapsVerifiedDependenciesWithoutLifecycleSideEffects(t *testing.T) {
	input, deps, capture := testComposition(t)

	result, err := buildHost(input, deps)
	if err != nil {
		t.Fatalf("buildHost() error = %v", err)
	}
	if result.Bundle.BundleRoot != capture.installed.BundleRoot ||
		result.Bundle.Manifest.BundleID != capture.installed.Manifest.BundleID ||
		result.Runtime != capture.runtime || result.Identity != capture.identity {
		t.Fatalf("build() identity mapping = %#v", result)
	}
	if result.Host.Store.StatePath() != capture.store.StatePath() || result.Host.WorkerClient != capture.client || result.Host.Controller != capture.controller {
		t.Fatalf("build() dependency mapping = %#v", result)
	}
	if capture.loadedRoots != input.Roots || capture.loadedBundleID != capture.runtime.BundleID ||
		capture.resolvedBundle.BundleRoot != capture.installed.BundleRoot || capture.resolvedChip != "m1" ||
		capture.derivedManifest.BundleID != capture.installed.Manifest.BundleID ||
		capture.derivedRuntime != capture.runtime || capture.derivedVersions != input.Versions ||
		capture.storeRoots != input.Roots || capture.storeBundleID != capture.runtime.BundleID ||
		capture.clientAlias != capture.runtime.BundleID || capture.lockerRoot != input.Roots.Runtime ||
		capture.verifierRoots != input.Roots {
		t.Fatalf("constructor mapping = %#v", capture)
	}
	if capture.supervisor.Runtime != (daemon.RuntimeSpec{
		BundleID:        capture.runtime.BundleID,
		RuntimePath:     capture.runtime.RuntimePath,
		ModelPath:       capture.runtime.ModelPath,
		WorkingDir:      capture.installed.BundleRoot,
		Alias:           capture.runtime.BundleID,
		Dimension:       embeddingDimension,
		LlamaAppVersion: capture.runtime.RuntimeVersion,
		RuntimeSHA256:   capture.runtime.RuntimeSHA256,
		ChipVariant:     capture.runtime.Chip,
		ModelSHA256:     capture.runtime.ModelSHA256,
		SupportLevel:    capture.runtime.SupportLevel,
	}) {
		t.Fatalf("supervisor runtime = %#v", capture.supervisor.Runtime)
	}
	if capture.supervisor.Verifier != capture.verifier || capture.supervisor.Client != capture.client ||
		capture.supervisor.Store.StatePath() != capture.store.StatePath() ||
		capture.supervisor.Locker != capture.locker || capture.supervisor.Allocator != capture.allocator ||
		capture.supervisor.Factory != capture.factory {
		t.Fatalf("supervisor dependency mapping = %#v", capture.supervisor)
	}

	if capture.verifier.calls != 0 || capture.locker.calls != 0 || capture.allocator.calls != 0 ||
		capture.factory.newCalls != 0 || capture.factory.openCalls != 0 {
		t.Fatalf("build() invoked lifecycle dependency: verifier=%d locker=%d allocator=%d factory=%d/%d",
			capture.verifier.calls, capture.locker.calls, capture.allocator.calls, capture.factory.newCalls, capture.factory.openCalls)
	}
	if _, err := os.Stat(input.Roots.Runtime); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("build() created runtime directory: stat error = %v", err)
	}
}

func TestBuildReturnsServiceClientThroughExistingContracts(t *testing.T) {
	input, deps, capture := testComposition(t)

	result, err := buildClient(input, deps)
	if err != nil {
		t.Fatalf("buildClient() error = %v", err)
	}
	if result.Controller != capture.serviceClient || result.TokenCounter != capture.serviceClient ||
		result.Embedder != capture.serviceClient || result.QueryEmbedder != capture.serviceClient {
		t.Fatalf("client contracts = %#v", result)
	}
	if capture.supervisor.Runtime.BundleID != "" || capture.storeBundleID != "" || capture.clientAlias != "" {
		t.Fatalf("client build constructed worker dependencies: %#v", capture)
	}
}

func TestBuildSanitizesMajorConstructionFailures(t *testing.T) {
	tests := []struct {
		name  string
		setup func(dependencies, *compositionCapture) dependencies
		code  contracts.ReasonCode
	}{
		{
			name: "trusted manifest",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.trustedBundleID = func() (string, error) {
					return "", errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonBundleMissing,
		},
		{
			name: "bundle load",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.loadInstalled = func(paths.SemanticRoots, string, string) (bundle.InstalledBundle, error) {
					return bundle.InstalledBundle{}, errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonBundleMissing,
		},
		{
			name: "platform detection",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.detectChip = func() (string, error) { return "", bundle.ErrUnsupportedPlatform }
				return deps
			},
			code: contracts.ReasonPlatformUnsupported,
		},
		{
			name: "runtime resolution",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.resolveRuntime = func(bundle.InstalledBundle, string) (bundle.ResolvedRuntime, error) {
					return bundle.ResolvedRuntime{}, bundle.ErrRuntimeVariantUnavailable
				}
				return deps
			},
			code: contracts.ReasonPlatformUnsupported,
		},
		{
			name: "profile derivation",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.deriveIdentity = func(bundle.Manifest, bundle.ResolvedRuntime, profile.SubsystemVersions) (profile.Identity, error) {
					return profile.Identity{}, errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonRuntimeUnavailable,
		},
		{
			name: "store construction",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.newStore = func(paths.SemanticRoots, string) (daemon.Store, error) {
					return daemon.Store{}, errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonRuntimeUnavailable,
		},
		{
			name: "HTTP client construction",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.newHTTPClient = func(string) (*daemon.HTTPClient, error) {
					return nil, errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonRuntimeUnavailable,
		},
		{
			name: "start locker construction",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.newStartLocker = func(string) (daemon.StartLocker, error) {
					return nil, errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonRuntimeUnavailable,
		},
		{
			name: "supervisor construction",
			setup: func(deps dependencies, _ *compositionCapture) dependencies {
				deps.newSupervisor = func(daemon.SupervisorConfig) (daemon.Controller, error) {
					return nil, errors.New("secret /host/path")
				}
				return deps
			},
			code: contracts.ReasonRuntimeUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input, deps, capture := testComposition(t)

			_, err := buildHost(input, test.setup(deps, capture))
			var constructionErr *Error
			if !errors.As(err, &constructionErr) {
				t.Fatalf("build() error = %T %[1]v, want *Error", err)
			}
			if constructionErr.Code != test.code {
				t.Fatalf("build() code = %q, want %q", constructionErr.Code, test.code)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), input.Roots.Runtime) {
				t.Fatalf("build() exposed sensitive detail: %q", err)
			}
			if capture.verifier.calls != 0 || capture.locker.calls != 0 || capture.allocator.calls != 0 ||
				capture.factory.newCalls != 0 || capture.factory.openCalls != 0 {
				t.Fatalf("failure path invoked lifecycle dependency: verifier=%d locker=%d allocator=%d factory=%d/%d",
					capture.verifier.calls, capture.locker.calls, capture.allocator.calls, capture.factory.newCalls, capture.factory.openCalls)
			}
			if _, statErr := os.Stat(input.Roots.Runtime); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("failure path created runtime directory: stat error = %v", statErr)
			}
		})
	}
}

func TestProductionVerifierUsesCompositionRoots(t *testing.T) {
	roots := paths.SemanticRoots{
		Cache:   "/cache",
		Runtime: "/runtime",
		Logs:    "/logs",
	}

	verifier, ok := productionDependencies().newRuntimeVerifier(roots).(*bundle.RuntimeVerifier)
	if !ok {
		t.Fatalf("production runtime verifier = %T, want *bundle.RuntimeVerifier", verifier)
	}
	if verifier.Roots != roots {
		t.Fatalf("runtime verifier roots = %#v, want %#v", verifier.Roots, roots)
	}
}

func TestDefaultSubsystemVersionsUsesApprovedV1Profile(t *testing.T) {
	want := profile.SubsystemVersions{
		ChunkerVersion:   chunk.Version,
		IndexingVersion:  policy.Version,
		LexicalVersion:   "worktrail-fts5-gse-v2",
		SQLiteVecVersion: "sqlite-vec-v0.1.9",
	}

	first := DefaultSubsystemVersions()
	if first != want {
		t.Fatalf("DefaultSubsystemVersions() = %#v, want %#v", first, want)
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("DefaultSubsystemVersions() validation error = %v", err)
	}

	first.ChunkerVersion = "changed"
	first.IndexingVersion = "changed"
	first.LexicalVersion = "changed"
	first.SQLiteVecVersion = "changed"
	if second := DefaultSubsystemVersions(); second != want {
		t.Fatalf("DefaultSubsystemVersions() after mutation = %#v, want %#v", second, want)
	}
}

func TestDefaultSubsystemVersionsDerivesReproducibleProfile(t *testing.T) {
	manifest, err := bundle.ParseTrustedManifest(bundle.EmbeddedTrustedManifestM1)
	if err != nil {
		t.Fatal(err)
	}
	variant := manifest.Runtime.Variants[0]
	runtime := bundle.ResolvedRuntime{
		BundleID:       manifest.BundleID,
		RuntimeSHA256:  variant.ExecutableSHA256,
		ModelSHA256:    manifest.Model.SHA256,
		RuntimeVersion: variant.ExecutableVersion,
		Chip:           variant.Chip,
	}

	first, err := profile.Derive(manifest, runtime, DefaultSubsystemVersions())
	if err != nil {
		t.Fatalf("first profile.Derive() error = %v", err)
	}
	second, err := profile.Derive(manifest, runtime, DefaultSubsystemVersions())
	if err != nil {
		t.Fatalf("second profile.Derive() error = %v", err)
	}
	if first != second {
		t.Fatalf("profile.Derive() = %#v, then %#v", first, second)
	}
	if first.RecallProfileID == "" {
		t.Fatalf("derived recall profile ID is empty: %#v", first)
	}
}

type compositionCapture struct {
	installed     bundle.InstalledBundle
	runtime       bundle.ResolvedRuntime
	identity      profile.Identity
	store         daemon.Store
	client        *daemon.HTTPClient
	verifier      *compositionVerifier
	locker        *compositionLocker
	allocator     *compositionAllocator
	factory       *compositionFactory
	controller    daemon.Controller
	serviceClient *service.Client
	supervisor    daemon.SupervisorConfig

	loadedRoots     paths.SemanticRoots
	loadedBundleID  string
	resolvedBundle  bundle.InstalledBundle
	resolvedChip    string
	derivedManifest bundle.Manifest
	derivedRuntime  bundle.ResolvedRuntime
	derivedVersions profile.SubsystemVersions
	storeRoots      paths.SemanticRoots
	storeBundleID   string
	clientAlias     string
	lockerRoot      string
	verifierRoots   paths.SemanticRoots
}

func testComposition(t *testing.T) (Input, dependencies, *compositionCapture) {
	t.Helper()

	base := t.TempDir()
	input := Input{
		Roots: paths.SemanticRoots{
			Cache:   filepath.Join(base, "cache"),
			Runtime: filepath.Join(base, "runtime"),
			Logs:    filepath.Join(base, "logs"),
		},
		Versions: profile.SubsystemVersions{
			ChunkerVersion:   "chunker-v2",
			IndexingVersion:  "index-v1",
			LexicalVersion:   "lexical-v1",
			SQLiteVecVersion: "sqlite-vec-v1",
		},
	}
	const bundleID = "bundle-a"
	store, err := daemon.NewStore(input.Roots, bundleID)
	if err != nil {
		t.Fatalf("NewStore() error = %v", err)
	}
	client, err := daemon.NewHTTPClient(bundleID)
	if err != nil {
		t.Fatalf("NewHTTPClient() error = %v", err)
	}
	serviceClient, err := service.NewClient(input.Roots, bundleID, "runtime-fingerprint", "verified", "m1")
	if err != nil {
		t.Fatalf("service.NewClient() error = %v", err)
	}
	capture := &compositionCapture{
		installed: bundle.InstalledBundle{
			Manifest:   bundle.Manifest{BundleID: bundleID},
			BundleRoot: filepath.Join(input.Roots.Cache, "bundles", bundleID),
		},
		runtime: bundle.ResolvedRuntime{
			BundleID:       bundleID,
			RuntimePath:    filepath.Join(input.Roots.Cache, "bundles", bundleID, "llama-server"),
			ModelPath:      filepath.Join(input.Roots.Cache, "bundles", bundleID, "model.gguf"),
			RuntimeVersion: "v1",
			RuntimeSHA256:  strings.Repeat("a", 64),
			ModelSHA256:    strings.Repeat("b", 64),
			Chip:           "m1",
			SupportLevel:   "verified",
		},
		identity:      profile.Identity{ModelSpaceID: "model-space", RecallProfileID: "recall-profile"},
		store:         store,
		client:        client,
		verifier:      &compositionVerifier{},
		locker:        &compositionLocker{},
		allocator:     &compositionAllocator{},
		factory:       &compositionFactory{},
		controller:    daemon.UnavailableController{},
		serviceClient: serviceClient,
	}
	deps := dependencies{
		trustedBundleID: func() (string, error) { return bundleID, nil },
		loadInstalled: func(roots paths.SemanticRoots, id, chip string) (bundle.InstalledBundle, error) {
			capture.loadedRoots = roots
			capture.loadedBundleID = id
			return capture.installed, nil
		},
		detectChip: func() (string, error) { return "m1", nil },
		resolveRuntime: func(installed bundle.InstalledBundle, chip string) (bundle.ResolvedRuntime, error) {
			capture.resolvedBundle = installed
			capture.resolvedChip = chip
			return capture.runtime, nil
		},
		deriveIdentity: func(manifest bundle.Manifest, runtime bundle.ResolvedRuntime, versions profile.SubsystemVersions) (profile.Identity, error) {
			capture.derivedManifest = manifest
			capture.derivedRuntime = runtime
			capture.derivedVersions = versions
			return capture.identity, nil
		},
		newStore: func(roots paths.SemanticRoots, id string) (daemon.Store, error) {
			capture.storeRoots = roots
			capture.storeBundleID = id
			return capture.store, nil
		},
		newHTTPClient: func(alias string) (*daemon.HTTPClient, error) {
			capture.clientAlias = alias
			return capture.client, nil
		},
		newRuntimeVerifier: func(roots paths.SemanticRoots) daemon.RuntimeVerifier {
			capture.verifierRoots = roots
			return capture.verifier
		},
		newStartLocker: func(root string) (daemon.StartLocker, error) {
			capture.lockerRoot = root
			return capture.locker, nil
		},
		newEndpointAllocator: func() daemon.EndpointAllocator { return capture.allocator },
		newFactory:           func() daemon.Factory { return capture.factory },
		newSupervisor: func(config daemon.SupervisorConfig) (daemon.Controller, error) {
			capture.supervisor = config
			return capture.controller, nil
		},
		newServiceClient: func(paths.SemanticRoots, string, string, string, string) (*service.Client, error) {
			return capture.serviceClient, nil
		},
	}
	return input, deps, capture
}

type compositionVerifier struct{ calls int }

func (verifier *compositionVerifier) VerifyRuntime(context.Context, daemon.RuntimeSpec) error {
	verifier.calls++
	return nil
}

type compositionLocker struct{ calls int }

func (locker *compositionLocker) Lock(context.Context, string) (func(), error) {
	locker.calls++
	return func() {}, nil
}

type compositionAllocator struct{ calls int }

func (allocator *compositionAllocator) Allocate(context.Context) (string, error) {
	allocator.calls++
	return "127.0.0.1:1", nil
}

type compositionFactory struct {
	newCalls  int
	openCalls int
}

func (factory *compositionFactory) New(daemon.Command) daemon.Process {
	factory.newCalls++
	return compositionProcess{}
}

func (factory *compositionFactory) Open(daemon.Identity) (daemon.Process, error) {
	factory.openCalls++
	return compositionProcess{}, nil
}

type compositionProcess struct{}

func (compositionProcess) Start(context.Context) error { return nil }
func (compositionProcess) Identity() (daemon.Identity, error) {
	return daemon.Identity{PID: 1, StartedAt: time.Now()}, nil
}
func (compositionProcess) Signal(os.Signal) error           { return nil }
func (compositionProcess) WaitExited(context.Context) error { return nil }
func (compositionProcess) Release() error                   { return nil }
