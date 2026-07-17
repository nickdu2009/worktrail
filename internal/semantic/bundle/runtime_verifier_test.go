package bundle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

func TestRuntimeVerifierVerifiesManifestResolvedRuntimeWithoutMutation(t *testing.T) {
	roots, manifest, root, spec := runtimeVerifierFixture(t)
	loadCalls := 0
	detectCalls := 0
	verifier := &RuntimeVerifier{
		Roots: roots,
		loadInstalled: func(roots paths.SemanticRoots, bundleID, chip string) (InstalledBundle, error) {
			loadCalls++
			return loadInstalled(roots, bundleID, manifest, chip)
		},
		detectDarwinChip: func() (string, error) {
			detectCalls++
			return "m1", nil
		},
	}
	before := bundleContents(t, root)

	if err := verifier.VerifyRuntime(context.Background(), spec); err != nil {
		t.Fatalf("VerifyRuntime() error = %v", err)
	}

	if loadCalls != 1 || detectCalls != 1 {
		t.Fatalf("verification calls = load:%d detect:%d, want one each", loadCalls, detectCalls)
	}
	if after := bundleContents(t, root); !reflect.DeepEqual(after, before) {
		t.Fatalf("VerifyRuntime() mutated bundle:\nbefore: %#v\nafter: %#v", before, after)
	}
}

func TestRuntimeVerifierRejectsTamperedOrMissingBundleSafely(t *testing.T) {
	t.Run("tampered artifact", func(t *testing.T) {
		roots, manifest, root, spec := runtimeVerifierFixture(t)
		writeInstalledTestFile(t, filepath.Join(root, "llama-server"), []byte("tampered"), 0o700)
		verifier := verifierForManifest(roots, manifest, func() (string, error) {
			return "m1", nil
		})
		before := bundleContents(t, root)

		err := verifier.VerifyRuntime(context.Background(), spec)

		assertVerifierFailure(t, err, contracts.ReasonRuntimeUnavailable, root)
		if after := bundleContents(t, root); !reflect.DeepEqual(after, before) {
			t.Fatalf("VerifyRuntime() mutated tampered bundle:\nbefore: %#v\nafter: %#v", before, after)
		}
	})

	t.Run("missing bundle", func(t *testing.T) {
		roots, manifest, root, spec := runtimeVerifierFixture(t)
		if err := os.RemoveAll(root); err != nil {
			t.Fatal(err)
		}
		verifier := verifierForManifest(roots, manifest, func() (string, error) {
			return "m1", nil
		})

		err := verifier.VerifyRuntime(context.Background(), spec)

		assertVerifierFailure(t, err, contracts.ReasonBundleMissing, root)
	})
}

func TestRuntimeVerifierRejectsUnsupportedOrMissingVariantSafely(t *testing.T) {
	t.Run("unsupported platform", func(t *testing.T) {
		roots, manifest, root, spec := runtimeVerifierFixture(t)
		verifier := verifierForManifest(roots, manifest, func() (string, error) {
			return "", ErrUnsupportedPlatform
		})

		err := verifier.VerifyRuntime(context.Background(), spec)

		assertVerifierFailure(t, err, contracts.ReasonPlatformUnsupported, root)
	})

	t.Run("missing trusted variant", func(t *testing.T) {
		roots, manifest, root, spec := runtimeVerifierFixture(t)
		verifier := verifierForManifest(roots, manifest, func() (string, error) {
			return "m2", nil
		})

		err := verifier.VerifyRuntime(context.Background(), spec)

		assertVerifierFailure(t, err, contracts.ReasonPlatformUnsupported, root)
	})
}

func TestRuntimeVerifierRejectsMismatchedRuntimeSpec(t *testing.T) {
	roots, manifest, root, spec := runtimeVerifierFixture(t)
	tests := []struct {
		name string
		edit func(*daemon.RuntimeSpec)
		code contracts.ReasonCode
	}{
		{
			name: "bundle ID",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.BundleID = "other-bundle"
			},
			code: contracts.ReasonBundleMissing,
		},
		{
			name: "runtime path",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.RuntimePath = "/untrusted/runtime"
			},
		},
		{
			name: "model path",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.ModelPath = "/untrusted/model"
			},
		},
		{
			name: "working directory",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.WorkingDir = "/untrusted"
			},
		},
		{
			name: "alias",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.Alias = "other-alias"
			},
		},
		{
			name: "dimension",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.Dimension = 384
			},
		},
		{
			name: "llama app version",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.LlamaAppVersion = "other-version"
			},
		},
		{
			name: "runtime SHA-256",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.RuntimeSHA256 = strings.Repeat("f", 64)
			},
		},
		{
			name: "chip variant",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.ChipVariant = "m2"
			},
		},
		{
			name: "model SHA-256",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.ModelSHA256 = strings.Repeat("f", 64)
			},
		},
		{
			name: "support level",
			edit: func(spec *daemon.RuntimeSpec) {
				spec.SupportLevel = "experimental"
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mismatched := spec
			test.edit(&mismatched)
			verifier := verifierForManifest(roots, manifest, func() (string, error) {
				return "m1", nil
			})
			code := test.code
			if code == "" {
				code = contracts.ReasonRuntimeUnavailable
			}

			err := verifier.VerifyRuntime(context.Background(), mismatched)

			assertVerifierFailure(t, err, code, root)
		})
	}
}

func runtimeVerifierFixture(t *testing.T) (paths.SemanticRoots, Manifest, string, daemon.RuntimeSpec) {
	t.Helper()
	model := []byte("model")
	runtime := []byte("runtime")
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", compressed(t, runtime), runtime),
	})
	roots := testRoots(t)
	root := writeInstalledBundle(t, roots, manifest.BundleID, manifest, model, map[string][]byte{
		"m1": runtime,
	})
	installed, err := loadInstalled(roots, manifest.BundleID, manifest, "m1")
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := installed.ResolveRuntime("m1")
	if err != nil {
		t.Fatal(err)
	}
	return roots, manifest, root, daemon.RuntimeSpec{
		BundleID:        resolved.BundleID,
		RuntimePath:     resolved.RuntimePath,
		ModelPath:       resolved.ModelPath,
		WorkingDir:      installed.BundleRoot,
		Alias:           resolved.BundleID,
		Dimension:       expectedEmbeddingDimension,
		LlamaAppVersion: resolved.RuntimeVersion,
		RuntimeSHA256:   resolved.RuntimeSHA256,
		ChipVariant:     resolved.Chip,
		ModelSHA256:     resolved.ModelSHA256,
		SupportLevel:    resolved.SupportLevel,
	}
}

func verifierForManifest(roots paths.SemanticRoots, manifest Manifest, detect func() (string, error)) *RuntimeVerifier {
	return &RuntimeVerifier{
		Roots: roots,
		loadInstalled: func(roots paths.SemanticRoots, bundleID, chip string) (InstalledBundle, error) {
			return loadInstalled(roots, bundleID, manifest, chip)
		},
		detectDarwinChip: detect,
	}
}

func assertVerifierFailure(t *testing.T, err error, want contracts.ReasonCode, forbidden string) {
	t.Helper()
	var daemonErr *daemon.Error
	if !errors.As(err, &daemonErr) {
		t.Fatalf("VerifyRuntime() error = %T %[1]v, want *daemon.Error", err)
	}
	if daemonErr.Code != want {
		t.Fatalf("VerifyRuntime() code = %q, want %q", daemonErr.Code, want)
	}
	if daemonErr.Error() != runtimeVerificationMessage || daemonErr.Err != nil {
		t.Fatalf("VerifyRuntime() unsafe error = %#v", daemonErr)
	}
	if strings.Contains(err.Error(), forbidden) {
		t.Fatalf("VerifyRuntime() exposed bundle path in %q", err)
	}
}

func bundleContents(t *testing.T, root string) map[string]string {
	t.Helper()
	contents := make(map[string]string)
	if err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		contents[relative] = string(data)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return contents
}
