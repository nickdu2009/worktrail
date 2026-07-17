package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
)

func TestRunSemanticStatusUsesOnlyStatus(t *testing.T) {
	controller := &semanticController{
		statusReport: semanticReport("status", daemon.StateUnavailable),
	}
	var out bytes.Buffer

	if err := runSemanticWithController(context.Background(), IO{Out: &out}, []string{"status"}, controller); err != nil {
		t.Fatalf("runSemanticWithController(status): %v", err)
	}
	if controller.statusCalls != 1 || controller.startCalls != 0 || controller.stopCalls != 0 || controller.restartCalls != 0 {
		t.Fatalf("controller calls = %+v, want only status", controller)
	}
	for _, want := range []string{
		"schema\t" + daemon.ReportSchema,
		"operation\tstatus",
		"state\t" + daemon.StateUnavailable,
		"reason\tsemantic_runtime_unavailable",
	} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("text report missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunSemanticStatusJSONWritesReport(t *testing.T) {
	want := semanticReport("status", daemon.StateUnavailable)
	controller := &semanticController{statusReport: want}
	var out bytes.Buffer

	if err := runSemanticWithController(context.Background(), IO{Out: &out}, []string{"status", "--format", "json"}, controller); err != nil {
		t.Fatalf("runSemanticWithController(status --format json): %v", err)
	}
	var got daemon.Report
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal status JSON: %v stdout=%s", err, out.String())
	}
	if got != want {
		t.Fatalf("status report = %#v, want %#v", got, want)
	}
	if controller.statusCalls != 1 || controller.startCalls != 0 {
		t.Fatalf("controller calls = %+v, want only status", controller)
	}
}

func TestRunSemanticStatusJSONAddsRuntimeSupportInformation(t *testing.T) {
	controller := &semanticController{statusReport: daemon.Report{
		Schema:       daemon.ReportSchema,
		Operation:    "status",
		State:        daemon.StateReady,
		SupportLevel: "experimental",
		Chip:         "m5",
		Warning:      "experimental runtime variant; verified by the mandatory installation self-check",
	}}
	var out bytes.Buffer

	if err := runSemanticWithController(context.Background(), IO{Out: &out}, []string{"status", "--format=json"}, controller); err != nil {
		t.Fatalf("runSemanticWithController(status --format json): %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal status JSON: %v stdout=%s", err, out.String())
	}
	for key, want := range map[string]string{
		"schema":        daemon.ReportSchema,
		"operation":     "status",
		"state":         daemon.StateReady,
		"support_level": "experimental",
		"chip":          "m5",
		"warning":       "experimental runtime variant; verified by the mandatory installation self-check",
	} {
		if got[key] != want {
			t.Fatalf("status JSON %s = %#v, want %q", key, got[key], want)
		}
	}
}

func TestRunSemanticStartAndRestartPreserveTypedError(t *testing.T) {
	for _, command := range []string{"start", "restart"} {
		t.Run(command, func(t *testing.T) {
			want := &daemon.Error{
				Code:    contracts.ReasonRuntimeUnavailable,
				Message: "release attestation is required",
			}
			controller := &semanticController{
				startErr:   want,
				restartErr: want,
			}
			var textOut bytes.Buffer

			err := runSemanticWithController(context.Background(), IO{Out: &textOut}, []string{command}, controller)
			if !errors.Is(err, want) {
				t.Fatalf("%s text error = %v, want typed error %v", command, err, want)
			}
			assertSemanticOperationCall(t, controller, command)

			controller = &semanticController{startErr: want, restartErr: want}
			var jsonOut bytes.Buffer
			if err := runSemanticWithController(context.Background(), IO{Out: &jsonOut}, []string{command, "--format=json"}, controller); err != nil {
				t.Fatalf("%s JSON error = %v stdout=%s", command, err, jsonOut.String())
			}
			assertCLIErrorEnvelope(t, jsonOut.String(), string(contracts.ReasonRuntimeUnavailable))
			assertSemanticOperationCall(t, controller, command)
		})
	}
}

func TestRunSemanticStopIsSuccessfulNoOp(t *testing.T) {
	controller := &semanticController{
		stopReport: semanticReport("stop", daemon.StateStopped),
	}
	var out bytes.Buffer

	if err := runSemanticWithController(context.Background(), IO{Out: &out}, []string{"stop"}, controller); err != nil {
		t.Fatalf("runSemanticWithController(stop): %v", err)
	}
	if controller.stopCalls != 1 || controller.statusCalls != 0 || controller.startCalls != 0 || controller.restartCalls != 0 {
		t.Fatalf("controller calls = %+v, want only stop", controller)
	}
	if !strings.Contains(out.String(), "state\t"+daemon.StateStopped) {
		t.Fatalf("stop output = %s", out.String())
	}
}

func TestRunSemanticLifecycleInjectsProductionController(t *testing.T) {
	roots := paths.SemanticRoots{Cache: "cache-root", Runtime: "runtime-root", Logs: "logs-root"}
	controller := &semanticController{statusReport: semanticReport("status", daemon.StateStopped)}
	rebuild, _ := semanticRebuildDeps(t)
	var input composition.Input
	deps := semanticDependencies{
		lifecycle: semanticLifecycleDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return roots, nil },
			build: func(got composition.Input) (composition.Result, error) {
				input = got
				return composition.Result{Controller: controller}, nil
			},
		},
		rebuild: rebuild,
	}
	var out bytes.Buffer
	if err := runSemanticWithDependencies(context.Background(), IO{Out: &out}, []string{"status", "--format=json"}, deps); err != nil {
		t.Fatalf("runSemanticWithDependencies(status): %v", err)
	}
	var report daemon.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal status: %v stdout=%s", err, out.String())
	}
	if input.Roots != roots || input.Versions != composition.DefaultSubsystemVersions() {
		t.Fatalf("composition input = %#v", input)
	}
	if report != controller.statusReport || controller.statusCalls != 1 {
		t.Fatalf("status report/calls = %#v/%d", report, controller.statusCalls)
	}
}

func TestRunSemanticLifecycleStatusCompositionFailureReturnsUnavailableReport(t *testing.T) {
	rebuild, _ := semanticRebuildDeps(t)
	deps := semanticDependencies{
		lifecycle: semanticLifecycleDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
			build: func(composition.Input) (composition.Result, error) {
				return composition.Result{}, &composition.Error{Code: contracts.ReasonBundleMissing}
			},
		},
		rebuild: rebuild,
	}
	var out bytes.Buffer
	if err := runSemanticWithDependencies(context.Background(), IO{Out: &out}, []string{"status", "--format=json"}, deps); err != nil {
		t.Fatalf("status composition failure = %v", err)
	}
	var report daemon.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal status report: %v stdout=%s", err, out.String())
	}
	if report.Schema != daemon.ReportSchema || report.Operation != "status" ||
		report.State != daemon.StateUnavailable || report.Reason != contracts.ReasonBundleMissing {
		t.Fatalf("status report = %#v", report)
	}
	if strings.Contains(out.String(), "cache-root") || strings.Contains(out.String(), "/private") {
		t.Fatalf("status report leaked host data: %s", out.String())
	}
}

func TestRunSemanticLifecycleCompositionFailureIsTypedAndSanitized(t *testing.T) {
	for _, command := range []string{"start", "stop", "restart"} {
		t.Run(command, func(t *testing.T) {
			rebuild, _ := semanticRebuildDeps(t)
			controller := &semanticController{}
			deps := semanticDependencies{
				lifecycle: semanticLifecycleDependencies{
					discoverRoots: func() (paths.SemanticRoots, error) { return paths.SemanticRoots{}, nil },
					build: func(composition.Input) (composition.Result, error) {
						return composition.Result{Controller: controller}, &daemon.Error{
							Code:    contracts.ReasonPlatformUnsupported,
							Message: "api-key=secret at /private/runtime",
						}
					},
				},
				rebuild: rebuild,
			}
			var textOut bytes.Buffer
			err := runSemanticWithDependencies(context.Background(), IO{Out: &textOut}, []string{command}, deps)
			var semanticErr *daemon.Error
			if !errors.As(err, &semanticErr) || semanticErr.Code != contracts.ReasonPlatformUnsupported {
				t.Fatalf("%s text error = %v, want platform unsupported", command, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "/private/runtime") {
				t.Fatalf("%s text error leaked host data: %v", command, err)
			}
			if controller.statusCalls != 0 || controller.startCalls != 0 || controller.stopCalls != 0 || controller.restartCalls != 0 {
				t.Fatalf("%s controller was invoked after composition failure: %+v", command, controller)
			}

			var jsonOut bytes.Buffer
			if err := runSemanticWithDependencies(context.Background(), IO{Out: &jsonOut}, []string{command, "--format=json"}, deps); err != nil {
				t.Fatalf("%s JSON error = %v stdout=%s", command, err, jsonOut.String())
			}
			assertCLIErrorEnvelope(t, jsonOut.String(), string(contracts.ReasonPlatformUnsupported))
			if strings.Contains(jsonOut.String(), "secret") || strings.Contains(jsonOut.String(), "/private/runtime") {
				t.Fatalf("%s JSON error leaked host data: %s", command, jsonOut.String())
			}
		})
	}
}

func TestRunSemanticRoutesRebuildWithoutLifecycleComposition(t *testing.T) {
	rebuild, controller := semanticRebuildDeps(t)
	deps := semanticDependencies{
		lifecycle: semanticLifecycleDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) {
				t.Fatal("rebuild must not discover lifecycle roots")
				return paths.SemanticRoots{}, nil
			},
			build: func(composition.Input) (composition.Result, error) {
				t.Fatal("rebuild must not build lifecycle composition")
				return composition.Result{}, nil
			},
		},
		rebuild: rebuild,
	}
	if err := runSemanticWithDependencies(context.Background(), IO{Out: &bytes.Buffer{}}, []string{"rebuild", "--scope=project"}, deps); err != nil {
		t.Fatalf("rebuild routing error = %v", err)
	}
	if controller.startCalls != 1 {
		t.Fatalf("rebuild controller Start calls = %d, want 1", controller.startCalls)
	}
}

func TestRunSemanticUsageDoesNotComposeRuntime(t *testing.T) {
	deps := semanticDependencies{
		lifecycle: semanticLifecycleDependencies{
			discoverRoots: func() (paths.SemanticRoots, error) {
				t.Fatal("usage handling must not discover semantic roots")
				return paths.SemanticRoots{}, nil
			},
			build: func(composition.Input) (composition.Result, error) {
				t.Fatal("usage handling must not build semantic composition")
				return composition.Result{}, nil
			},
		},
	}
	err := runSemanticWithDependencies(context.Background(), IO{Out: &bytes.Buffer{}}, nil, deps)
	if err == nil || !strings.Contains(err.Error(), "usage: worktrail semantic") {
		t.Fatalf("usage error = %v", err)
	}
}

func TestRunDispatchesSemanticStatus(t *testing.T) {
	project := t.TempDir()
	home := filepath.Join(t.TempDir(), "home")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WORKTRAIL_PROJECT_ROOT", project)
	t.Setenv("WORKTRAIL_HOME", home)
	var out, errw bytes.Buffer

	if err := Run(context.Background(), []string{"semantic", "status", "--format=json"}, nil, &out, &errw); err != nil {
		t.Fatalf("Run semantic status: %v stderr=%s", err, errw.String())
	}
	var report daemon.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("unmarshal dispatched status: %v stdout=%s", err, out.String())
	}
	if report.Operation != "status" || report.Schema != daemon.ReportSchema {
		t.Fatalf("dispatched status = %#v", report)
	}
}

func TestRunSemanticRejectsUnknownAndExtraArguments(t *testing.T) {
	controller := &semanticController{}
	for _, args := range [][]string{
		nil,
		{"unknown"},
		{"status", "extra"},
		{"status", "--unexpected"},
		{"status", "--format", "yaml"},
		{"status", "--format", "text", "--format", "text"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			err := runSemanticWithController(context.Background(), IO{Out: &bytes.Buffer{}}, args, controller)
			if err == nil || !strings.Contains(err.Error(), "usage: worktrail semantic") {
				t.Fatalf("runSemanticWithController(%v) error = %v", args, err)
			}
		})
	}
}

func TestRunSemanticJSONUsageErrorUsesCLIEnvelope(t *testing.T) {
	var out bytes.Buffer
	if err := runSemanticWithController(context.Background(), IO{Out: &out}, []string{"status", "extra", "--format", "json"}, &semanticController{}); err != nil {
		t.Fatalf("runSemanticWithController JSON usage error = %v", err)
	}
	assertCLIErrorEnvelope(t, out.String(), "cli_usage_error")
}

type semanticController struct {
	statusReport  daemon.Report
	statusErr     error
	startReport   daemon.Report
	startErr      error
	stopReport    daemon.Report
	stopErr       error
	restartReport daemon.Report
	restartErr    error
	statusCalls   int
	startCalls    int
	stopCalls     int
	restartCalls  int
}

func (c *semanticController) Status(context.Context) (daemon.Report, error) {
	c.statusCalls++
	return c.statusReport, c.statusErr
}

func (c *semanticController) Start(context.Context) (daemon.Report, error) {
	c.startCalls++
	return c.startReport, c.startErr
}

func (c *semanticController) Stop(context.Context) (daemon.Report, error) {
	c.stopCalls++
	return c.stopReport, c.stopErr
}

func (c *semanticController) Restart(context.Context) (daemon.Report, error) {
	c.restartCalls++
	return c.restartReport, c.restartErr
}

func semanticReport(operation, state string) daemon.Report {
	return daemon.Report{
		Schema:    daemon.ReportSchema,
		Operation: operation,
		State:     state,
		Reason:    contracts.ReasonRuntimeUnavailable,
		NextStep:  "release attestation is required",
	}
}

func assertSemanticOperationCall(t *testing.T, controller *semanticController, operation string) {
	t.Helper()
	if controller.statusCalls != 0 || controller.stopCalls != 0 {
		t.Fatalf("%s controller calls = %+v, unexpected status or stop", operation, controller)
	}
	switch operation {
	case "start":
		if controller.startCalls != 1 || controller.restartCalls != 0 {
			t.Fatalf("start controller calls = %+v", controller)
		}
	case "restart":
		if controller.restartCalls != 1 || controller.startCalls != 0 {
			t.Fatalf("restart controller calls = %+v", controller)
		}
	}
}
