package generation

import (
	"strings"
	"testing"
)

func TestNewRebuildMetadata(t *testing.T) {
	got, err := NewRebuildMetadata("profile-test-001", "model-space-test-001", "sqlite-vec-test", 1024)
	if err != nil {
		t.Fatalf("NewRebuildMetadata() error = %v", err)
	}

	want := Metadata{
		Schema:     databaseSchema,
		Profile:    "profile-test-001",
		ModelSpace: "model-space-test-001",
		SQLiteVec:  "sqlite-vec-test",
		Dimension:  1024,
	}
	if got != want {
		t.Fatalf("NewRebuildMetadata() = %#v, want %#v", got, want)
	}
}

func TestNewRebuildMetadataRejectsInvalidInputs(t *testing.T) {
	for _, test := range []struct {
		name       string
		profile    string
		modelSpace string
		sqliteVec  string
		dimension  int
		wantError  string
	}{
		{
			name:       "empty profile",
			profile:    " ",
			modelSpace: "model-space-test-001",
			sqliteVec:  "sqlite-vec-test",
			dimension:  1024,
			wantError:  "profile is required",
		},
		{
			name:       "empty model space",
			profile:    "profile-test-001",
			modelSpace: "\t",
			sqliteVec:  "sqlite-vec-test",
			dimension:  1024,
			wantError:  "model-space is required",
		},
		{
			name:       "empty sqlite vec",
			profile:    "profile-test-001",
			modelSpace: "model-space-test-001",
			sqliteVec:  "\n",
			dimension:  1024,
			wantError:  "sqlite-vec is required",
		},
		{
			name:       "zero dimension",
			profile:    "profile-test-001",
			modelSpace: "model-space-test-001",
			sqliteVec:  "sqlite-vec-test",
			dimension:  0,
			wantError:  "dimension must be between 1 and 8192",
		},
		{
			name:       "negative dimension",
			profile:    "profile-test-001",
			modelSpace: "model-space-test-001",
			sqliteVec:  "sqlite-vec-test",
			dimension:  -1,
			wantError:  "dimension must be between 1 and 8192",
		},
		{
			name:       "dimension above maximum",
			profile:    "profile-test-001",
			modelSpace: "model-space-test-001",
			sqliteVec:  "sqlite-vec-test",
			dimension:  8193,
			wantError:  "dimension must be between 1 and 8192",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewRebuildMetadata(test.profile, test.modelSpace, test.sqliteVec, test.dimension); err == nil {
				t.Fatal("NewRebuildMetadata() error = nil")
			} else if !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("NewRebuildMetadata() error = %q, want substring %q", err, test.wantError)
			}
		})
	}
}
