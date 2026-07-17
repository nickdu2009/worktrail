package bundle

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestLoadInstalledVerifiesCompleteBundleAndResolvesVariant(t *testing.T) {
	model := []byte("model")
	m1Runtime := []byte("m1 runtime")
	m3Runtime := []byte("m3 runtime")
	m3Variant := testVariant("m3", "experimental", "runtime://m3", compressed(t, m3Runtime), m3Runtime)
	m3Variant.Executable = "llama-server-m3"
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", compressed(t, m1Runtime), m1Runtime),
		m3Variant,
	})
	roots := testRoots(t)
	writeInstalledBundle(t, roots, manifest.BundleID, manifest, model, map[string][]byte{
		"m1": m1Runtime,
		"m3": m3Runtime,
	})

	bundle, err := loadInstalled(roots, manifest.BundleID, manifest, "m1")
	if err != nil {
		t.Fatalf("loadInstalled() error = %v", err)
	}
	if bundle.BundleRoot == "" || !filepath.IsAbs(bundle.BundleRoot) {
		t.Fatalf("BundleRoot = %q, want absolute path", bundle.BundleRoot)
	}

	resolved, err := bundle.ResolveRuntime("m1")
	if err != nil {
		t.Fatalf("ResolveRuntime(m1) error = %v", err)
	}
	if resolved.BundleID != manifest.BundleID ||
		resolved.RuntimeSHA256 != manifest.Runtime.Variants[0].ExecutableSHA256 ||
		resolved.ModelSHA256 != manifest.Model.SHA256 ||
		resolved.RuntimeVersion != manifest.Runtime.Variants[0].ExecutableVersion ||
		resolved.Chip != "m1" ||
		resolved.SupportLevel != "verified" {
		t.Fatalf("resolved M1 = %#v", resolved)
	}
	if !filepath.IsAbs(resolved.RuntimePath) || !filepath.IsAbs(resolved.ModelPath) {
		t.Fatalf("resolved paths must be absolute: %#v", resolved)
	}

	experimental, err := bundle.ResolveRuntime("m3")
	if err != nil {
		t.Fatalf("ResolveRuntime(m3) error = %v", err)
	}
	if experimental.SupportLevel != "experimental" {
		t.Fatalf("M3 support level = %q, want experimental", experimental.SupportLevel)
	}
	if _, err := bundle.ResolveRuntime("m2"); !errors.Is(err, ErrRuntimeVariantUnavailable) {
		t.Fatalf("ResolveRuntime(m2) error = %v, want ErrRuntimeVariantUnavailable", err)
	}
}

func TestLoadInstalledRequiresOnlySelectedExperimentalRuntime(t *testing.T) {
	for _, chip := range []string{"m2", "m3", "m4", "m5"} {
		t.Run(chip, func(t *testing.T) {
			model := []byte("model")
			runtime := []byte(chip + " runtime")
			manifest := testManifest(model, []RuntimeVariant{
				testVariant("m1", "verified", "runtime://m1", compressed(t, []byte("m1 runtime")), []byte("m1 runtime")),
				testVariant(chip, "experimental", "runtime://"+chip, compressed(t, runtime), runtime),
			})
			roots := testRoots(t)
			root, err := roots.Bundle(manifest.BundleID)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(root, 0o700); err != nil {
				t.Fatal(err)
			}
			writeManifestFile(t, root, manifest)
			writeInstalledTestFile(t, filepath.Join(root, manifest.Model.File), model, 0o600)
			variant, err := selectedRuntimeVariant(manifest.Runtime.Variants, chip)
			if err != nil {
				t.Fatal(err)
			}
			writeInstalledTestFile(t, filepath.Join(root, variant.Executable), runtime, 0o700)
			for _, reference := range installedAssetReferences(manifest) {
				relative, err := embeddedAssetRelativePath(reference.Path)
				if err != nil {
					t.Fatal(err)
				}
				data, err := EmbeddedBundleAssets.ReadFile(reference.Path)
				if err != nil {
					t.Fatal(err)
				}
				writeInstalledTestFile(t, filepath.Join(root, relative), data, 0o600)
			}

			installed, err := loadInstalled(roots, manifest.BundleID, manifest, chip)
			if err != nil {
				t.Fatalf("loadInstalled(%s): %v", chip, err)
			}
			resolved, err := installed.ResolveRuntime(chip)
			if err != nil {
				t.Fatalf("ResolveRuntime(%s): %v", chip, err)
			}
			if resolved.Chip != chip || resolved.SupportLevel != "experimental" {
				t.Fatalf("resolved runtime = %#v", resolved)
			}
		})
	}
}

func TestLoadInstalledRejectsManifestMismatch(t *testing.T) {
	model := []byte("model")
	runtime := []byte("runtime")
	trusted := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", compressed(t, runtime), runtime),
	})
	actual := trusted
	actual.Model.Revision = "other-revision"
	refreshBundleID(t, &actual)

	roots := testRoots(t)
	writeInstalledBundle(t, roots, trusted.BundleID, actual, model, map[string][]byte{"m1": runtime})
	if _, err := loadInstalled(roots, trusted.BundleID, trusted, "m1"); !errors.Is(err, ErrInstalledBundleInvalid) {
		t.Fatalf("loadInstalled() error = %v, want invalid bundle", err)
	}
}

func TestLoadInstalledRejectsProfileManifestMismatch(t *testing.T) {
	model := []byte("model")
	runtime := []byte("runtime")
	trusted := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", compressed(t, runtime), runtime),
	})
	actual := trusted
	actual.Embedding.QueryTemplate = "query: {text}"
	refreshBundleID(t, &actual)

	roots := testRoots(t)
	writeInstalledBundle(t, roots, trusted.BundleID, actual, model, map[string][]byte{"m1": runtime})
	if _, err := loadInstalled(roots, trusted.BundleID, trusted, "m1"); !errors.Is(err, ErrInstalledBundleInvalid) {
		t.Fatalf("loadInstalled() error = %v, want invalid bundle", err)
	}
}

func TestLoadInstalledRejectsTamperedOrPartialArtifacts(t *testing.T) {
	model := []byte("model")
	runtime := []byte("runtime")
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", compressed(t, runtime), runtime),
	})

	tests := []struct {
		name   string
		mutate func(t *testing.T, root string)
	}{
		{
			name: "model hash",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "embedding.gguf"), []byte("other"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "runtime size",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(root, "llama-server"), append(runtime, '!'), 0o700); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "partial embedded asset",
			mutate: func(t *testing.T, root string) {
				t.Helper()
				if err := os.Remove(filepath.Join(root, "licenses", "MIT.txt")); err != nil {
					t.Fatal(err)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roots := testRoots(t)
			root := writeInstalledBundle(t, roots, manifest.BundleID, manifest, model, map[string][]byte{"m1": runtime})
			test.mutate(t, root)
			if _, err := loadInstalled(roots, manifest.BundleID, manifest, "m1"); !errors.Is(err, ErrInstalledBundleInvalid) {
				t.Fatalf("loadInstalled() error = %v, want invalid bundle", err)
			}
		})
	}
}

func TestLoadInstalledRejectsPathTraversal(t *testing.T) {
	model := []byte("model")
	runtime := []byte("runtime")
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", compressed(t, runtime), runtime),
	})
	manifest.Model.File = "../outside.gguf"
	refreshBundleID(t, &manifest)

	roots := testRoots(t)
	root, err := roots.Bundle(manifest.BundleID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, root, manifest)
	if _, err := loadInstalled(roots, manifest.BundleID, manifest, "m1"); !errors.Is(err, ErrInstalledBundleInvalid) {
		t.Fatalf("loadInstalled() error = %v, want invalid bundle", err)
	}
}

func TestLoadInstalledRejectsUntrustedBundleID(t *testing.T) {
	if _, err := LoadInstalled(testRoots(t), "not-the-embedded-bundle"); !errors.Is(err, ErrUntrustedBundleID) {
		t.Fatalf("LoadInstalled() error = %v, want ErrUntrustedBundleID", err)
	}
}

func writeInstalledBundle(t *testing.T, roots paths.SemanticRoots, rootBundleID string, manifest Manifest, model []byte, runtimes map[string][]byte) string {
	t.Helper()
	root, err := roots.Bundle(rootBundleID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(root, 0o700); err != nil {
		t.Fatal(err)
	}
	writeManifestFile(t, root, manifest)
	writeInstalledTestFile(t, filepath.Join(root, manifest.Model.File), model, 0o600)
	for _, variant := range manifest.Runtime.Variants {
		writeInstalledTestFile(t, filepath.Join(root, variant.Executable), runtimes[variant.Chip], 0o700)
	}
	for _, reference := range installedAssetReferences(manifest) {
		relative, err := embeddedAssetRelativePath(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := EmbeddedBundleAssets.ReadFile(reference.Path)
		if err != nil {
			t.Fatal(err)
		}
		writeInstalledTestFile(t, filepath.Join(root, relative), data, 0o600)
	}
	return root
}

func writeManifestFile(t *testing.T, root string, manifest Manifest) {
	t.Helper()
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	writeInstalledTestFile(t, filepath.Join(root, installedManifestName), data, 0o600)
}

func writeInstalledTestFile(t *testing.T, filePath string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, data, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filePath, mode); err != nil {
		t.Fatal(err)
	}
}
