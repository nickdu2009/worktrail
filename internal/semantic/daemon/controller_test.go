package daemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

var _ Controller = UnavailableController{}

func TestUnavailableControllerStatus(t *testing.T) {
	report, err := (UnavailableController{}).Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	if report.State != StateUnavailable {
		t.Errorf("Status().State = %q, want %q", report.State, StateUnavailable)
	}
	if report.Reason != contracts.ReasonRuntimeUnavailable {
		t.Errorf("Status().Reason = %q, want %q", report.Reason, contracts.ReasonRuntimeUnavailable)
	}
	if report.NextStep != localRuntimeNotConfiguredMessage+"; "+trustedBundleRequiredMessage {
		t.Errorf("Status().NextStep = %q, want local runtime configuration guidance", report.NextStep)
	}
}

func TestUnavailableControllerStartAndRestartReturnTypedError(t *testing.T) {
	controller := UnavailableController{}

	for _, operation := range []struct {
		name string
		run  func(context.Context) (Report, error)
	}{
		{name: "start", run: controller.Start},
		{name: "restart", run: controller.Restart},
	} {
		t.Run(operation.name, func(t *testing.T) {
			report, err := operation.run(context.Background())
			if report.State != StateUnavailable {
				t.Errorf("%s report state = %q, want %q", operation.name, report.State, StateUnavailable)
			}

			var runtimeErr *Error
			if !errors.As(err, &runtimeErr) {
				t.Fatalf("%s error = %v, want *Error", operation.name, err)
			}
			if runtimeErr.Code != contracts.ReasonRuntimeUnavailable {
				t.Errorf("%s error code = %q, want %q", operation.name, runtimeErr.Code, contracts.ReasonRuntimeUnavailable)
			}
			if err.Error() != localRuntimeNotConfiguredMessage+": "+trustedBundleRequiredMessage {
				t.Errorf("%s error = %q, want generic local runtime guidance", operation.name, err)
			}
		})
	}
}

func TestUnavailableControllerStopIsNoOp(t *testing.T) {
	report, err := (UnavailableController{}).Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if report.Operation != "stop" {
		t.Errorf("Stop().Operation = %q, want %q", report.Operation, "stop")
	}
	if report.State != StateStopped {
		t.Errorf("Stop().State = %q, want %q", report.State, StateStopped)
	}
	if !strings.Contains(report.NextStep, "no action is required") {
		t.Errorf("Stop().NextStep = %q, want no-op guidance", report.NextStep)
	}
	if !strings.Contains(report.NextStep, localRuntimeNotConfiguredMessage) ||
		!strings.Contains(report.NextStep, trustedBundleRequiredMessage) {
		t.Errorf("Stop().NextStep = %q, want generic local runtime guidance", report.NextStep)
	}
}

func TestUnavailableControllerReportsUseStatusSchema(t *testing.T) {
	controller := UnavailableController{}

	reports := []Report{}
	status, err := controller.Status(context.Background())
	if err != nil {
		t.Fatalf("Status() error = %v", err)
	}
	reports = append(reports, status)

	start, _ := controller.Start(context.Background())
	reports = append(reports, start)

	stop, err := controller.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	reports = append(reports, stop)

	restart, _ := controller.Restart(context.Background())
	reports = append(reports, restart)

	for _, report := range reports {
		if report.Schema != ReportSchema {
			t.Errorf("%s schema = %q, want %q", report.Operation, report.Schema, ReportSchema)
		}
	}
}

func TestUnavailableControllerDoesNotExposeBundlePathsOrKeys(t *testing.T) {
	controller := UnavailableController{}
	operations := []func(context.Context) (Report, error){
		controller.Status,
		controller.Start,
		controller.Stop,
		controller.Restart,
	}

	for _, operation := range operations {
		report, err := operation(context.Background())
		for _, message := range []string{report.NextStep, errorMessage(err)} {
			if strings.Contains(message, "/") || strings.Contains(strings.ToLower(message), "key") {
				t.Fatalf("controller exposed implementation detail: %q", message)
			}
		}
	}
}

func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
