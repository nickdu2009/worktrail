package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

func TestRunInitNoSemanticLeavesProductionInstallerIdle(t *testing.T) {
	installer := productionSemanticInstaller{deps: semanticInstallerProductionDependencies{
		discoverRoots: func() (paths.SemanticRoots, error) {
			t.Fatal("semantic roots were discovered")
			return paths.SemanticRoots{}, nil
		},
		parseManifest: func([]byte) (bundle.Manifest, error) {
			t.Fatal("semantic manifest was parsed")
			return bundle.Manifest{}, nil
		},
		detectChip: func() (string, error) {
			t.Fatal("semantic chip was detected")
			return "", nil
		},
		buildInstaller: func(paths.SemanticRoots) (bundle.Installer, error) {
			t.Fatal("semantic installer was built")
			return bundle.Installer{}, nil
		},
		install: func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error) {
			t.Fatal("semantic installer was called")
			return bundle.InstallResult{}, nil
		},
	}}

	for _, args := range [][]string{nil, {"--no-semantic"}} {
		env := initTestEnv(t)
		if err := runInitWithInstaller(context.Background(), env, IO{Out: &bytes.Buffer{}}, args, installer); err != nil {
			t.Fatalf("runInitWithInstaller(%v): %v", args, err)
		}
		assertCoreInit(t, env)
	}
}

func TestProductionSemanticInstallerWiresTrustedInputs(t *testing.T) {
	wantRoots := paths.SemanticRoots{
		Cache:   "/semantic/cache",
		Runtime: "/semantic/runtime",
		Logs:    "/semantic/logs",
	}
	wantManifest := bundle.Manifest{
		BundleID: "trusted-bundle",
		CanonicalManifest: bundle.CanonicalManifest{
			Runtime: bundle.Runtime{Variants: []bundle.RuntimeVariant{{Chip: "m1", SupportLevel: "verified"}}},
		},
	}
	installer := productionSemanticInstaller{deps: semanticInstallerProductionDependencies{
		discoverRoots: func() (paths.SemanticRoots, error) {
			return wantRoots, nil
		},
		parseManifest: func(data []byte) (bundle.Manifest, error) {
			if !bytes.Equal(data, bundle.EmbeddedTrustedManifestM1) {
				t.Fatal("manifest source was not the embedded trusted manifest")
			}
			return wantManifest, nil
		},
		detectChip: func() (string, error) {
			return "m1", nil
		},
		buildInstaller: func(gotRoots paths.SemanticRoots) (bundle.Installer, error) {
			if gotRoots != wantRoots {
				t.Fatalf("installer roots = %#v, want %#v", gotRoots, wantRoots)
			}
			return bundle.Installer{Roots: gotRoots}, nil
		},
		install: func(_ context.Context, gotInstaller bundle.Installer, gotManifest bundle.Manifest, gotChip string) (bundle.InstallResult, error) {
			if gotInstaller.Roots != wantRoots {
				t.Fatalf("install roots = %#v, want %#v", gotInstaller.Roots, wantRoots)
			}
			if !reflect.DeepEqual(gotManifest, wantManifest) {
				t.Fatalf("manifest = %#v, want %#v", gotManifest, wantManifest)
			}
			if gotChip != "m1" {
				t.Fatalf("chip = %q, want m1", gotChip)
			}
			return bundle.InstallResult{}, nil
		},
	}}

	info, err := installer.Install(context.Background(), paths.Env{})
	if err != nil {
		t.Fatalf("Install(): %v", err)
	}
	if info.SupportLevel != "verified" || info.Chip != "m1" || info.Warning != "" {
		t.Fatalf("install info = %#v", info)
	}
}

func TestProductionSemanticInstallerSelfChecksExperimentalRuntime(t *testing.T) {
	roots := paths.SemanticRoots{Cache: "/semantic/cache", Runtime: "/semantic/runtime", Logs: "/semantic/logs"}
	for _, chip := range []string{"m2", "m3", "m4", "m5"} {
		t.Run(chip, func(t *testing.T) {
			manifest := bundle.Manifest{CanonicalManifest: bundle.CanonicalManifest{Runtime: bundle.Runtime{Variants: []bundle.RuntimeVariant{{
				Chip: chip, SupportLevel: "experimental",
			}}}}}
			selfCheckCalls := 0
			installer := productionSemanticInstaller{deps: semanticInstallerProductionDependencies{
				discoverRoots: func() (paths.SemanticRoots, error) { return roots, nil },
				parseManifest: func([]byte) (bundle.Manifest, error) { return manifest, nil },
				detectChip:    func() (string, error) { return chip, nil },
				buildInstaller: func(paths.SemanticRoots) (bundle.Installer, error) {
					return bundle.Installer{}, nil
				},
				install: func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error) {
					t.Fatal("experimental runtime used unlocked install")
					return bundle.InstallResult{}, nil
				},
				installAndCheck: func(ctx context.Context, _ bundle.Installer, _ bundle.Manifest, gotChip string, check bundle.InstallCheck) (bundle.InstallResult, error) {
					if gotChip != chip {
						t.Fatalf("self-check chip = %q, want %q", gotChip, chip)
					}
					result := bundle.InstallResult{BundlePath: "/semantic/cache/bundle"}
					if err := check(ctx, result); err != nil {
						return bundle.InstallResult{}, err
					}
					return result, nil
				},
				selfCheck: func(context.Context, paths.SemanticRoots) error {
					selfCheckCalls++
					return nil
				},
			}}

			info, err := installer.Install(context.Background(), paths.Env{})
			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if info.SupportLevel != "experimental" || info.Chip != chip || info.Warning == "" {
				t.Fatalf("install info = %#v", info)
			}
			if selfCheckCalls != 1 {
				t.Fatalf("self-check calls = %d, want 1", selfCheckCalls)
			}
		})
	}
}

func TestProductionSemanticInstallerRunsExperimentalSelfCheckInsideBundleTransaction(t *testing.T) {
	roots := paths.SemanticRoots{Cache: t.TempDir(), Runtime: t.TempDir(), Logs: t.TempDir()}
	manifest := bundle.Manifest{CanonicalManifest: bundle.CanonicalManifest{
		Runtime: bundle.Runtime{Variants: []bundle.RuntimeVariant{{Chip: "m4", SupportLevel: "experimental"}}},
	}}
	checkCalls := 0
	installer := productionSemanticInstaller{deps: semanticInstallerProductionDependencies{
		discoverRoots:  func() (paths.SemanticRoots, error) { return roots, nil },
		parseManifest:  func([]byte) (bundle.Manifest, error) { return manifest, nil },
		detectChip:     func() (string, error) { return "m4", nil },
		buildInstaller: func(paths.SemanticRoots) (bundle.Installer, error) { return bundle.Installer{}, nil },
		install: func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error) {
			t.Fatal("experimental runtime used unlocked install")
			return bundle.InstallResult{}, nil
		},
		installAndCheck: func(ctx context.Context, _ bundle.Installer, _ bundle.Manifest, _ string, check bundle.InstallCheck) (bundle.InstallResult, error) {
			checkCalls++
			if err := check(ctx, bundle.InstallResult{}); err != nil {
				return bundle.InstallResult{}, err
			}
			return bundle.InstallResult{}, nil
		},
		selfCheck: func(context.Context, paths.SemanticRoots) error { return errors.New("self-check failed") },
	}}

	_, err := installer.Install(context.Background(), paths.Env{})
	assertSemanticInstallerReason(t, err, ErrSemanticInstallerFailed, contracts.ReasonRuntimeUnavailable)
	if checkCalls != 1 {
		t.Fatalf("locked self-check calls = %d, want 1", checkCalls)
	}
}

func TestRunInitExperimentalSelfCheckFailurePreservesCoreInitialization(t *testing.T) {
	env := initTestEnv(t)
	installer := productionSemanticInstaller{deps: semanticInstallerProductionDependencies{
		discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
		parseManifest: func([]byte) (bundle.Manifest, error) {
			return bundle.Manifest{CanonicalManifest: bundle.CanonicalManifest{Runtime: bundle.Runtime{Variants: []bundle.RuntimeVariant{{Chip: "m3", SupportLevel: "experimental"}}}}}, nil
		},
		detectChip: func() (string, error) { return "m3", nil },
		buildInstaller: func(paths.SemanticRoots) (bundle.Installer, error) {
			return bundle.Installer{}, nil
		},
		install: func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error) {
			return bundle.InstallResult{}, nil
		},
		installAndCheck: func(ctx context.Context, _ bundle.Installer, _ bundle.Manifest, _ string, check bundle.InstallCheck) (bundle.InstallResult, error) {
			return bundle.InstallResult{}, check(ctx, bundle.InstallResult{})
		},
		selfCheck: func(context.Context, paths.SemanticRoots) error { return errors.New("self-check failed") },
	}}

	err := runInitWithInstaller(context.Background(), env, IO{Out: &bytes.Buffer{}}, []string{"--semantic"}, installer)
	assertSemanticInstallerReason(t, err, ErrSemanticInstallerFailed, contracts.ReasonRuntimeUnavailable)
	assertCoreInit(t, env)
}

func TestExperimentalSelfCheckCleanupPreservesHealthyDaemonAndCleansFailedManagedDaemon(t *testing.T) {
	controller := &installerControllerStub{}
	removals := 0

	// A fully successful check against an existing daemon retains its state.
	if err := cleanupExperimentalSelfCheck(context.Background(), false, controller, func() error {
		removals++
		return nil
	}); err != nil {
		t.Fatalf("healthy daemon cleanup error = %v", err)
	}
	if controller.stopCalls != 0 || removals != 0 {
		t.Fatalf("existing daemon cleanup = stops:%d removals:%d, want 0/0", controller.stopCalls, removals)
	}

	// A failed token/embedding check happens only after Start authenticated and
	// verified the existing daemon, so it is safe and necessary to clean it.
	if err := cleanupExperimentalSelfCheck(context.Background(), true, controller, func() error {
		removals++
		return nil
	}); err != nil {
		t.Fatalf("managed daemon cleanup error = %v", err)
	}
	if controller.stopCalls != 1 || removals != 1 {
		t.Fatalf("failed managed daemon cleanup = stops:%d removals:%d, want 1/1", controller.stopCalls, removals)
	}

	controller.stopErr = errors.New("stop failed")
	if err := cleanupExperimentalSelfCheck(context.Background(), true, controller, func() error {
		removals++
		return nil
	}); err == nil {
		t.Fatal("Stop failure cleanup error = nil")
	}
	if controller.stopCalls != 2 || removals != 1 {
		t.Fatalf("Stop failure cleanup = stops:%d removals:%d, want 2/1", controller.stopCalls, removals)
	}
}

func TestProductionSemanticInstallerConfiguresRestrictedHTTPSDownloader(t *testing.T) {
	deps := productionSemanticInstallerDeps()
	installer, err := deps.buildInstaller(paths.SemanticRoots{})
	if err != nil {
		t.Fatalf("build installer: %v", err)
	}
	downloader, ok := installer.Downloader.(bundle.HTTPDownloader)
	if !ok {
		t.Fatalf("downloader = %T, want bundle.HTTPDownloader", installer.Downloader)
	}
	if downloader.Client != nil {
		t.Fatal("production downloader unexpectedly has an HTTP client")
	}
	if downloader.MaxRedirects != immutableArtifactMaxRedirects {
		t.Fatalf("maximum redirects = %d, want %d", downloader.MaxRedirects, immutableArtifactMaxRedirects)
	}
}

func TestProductionSemanticInstallerSanitizesFailures(t *testing.T) {
	secret := "token=super-secret /private/user/worktrail"
	tests := []struct {
		name   string
		mutate func(*semanticInstallerProductionDependencies)
		want   error
		code   contracts.ReasonCode
	}{
		{
			name: "malformed manifest",
			mutate: func(deps *semanticInstallerProductionDependencies) {
				deps.parseManifest = func([]byte) (bundle.Manifest, error) {
					return bundle.Manifest{}, errors.New(secret)
				}
			},
			want: ErrSemanticInstallerUnavailable,
			code: contracts.ReasonRuntimeUnavailable,
		},
		{
			name: "unsupported platform",
			mutate: func(deps *semanticInstallerProductionDependencies) {
				deps.detectChip = func() (string, error) {
					return "", fmt.Errorf("%w: %s", bundle.ErrUnsupportedPlatform, secret)
				}
			},
			want: ErrSemanticPlatformUnsupported,
			code: contracts.ReasonPlatformUnsupported,
		},
		{
			name: "unsupported chip",
			mutate: func(deps *semanticInstallerProductionDependencies) {
				deps.detectChip = func() (string, error) {
					return "", fmt.Errorf("%w: %s", bundle.ErrUnsupportedChip, secret)
				}
			},
			want: ErrSemanticPlatformUnsupported,
			code: contracts.ReasonPlatformUnsupported,
		},
		{
			name: "installer construction",
			mutate: func(deps *semanticInstallerProductionDependencies) {
				deps.buildInstaller = func(paths.SemanticRoots) (bundle.Installer, error) {
					return bundle.Installer{}, errors.New(secret)
				}
			},
			want: ErrSemanticInstallerUnavailable,
			code: contracts.ReasonRuntimeUnavailable,
		},
		{
			name: "installer error",
			mutate: func(deps *semanticInstallerProductionDependencies) {
				deps.install = func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error) {
					return bundle.InstallResult{}, errors.New(secret)
				}
			},
			want: ErrSemanticInstallerFailed,
			code: contracts.ReasonRuntimeUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			deps := semanticInstallerProductionDependencies{
				discoverRoots: func() (paths.SemanticRoots, error) {
					return paths.SemanticRoots{}, nil
				},
				parseManifest: func([]byte) (bundle.Manifest, error) {
					return bundle.Manifest{}, nil
				},
				detectChip: func() (string, error) {
					return "m1", nil
				},
				buildInstaller: func(paths.SemanticRoots) (bundle.Installer, error) {
					return bundle.Installer{}, nil
				},
				install: func(context.Context, bundle.Installer, bundle.Manifest, string) (bundle.InstallResult, error) {
					return bundle.InstallResult{}, nil
				},
			}
			test.mutate(&deps)

			_, err := (productionSemanticInstaller{deps: deps}).Install(context.Background(), paths.Env{})
			assertSemanticInstallerReason(t, err, test.want, test.code)
			if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "/private/") {
				t.Fatalf("Install() leaked sensitive detail: %q", err)
			}
		})
	}
}

func assertSemanticInstallerReason(t *testing.T, err, cause error, code contracts.ReasonCode) {
	t.Helper()
	if !errors.Is(err, cause) {
		t.Fatalf("error = %v, want wrapped %v", err, cause)
	}
	var semanticErr *daemon.Error
	if !errors.As(err, &semanticErr) || semanticErr.Code != code {
		t.Fatalf("error = %#v, want semantic reason %q", err, code)
	}
}

type installerControllerStub struct {
	stopCalls int
	stopErr   error
}

func (s *installerControllerStub) Status(context.Context) (daemon.Report, error) {
	return daemon.Report{}, nil
}

func (s *installerControllerStub) Start(context.Context) (daemon.Report, error) {
	return daemon.Report{}, nil
}

func (s *installerControllerStub) Stop(context.Context) (daemon.Report, error) {
	s.stopCalls++
	return daemon.Report{}, s.stopErr
}

func (s *installerControllerStub) Restart(context.Context) (daemon.Report, error) {
	return daemon.Report{}, nil
}
