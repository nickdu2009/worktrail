package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/bundle"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/profile"
)

func TestParseSemanticCommandRebuildContract(t *testing.T) {
	for _, tc := range []struct {
		name  string
		args  []string
		want  semanticCommand
		valid bool
	}{
		{"project", []string{"rebuild", "--scope", "project"}, semanticCommand{name: "rebuild", scope: "project", format: "text"}, true},
		{"user JSON", []string{"--format=json", "rebuild", "--scope=user"}, semanticCommand{name: "rebuild", scope: "user", format: "json"}, true},
		{"all", []string{"rebuild", "--scope", "all"}, semanticCommand{name: "rebuild", scope: "all", format: "text"}, true},
		{"missing scope", []string{"rebuild"}, semanticCommand{}, false},
		{"invalid scope", []string{"rebuild", "--scope", "other"}, semanticCommand{}, false},
		{"duplicate scope", []string{"rebuild", "--scope", "project", "--scope=user"}, semanticCommand{}, false},
		{"scope on lifecycle", []string{"status", "--scope", "project"}, semanticCommand{}, false},
		{"missing scope value", []string{"rebuild", "--scope", "--format=json"}, semanticCommand{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseSemanticCommand(tc.args)
			if tc.valid {
				if err != nil || got != tc.want {
					t.Fatalf("parseSemanticCommand(%v) = %#v, %v; want %#v, nil", tc.args, got, err, tc.want)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "worktrail semantic rebuild --scope project|user|all") {
				t.Fatalf("parseSemanticCommand(%v) error = %v", tc.args, err)
			}
		})
	}
}

func TestRunSemanticRebuildAllRunsUserThenProjectAndFailsFast(t *testing.T) {
	deps, controller := semanticRebuildDeps(t)
	var scopes []string
	deps.rebuild = func(_ context.Context, request generation.RebuildRequest) (generation.Pointer, error) {
		scopes = append(scopes, request.Scope)
		if request.Scope == "project" {
			return generation.Pointer{}, errors.New("sources changed")
		}
		return rebuildPointer(request), nil
	}

	err := runSemanticRebuild(context.Background(), IO{Out: &bytes.Buffer{}}, []string{"rebuild", "--scope=all"}, deps)
	if err == nil {
		t.Fatal("runSemanticRebuild() error = nil")
	}
	if got, want := strings.Join(scopes, ","), "user,project"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
	if controller.startCalls != 1 {
		t.Fatalf("Start calls = %d, want 1", controller.startCalls)
	}
}

func TestRunSemanticRebuildAllStopsAfterUserFailure(t *testing.T) {
	deps, _ := semanticRebuildDeps(t)
	var scopes []string
	deps.rebuild = func(_ context.Context, request generation.RebuildRequest) (generation.Pointer, error) {
		scopes = append(scopes, request.Scope)
		return generation.Pointer{}, errors.New("sources changed")
	}

	if err := runSemanticRebuild(context.Background(), IO{Out: &bytes.Buffer{}}, []string{"rebuild", "--scope=all"}, deps); err == nil {
		t.Fatal("runSemanticRebuild() error = nil")
	}
	if got, want := strings.Join(scopes, ","), "user"; got != want {
		t.Fatalf("scopes = %q, want %q", got, want)
	}
}

func TestRunSemanticRebuildMapsRequestAndMetadataIdentity(t *testing.T) {
	deps, controller := semanticRebuildDeps(t)
	var input composition.Input
	deps.build = func(got composition.Input) (composition.Result, error) {
		input = got
		return rebuildComposition(controller), nil
	}
	var request generation.RebuildRequest
	deps.rebuild = func(_ context.Context, got generation.RebuildRequest) (generation.Pointer, error) {
		request = got
		return rebuildPointer(got), nil
	}
	deps.newGenerationID = func() (string, error) { return "g-test-project", nil }
	var out bytes.Buffer

	if err := runSemanticRebuild(context.Background(), IO{Out: &out}, []string{"rebuild", "--scope", "project"}, deps); err != nil {
		t.Fatalf("runSemanticRebuild() error = %v", err)
	}
	if input.Roots.Cache != "cache-root" || input.Versions.SQLiteVecVersion != composition.DefaultSubsystemVersions().SQLiteVecVersion {
		t.Fatalf("composition input = %#v", input)
	}
	if request.Scope != "project" || request.Root != "/project/.worktrail" || request.SemanticDir != "/project/.worktrail/index/semantic" {
		t.Fatalf("request paths = %#v", request)
	}
	if request.GenerationID != "g-test-project" || request.BundleID != "bundle-test-001" {
		t.Fatalf("request identity = %#v", request)
	}
	if request.Metadata.Profile != "profile-test-001" || request.Metadata.ModelSpace != "model-test-001" ||
		request.Metadata.SQLiteVec != "sqlite-vec-test" || request.Metadata.Dimension != 1024 {
		t.Fatalf("metadata = %#v", request.Metadata)
	}
	for _, want := range []string{
		"scope\tproject",
		"generation_id\tg-test-project",
		"profile_id\tprofile-test-001",
		"bundle_id\tbundle-test-001",
		"snapshot_hash\tsnapshot-test-001",
		"retirement_state\tnot_required",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("text report missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunSemanticRebuildRetirementWarningPreservesActivePointer(t *testing.T) {
	deps, _ := semanticRebuildDeps(t)
	deps.retirePending = func(context.Context, string) error { return errors.New("lease held at /private/index") }
	var out bytes.Buffer

	if err := runSemanticRebuild(context.Background(), IO{Out: &out}, []string{"rebuild", "--scope=project", "--format=json"}, deps); err != nil {
		t.Fatalf("runSemanticRebuild() error = %v", err)
	}
	var report semanticRebuildReport
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal report: %v stdout=%s", err, out.String())
	}
	if report.Schema != semanticRebuildReportSchema || report.Operation != "rebuild" || len(report.Scopes) != 1 {
		t.Fatalf("report = %#v", report)
	}
	got := report.Scopes[0]
	if got.GenerationID != "g-test-001" || got.ProfileID != "profile-test-001" ||
		got.BundleID != "bundle-test-001" || got.SnapshotHash != "snapshot-test-001" {
		t.Fatalf("active pointer was not preserved: %#v", got)
	}
	if got.RetirementState != "pending" || got.RetirementWarning == "" || strings.Contains(out.String(), "/private/index") {
		t.Fatalf("retirement warning = %#v stdout=%s", got, out.String())
	}
}

func TestRunSemanticRebuildSanitizesFailures(t *testing.T) {
	deps, _ := semanticRebuildDeps(t)
	deps.build = func(composition.Input) (composition.Result, error) {
		return composition.Result{}, &composition.Error{Code: contracts.ReasonBundleMissing}
	}
	var out bytes.Buffer

	if err := runSemanticRebuild(context.Background(), IO{Out: &out}, []string{"rebuild", "--scope=project", "--format=json"}, deps); err != nil {
		t.Fatalf("JSON rebuild error = %v", err)
	}
	assertCLIErrorEnvelope(t, out.String(), string(contracts.ReasonBundleMissing))
	if strings.Contains(out.String(), "cache-root") || strings.Contains(out.String(), "/project") {
		t.Fatalf("rebuild error leaked host data: %s", out.String())
	}
}

func TestRunSemanticRebuildStartFailureSkipsGeneration(t *testing.T) {
	deps, controller := semanticRebuildDeps(t)
	controller.startErr = &daemon.Error{
		Code:    contracts.ReasonRuntimeUnavailable,
		Message: "api-key=secret-value at /private/runtime",
	}
	called := false
	deps.rebuild = func(context.Context, generation.RebuildRequest) (generation.Pointer, error) {
		called = true
		return generation.Pointer{}, nil
	}
	var out bytes.Buffer

	if err := runSemanticRebuild(context.Background(), IO{Out: &out}, []string{"rebuild", "--scope=project", "--format=json"}, deps); err != nil {
		t.Fatalf("JSON rebuild error = %v", err)
	}
	assertCLIErrorEnvelope(t, out.String(), string(contracts.ReasonRuntimeUnavailable))
	if called || controller.startCalls != 1 || strings.Contains(out.String(), "secret-value") || strings.Contains(out.String(), "/private/runtime") {
		t.Fatalf("start failure handling leaked or rebuilt: called=%t start=%d output=%s", called, controller.startCalls, out.String())
	}
}

func TestSemanticRebuildReasonSanitizesGenerationFailures(t *testing.T) {
	for _, err := range []error{
		generation.ErrSourcesChanged,
		errors.New("activated semantic generation pointer does not match rebuild candidate at /private/index with api-key=secret"),
	} {
		var out bytes.Buffer
		deps, _ := semanticRebuildDeps(t)
		deps.rebuild = func(context.Context, generation.RebuildRequest) (generation.Pointer, error) {
			return generation.Pointer{}, err
		}

		if runErr := runSemanticRebuild(context.Background(), IO{Out: &out}, []string{"rebuild", "--scope=project", "--format=json"}, deps); runErr != nil {
			t.Fatalf("JSON rebuild error = %v", runErr)
		}
		assertCLIErrorEnvelope(t, out.String(), string(contracts.ReasonProfileStale))
		if strings.Contains(out.String(), "/private/index") || strings.Contains(out.String(), "api-key=secret") {
			t.Fatalf("generation failure leaked details: %s", out.String())
		}
	}
}

func TestFreshSemanticGenerationIDIsSafeAndUnique(t *testing.T) {
	first, err := freshSemanticGenerationID()
	if err != nil {
		t.Fatalf("first generation ID: %v", err)
	}
	second, err := freshSemanticGenerationID()
	if err != nil {
		t.Fatalf("second generation ID: %v", err)
	}
	if first == second || !strings.HasPrefix(first, "g-") || strings.ContainsAny(first, "/\\ \t\n") {
		t.Fatalf("unsafe generation IDs: %q, %q", first, second)
	}
}

func semanticRebuildDeps(t *testing.T) (semanticRebuildDependencies, *semanticRebuildController) {
	t.Helper()
	controller := &semanticRebuildController{}
	deps := semanticRebuildDependencies{
		discoverEnv: func() (paths.Env, error) {
			return paths.Env{
				UserRoot:    "/user/.worktrail",
				ProjectRoot: "/project",
				ProjectWT:   "/project/.worktrail",
			}, nil
		},
		discoverRoots: func() (paths.SemanticRoots, error) {
			return paths.SemanticRoots{Cache: "cache-root", Runtime: "runtime-root", Logs: "logs-root"}, nil
		},
		build: func(composition.Input) (composition.Result, error) {
			return rebuildComposition(controller), nil
		},
		rebuild: func(_ context.Context, request generation.RebuildRequest) (generation.Pointer, error) {
			return rebuildPointer(request), nil
		},
		retirePending: func(context.Context, string) error { return nil },
		newMetadata:   generation.NewRebuildMetadata,
		newGenerationID: func() (string, error) {
			return "g-test-001", nil
		},
	}
	return deps, controller
}

func rebuildComposition(controller daemon.Controller) composition.Result {
	return composition.Result{
		Runtime: bundle.ResolvedRuntime{BundleID: "bundle-test-001"},
		Identity: profile.Identity{
			ModelSpace:      profile.ModelSpace{Dimension: 1024},
			ModelSpaceID:    "model-test-001",
			RecallProfile:   profile.RecallProfile{SQLiteVecVersion: "sqlite-vec-test"},
			RecallProfileID: "profile-test-001",
		},
		Controller: controller,
	}
}

func rebuildPointer(request generation.RebuildRequest) generation.Pointer {
	return generation.Pointer{
		Scope:           request.Scope,
		GenerationID:    request.GenerationID,
		RecallProfileID: request.Metadata.Profile,
		BundleID:        request.BundleID,
		SnapshotHash:    "snapshot-test-001",
	}
}

type semanticRebuildController struct {
	startCalls int
	startErr   error
}

func (c *semanticRebuildController) Status(context.Context) (daemon.Report, error) {
	return daemon.Report{}, nil
}

func (c *semanticRebuildController) Start(context.Context) (daemon.Report, error) {
	c.startCalls++
	return daemon.Report{}, c.startErr
}

func (c *semanticRebuildController) Stop(context.Context) (daemon.Report, error) {
	return daemon.Report{}, nil
}

func (c *semanticRebuildController) Restart(context.Context) (daemon.Report, error) {
	return daemon.Report{}, nil
}
