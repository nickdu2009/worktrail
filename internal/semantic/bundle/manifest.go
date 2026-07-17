// Package bundle verifies and installs immutable semantic runtime bundles.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

const (
	TrustedManifestSchema   = "worktrail.semantic-bundle.v1"
	BGEM3EmbeddingDimension = 1024
	BGEM3Tokenizer          = "BAAI/bge-m3@5617a9f61b028005a4858fdac845db406aefb181"
	BGEM3Pooling            = "cls"
	BGEM3Normalization      = "l2"
	BGEM3QueryTemplate      = "{text}"
	BGEM3DocumentTemplate   = "{text}"
	CosineVectorMetric      = "cosine"
)

// AssetReference pins a file embedded in the semantic bundle.
type AssetReference struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

type Model struct {
	Repository      string          `json:"repository"`
	Revision        string          `json:"revision"`
	File            string          `json:"file"`
	URL             string          `json:"url"`
	Size            int64           `json:"size"`
	SHA256          string          `json:"sha256"`
	License         string          `json:"license"`
	Attribution     string          `json:"attribution"`
	LicenseFile     *AssetReference `json:"license_file"`
	AttributionFile *AssetReference `json:"attribution_file"`
}

type ResourceBudget struct {
	ColdReadinessMSMax          int64 `json:"cold_readiness_ms_max"`
	WarmSingleEmbeddingP95MSMax int64 `json:"warm_single_embedding_p95_ms_max"`
	PeakRSSBytesMax             int64 `json:"peak_rss_bytes_max"`
}

type RuntimeVariant struct {
	Chip                 string          `json:"chip"`
	SupportLevel         string          `json:"support_level"`
	Platform             string          `json:"platform"`
	RuntimeURL           string          `json:"runtime_url"`
	CompressedSize       int64           `json:"compressed_size"`
	CompressedSHA256     string          `json:"compressed_sha256"`
	Executable           string          `json:"executable"`
	ExecutableVersion    string          `json:"executable_version"`
	ExecutableSize       int64           `json:"executable_size"`
	ExecutableSHA256     string          `json:"executable_sha256"`
	DeclaredMinimumMacOS string          `json:"declared_minimum_macos,omitempty"`
	ResourceBudget       *ResourceBudget `json:"verified_resource_budget,omitempty"`
	License              string          `json:"license"`
	Attribution          string          `json:"attribution"`
	LicenseFile          *AssetReference `json:"license_file"`
	AttributionFile      *AssetReference `json:"attribution_file"`
}

type Runtime struct {
	Distribution string           `json:"distribution"`
	Version      string           `json:"version"`
	Variants     []RuntimeVariant `json:"variants"`
}

// EmbeddingProfile defines the model-space behavior verified by the parity
// capture. It is canonical bundle data because every field changes embedding
// coordinates.
type EmbeddingProfile struct {
	Dimension        int    `json:"dimension"`
	Tokenizer        string `json:"tokenizer"`
	Pooling          string `json:"pooling"`
	Normalization    string `json:"normalization"`
	QueryTemplate    string `json:"query_template"`
	DocumentTemplate string `json:"document_template"`
}

type CanonicalManifest struct {
	Schema                  string           `json:"schema"`
	SemanticAPIVersion      int              `json:"semantic_api_version"`
	GenerationSchemaVersion int              `json:"generation_schema_version"`
	Model                   Model            `json:"model"`
	Runtime                 Runtime          `json:"runtime"`
	Embedding               EmbeddingProfile `json:"embedding"`
	VectorMetric            string           `json:"vector_metric"`
}

type Manifest struct {
	BundleID string `json:"bundle_id"`
	CanonicalManifest
}

// EmbeddedTrustedManifestM1 is the immutable M1–M5 trust root used to verify
// installed bundle contents and authorize the selected runtime lifecycle.
//
//go:embed assets/trusted-manifest-m1.json
var EmbeddedTrustedManifestM1 []byte

// EmbeddedBundleAssets exposes the license and attribution files referenced by
// embedded trusted manifests.
//
//go:embed assets/licenses/MIT.txt assets/ATTRIBUTIONS.md
var EmbeddedBundleAssets embed.FS

func ParseTrustedManifest(data []byte) (Manifest, error) {
	var manifest Manifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode trusted semantic manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return Manifest{}, errors.New("trusted semantic manifest has trailing JSON")
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// ValidateEmbeddedManifestAssets verifies that every asset referenced by a
// trusted manifest is present in the embedded asset set and matches its
// declared SHA-256. It does not read from the local filesystem.
func ValidateEmbeddedManifestAssets(manifest Manifest) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	references := []*AssetReference{
		manifest.Model.LicenseFile,
		manifest.Model.AttributionFile,
	}
	for _, variant := range manifest.Runtime.Variants {
		references = append(references, variant.LicenseFile, variant.AttributionFile)
	}
	for _, reference := range references {
		data, err := EmbeddedBundleAssets.ReadFile(reference.Path)
		if err != nil {
			return fmt.Errorf("read embedded bundle asset %q: %w", reference.Path, err)
		}
		sum := sha256.Sum256(data)
		if hex.EncodeToString(sum[:]) != reference.SHA256 {
			return fmt.Errorf("embedded bundle asset %q SHA-256 mismatch", reference.Path)
		}
	}
	return nil
}

func (m Manifest) CanonicalBytes() ([]byte, error) {
	return json.Marshal(m.CanonicalManifest)
}

func (m Manifest) ExpectedBundleID() (string, error) {
	data, err := m.CanonicalBytes()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func (m Manifest) Validate() error {
	if m.Schema != TrustedManifestSchema {
		return fmt.Errorf("unsupported semantic manifest schema %q", m.Schema)
	}
	if m.SemanticAPIVersion != 1 || m.GenerationSchemaVersion != 1 {
		return errors.New("unsupported semantic manifest API or generation schema")
	}
	if err := validateModel(m.Model); err != nil {
		return err
	}
	if err := validateEmbeddingProfile(m.Embedding); err != nil {
		return err
	}
	if m.VectorMetric != CosineVectorMetric {
		return fmt.Errorf("unsupported semantic vector metric %q", m.VectorMetric)
	}
	if m.Runtime.Distribution != "llama.app" || m.Runtime.Version == "" || len(m.Runtime.Variants) == 0 {
		return errors.New("invalid semantic runtime manifest")
	}
	for _, variant := range m.Runtime.Variants {
		if err := validateVariant(variant); err != nil {
			return err
		}
	}
	expected, err := m.ExpectedBundleID()
	if err != nil {
		return err
	}
	if m.BundleID != expected {
		return errors.New("semantic bundle ID does not match canonical manifest")
	}
	return nil
}

func validateModel(model Model) error {
	if model.Repository == "" || model.Revision == "" || model.File == "" || model.URL == "" ||
		model.Size <= 0 || !isSHA256(model.SHA256) || model.License == "" || model.Attribution == "" {
		return errors.New("invalid semantic model manifest")
	}
	return validateAssetReferences(model.LicenseFile, model.AttributionFile)
}

func validateEmbeddingProfile(profile EmbeddingProfile) error {
	switch {
	case profile.Dimension != BGEM3EmbeddingDimension:
		return fmt.Errorf("unsupported BGE-M3 embedding dimension %d", profile.Dimension)
	case profile.Tokenizer != BGEM3Tokenizer:
		return fmt.Errorf("unsupported BGE-M3 tokenizer %q", profile.Tokenizer)
	case profile.Pooling != BGEM3Pooling:
		return fmt.Errorf("unsupported BGE-M3 pooling %q", profile.Pooling)
	case profile.Normalization != BGEM3Normalization:
		return fmt.Errorf("unsupported BGE-M3 normalization %q", profile.Normalization)
	case profile.QueryTemplate != BGEM3QueryTemplate:
		return fmt.Errorf("unsupported BGE-M3 query template %q", profile.QueryTemplate)
	case profile.DocumentTemplate != BGEM3DocumentTemplate:
		return fmt.Errorf("unsupported BGE-M3 document template %q", profile.DocumentTemplate)
	}
	return nil
}

func validateVariant(variant RuntimeVariant) error {
	if !isSupportedDarwinChip(variant.Chip) || variant.Platform != "darwin-arm64" ||
		(variant.SupportLevel != "verified" && variant.SupportLevel != "experimental") ||
		variant.RuntimeURL == "" || variant.CompressedSize <= 0 ||
		!isSHA256(variant.CompressedSHA256) || variant.Executable == "" ||
		variant.ExecutableVersion == "" || variant.ExecutableSize <= 0 ||
		!isSHA256(variant.ExecutableSHA256) || variant.License == "" || variant.Attribution == "" {
		return errors.New("invalid semantic runtime variant")
	}
	if variant.SupportLevel == "verified" &&
		(variant.DeclaredMinimumMacOS == "" || variant.ResourceBudget == nil ||
			variant.ResourceBudget.ColdReadinessMSMax <= 0 ||
			variant.ResourceBudget.WarmSingleEmbeddingP95MSMax <= 0 ||
			variant.ResourceBudget.PeakRSSBytesMax <= 0) {
		return errors.New("verified runtime variant lacks release evidence")
	}
	if variant.Chip == "m1" && variant.SupportLevel != "verified" {
		return errors.New("M1 runtime variant must be verified")
	}
	if variant.Chip != "m1" && variant.SupportLevel != "experimental" {
		return errors.New("M2-M5 runtime variants must be experimental")
	}
	return validateAssetReferences(variant.LicenseFile, variant.AttributionFile)
}

func validateAssetReferences(license, attribution *AssetReference) error {
	if license == nil {
		return errors.New("license file reference is required")
	}
	if attribution == nil {
		return errors.New("attribution file reference is required")
	}
	if err := validateAssetReference(*license); err != nil {
		return fmt.Errorf("invalid license file reference: %w", err)
	}
	if err := validateAssetReference(*attribution); err != nil {
		return fmt.Errorf("invalid attribution file reference: %w", err)
	}
	if license.Path == attribution.Path {
		return errors.New("license and attribution file references must differ")
	}
	return nil
}

func validateAssetReference(reference AssetReference) error {
	if reference.Path == "" || !strings.HasPrefix(reference.Path, "assets/") ||
		path.Clean(reference.Path) != reference.Path || strings.HasSuffix(reference.Path, "/") ||
		!isSHA256(reference.SHA256) {
		return errors.New("path must name a pinned bundle asset")
	}
	return nil
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
