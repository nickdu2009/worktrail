// Package profile defines immutable semantic identity inputs.
package profile

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
)

const (
	ModelSpaceSchema    = "worktrail.semantic.model-space.v1"
	RecallProfileSchema = "worktrail.semantic.recall-profile.v1"
)

type ModelSpace struct {
	Schema           string `json:"schema"`
	Repository       string `json:"repository"`
	Revision         string `json:"revision"`
	ArtifactSHA256   string `json:"artifact_sha256"`
	Dimension        int    `json:"dimension"`
	Tokenizer        string `json:"tokenizer"`
	Pooling          string `json:"pooling"`
	Normalization    string `json:"normalization"`
	QueryTemplate    string `json:"query_template"`
	DocumentTemplate string `json:"document_template"`
}

type RecallProfile struct {
	Schema                   string     `json:"schema"`
	ModelSpace               ModelSpace `json:"model_space"`
	RuntimeDistribution      string     `json:"runtime_distribution"`
	RuntimeVersion           string     `json:"runtime_version"`
	RuntimeExecutableVersion string     `json:"runtime_executable_version"`
	RuntimeExecutableSHA256  string     `json:"runtime_executable_sha256"`
	ChipVariant              string     `json:"chip_variant"`
	SemanticAPIVersion       int        `json:"semantic_api_version"`
	ChunkerVersion           string     `json:"chunker_version"`
	IndexingVersion          string     `json:"indexing_version"`
	LexicalVersion           string     `json:"lexical_version"`
	VectorMetric             string     `json:"vector_metric"`
	GenerationSchemaVersion  int        `json:"generation_schema_version"`
	SQLiteVecVersion         string     `json:"sqlite_vec_version"`
}

// SubsystemVersions holds current, versioned identity inputs not owned by a
// trusted runtime bundle.
type SubsystemVersions struct {
	ChunkerVersion   string
	IndexingVersion  string
	LexicalVersion   string
	SQLiteVecVersion string
}

// Identity contains the complete model-space and recall-profile contracts and
// their content-addressed identifiers.
type Identity struct {
	ModelSpace      ModelSpace
	ModelSpaceID    string
	RecallProfile   RecallProfile
	RecallProfileID string
}

// Derive creates semantic identity contracts from a verified trusted manifest,
// its selected resolved runtime, and the current subsystem versions.
func Derive(manifest bundle.Manifest, runtime bundle.ResolvedRuntime, versions SubsystemVersions) (Identity, error) {
	if err := manifest.Validate(); err != nil {
		return Identity{}, fmt.Errorf("validate semantic manifest: %w", err)
	}
	if err := validateResolvedRuntime(manifest, runtime); err != nil {
		return Identity{}, err
	}
	if err := versions.Validate(); err != nil {
		return Identity{}, err
	}

	modelSpace := ModelSpace{
		Schema:           ModelSpaceSchema,
		Repository:       manifest.Model.Repository,
		Revision:         manifest.Model.Revision,
		ArtifactSHA256:   manifest.Model.SHA256,
		Dimension:        manifest.Embedding.Dimension,
		Tokenizer:        manifest.Embedding.Tokenizer,
		Pooling:          manifest.Embedding.Pooling,
		Normalization:    manifest.Embedding.Normalization,
		QueryTemplate:    manifest.Embedding.QueryTemplate,
		DocumentTemplate: manifest.Embedding.DocumentTemplate,
	}
	modelSpaceID, err := ModelSpaceID(modelSpace)
	if err != nil {
		return Identity{}, err
	}
	recallProfile := RecallProfile{
		Schema:                   RecallProfileSchema,
		ModelSpace:               modelSpace,
		RuntimeDistribution:      manifest.Runtime.Distribution,
		RuntimeVersion:           manifest.Runtime.Version,
		RuntimeExecutableVersion: runtime.RuntimeVersion,
		RuntimeExecutableSHA256:  runtime.RuntimeSHA256,
		ChipVariant:              runtime.Chip,
		SemanticAPIVersion:       manifest.SemanticAPIVersion,
		ChunkerVersion:           versions.ChunkerVersion,
		IndexingVersion:          versions.IndexingVersion,
		LexicalVersion:           versions.LexicalVersion,
		VectorMetric:             manifest.VectorMetric,
		GenerationSchemaVersion:  manifest.GenerationSchemaVersion,
		SQLiteVecVersion:         versions.SQLiteVecVersion,
	}
	recallProfileID, err := RecallProfileID(recallProfile)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		ModelSpace:      modelSpace,
		ModelSpaceID:    modelSpaceID,
		RecallProfile:   recallProfile,
		RecallProfileID: recallProfileID,
	}, nil
}

func (v SubsystemVersions) Validate() error {
	for _, field := range []struct {
		name  string
		value string
	}{
		{"chunker version", v.ChunkerVersion},
		{"indexing version", v.IndexingVersion},
		{"lexical version", v.LexicalVersion},
		{"sqlite-vec version", v.SQLiteVecVersion},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("%s is required", field.name)
		}
	}
	return nil
}

func validateResolvedRuntime(manifest bundle.Manifest, runtime bundle.ResolvedRuntime) error {
	if runtime.BundleID != manifest.BundleID {
		return errors.New("resolved runtime bundle ID does not match manifest")
	}
	if runtime.ModelSHA256 != manifest.Model.SHA256 {
		return errors.New("resolved runtime model SHA-256 does not match manifest")
	}
	if strings.TrimSpace(runtime.Chip) == "" {
		return errors.New("resolved runtime chip is required")
	}
	var selected *bundle.RuntimeVariant
	for i := range manifest.Runtime.Variants {
		variant := &manifest.Runtime.Variants[i]
		if variant.Chip != runtime.Chip {
			continue
		}
		if selected != nil {
			return fmt.Errorf("manifest has duplicate runtime variants for %q", runtime.Chip)
		}
		selected = variant
	}
	if selected == nil {
		return fmt.Errorf("manifest has no runtime variant for %q", runtime.Chip)
	}
	if runtime.RuntimeVersion != selected.ExecutableVersion {
		return errors.New("resolved runtime version does not match manifest")
	}
	if runtime.RuntimeSHA256 != selected.ExecutableSHA256 {
		return errors.New("resolved runtime SHA-256 does not match manifest")
	}
	return nil
}

func ModelSpaceID(space ModelSpace) (string, error) {
	if err := space.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(space)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func RecallProfileID(profile RecallProfile) (string, error) {
	if err := profile.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(profile)
	if err != nil {
		return "", err
	}
	return sha256Hex(data), nil
}

func (s ModelSpace) Validate() error {
	if s.Schema != ModelSpaceSchema {
		return fmt.Errorf("unsupported model-space schema %q", s.Schema)
	}
	if strings.TrimSpace(s.Repository) == "" || strings.TrimSpace(s.Revision) == "" {
		return errors.New("model-space repository and revision are required")
	}
	if !isSHA256(s.ArtifactSHA256) {
		return errors.New("model-space artifact SHA-256 is invalid")
	}
	if s.Dimension <= 0 {
		return errors.New("model-space dimension must be positive")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"tokenizer", s.Tokenizer},
		{"pooling", s.Pooling},
		{"normalization", s.Normalization},
		{"query template", s.QueryTemplate},
		{"document template", s.DocumentTemplate},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("model-space %s is required", field.name)
		}
	}
	return nil
}

func (p RecallProfile) Validate() error {
	if p.Schema != RecallProfileSchema {
		return fmt.Errorf("unsupported recall-profile schema %q", p.Schema)
	}
	if err := p.ModelSpace.Validate(); err != nil {
		return fmt.Errorf("invalid recall-profile model space: %w", err)
	}
	if !isSHA256(p.RuntimeExecutableSHA256) {
		return errors.New("recall-profile runtime executable SHA-256 is invalid")
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"runtime distribution", p.RuntimeDistribution},
		{"runtime version", p.RuntimeVersion},
		{"runtime executable version", p.RuntimeExecutableVersion},
		{"chip variant", p.ChipVariant},
		{"chunker version", p.ChunkerVersion},
		{"indexing version", p.IndexingVersion},
		{"lexical version", p.LexicalVersion},
		{"vector metric", p.VectorMetric},
		{"sqlite-vec version", p.SQLiteVecVersion},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("recall-profile %s is required", field.name)
		}
	}
	if p.SemanticAPIVersion <= 0 || p.GenerationSchemaVersion <= 0 {
		return errors.New("recall-profile schema versions must be positive")
	}
	return nil
}

// BundleID hashes the versioned canonical trusted manifest bytes. Callers must
// exclude bundle_id, signatures, timestamps, and mutable local state before
// supplying the canonical representation.
func BundleID(canonicalManifest []byte) string {
	return sha256Hex(canonicalManifest)
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func isSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
