package profile

import (
	"testing"

	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
)

func testIdentity(t *testing.T) Identity {
	t.Helper()
	manifest, err := bundle.ParseTrustedManifest(bundle.EmbeddedTrustedManifestM1)
	if err != nil {
		t.Fatal(err)
	}
	variant := manifest.Runtime.Variants[0]
	identity, err := Derive(manifest, bundle.ResolvedRuntime{
		BundleID:       manifest.BundleID,
		RuntimeSHA256:  variant.ExecutableSHA256,
		ModelSHA256:    manifest.Model.SHA256,
		RuntimeVersion: variant.ExecutableVersion,
		Chip:           variant.Chip,
	}, SubsystemVersions{
		ChunkerVersion:   "chunker-v1",
		IndexingVersion:  "semantic-policy-v1",
		LexicalVersion:   "gse-v1.0.2",
		SQLiteVecVersion: "v0.1.9",
	})
	if err != nil {
		t.Fatal(err)
	}
	return identity
}

func TestDeriveStableEmbeddedManifestIdentity(t *testing.T) {
	first := testIdentity(t)
	second := testIdentity(t)
	if first != second {
		t.Fatalf("Derive() = %#v, then %#v", first, second)
	}
	if first.ModelSpaceID == "" || first.RecallProfileID == "" {
		t.Fatalf("derived identity = %#v", first)
	}
	if first.ModelSpace.Tokenizer != bundle.BGEM3Tokenizer ||
		first.RecallProfile.RuntimeVersion != "b9986" ||
		first.RecallProfile.VectorMetric != bundle.CosineVectorMetric {
		t.Fatalf("derived embedded identity = %#v", first)
	}
}

func TestModelSpaceIDChangesWithEveryInput(t *testing.T) {
	identity := testIdentity(t)
	base := identity.ModelSpace
	baseID, err := ModelSpaceID(base)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*ModelSpace)
	}{
		{"repository", func(space *ModelSpace) { space.Repository = "other/model" }},
		{"revision", func(space *ModelSpace) { space.Revision = "other-revision" }},
		{"artifact SHA-256", func(space *ModelSpace) {
			space.ArtifactSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{"dimension", func(space *ModelSpace) { space.Dimension++ }},
		{"tokenizer", func(space *ModelSpace) { space.Tokenizer = "other-tokenizer" }},
		{"pooling", func(space *ModelSpace) { space.Pooling = "mean" }},
		{"normalization", func(space *ModelSpace) { space.Normalization = "none" }},
		{"query template", func(space *ModelSpace) { space.QueryTemplate = "query: {text}" }},
		{"document template", func(space *ModelSpace) { space.DocumentTemplate = "document: {text}" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.mutate(&changed)
			id, err := ModelSpaceID(changed)
			if err != nil {
				t.Fatal(err)
			}
			if id == baseID {
				t.Fatal("model-space input change did not change ID")
			}
		})
	}
}

func TestRecallProfileIDChangesWithEveryInput(t *testing.T) {
	identity := testIdentity(t)
	base := identity.RecallProfile
	baseID, err := RecallProfileID(base)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(*RecallProfile)
	}{
		{"model space", func(profile *RecallProfile) { profile.ModelSpace.Pooling = "mean" }},
		{"runtime distribution", func(profile *RecallProfile) { profile.RuntimeDistribution = "other-runtime" }},
		{"runtime version", func(profile *RecallProfile) { profile.RuntimeVersion = "other-version" }},
		{"runtime executable version", func(profile *RecallProfile) { profile.RuntimeExecutableVersion = "other-executable-version" }},
		{"runtime executable SHA-256", func(profile *RecallProfile) {
			profile.RuntimeExecutableSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		}},
		{"chip variant", func(profile *RecallProfile) { profile.ChipVariant = "m2" }},
		{"semantic API version", func(profile *RecallProfile) { profile.SemanticAPIVersion++ }},
		{"chunker version", func(profile *RecallProfile) { profile.ChunkerVersion = "chunker-v2" }},
		{"indexing version", func(profile *RecallProfile) { profile.IndexingVersion = "semantic-policy-v2" }},
		{"lexical version", func(profile *RecallProfile) { profile.LexicalVersion = "gse-v2" }},
		{"vector metric", func(profile *RecallProfile) { profile.VectorMetric = "l2" }},
		{"generation schema", func(profile *RecallProfile) { profile.GenerationSchemaVersion++ }},
		{"sqlite-vec version", func(profile *RecallProfile) { profile.SQLiteVecVersion = "v0.2.0" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			changed := base
			test.mutate(&changed)
			id, err := RecallProfileID(changed)
			if err != nil {
				t.Fatal(err)
			}
			if id == baseID {
				t.Fatal("recall-profile input change did not change ID")
			}
		})
	}
}

func TestIdentityRejectsMissingOrInvalidInputs(t *testing.T) {
	identity := testIdentity(t)
	if _, err := ModelSpaceID(ModelSpace{}); err == nil {
		t.Fatal("expected invalid model-space rejection")
	}
	if _, err := RecallProfileID(RecallProfile{}); err == nil {
		t.Fatal("expected invalid recall-profile rejection")
	}
	invalidSpace := identity.ModelSpace
	invalidSpace.Schema = "worktrail.semantic.model-space.v2"
	if _, err := ModelSpaceID(invalidSpace); err == nil {
		t.Fatal("expected unknown model-space schema rejection")
	}
	invalidProfile := identity.RecallProfile
	invalidProfile.Schema = "worktrail.semantic.recall-profile.v2"
	if _, err := RecallProfileID(invalidProfile); err == nil {
		t.Fatal("expected unknown recall-profile schema rejection")
	}

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
	versions := SubsystemVersions{
		ChunkerVersion:   identity.RecallProfile.ChunkerVersion,
		IndexingVersion:  identity.RecallProfile.IndexingVersion,
		LexicalVersion:   identity.RecallProfile.LexicalVersion,
		SQLiteVecVersion: identity.RecallProfile.SQLiteVecVersion,
	}
	versions.ChunkerVersion = ""
	if _, err := Derive(manifest, runtime, versions); err == nil {
		t.Fatal("expected missing subsystem version rejection")
	}
	runtime.RuntimeSHA256 = ""
	if _, err := Derive(manifest, runtime, SubsystemVersions{
		ChunkerVersion:   identity.RecallProfile.ChunkerVersion,
		IndexingVersion:  identity.RecallProfile.IndexingVersion,
		LexicalVersion:   identity.RecallProfile.LexicalVersion,
		SQLiteVecVersion: identity.RecallProfile.SQLiteVecVersion,
	}); err == nil {
		t.Fatal("expected mismatched resolved runtime rejection")
	}
	if BundleID([]byte(`{"schema":"worktrail.semantic.bundle.v1"}`)) ==
		BundleID([]byte(`{"schema":"worktrail.semantic.bundle.v2"}`)) {
		t.Fatal("manifest schema change did not change bundle ID")
	}
}
