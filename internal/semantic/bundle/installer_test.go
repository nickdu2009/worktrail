package bundle

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/nickdu2009/worktrail/internal/paths"
)

func TestInstallerSelectsVerifiedAndExperimentalVariants(t *testing.T) {
	model := []byte("model bytes")
	verifiedRuntime := compressed(t, []byte("verified executable"))
	experimentalRuntime := compressed(t, []byte("experimental executable"))
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", verifiedRuntime, []byte("verified executable")),
		testVariant("m3", "experimental", "runtime://experimental", experimentalRuntime, []byte("experimental executable")),
	})

	for _, test := range []struct {
		name       string
		chip       string
		executable []byte
	}{
		{name: "verified", chip: "m1", executable: []byte("verified executable")},
		{name: "experimental", chip: "m3", executable: []byte("experimental executable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			roots := testRoots(t)
			installer := Installer{
				Roots: roots,
				Downloader: fakeDownloader{files: map[string][]byte{
					"model://embedding":      model,
					"runtime://verified":     verifiedRuntime,
					"runtime://experimental": experimentalRuntime,
				}},
			}
			result, err := installer.Install(context.Background(), manifest, test.chip)
			if err != nil {
				t.Fatalf("Install() error = %v", err)
			}
			if result.Reused {
				t.Fatal("Install() unexpectedly reused a new bundle")
			}
			got, err := os.ReadFile(filepath.Join(result.BundlePath, "llama-server"))
			if err != nil {
				t.Fatalf("read executable: %v", err)
			}
			if !bytes.Equal(got, test.executable) {
				t.Fatalf("executable = %q, want %q", got, test.executable)
			}
		})
	}
}

func TestInstallerReplacesTrustedBundleForOtherChip(t *testing.T) {
	model := []byte("model bytes")
	m1Executable := []byte("m1 executable")
	m3Executable := []byte("m3 executable")
	m1Runtime := compressed(t, m1Executable)
	m3Runtime := compressed(t, m3Executable)
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://m1", m1Runtime, m1Executable),
		testVariant("m3", "experimental", "runtime://m3", m3Runtime, m3Executable),
	})
	roots := testRoots(t)
	installer := Installer{
		Roots: roots,
		Downloader: fakeDownloader{files: map[string][]byte{
			"model://embedding": model,
			"runtime://m1":      m1Runtime,
			"runtime://m3":      m3Runtime,
		}},
	}

	first, err := installer.Install(context.Background(), manifest, "m1")
	if err != nil {
		t.Fatalf("initial Install() error = %v", err)
	}
	if first.Reused {
		t.Fatal("initial Install() unexpectedly reused bundle")
	}

	replaced, err := installer.Install(context.Background(), manifest, "m3")
	if err != nil {
		t.Fatalf("cross-chip Install() error = %v", err)
	}
	if replaced.Reused {
		t.Fatal("cross-chip Install() reused the other chip runtime")
	}
	got, err := os.ReadFile(filepath.Join(replaced.BundlePath, "llama-server"))
	if err != nil {
		t.Fatalf("read replaced runtime: %v", err)
	}
	if !bytes.Equal(got, m3Executable) {
		t.Fatalf("replaced runtime = %q, want current-chip artifact %q", got, m3Executable)
	}

	m1Variant, err := selectVariant(manifest.Runtime.Variants, "m1")
	if err != nil {
		t.Fatalf("select m1 variant: %v", err)
	}
	m3Variant, err := selectVariant(manifest.Runtime.Variants, "m3")
	if err != nil {
		t.Fatalf("select m3 variant: %v", err)
	}
	embeddedAssets, err := requiredEmbeddedAssets(manifest)
	if err != nil {
		t.Fatalf("collect embedded assets: %v", err)
	}
	if err := verifyInstalledBundle(replaced.BundlePath, manifest, m3Variant, manifest.Model.File, m3Variant.Executable, embeddedAssets); err != nil {
		t.Fatalf("replaced bundle does not verify for m3: %v", err)
	}
	if err := verifyInstalledBundle(replaced.BundlePath, manifest, m1Variant, manifest.Model.File, m1Variant.Executable, embeddedAssets); err == nil {
		t.Fatal("replaced bundle verified for m1, want no cross-chip fallback")
	}

	reused, err := installer.Install(context.Background(), manifest, "m3")
	if err != nil {
		t.Fatalf("same-chip reuse Install() error = %v", err)
	}
	if !reused.Reused {
		t.Fatal("same-chip Install() did not reuse current-chip bundle")
	}
}

func TestInstallerRejectsUnavailableChipWithoutPublishing(t *testing.T) {
	model := []byte("model bytes")
	runtime := compressed(t, []byte("runtime"))
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, []byte("runtime")),
	})
	roots := testRoots(t)
	installer := Installer{
		Roots:      roots,
		Downloader: fakeDownloader{files: map[string][]byte{"model://embedding": model, "runtime://verified": runtime}},
	}

	if _, err := installer.Install(context.Background(), manifest, "m2"); err == nil {
		t.Fatal("Install() error = nil, want unavailable chip error")
	}
	assertNoPublishedBundle(t, roots, manifest.BundleID)
}

func TestInstallerChecksumFailureLeavesNoPartialBundle(t *testing.T) {
	model := []byte("model bytes")
	runtime := compressed(t, []byte("runtime"))
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, []byte("runtime")),
	})
	roots := testRoots(t)
	installer := Installer{
		Roots: roots,
		Downloader: fakeDownloader{files: map[string][]byte{
			"model://embedding":  []byte("tampered model"),
			"runtime://verified": runtime,
		}},
	}

	if _, err := installer.Install(context.Background(), manifest, "m1"); err == nil {
		t.Fatal("Install() error = nil, want checksum error")
	}
	assertNoPublishedBundle(t, roots, manifest.BundleID)
}

func TestInstallerRejectsMissingOrTamperedEmbeddedAssetsBeforeDownload(t *testing.T) {
	model := []byte("model bytes")
	runtime := compressed(t, []byte("runtime"))
	base := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, []byte("runtime")),
	})

	for _, test := range []struct {
		name   string
		mutate func(*Manifest)
	}{
		{
			name: "missing",
			mutate: func(manifest *Manifest) {
				manifest.Model.LicenseFile.Path = "assets/licenses/missing.txt"
				manifest.Model.LicenseFile.SHA256 = digest([]byte("missing"))
			},
		},
		{
			name: "tampered",
			mutate: func(manifest *Manifest) {
				manifest.Model.LicenseFile.SHA256 = digest([]byte("different embedded asset"))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest := base
			manifest.Model.LicenseFile = &AssetReference{
				Path:   base.Model.LicenseFile.Path,
				SHA256: base.Model.LicenseFile.SHA256,
			}
			test.mutate(&manifest)
			refreshBundleID(t, &manifest)

			roots := testRoots(t)
			downloader := &countingDownloader{}
			installer := Installer{Roots: roots, Downloader: downloader}
			if _, err := installer.Install(context.Background(), manifest, "m1"); err == nil {
				t.Fatal("Install() error = nil, want embedded asset validation error")
			}
			if downloader.calls != 0 {
				t.Fatalf("downloads = %d, want 0", downloader.calls)
			}
			assertNoPublishedBundle(t, roots, manifest.BundleID)
		})
	}
}

func TestInstallerRejectsDownloadOverflowWithoutPublishing(t *testing.T) {
	model := []byte("model")
	runtime := compressed(t, []byte("runtime"))
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, []byte("runtime")),
	})

	for _, test := range []struct {
		name        string
		model       []byte
		runtime     []byte
		maxDownload int64
	}{
		{name: "model declared size", model: append(model, '!'), runtime: runtime},
		{name: "runtime declared size", model: model, runtime: append(runtime, '!')},
		{name: "configured maximum", model: model, runtime: runtime, maxDownload: int64(len(model) - 1)},
	} {
		t.Run(test.name, func(t *testing.T) {
			roots := testRoots(t)
			installer := Installer{
				Roots:           roots,
				MaxDownloadSize: test.maxDownload,
				Downloader: fakeDownloader{files: map[string][]byte{
					"model://embedding":  test.model,
					"runtime://verified": test.runtime,
				}},
			}
			if _, err := installer.Install(context.Background(), manifest, "m1"); err == nil {
				t.Fatal("Install() error = nil, want download size limit error")
			}
			assertNoPublishedBundle(t, roots, manifest.BundleID)
		})
	}
}

func TestInstallerEnforcesDecompressionLimitWithoutPublishing(t *testing.T) {
	model := []byte("model bytes")
	declaredExecutable := []byte("runtime")
	runtime := compressed(t, []byte("runtime larger than declared"))
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, declaredExecutable),
	})
	roots := testRoots(t)
	installer := Installer{
		Roots: roots,
		Downloader: fakeDownloader{files: map[string][]byte{
			"model://embedding":  model,
			"runtime://verified": runtime,
		}},
	}

	if _, err := installer.Install(context.Background(), manifest, "m1"); err == nil {
		t.Fatal("Install() error = nil, want decompression limit error")
	}
	assertNoPublishedBundle(t, roots, manifest.BundleID)
}

func TestInstallerPublishesAtomicallyAndReusesCompleteBundle(t *testing.T) {
	model := []byte("model bytes")
	executable := []byte("runtime")
	runtime := compressed(t, executable)
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, executable),
	})
	roots := testRoots(t)
	bundlePath, err := roots.Bundle(manifest.BundleID)
	if err != nil {
		t.Fatalf("bundle path: %v", err)
	}
	downloader := &observingDownloader{
		files: map[string][]byte{
			"model://embedding":  model,
			"runtime://verified": runtime,
		},
		publishedPath: bundlePath,
	}
	installer := Installer{Roots: roots, Downloader: downloader}

	result, err := installer.Install(context.Background(), manifest, "m1")
	if err != nil {
		t.Fatalf("Install() error = %v", err)
	}
	if result.BundlePath != bundlePath {
		t.Fatalf("BundlePath = %q, want %q", result.BundlePath, bundlePath)
	}
	if downloader.calls != 2 {
		t.Fatalf("downloads = %d, want 2", downloader.calls)
	}
	assertMode(t, bundlePath, 0o700)
	assertMode(t, filepath.Join(bundlePath, "llama-server"), 0o700)
	embeddedLicense, err := EmbeddedBundleAssets.ReadFile("assets/licenses/MIT.txt")
	if err != nil {
		t.Fatalf("read embedded license: %v", err)
	}
	installedLicensePath := filepath.Join(bundlePath, "licenses", "MIT.txt")
	installedLicense, err := os.ReadFile(installedLicensePath)
	if err != nil {
		t.Fatalf("read installed license: %v", err)
	}
	if !bytes.Equal(installedLicense, embeddedLicense) {
		t.Fatal("installed license does not match embedded license")
	}
	assertMode(t, installedLicensePath, 0o600)

	result, err = installer.Install(context.Background(), manifest, "m1")
	if err != nil {
		t.Fatalf("reuse Install() error = %v", err)
	}
	if !result.Reused {
		t.Fatal("reuse Install() did not report Reused")
	}
	if downloader.calls != 2 {
		t.Fatalf("reuse downloaded %d files, want 2 total", downloader.calls)
	}

	if err := os.Remove(filepath.Join(bundlePath, "llama-server")); err != nil {
		t.Fatalf("remove executable from installed bundle: %v", err)
	}
	if _, err := installer.Install(context.Background(), manifest, "m1"); err == nil {
		t.Fatal("Install() error = nil for incomplete target")
	}
	if downloader.calls != 2 {
		t.Fatalf("incomplete target triggered downloads: %d, want 2 total", downloader.calls)
	}
}

func TestInstallerDoesNotReuseBundleWithTamperedEmbeddedAsset(t *testing.T) {
	model := []byte("model bytes")
	executable := []byte("runtime")
	runtime := compressed(t, executable)
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, executable),
		testVariant("m3", "experimental", "runtime://experimental", runtime, executable),
	})
	roots := testRoots(t)
	bundlePath, err := roots.Bundle(manifest.BundleID)
	if err != nil {
		t.Fatalf("bundle path: %v", err)
	}
	downloader := &observingDownloader{
		files: map[string][]byte{
			"model://embedding":      model,
			"runtime://verified":     runtime,
			"runtime://experimental": runtime,
		},
		publishedPath: bundlePath,
	}
	installer := Installer{Roots: roots, Downloader: downloader}
	if _, err := installer.Install(context.Background(), manifest, "m1"); err != nil {
		t.Fatalf("initial Install() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundlePath, "licenses", "MIT.txt"), []byte("tampered"), 0o600); err != nil {
		t.Fatalf("tamper installed license: %v", err)
	}
	if _, err := installer.Install(context.Background(), manifest, "m3"); err == nil {
		t.Fatal("Install() error = nil for tampered embedded asset")
	}
	if downloader.calls != 2 {
		t.Fatalf("tampered bundle triggered downloads: %d, want 2 total", downloader.calls)
	}
}

func TestInstallerSerializesConcurrentInstalls(t *testing.T) {
	model := []byte("model bytes")
	executable := []byte("runtime")
	runtime := compressed(t, executable)
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, executable),
	})
	downloader := &blockingDownloader{
		files: map[string][]byte{
			"model://embedding":  model,
			"runtime://verified": runtime,
		},
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	installer := Installer{Roots: testRoots(t), Downloader: downloader}
	type installResult struct {
		result InstallResult
		err    error
	}
	results := make(chan installResult, 2)
	go func() {
		result, err := installer.Install(context.Background(), manifest, "m1")
		results <- installResult{result: result, err: err}
	}()
	<-downloader.started
	go func() {
		result, err := installer.Install(context.Background(), manifest, "m1")
		results <- installResult{result: result, err: err}
	}()
	close(downloader.release)

	first, second := <-results, <-results
	if first.err != nil || second.err != nil {
		t.Fatalf("concurrent installs returned errors: %v; %v", first.err, second.err)
	}
	if downloader.callCount() != 2 {
		t.Fatalf("downloads = %d, want exactly 2", downloader.callCount())
	}
	if first.result.Reused == second.result.Reused {
		t.Fatalf("reuse results = %t, %t; want one install and one reuse", first.result.Reused, second.result.Reused)
	}
}

func TestInstallerKeepsBundleLockThroughFailedInstallCheck(t *testing.T) {
	model := []byte("model bytes")
	executable := []byte("runtime")
	runtime := compressed(t, executable)
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, executable),
	})
	roots := testRoots(t)
	installer := Installer{
		Roots: roots,
		Downloader: fakeDownloader{files: map[string][]byte{
			"model://embedding":  model,
			"runtime://verified": runtime,
		}},
	}

	checkStarted := make(chan struct{})
	releaseCheck := make(chan struct{})
	firstDone := make(chan error, 1)
	go func() {
		_, err := installer.InstallAndCheck(context.Background(), manifest, "m1", func(context.Context, InstallResult) error {
			close(checkStarted)
			<-releaseCheck
			return errors.New("self-check failed")
		})
		firstDone <- err
	}()
	<-checkStarted

	secondDone := make(chan error, 1)
	go func() {
		_, err := installer.Install(context.Background(), manifest, "m1")
		secondDone <- err
	}()
	select {
	case err := <-secondDone:
		t.Fatalf("concurrent install completed before failed check released lock: %v", err)
	default:
	}

	close(releaseCheck)
	if err := <-firstDone; err == nil {
		t.Fatal("InstallAndCheck() error = nil, want failed check")
	}
	if err := <-secondDone; err != nil {
		t.Fatalf("concurrent Install() error = %v", err)
	}
	bundlePath, err := roots.Bundle(manifest.BundleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("successful concurrent install did not retain bundle: %v", err)
	}
}

func TestInstallerRetainsBundleWhenInstallCheckCannotStopDaemon(t *testing.T) {
	model := []byte("model bytes")
	executable := []byte("runtime")
	runtime := compressed(t, executable)
	manifest := testManifest(model, []RuntimeVariant{
		testVariant("m1", "verified", "runtime://verified", runtime, executable),
	})
	roots := testRoots(t)
	installer := Installer{
		Roots: roots,
		Downloader: fakeDownloader{files: map[string][]byte{
			"model://embedding":  model,
			"runtime://verified": runtime,
		}},
	}
	if _, err := installer.Install(context.Background(), manifest, "m1"); err != nil {
		t.Fatalf("initial Install() error = %v", err)
	}

	_, err := installer.InstallAndCheck(context.Background(), manifest, "m1", func(context.Context, InstallResult) error {
		return RetainBundle(errors.New("controlled stop failed"))
	})
	var retained *RetainBundleError
	if !errors.As(err, &retained) {
		t.Fatalf("InstallAndCheck() error = %T %[1]v, want retained bundle error", err)
	}
	bundlePath, err := roots.Bundle(manifest.BundleID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(bundlePath); err != nil {
		t.Fatalf("Stop failure did not retain trusted bundle: %v", err)
	}
}

type fakeDownloader struct {
	files map[string][]byte
}

func (d fakeDownloader) Download(_ context.Context, url string, destination io.Writer) error {
	data, ok := d.files[url]
	if !ok {
		return errors.New("unexpected download URL")
	}
	_, err := destination.Write(data)
	return err
}

type countingDownloader struct {
	calls int
}

func (d *countingDownloader) Download(_ context.Context, _ string, _ io.Writer) error {
	d.calls++
	return errors.New("unexpected download")
}

type observingDownloader struct {
	files         map[string][]byte
	publishedPath string
	calls         int
}

func (d *observingDownloader) Download(_ context.Context, url string, destination io.Writer) error {
	if _, err := os.Stat(d.publishedPath); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			return errors.New("bundle published before downloads completed")
		}
		return err
	}
	d.calls++
	return fakeDownloader{files: d.files}.Download(context.Background(), url, destination)
}

type blockingDownloader struct {
	files   map[string][]byte
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (d *blockingDownloader) Download(ctx context.Context, url string, destination io.Writer) error {
	d.mu.Lock()
	d.calls++
	d.mu.Unlock()
	block := false
	d.once.Do(func() {
		block = true
		close(d.started)
	})
	if block {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-d.release:
		}
	}
	return fakeDownloader{files: d.files}.Download(ctx, url, destination)
}

func (d *blockingDownloader) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

func testRoots(t *testing.T) paths.SemanticRoots {
	t.Helper()
	return paths.SemanticRoots{Cache: t.TempDir(), Runtime: t.TempDir(), Logs: t.TempDir()}
}

func testManifest(model []byte, variants []RuntimeVariant) Manifest {
	manifest := Manifest{
		CanonicalManifest: CanonicalManifest{
			Schema:                  TrustedManifestSchema,
			SemanticAPIVersion:      1,
			GenerationSchemaVersion: 1,
			Embedding: EmbeddingProfile{
				Dimension:        BGEM3EmbeddingDimension,
				Tokenizer:        BGEM3Tokenizer,
				Pooling:          BGEM3Pooling,
				Normalization:    BGEM3Normalization,
				QueryTemplate:    BGEM3QueryTemplate,
				DocumentTemplate: BGEM3DocumentTemplate,
			},
			VectorMetric: CosineVectorMetric,
			Model: Model{
				Repository:      "example/embedding",
				Revision:        "1234567",
				File:            "embedding.gguf",
				URL:             "model://embedding",
				Size:            int64(len(model)),
				SHA256:          digest(model),
				License:         "MIT",
				Attribution:     "Example",
				LicenseFile:     embeddedAssetReference("assets/licenses/MIT.txt"),
				AttributionFile: embeddedAssetReference("assets/ATTRIBUTIONS.md"),
			},
			Runtime: Runtime{
				Distribution: "llama.app",
				Version:      "1.0.0",
				Variants:     variants,
			},
		},
	}
	bundleID, err := manifest.ExpectedBundleID()
	if err != nil {
		panic(err)
	}
	manifest.BundleID = bundleID
	return manifest
}

func testVariant(chip, supportLevel, url string, compressedRuntime, executable []byte) RuntimeVariant {
	variant := RuntimeVariant{
		Chip:              chip,
		SupportLevel:      supportLevel,
		Platform:          "darwin-arm64",
		RuntimeURL:        url,
		CompressedSize:    int64(len(compressedRuntime)),
		CompressedSHA256:  digest(compressedRuntime),
		Executable:        "llama-server",
		ExecutableVersion: "1.0.0",
		ExecutableSize:    int64(len(executable)),
		ExecutableSHA256:  digest(executable),
		License:           "MIT",
		Attribution:       "Example",
		LicenseFile:       embeddedAssetReference("assets/licenses/MIT.txt"),
		AttributionFile:   embeddedAssetReference("assets/ATTRIBUTIONS.md"),
	}
	if supportLevel == "verified" {
		variant.DeclaredMinimumMacOS = "14.0"
		variant.ResourceBudget = &ResourceBudget{
			ColdReadinessMSMax:          1,
			WarmSingleEmbeddingP95MSMax: 1,
			PeakRSSBytesMax:             1,
		}
	}
	return variant
}

func compressed(t *testing.T, data []byte) []byte {
	t.Helper()
	var output bytes.Buffer
	encoder, err := zstd.NewWriter(&output)
	if err != nil {
		t.Fatalf("create zstd encoder: %v", err)
	}
	if _, err := encoder.Write(data); err != nil {
		t.Fatalf("write zstd data: %v", err)
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("close zstd encoder: %v", err)
	}
	return output.Bytes()
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func embeddedAssetReference(assetPath string) *AssetReference {
	data, err := EmbeddedBundleAssets.ReadFile(assetPath)
	if err != nil {
		panic(err)
	}
	return &AssetReference{Path: assetPath, SHA256: digest(data)}
}

func refreshBundleID(t *testing.T, manifest *Manifest) {
	t.Helper()
	bundleID, err := manifest.ExpectedBundleID()
	if err != nil {
		t.Fatalf("expected bundle ID: %v", err)
	}
	manifest.BundleID = bundleID
}

func assertNoPublishedBundle(t *testing.T, roots paths.SemanticRoots, bundleID string) {
	t.Helper()
	bundlePath, err := roots.Bundle(bundleID)
	if err != nil {
		t.Fatalf("bundle path: %v", err)
	}
	if _, err := os.Stat(bundlePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("bundle path %q unexpectedly exists or could not be checked: %v", bundlePath, err)
	}
	parent := filepath.Dir(bundlePath)
	entries, err := os.ReadDir(parent)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		t.Fatalf("read bundle parent: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() == bundleID || len(entry.Name()) >= len(".semantic-bundle-staging-") && entry.Name()[:len(".semantic-bundle-staging-")] == ".semantic-bundle-staging-" {
			t.Fatalf("partial bundle artifact remains: %s", entry.Name())
		}
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode for %q = %o, want %o", path, got, want)
	}
}
