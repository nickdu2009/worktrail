package bundle

import (
	"encoding/json"
	"strings"
	"testing"
)

func trustedManifest(t *testing.T) Manifest {
	t.Helper()
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
				Repository:  "ggml-org/bge-m3-Q8_0-GGUF",
				Revision:    "9eba04c5",
				File:        "bge-m3-q8_0.gguf",
				URL:         "https://example.invalid/model",
				Size:        634553760,
				SHA256:      strings.Repeat("a", 64),
				License:     "MIT",
				Attribution: "test model",
				LicenseFile: &AssetReference{
					Path:   "assets/licenses/MIT.txt",
					SHA256: strings.Repeat("d", 64),
				},
				AttributionFile: &AssetReference{
					Path:   "assets/ATTRIBUTIONS.md",
					SHA256: strings.Repeat("e", 64),
				},
			},
			Runtime: Runtime{
				Distribution: "llama.app",
				Version:      "b9986",
				Variants: []RuntimeVariant{{
					Chip:                 "m1",
					SupportLevel:         "verified",
					Platform:             "darwin-arm64",
					RuntimeURL:           "https://example.invalid/llama-app.zst",
					CompressedSize:       6887500,
					CompressedSHA256:     strings.Repeat("b", 64),
					Executable:           "llama",
					ExecutableVersion:    "b9986-91c631b21",
					ExecutableSize:       17114936,
					ExecutableSHA256:     strings.Repeat("c", 64),
					DeclaredMinimumMacOS: "15.7.3",
					ResourceBudget: &ResourceBudget{
						ColdReadinessMSMax:          25000,
						WarmSingleEmbeddingP95MSMax: 35,
						PeakRSSBytesMax:             1073741824,
					},
					License:     "MIT",
					Attribution: "test runtime",
					LicenseFile: &AssetReference{
						Path:   "assets/licenses/MIT.txt",
						SHA256: strings.Repeat("d", 64),
					},
					AttributionFile: &AssetReference{
						Path:   "assets/ATTRIBUTIONS.md",
						SHA256: strings.Repeat("e", 64),
					},
				}},
			},
		},
	}
	id, err := manifest.ExpectedBundleID()
	if err != nil {
		t.Fatal(err)
	}
	manifest.BundleID = id
	return manifest
}

func TestParseTrustedManifest(t *testing.T) {
	want := trustedManifest(t)
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseTrustedManifest(data)
	if err != nil {
		t.Fatal(err)
	}
	if got.BundleID != want.BundleID {
		t.Fatalf("bundle ID = %q, want %q", got.BundleID, want.BundleID)
	}
}

func TestManifestRejectsChangedCanonicalFields(t *testing.T) {
	manifest := trustedManifest(t)
	manifest.Runtime.Variants[0].Chip = "m2"
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected changed manifest rejection")
	}
}

func TestManifestRejectsNonExperimentalM2ToM5Variants(t *testing.T) {
	manifest := trustedManifest(t)
	manifest.Runtime.Variants[0].Chip = "m2"
	refreshBundleID(t, &manifest)
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected non-experimental M2 variant rejection")
	}
}

func TestManifestRejectsNonVerifiedM1Variant(t *testing.T) {
	manifest := trustedManifest(t)
	manifest.Runtime.Variants[0].SupportLevel = "experimental"
	refreshBundleID(t, &manifest)
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected non-verified M1 variant rejection")
	}
}

func TestParseTrustedManifestRejectsUnknownFields(t *testing.T) {
	manifest := trustedManifest(t)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	data = append(data[:len(data)-1], []byte(`,"candidate_status":"not_installable"}`)...)
	if _, err := ParseTrustedManifest(data); err == nil {
		t.Fatal("expected unknown field rejection")
	}
}

func TestParseTrustedManifestRejectsTrailingJSON(t *testing.T) {
	manifest := trustedManifest(t)
	data, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseTrustedManifest(append(data, []byte(` {}`)...)); err == nil {
		t.Fatal("expected trailing JSON rejection")
	}
}

func TestEmbeddedTrustedManifestM1(t *testing.T) {
	manifest, err := ParseTrustedManifest(EmbeddedTrustedManifestM1)
	if err != nil {
		t.Fatalf("parse embedded M1 trusted manifest: %v", err)
	}
	if manifest.BundleID != "60e883f9f5fb62d0f6986d24df30ad8f129f3485a8a19b691eeb2662a438d4d2" {
		t.Fatalf("embedded bundle ID = %q", manifest.BundleID)
	}
	if manifest.Embedding != (EmbeddingProfile{
		Dimension:        BGEM3EmbeddingDimension,
		Tokenizer:        BGEM3Tokenizer,
		Pooling:          BGEM3Pooling,
		Normalization:    BGEM3Normalization,
		QueryTemplate:    BGEM3QueryTemplate,
		DocumentTemplate: BGEM3DocumentTemplate,
	}) || manifest.VectorMetric != CosineVectorMetric {
		t.Fatalf("embedded profile = %#v, metric = %q", manifest.Embedding, manifest.VectorMetric)
	}
	if len(manifest.Runtime.Variants) != 5 {
		t.Fatalf("embedded variants = %d, want 5", len(manifest.Runtime.Variants))
	}
	variant := manifest.Runtime.Variants[0]
	if variant.Chip != "m1" || variant.SupportLevel != "verified" ||
		variant.DeclaredMinimumMacOS != "15.7.3" {
		t.Fatalf("embedded M1 verification envelope = %#v", variant)
	}
	for _, variant := range manifest.Runtime.Variants[1:] {
		if variant.SupportLevel != "experimental" || variant.DeclaredMinimumMacOS != "" || variant.ResourceBudget != nil {
			t.Fatalf("embedded experimental variant = %#v", variant)
		}
	}
	wantExperimentalArtifacts := map[string]struct {
		compressedSize int64
		compressedSHA  string
		executableSize int64
		executableSHA  string
	}{
		"m2": {6869848, "5232c73b0f3a2c683093a4645638ff8cfce4b57e8d125ad94f8cbeeda86c5a91", 17026856, "161c80da12d1529e2e26caf6242588cb25afc7b3eb716ed20cf0da7d4870e395"},
		"m3": {6869846, "6d3ee3117cc4f680345dbd1bfec8fc65dbc189e0ade149480b32cb780ddfa8e9", 17026856, "b231209704f6d9110fc7b84f6160b88043e2868c309faa50b7a9e07289943ef9"},
		"m4": {6868121, "5ee16a0bc28a375636895b52512238fe6abcc49bcc7534934999e337e206780c", 17026280, "2711828bd457085761d042316eb10680984c1926cb708f5d17ba9bff9e34ccf6"},
		"m5": {6867966, "f10b4638831d51e738374b6217a7fff49beefb68ac6f523ebf7beb93492f7219", 17026232, "8f3b18a6c9c6ac4f7b8da7f9e54225b97325b90c8911a40ccaaa26441e5f4f7e"},
	}
	for _, variant := range manifest.Runtime.Variants[1:] {
		want, ok := wantExperimentalArtifacts[variant.Chip]
		if !ok || variant.RuntimeURL != "https://huggingface.co/buckets/ggml-org/install.sh/resolve/b9986/aarch64/macos/metal/"+variant.Chip+"/llama-app.zst" ||
			variant.ExecutableVersion != "b9986-91c631b21" ||
			variant.CompressedSize != want.compressedSize || variant.CompressedSHA256 != want.compressedSHA ||
			variant.ExecutableSize != want.executableSize || variant.ExecutableSHA256 != want.executableSHA {
			t.Fatalf("embedded experimental artifact for %s = %#v", variant.Chip, variant)
		}
	}
	if err := ValidateEmbeddedManifestAssets(manifest); err != nil {
		t.Fatalf("validate embedded M1 manifest assets: %v", err)
	}
}

func TestAssetReferencesChangeBundleID(t *testing.T) {
	manifest := trustedManifest(t)
	manifest.Model.LicenseFile = &AssetReference{
		Path:   "assets/licenses/MIT.txt",
		SHA256: strings.Repeat("f", 64),
	}
	manifest.Model.AttributionFile = &AssetReference{
		Path:   "assets/ATTRIBUTIONS.md",
		SHA256: strings.Repeat("e", 64),
	}
	expected, err := manifest.ExpectedBundleID()
	if err != nil {
		t.Fatal(err)
	}
	if expected == manifest.BundleID {
		t.Fatal("asset references did not change canonical bundle ID")
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("expected changed asset references rejection")
	}
}

func TestManifestRejectsMissingAssetReferences(t *testing.T) {
	tests := []struct {
		name   string
		remove func(*Manifest)
	}{
		{
			name: "model license",
			remove: func(manifest *Manifest) {
				manifest.Model.LicenseFile = nil
			},
		},
		{
			name: "model attribution",
			remove: func(manifest *Manifest) {
				manifest.Model.AttributionFile = nil
			},
		},
		{
			name: "model pair",
			remove: func(manifest *Manifest) {
				manifest.Model.LicenseFile = nil
				manifest.Model.AttributionFile = nil
			},
		},
		{
			name: "runtime license",
			remove: func(manifest *Manifest) {
				manifest.Runtime.Variants[0].LicenseFile = nil
			},
		},
		{
			name: "runtime attribution",
			remove: func(manifest *Manifest) {
				manifest.Runtime.Variants[0].AttributionFile = nil
			},
		},
		{
			name: "runtime pair",
			remove: func(manifest *Manifest) {
				manifest.Runtime.Variants[0].LicenseFile = nil
				manifest.Runtime.Variants[0].AttributionFile = nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := trustedManifest(t)
			test.remove(&manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("expected missing asset reference rejection")
			}
		})
	}
}

func TestManifestRejectsMissingOrChangedProfileFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{
			name: "dimension",
			mutate: func(manifest *Manifest) {
				manifest.Embedding.Dimension = 0
			},
		},
		{
			name: "tokenizer",
			mutate: func(manifest *Manifest) {
				manifest.Embedding.Tokenizer = ""
			},
		},
		{
			name: "pooling",
			mutate: func(manifest *Manifest) {
				manifest.Embedding.Pooling = "mean"
			},
		},
		{
			name: "normalization",
			mutate: func(manifest *Manifest) {
				manifest.Embedding.Normalization = ""
			},
		},
		{
			name: "query template",
			mutate: func(manifest *Manifest) {
				manifest.Embedding.QueryTemplate = ""
			},
		},
		{
			name: "document template",
			mutate: func(manifest *Manifest) {
				manifest.Embedding.DocumentTemplate = ""
			},
		},
		{
			name: "vector metric",
			mutate: func(manifest *Manifest) {
				manifest.VectorMetric = ""
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := trustedManifest(t)
			test.mutate(&manifest)
			refreshBundleID(t, &manifest)
			if err := manifest.Validate(); err == nil {
				t.Fatal("expected invalid profile field rejection")
			}
		})
	}
}
