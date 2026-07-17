package bundle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/klauspost/compress/zstd"

	"github.com/nickdu2009/worktrail/internal/paths"
)

const installedManifestName = "manifest.json"

// Downloader writes the content at url to destination. The installer owns the
// destination file and enforces its size limit while receiving the download.
type Downloader interface {
	Download(ctx context.Context, url string, destination io.Writer) error
}

// Installer installs a trusted semantic bundle into the semantic cache.
type Installer struct {
	Roots      paths.SemanticRoots
	Downloader Downloader

	// MaxDownloadSize rejects declared downloads larger than this limit. Zero
	// means the manifest's exact expected size is the limit.
	MaxDownloadSize int64
	// MaxDecompressedSize rejects executable output larger than this limit.
	// Zero means the manifest's exact executable size is the limit.
	MaxDecompressedSize int64
}

// InstallResult identifies the installed or reused bundle.
type InstallResult struct {
	BundlePath string
	Reused     bool
}

// InstallCheck runs after a bundle is installed or reused while its bundle lock
// is still held. Returning an error rejects and removes that bundle before a
// concurrent installer can reuse it.
type InstallCheck func(context.Context, InstallResult) error

// RetainBundleError tells InstallAndCheck that a failed check could not safely
// stop and clear its managed daemon. The trusted bundle must remain available
// so a later controlled Stop or recovery operation can still manage it.
type RetainBundleError struct {
	Err error
}

func (e *RetainBundleError) Error() string {
	if e == nil || e.Err == nil {
		return "semantic bundle install check requires bundle retention"
	}
	return e.Err.Error()
}

func (e *RetainBundleError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// RetainBundle wraps a failed install check when removing the bundle would
// orphan a daemon that still requires the trusted runtime to be managed.
func RetainBundle(err error) error {
	if err == nil {
		return nil
	}
	return &RetainBundleError{Err: err}
}

// Install fetches, verifies, and atomically publishes the bundle selected for
// chip. It only exposes the bundle path after every artifact is verified.
func (i Installer) Install(ctx context.Context, manifest Manifest, chip string) (InstallResult, error) {
	return i.install(ctx, manifest, chip, nil)
}

// InstallAndCheck performs installation or reuse, then runs check within the
// same bundle-lock transaction. A failed check removes the bundle before the
// lock is released.
func (i Installer) InstallAndCheck(ctx context.Context, manifest Manifest, chip string, check InstallCheck) (InstallResult, error) {
	if check == nil {
		return InstallResult{}, errors.New("semantic bundle install check is required")
	}
	return i.install(ctx, manifest, chip, check)
}

func (i Installer) install(ctx context.Context, manifest Manifest, chip string, check InstallCheck) (InstallResult, error) {
	if i.Downloader == nil {
		return InstallResult{}, errors.New("semantic bundle downloader is required")
	}
	if err := manifest.Validate(); err != nil {
		return InstallResult{}, fmt.Errorf("validate semantic bundle manifest: %w", err)
	}
	if err := ValidateEmbeddedManifestAssets(manifest); err != nil {
		return InstallResult{}, fmt.Errorf("validate embedded semantic bundle assets: %w", err)
	}
	embeddedAssets, err := requiredEmbeddedAssets(manifest)
	if err != nil {
		return InstallResult{}, fmt.Errorf("collect embedded semantic bundle assets: %w", err)
	}
	variant, err := selectVariant(manifest.Runtime.Variants, chip)
	if err != nil {
		return InstallResult{}, err
	}
	modelName, err := safeArtifactName(manifest.Model.File)
	if err != nil {
		return InstallResult{}, fmt.Errorf("invalid model filename: %w", err)
	}
	executableName, err := safeArtifactName(variant.Executable)
	if err != nil {
		return InstallResult{}, fmt.Errorf("invalid executable filename: %w", err)
	}
	if modelName == executableName || modelName == installedManifestName || executableName == installedManifestName {
		return InstallResult{}, errors.New("semantic bundle artifacts have conflicting names")
	}
	if err := i.checkDecompressedLimit(variant.ExecutableSize); err != nil {
		return InstallResult{}, fmt.Errorf("runtime executable: %w", err)
	}

	bundlePath, err := i.Roots.Bundle(manifest.BundleID)
	if err != nil {
		return InstallResult{}, fmt.Errorf("resolve semantic bundle path: %w", err)
	}
	parent := filepath.Dir(bundlePath)
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("create semantic bundle cache: %w", err)
	}
	releaseLock, err := acquireBundleLock(ctx, parent, manifest.BundleID)
	if err != nil {
		return InstallResult{}, fmt.Errorf("lock semantic bundle installation: %w", err)
	}
	defer releaseLock()
	replaceExisting := false
	if info, err := os.Lstat(bundlePath); err == nil {
		if !info.IsDir() {
			return InstallResult{}, fmt.Errorf("semantic bundle target is not a directory: %s", bundlePath)
		}
		if err := verifyInstalledBundle(bundlePath, manifest, variant, modelName, executableName, embeddedAssets); err != nil {
			if !installedForOtherChip(bundlePath, manifest, chip, modelName, embeddedAssets) {
				return InstallResult{}, fmt.Errorf("semantic bundle target is incomplete or mismatched: %w", err)
			}
			replaceExisting = true
		} else {
			return i.finishInstall(ctx, InstallResult{BundlePath: bundlePath, Reused: true}, check)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return InstallResult{}, fmt.Errorf("inspect semantic bundle target: %w", err)
	}
	stagingPath, err := os.MkdirTemp(parent, ".semantic-bundle-staging-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("create semantic bundle staging directory: %w", err)
	}
	defer os.RemoveAll(stagingPath)
	if err := os.Chmod(stagingPath, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("secure semantic bundle staging directory: %w", err)
	}
	if err := writeEmbeddedAssets(stagingPath, embeddedAssets); err != nil {
		return InstallResult{}, fmt.Errorf("write embedded semantic bundle assets: %w", err)
	}

	modelTemporary := filepath.Join(stagingPath, ".model.download")
	if err := i.downloadAndVerify(ctx, manifest.Model.URL, modelTemporary, manifest.Model.Size, manifest.Model.SHA256); err != nil {
		return InstallResult{}, fmt.Errorf("download semantic model: %w", err)
	}
	modelPath := filepath.Join(stagingPath, modelName)
	if err := os.Rename(modelTemporary, modelPath); err != nil {
		return InstallResult{}, fmt.Errorf("place semantic model: %w", err)
	}
	if err := os.Chmod(modelPath, 0o600); err != nil {
		return InstallResult{}, fmt.Errorf("secure semantic model: %w", err)
	}

	runtimeTemporary := filepath.Join(stagingPath, ".runtime.download")
	if err := i.downloadAndVerify(ctx, variant.RuntimeURL, runtimeTemporary, variant.CompressedSize, variant.CompressedSHA256); err != nil {
		return InstallResult{}, fmt.Errorf("download semantic runtime: %w", err)
	}
	executablePath := filepath.Join(stagingPath, executableName)
	if err := decompressAndVerify(runtimeTemporary, executablePath, variant.ExecutableSize, variant.ExecutableSHA256, i.decompressedLimit(variant.ExecutableSize)); err != nil {
		return InstallResult{}, fmt.Errorf("decompress semantic runtime: %w", err)
	}
	if err := os.Remove(runtimeTemporary); err != nil {
		return InstallResult{}, fmt.Errorf("remove compressed semantic runtime: %w", err)
	}
	if err := os.Chmod(executablePath, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("make semantic runtime executable: %w", err)
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return InstallResult{}, fmt.Errorf("encode installed semantic manifest: %w", err)
	}
	if err := writeAndSyncFile(filepath.Join(stagingPath, installedManifestName), manifestData, 0o600); err != nil {
		return InstallResult{}, fmt.Errorf("write installed semantic manifest: %w", err)
	}
	if err := os.Chmod(stagingPath, 0o700); err != nil {
		return InstallResult{}, fmt.Errorf("secure semantic bundle directory: %w", err)
	}
	if err := syncDirectory(stagingPath); err != nil {
		return InstallResult{}, fmt.Errorf("sync semantic bundle staging directory: %w", err)
	}
	if err := publishStagedBundle(stagingPath, bundlePath, parent, replaceExisting); err != nil {
		return InstallResult{}, err
	}
	return i.finishInstall(ctx, InstallResult{BundlePath: bundlePath}, check)
}

func installedForOtherChip(bundlePath string, manifest Manifest, chip, modelName string, embeddedAssets []embeddedAsset) bool {
	for _, candidate := range manifest.Runtime.Variants {
		if candidate.Chip == chip {
			continue
		}
		executableName, err := safeArtifactName(candidate.Executable)
		if err != nil {
			continue
		}
		if err := verifyInstalledBundle(bundlePath, manifest, candidate, modelName, executableName, embeddedAssets); err == nil {
			return true
		}
	}
	return false
}

func publishStagedBundle(stagingPath, bundlePath, parent string, replaceExisting bool) error {
	if !replaceExisting {
		if err := os.Rename(stagingPath, bundlePath); err != nil {
			return fmt.Errorf("publish semantic bundle: %w", err)
		}
		if err := syncDirectory(parent); err != nil {
			return fmt.Errorf("sync semantic bundle parent directory: %w", err)
		}
		return nil
	}

	previousPath := stagingPath + ".previous"
	if err := os.Rename(bundlePath, previousPath); err != nil {
		return fmt.Errorf("move existing semantic bundle aside: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return restorePreviousBundle(previousPath, bundlePath, parent, fmt.Errorf("sync semantic bundle parent directory: %w", err))
	}
	if err := os.Rename(stagingPath, bundlePath); err != nil {
		return restorePreviousBundle(previousPath, bundlePath, parent, fmt.Errorf("replace semantic bundle: %w", err))
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync replaced semantic bundle parent directory: %w", err)
	}
	if err := os.RemoveAll(previousPath); err != nil {
		return fmt.Errorf("remove replaced semantic bundle: %w", err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("sync cleaned semantic bundle parent directory: %w", err)
	}
	return nil
}

func restorePreviousBundle(previousPath, bundlePath, parent string, original error) error {
	if err := os.Rename(previousPath, bundlePath); err != nil {
		return fmt.Errorf("%v; restore previous semantic bundle: %w", original, err)
	}
	if err := syncDirectory(parent); err != nil {
		return fmt.Errorf("%v; sync restored semantic bundle parent directory: %w", original, err)
	}
	return original
}

func (i Installer) finishInstall(ctx context.Context, result InstallResult, check InstallCheck) (InstallResult, error) {
	if check == nil {
		return result, nil
	}
	if err := check(ctx, result); err == nil {
		return result, nil
	} else {
		var retain *RetainBundleError
		if errors.As(err, &retain) {
			return InstallResult{}, fmt.Errorf("semantic bundle install check retained bundle: %w", err)
		}
		if removeErr := os.RemoveAll(result.BundlePath); removeErr != nil {
			return InstallResult{}, fmt.Errorf("reject semantic bundle after install check: %w", removeErr)
		}
		if syncErr := syncDirectory(filepath.Dir(result.BundlePath)); syncErr != nil {
			return InstallResult{}, fmt.Errorf("sync rejected semantic bundle parent: %w", syncErr)
		}
		return InstallResult{}, fmt.Errorf("semantic bundle install check: %w", err)
	}
}

func selectVariant(variants []RuntimeVariant, chip string) (RuntimeVariant, error) {
	var matches []RuntimeVariant
	for _, variant := range variants {
		if variant.Chip == chip {
			matches = append(matches, variant)
		}
	}
	switch len(matches) {
	case 0:
		return RuntimeVariant{}, fmt.Errorf("no semantic runtime variant for chip %q", chip)
	case 1:
		return matches[0], nil
	default:
		return RuntimeVariant{}, fmt.Errorf("ambiguous semantic runtime variants for chip %q", chip)
	}
}

func safeArtifactName(name string) (string, error) {
	if name == "" || filepath.Base(name) != name || name == "." || name == ".." {
		return "", errors.New("must be a filename")
	}
	return name, nil
}

type embeddedAsset struct {
	reference AssetReference
	data      []byte
	relative  string
}

func requiredEmbeddedAssets(manifest Manifest) ([]embeddedAsset, error) {
	references := []*AssetReference{
		manifest.Model.LicenseFile,
		manifest.Model.AttributionFile,
	}
	for _, variant := range manifest.Runtime.Variants {
		references = append(references, variant.LicenseFile, variant.AttributionFile)
	}

	assets := make([]embeddedAsset, 0, len(references))
	seen := make(map[string]struct{}, len(references))
	for _, reference := range references {
		if reference == nil {
			return nil, errors.New("missing embedded asset reference")
		}
		if _, ok := seen[reference.Path]; ok {
			continue
		}
		relative, err := embeddedAssetRelativePath(reference.Path)
		if err != nil {
			return nil, err
		}
		data, err := EmbeddedBundleAssets.ReadFile(reference.Path)
		if err != nil {
			return nil, err
		}
		assets = append(assets, embeddedAsset{
			reference: *reference,
			data:      data,
			relative:  relative,
		})
		seen[reference.Path] = struct{}{}
	}
	return assets, nil
}

func embeddedAssetRelativePath(assetPath string) (string, error) {
	relative, ok := strings.CutPrefix(assetPath, "assets/")
	if !ok || relative == "" {
		return "", errors.New("embedded asset path must be inside assets/")
	}
	relative = filepath.FromSlash(relative)
	if filepath.IsAbs(relative) || relative == "." || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("embedded asset path escapes bundle")
	}
	return relative, nil
}

func writeEmbeddedAssets(stagingPath string, assets []embeddedAsset) error {
	for _, asset := range assets {
		target := filepath.Join(stagingPath, asset.relative)
		relative, err := filepath.Rel(stagingPath, target)
		if err != nil || relative == "." || relative == ".." ||
			strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return errors.New("embedded asset target escapes staging directory")
		}
		parent := filepath.Dir(target)
		if err := os.MkdirAll(parent, 0o700); err != nil {
			return err
		}
		if err := writeAndSyncFile(target, asset.data, 0o600); err != nil {
			return err
		}
		if err := os.Chmod(target, 0o600); err != nil {
			return err
		}
		if err := syncDirectory(parent); err != nil {
			return err
		}
	}
	return nil
}

func verifySafeEmbeddedAssetPath(bundlePath, relative string) (string, error) {
	target := filepath.Join(bundlePath, relative)
	contained, err := filepath.Rel(bundlePath, target)
	if err != nil || contained == "." || contained == ".." ||
		strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", errors.New("installed embedded asset path escapes bundle")
	}

	current := bundlePath
	parts := strings.Split(relative, string(filepath.Separator))
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", errors.New("invalid installed embedded asset path")
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("installed embedded asset path contains symlink")
		}
		if index < len(parts)-1 && !info.IsDir() {
			return "", errors.New("installed embedded asset parent is not a directory")
		}
		if index == len(parts)-1 && !info.Mode().IsRegular() {
			return "", errors.New("installed embedded asset is not a regular file")
		}
	}
	return target, nil
}

func (i Installer) checkDecompressedLimit(size int64) error {
	if size <= 0 {
		return errors.New("invalid expected size")
	}
	if i.MaxDecompressedSize > 0 && size > i.MaxDecompressedSize {
		return fmt.Errorf("declared size %d exceeds limit %d", size, i.MaxDecompressedSize)
	}
	return nil
}

func (i Installer) decompressedLimit(expected int64) int64 {
	if i.MaxDecompressedSize > 0 {
		return i.MaxDecompressedSize
	}
	return expected
}

func (i Installer) downloadAndVerify(ctx context.Context, url, destination string, expectedSize int64, expectedSHA256 string) error {
	if expectedSize <= 0 {
		return errors.New("invalid expected download size")
	}
	file, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	limited := &sizeLimitedWriter{writer: file, remaining: i.downloadLimit(expectedSize)}
	downloadErr := i.Downloader.Download(ctx, url, limited)
	syncErr := file.Sync()
	closeErr := file.Close()
	if downloadErr != nil {
		return downloadErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if limited.exceeded {
		return fmt.Errorf("download exceeds limit %d", i.downloadLimit(expectedSize))
	}
	if err := verifyFile(destination, expectedSize, expectedSHA256); err != nil {
		return err
	}
	return nil
}

func decompressAndVerify(source, destination string, expectedSize int64, expectedSHA256 string, maxSize int64) error {
	if maxSize <= 0 {
		return errors.New("invalid decompression limit")
	}
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()

	decoder, err := zstd.NewReader(input)
	if err != nil {
		return err
	}
	defer decoder.Close()

	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o700)
	if err != nil {
		return err
	}
	written, copyErr := io.Copy(output, io.LimitReader(decoder, maxSize+1))
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if syncErr != nil {
		return syncErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > maxSize {
		return fmt.Errorf("decompressed runtime exceeds limit %d", maxSize)
	}
	return verifyFile(destination, expectedSize, expectedSHA256)
}

type sizeLimitedWriter struct {
	writer    io.Writer
	remaining int64
	exceeded  bool
}

func (w *sizeLimitedWriter) Write(data []byte) (int, error) {
	if int64(len(data)) <= w.remaining {
		written, err := w.writer.Write(data)
		w.remaining -= int64(written)
		return written, err
	}
	w.exceeded = true
	if w.remaining == 0 {
		return 0, errors.New("download size limit exceeded")
	}
	written, err := w.writer.Write(data[:w.remaining])
	w.remaining -= int64(written)
	if err != nil {
		return written, err
	}
	return written, errors.New("download size limit exceeded")
}

func (i Installer) downloadLimit(expected int64) int64 {
	if i.MaxDownloadSize > 0 && i.MaxDownloadSize < expected {
		return i.MaxDownloadSize
	}
	return expected
}

func writeAndSyncFile(path string, data []byte, mode os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func verifyInstalledBundle(bundlePath string, expected Manifest, variant RuntimeVariant, modelName, executableName string, embeddedAssets []embeddedAsset) error {
	bundleInfo, err := os.Stat(bundlePath)
	if err != nil {
		return err
	}
	if bundleInfo.Mode().Perm() != 0o700 {
		return errors.New("installed bundle does not have mode 0700")
	}
	data, err := os.ReadFile(filepath.Join(bundlePath, installedManifestName))
	if err != nil {
		return err
	}
	actual, err := ParseTrustedManifest(data)
	if err != nil {
		return err
	}
	expectedCanonical, err := expected.CanonicalBytes()
	if err != nil {
		return err
	}
	actualCanonical, err := actual.CanonicalBytes()
	if err != nil {
		return err
	}
	if actual.BundleID != expected.BundleID || string(actualCanonical) != string(expectedCanonical) {
		return errors.New("installed manifest does not match requested manifest")
	}
	if err := verifyFile(filepath.Join(bundlePath, modelName), expected.Model.Size, expected.Model.SHA256); err != nil {
		return fmt.Errorf("installed model: %w", err)
	}
	if err := verifyFile(filepath.Join(bundlePath, executableName), variant.ExecutableSize, variant.ExecutableSHA256); err != nil {
		return fmt.Errorf("installed executable: %w", err)
	}
	info, err := os.Stat(filepath.Join(bundlePath, executableName))
	if err != nil {
		return err
	}
	if info.Mode().Perm() != 0o700 {
		return errors.New("installed executable does not have mode 0700")
	}
	for _, asset := range embeddedAssets {
		target, err := verifySafeEmbeddedAssetPath(bundlePath, asset.relative)
		if err != nil {
			return err
		}
		if err := verifyFile(target, int64(len(asset.data)), asset.reference.SHA256); err != nil {
			return fmt.Errorf("installed embedded asset %q: %w", asset.reference.Path, err)
		}
		info, err := os.Stat(target)
		if err != nil {
			return err
		}
		if info.Mode().Perm() != 0o600 {
			return fmt.Errorf("installed embedded asset %q does not have mode 0600", asset.reference.Path)
		}
	}
	return nil
}

func verifyFile(path string, expectedSize int64, expectedSHA256 string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() != expectedSize {
		return fmt.Errorf("size %d does not match expected %d", info.Size(), expectedSize)
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	actualSHA256 := hex.EncodeToString(hash.Sum(nil))
	if actualSHA256 != expectedSHA256 {
		return errors.New("SHA-256 does not match")
	}
	return nil
}
