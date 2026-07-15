package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	wtruntime "github.com/nickdu2009/worktrail/internal/runtime"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
)

type recoveryDoctorReport struct {
	Schema        string                     `json:"schema"`
	OK            bool                       `json:"ok"`
	Scope         string                     `json:"scope"`
	Apply         bool                       `json:"apply"`
	Coverage      []string                   `json:"coverage"`
	State         wtstate.QuarantineReport   `json:"state"`
	RuntimePlan   wtruntime.RecoveryProposal `json:"runtime_plan"`
	RuntimeResult wtruntime.RecoveryResult   `json:"runtime_result"`
}

func runDoctorRecovery(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	for _, arg := range args {
		if isHelpArg(arg) {
			printRecoveryDoctorHelp(ioctx.Out)
			return nil
		}
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{
		"apply": true, "confirm": true, "json": true,
	})
	format := flagValue(flags, "format", "text")
	if flags["json"] == "true" {
		format = "json"
	}
	fail := func(err error) error {
		return failCLICommand(ioctx, format, "worktrail doctor recovery", err)
	}
	if len(positional) != 0 {
		return fail(errors.New("usage: worktrail doctor recovery [--scope project|user] [--apply --confirm] [--format text|json]"))
	}
	for key := range flags {
		switch key {
		case "scope", "format", "apply", "confirm", "json":
		default:
			return fail(fmt.Errorf("unknown doctor recovery flag --%s", key))
		}
	}
	if format != "text" && format != "json" {
		return fail(errors.New("doctor recovery format must be text or json"))
	}
	apply := flags["apply"] == "true"
	confirm := flags["confirm"] == "true"
	if apply != confirm {
		return fail(errors.New("worktrail doctor recovery quarantine requires both --apply and --confirm"))
	}
	select {
	case <-ctx.Done():
		return fail(ctx.Err())
	default:
	}
	scope := strings.TrimSpace(flagValue(flags, "scope", "project"))
	stateReport, err := wtstate.QuarantineMalformed(env, wtstate.QuarantineRequest{
		Scope: scope,
	})
	if err != nil {
		return fail(err)
	}
	plan, err := wtruntime.RecoveryPlan(env, scope, time.Now().UTC())
	if err != nil {
		return fail(err)
	}
	report := recoveryDoctorReport{
		Schema:      "worktrail.recovery.doctor.v1",
		Scope:       plan.Scope,
		Apply:       apply,
		Coverage:    []string{"state", "runtime"},
		State:       stateReport,
		RuntimePlan: plan,
	}
	if apply {
		report.State, err = wtstate.QuarantineMalformed(env, wtstate.QuarantineRequest{
			Scope: scope, Apply: true, Confirm: true, Actor: "doctor:recovery",
		})
		if err != nil {
			return fail(err)
		}
		report.RuntimeResult, err = wtruntime.ApplyRecovery(plan)
		if err != nil {
			return fail(err)
		}
	}
	report.OK = recoveryDoctorOK(report)
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "recovery scope=%s mode=%s ok=%t malformed_state=%d malformed_runtime=%d\n",
		report.Scope, recoveryDoctorMode(apply), report.OK, len(report.State.Diagnostics), len(plan.Items))
	fmt.Fprintln(ioctx.Out, "coverage=state,runtime")
	for _, diagnostic := range report.State.Diagnostics {
		fmt.Fprintf(ioctx.Out, "state\t%s\t%s\trepairable=%t\t%s\n",
			diagnostic.Code, diagnostic.Path, diagnostic.Repairable, diagnostic.Message)
	}
	for _, item := range plan.Items {
		fmt.Fprintf(ioctx.Out, "runtime\tmalformed\t%s\t%s\t%s\n", item.RelPath, item.QuarantinePath, item.Error)
	}
	if apply {
		fmt.Fprintf(ioctx.Out, "quarantined_state=%d quarantined_runtime=%d runtime_operation=%s\n",
			quarantinedStateCount(report.State), report.RuntimeResult.Quarantined, report.RuntimeResult.OperationID)
	} else {
		fmt.Fprintln(ioctx.Out, "dry-run: no files moved; use --apply --confirm to quarantine repairable malformed state and runtime records")
	}
	return nil
}

func recoveryDoctorOK(report recoveryDoctorReport) bool {
	if !report.Apply {
		return len(report.State.Diagnostics) == 0 && len(report.RuntimePlan.Items) == 0
	}
	for _, diagnostic := range report.State.Diagnostics {
		if !diagnostic.Repairable {
			return false
		}
	}
	return true
}

func quarantinedStateCount(report wtstate.QuarantineReport) int {
	if !report.Applied {
		return 0
	}
	return len(report.Actions)
}

func recoveryDoctorMode(apply bool) string {
	if apply {
		return "apply"
	}
	return "dry-run"
}

func printRecoveryDoctorHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail doctor recovery [--scope project|user] [--apply --confirm] [--format text|json]")
	fmt.Fprintln(out, "Default mode is read-only. Apply quarantines repairable malformed state and runtime records under runtime/quarantine.")
	fmt.Fprintln(out, "Mutation requires both --apply and --confirm; --repair is not accepted.")
}
