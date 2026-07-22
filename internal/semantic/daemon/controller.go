// Package daemon defines the semantic runtime controller boundary.
package daemon

import (
	"context"

	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

const (
	ReportSchema = "worktrail.semantic.status.v1"

	StateUnavailable = "unavailable"
	StateStopped     = "stopped"

	localRuntimeNotConfiguredMessage = "local semantic runtime not configured"
)

// Controller owns semantic runtime lifecycle operations.
//
// UnavailableController reports that no local runtime composition is configured.
type Controller interface {
	Status(context.Context) (Report, error)
	Start(context.Context) (Report, error)
	Stop(context.Context) (Report, error)
	Restart(context.Context) (Report, error)
}

// Report is the machine-readable result of a semantic runtime operation.
type Report struct {
	Schema                   string               `json:"schema"`
	Operation                string               `json:"operation"`
	State                    string               `json:"state"`
	Reason                   contracts.ReasonCode `json:"reason"`
	NextStep                 string               `json:"next_step"`
	SupportLevel             string               `json:"support_level,omitempty"`
	Chip                     string               `json:"chip,omitempty"`
	Warning                  string               `json:"warning,omitempty"`
	ServiceRegistrationState string               `json:"service_registration_state,omitempty"`
	ServiceDomain            string               `json:"service_domain,omitempty"`
	HostProtocolVersion      int                  `json:"host_protocol_version,omitempty"`
	HostBuildID              string               `json:"host_build_id,omitempty"`
	HostState                string               `json:"host_state,omitempty"`
	HostPID                  int                  `json:"host_pid,omitempty"`
	HostStartTime            string               `json:"host_start_time,omitempty"`
	WorkerState              string               `json:"worker_state,omitempty"`
	ActiveBundleID           string               `json:"active_bundle_id,omitempty"`
	WorkerPID                int                  `json:"worker_pid,omitempty"`
	WorkerStartTime          string               `json:"worker_start_time,omitempty"`
	ActiveRequests           int                  `json:"active_requests,omitempty"`
	LastRequestCompletedAt   string               `json:"last_request_completed_at,omitempty"`
	IdleTimeout              string               `json:"idle_timeout,omitempty"`
	IdleDeadline             string               `json:"idle_deadline,omitempty"`
	ColdStartLatency         string               `json:"cold_start_latency,omitempty"`
	LastFailureCode          string               `json:"last_failure_code,omitempty"`
	// Started reports whether this Start request created the daemon. It is an
	// in-process lifecycle signal, not part of the public status contract.
	Started bool `json:"-"`
}

// Error reports a stable semantic runtime failure code.
type Error struct {
	Code    contracts.ReasonCode
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	return string(e.Code)
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// UnavailableController is the fake-first controller used until production
// composition injects a verified local runtime. It never starts, enumerates,
// or terminates processes.
type UnavailableController struct{}

func (UnavailableController) Status(context.Context) (Report, error) {
	return unavailableReport("status"), nil
}

func (UnavailableController) Start(context.Context) (Report, error) {
	report := unavailableReport("start")
	return report, unavailableError()
}

func (UnavailableController) Stop(context.Context) (Report, error) {
	return Report{
		Schema:    ReportSchema,
		Operation: "stop",
		State:     StateStopped,
		Reason:    contracts.ReasonRuntimeUnavailable,
		NextStep:  "no action is required; " + localRuntimeNotConfiguredMessage + "; " + trustedBundleRequiredMessage,
	}, nil
}

func (UnavailableController) Restart(context.Context) (Report, error) {
	report := unavailableReport("restart")
	return report, unavailableError()
}

func unavailableReport(operation string) Report {
	return Report{
		Schema:    ReportSchema,
		Operation: operation,
		State:     StateUnavailable,
		Reason:    contracts.ReasonRuntimeUnavailable,
		NextStep:  localRuntimeNotConfiguredMessage + "; " + trustedBundleRequiredMessage,
	}
}

func unavailableError() error {
	return &Error{
		Code:    contracts.ReasonRuntimeUnavailable,
		Message: localRuntimeNotConfiguredMessage + ": " + trustedBundleRequiredMessage,
	}
}
