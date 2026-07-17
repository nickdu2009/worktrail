package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/paths"
)

const maxInstalledManifestSize = 1 << 20

var (
	// ErrUntrustedBundleID indicates that the requested ID is not the embedded
	// trusted manifest's content address.
	ErrUntrustedBundleID = errors.New("semantic bundle ID is not trusted")
	// ErrInstalledBundleInvalid indicates an installed bundle failed local
	// integrity verification.
	ErrInstalledBundleInvalid = errors.New("installed semantic bundle is invalid")
	// ErrRuntimeVariantUnavailable indicates the trusted manifest has no
	// runtime variant for a supported chip.
	ErrRuntimeVariantUnavailable = errors.New("semantic runtime variant unavailable")
)

// InstalledBundle is a locally verified, immutable semantic bundle. The
// embedded trusted manifest is its trust root and authorizes lifecycle use
// when RuntimeVerifier re-verifies the selected runtime specification.
type InstalledBundle struct {
	Manifest   Manifest
	BundleRoot string

	trustedManifest Manifest
}

// ResolvedRuntime identifies the locally verified artifacts selected for a
// supported Darwin Apple-silicon chip.
type ResolvedRuntime struct {
	BundleID       string
	RuntimePath    string
	ModelPath      string
	RuntimeSHA256  string
	ModelSHA256    string
	RuntimeVersion string
	Chip           string
	SupportLevel   string
}

// LoadInstalled verifies the installed bundle selected by bundleID for the
// current chip against the embedded trusted manifest. It performs filesystem
// reads only: it never downloads, starts, or executes the runtime.
func LoadInstalled(roots paths.SemanticRoots, bundleID string) (InstalledBundle, error) {
	trusted, err := ParseTrustedManifest(EmbeddedTrustedManifestM1)
	if err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: parse embedded trusted manifest: %v", ErrInstalledBundleInvalid, err)
	}
	if bundleID != trusted.BundleID {
		return InstalledBundle{}, fmt.Errorf("%w: %q", ErrUntrustedBundleID, bundleID)
	}
	chip, err := DetectDarwinChip()
	if err != nil {
		return InstalledBundle{}, err
	}
	return loadInstalled(roots, bundleID, trusted, chip)
}

// LoadInstalledForChip verifies an installed bundle against the embedded trust
// root while requiring only the runtime artifact selected for chip. A bundle
// intentionally contains one local runtime artifact even though its immutable
// manifest lists all supported chip variants.
func LoadInstalledForChip(roots paths.SemanticRoots, bundleID, chip string) (InstalledBundle, error) {
	trusted, err := ParseTrustedManifest(EmbeddedTrustedManifestM1)
	if err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: parse embedded trusted manifest: %v", ErrInstalledBundleInvalid, err)
	}
	return loadInstalled(roots, bundleID, trusted, chip)
}

func loadInstalled(roots paths.SemanticRoots, bundleID string, trusted Manifest, chip string) (InstalledBundle, error) {
	if err := trusted.Validate(); err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: validate trusted manifest: %v", ErrInstalledBundleInvalid, err)
	}
	if err := ValidateEmbeddedManifestAssets(trusted); err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: validate embedded assets: %v", ErrInstalledBundleInvalid, err)
	}
	if bundleID != trusted.BundleID {
		return InstalledBundle{}, fmt.Errorf("%w: %q", ErrUntrustedBundleID, bundleID)
	}
	variant, err := selectedRuntimeVariant(trusted.Runtime.Variants, chip)
	if err != nil {
		return InstalledBundle{}, err
	}

	bundleRoot, err := roots.Bundle(bundleID)
	if err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: resolve bundle root: %v", ErrInstalledBundleInvalid, err)
	}
	if err := verifyBundleRoot(bundleRoot); err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: %v", ErrInstalledBundleInvalid, err)
	}

	installedManifestPath, err := bundleFilePath(bundleRoot, installedManifestName)
	if err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: manifest path: %v", ErrInstalledBundleInvalid, err)
	}
	data, err := readRegularFile(installedManifestPath, maxInstalledManifestSize)
	if err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: read installed manifest: %v", ErrInstalledBundleInvalid, err)
	}
	actual, err := ParseTrustedManifest(data)
	if err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: parse installed manifest: %v", ErrInstalledBundleInvalid, err)
	}
	if err := manifestsEqual(actual, trusted); err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: %v", ErrInstalledBundleInvalid, err)
	}
	if err := verifyInstalledArtifacts(bundleRoot, trusted, variant); err != nil {
		return InstalledBundle{}, fmt.Errorf("%w: %v", ErrInstalledBundleInvalid, err)
	}

	return InstalledBundle{
		Manifest:        trusted,
		BundleRoot:      bundleRoot,
		trustedManifest: trusted,
	}, nil
}

// ResolveRuntime selects the installed runtime for chip and returns absolute
// runtime and model paths. It does not start or execute either artifact.
func (b InstalledBundle) ResolveRuntime(chip string) (ResolvedRuntime, error) {
	if b.BundleRoot == "" || b.trustedManifest.BundleID == "" {
		return ResolvedRuntime{}, fmt.Errorf("%w: bundle was not loaded", ErrInstalledBundleInvalid)
	}
	if !isSupportedDarwinChip(chip) {
		return ResolvedRuntime{}, fmt.Errorf("%w: %q", ErrUnsupportedChip, chip)
	}
	variant, err := selectedRuntimeVariant(b.trustedManifest.Runtime.Variants, chip)
	if err != nil {
		return ResolvedRuntime{}, err
	}
	runtimePath, err := bundleFilePath(b.BundleRoot, variant.Executable)
	if err != nil {
		return ResolvedRuntime{}, fmt.Errorf("%w: runtime path: %v", ErrInstalledBundleInvalid, err)
	}
	modelPath, err := bundleFilePath(b.BundleRoot, b.trustedManifest.Model.File)
	if err != nil {
		return ResolvedRuntime{}, fmt.Errorf("%w: model path: %v", ErrInstalledBundleInvalid, err)
	}
	return ResolvedRuntime{
		BundleID:       b.trustedManifest.BundleID,
		RuntimePath:    runtimePath,
		ModelPath:      modelPath,
		RuntimeSHA256:  variant.ExecutableSHA256,
		ModelSHA256:    b.trustedManifest.Model.SHA256,
		RuntimeVersion: variant.ExecutableVersion,
		Chip:           variant.Chip,
		SupportLevel:   variant.SupportLevel,
	}, nil
}

func selectedRuntimeVariant(variants []RuntimeVariant, chip string) (RuntimeVariant, error) {
	var matches []RuntimeVariant
	for _, variant := range variants {
		if variant.Chip != chip {
			continue
		}
		if variant.Platform != "darwin-arm64" || !isSupportedDarwinChip(variant.Chip) {
			return RuntimeVariant{}, fmt.Errorf("%w: invalid Darwin runtime variant", ErrInstalledBundleInvalid)
		}
		if variant.Chip == "m1" && variant.SupportLevel != "verified" {
			return RuntimeVariant{}, fmt.Errorf("%w: m1 must be verified", ErrInstalledBundleInvalid)
		}
		if variant.Chip != "m1" && variant.SupportLevel != "experimental" {
			return RuntimeVariant{}, fmt.Errorf("%w: %s must be experimental", ErrInstalledBundleInvalid, variant.Chip)
		}
		matches = append(matches, variant)
	}
	switch len(matches) {
	case 0:
		return RuntimeVariant{}, fmt.Errorf("%w: %q", ErrRuntimeVariantUnavailable, chip)
	case 1:
		return matches[0], nil
	default:
		return RuntimeVariant{}, fmt.Errorf("%w: duplicate entries for %q", ErrInstalledBundleInvalid, chip)
	}
}

func verifyInstalledArtifacts(bundleRoot string, manifest Manifest, variant RuntimeVariant) error {
	if _, err := verifyBundleFile(bundleRoot, manifest.Model.File, manifest.Model.Size, manifest.Model.SHA256); err != nil {
		return fmt.Errorf("model: %w", err)
	}
	if _, err := verifyBundleFile(bundleRoot, variant.Executable, variant.ExecutableSize, variant.ExecutableSHA256); err != nil {
		return fmt.Errorf("runtime %q: %w", variant.Chip, err)
	}
	for _, reference := range installedAssetReferences(manifest) {
		relative, err := embeddedAssetRelativePath(reference.Path)
		if err != nil {
			return err
		}
		data, err := EmbeddedBundleAssets.ReadFile(reference.Path)
		if err != nil {
			return err
		}
		if _, err := verifyBundleFile(bundleRoot, relative, int64(len(data)), reference.SHA256); err != nil {
			return fmt.Errorf("embedded asset %q: %w", reference.Path, err)
		}
	}
	return nil
}

func installedAssetReferences(manifest Manifest) []AssetReference {
	seen := make(map[string]struct{})
	references := make([]AssetReference, 0, 2+len(manifest.Runtime.Variants)*2)
	add := func(reference *AssetReference) {
		if reference == nil {
			return
		}
		if _, ok := seen[reference.Path]; ok {
			return
		}
		seen[reference.Path] = struct{}{}
		references = append(references, *reference)
	}
	add(manifest.Model.LicenseFile)
	add(manifest.Model.AttributionFile)
	for _, variant := range manifest.Runtime.Variants {
		add(variant.LicenseFile)
		add(variant.AttributionFile)
	}
	return references
}

func manifestsEqual(actual, trusted Manifest) error {
	actualCanonical, err := actual.CanonicalBytes()
	if err != nil {
		return err
	}
	trustedCanonical, err := trusted.CanonicalBytes()
	if err != nil {
		return err
	}
	if actual.BundleID != trusted.BundleID || !bytes.Equal(actualCanonical, trustedCanonical) {
		return errors.New("installed manifest does not match embedded trusted manifest")
	}
	return nil
}

func verifyBundleRoot(bundleRoot string) error {
	info, err := os.Lstat(bundleRoot)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return errors.New("bundle root is not a directory")
	}
	return nil
}

func verifyBundleFile(bundleRoot, relative string, expectedSize int64, expectedSHA256 string) (string, error) {
	target, err := bundleFilePath(bundleRoot, relative)
	if err != nil {
		return "", err
	}
	info, err := os.Lstat(target)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("bundle artifact is not a regular file")
	}
	if info.Size() != expectedSize {
		return "", fmt.Errorf("size %d does not match expected %d", info.Size(), expectedSize)
	}
	file, err := os.Open(target)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	if hex.EncodeToString(hash.Sum(nil)) != expectedSHA256 {
		return "", errors.New("SHA-256 does not match")
	}
	return target, nil
}

func readRegularFile(filePath string, maxSize int64) ([]byte, error) {
	info, err := os.Lstat(filePath)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if info.Size() > maxSize {
		return nil, errors.New("file exceeds size limit")
	}
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxSize+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxSize {
		return nil, errors.New("file exceeds size limit")
	}
	return data, nil
}

func bundleFilePath(bundleRoot, relative string) (string, error) {
	if err := validateBundleRelativePath(relative); err != nil {
		return "", err
	}
	target := filepath.Join(bundleRoot, filepath.FromSlash(relative))
	contained, err := filepath.Rel(bundleRoot, target)
	if err != nil || contained == "." || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		return "", errors.New("bundle artifact path escapes bundle root")
	}

	current := bundleRoot
	for _, part := range strings.Split(filepath.FromSlash(relative), string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("bundle artifact path contains symlink")
		}
	}
	return target, nil
}

func validateBundleRelativePath(relative string) error {
	if relative == "" || strings.Contains(relative, "\\") || filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" || filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative))) != relative {
		return errors.New("bundle artifact path must be clean and relative")
	}
	return nil
}
