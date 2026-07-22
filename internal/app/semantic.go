package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/semantic/chunk"
	"github.com/nickdu2009/worktrail/internal/semantic/composition"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
	"github.com/nickdu2009/worktrail/internal/semantic/daemon"
	"github.com/nickdu2009/worktrail/internal/semantic/generation"
	"github.com/nickdu2009/worktrail/internal/semantic/service"
)

func runSemantic(ctx context.Context, ioctx IO, args []string) error {
	return runSemanticWithDependencies(ctx, ioctx, args, productionSemanticDependencies())
}

type semanticDependencies struct {
	lifecycle semanticLifecycleDependencies
	rebuild   semanticRebuildDependencies
	host      func(context.Context, paths.SemanticRoots) error
	watch     func(context.Context) error
	uninstall func(context.Context, paths.SemanticRoots) error
}

type semanticLifecycleDependencies struct {
	discoverRoots func() (paths.SemanticRoots, error)
	build         func(composition.Input) (composition.Result, error)
	serviceClient func(paths.SemanticRoots) (daemon.Controller, error)
}

func productionSemanticDependencies() semanticDependencies {
	return semanticDependencies{
		lifecycle: productionSemanticLifecycleDependencies(),
		rebuild:   productionSemanticRebuildDependencies(),
		host: func(ctx context.Context, roots paths.SemanticRoots) error {
			composed, err := composition.BuildHost(composition.Input{Roots: roots, Versions: composition.DefaultSubsystemVersions()})
			if err != nil {
				return err
			}
			return composed.Host.Run(ctx)
		},
		watch: daemon.RunWorkerWatch,
		uninstall: func(ctx context.Context, roots paths.SemanticRoots) error {
			manager, err := service.NewManager(roots)
			if err != nil {
				return err
			}
			return manager.Remove(ctx)
		},
	}
}

func productionSemanticLifecycleDependencies() semanticLifecycleDependencies {
	return semanticLifecycleDependencies{
		discoverRoots: paths.DiscoverSemanticRoots,
		build:         composition.Build,
		serviceClient: func(roots paths.SemanticRoots) (daemon.Controller, error) {
			return service.NewClient(roots, "", "", "", "")
		},
	}
}

func runSemanticWithDependencies(ctx context.Context, ioctx IO, args []string, deps semanticDependencies) error {
	if semanticWorkerWatchCommand(args) {
		if deps.watch == nil {
			return semanticInstallerError(contracts.ReasonRuntimeUnavailable, nil)
		}
		return deps.watch(ctx)
	}
	if semanticHostCommand(args) {
		roots, err := deps.lifecycle.discoverRoots()
		if err != nil || deps.host == nil {
			return semanticInstallerError(contracts.ReasonRuntimeUnavailable, err)
		}
		return deps.host(ctx, roots)
	}
	if semanticUninstallCommand(args) {
		roots, err := deps.lifecycle.discoverRoots()
		if err != nil || deps.uninstall == nil {
			return semanticInstallerError(contracts.ReasonRuntimeUnavailable, err)
		}
		return deps.uninstall(ctx, roots)
	}
	command, err := parseSemanticCommand(args)
	if err != nil {
		// Keep parsing and usage errors independent from the local runtime.
		return runSemanticWithController(ctx, ioctx, args, daemon.UnavailableController{})
	}
	if command.name == "rebuild" {
		return runSemanticRebuild(ctx, ioctx, args, deps.rebuild)
	}
	return runSemanticWithLifecycle(ctx, ioctx, args, command, deps.lifecycle)
}

func semanticWorkerWatchCommand(args []string) bool {
	return len(args) == 2 && args[0] == "worker-watch" && args[1] == "--launchd"
}

func runSemanticWithLifecycle(
	ctx context.Context,
	ioctx IO,
	args []string,
	command semanticCommand,
	deps semanticLifecycleDependencies,
) error {
	roots, err := deps.discoverRoots()
	if err != nil {
		return failSemanticLifecycle(ioctx, command, args, contracts.ReasonRuntimeUnavailable)
	}
	if command.name != "start" && deps.serviceClient != nil {
		controller, err := deps.serviceClient(roots)
		if err != nil {
			return failSemanticLifecycle(ioctx, command, args, semanticLifecycleReason(err))
		}
		return runSemanticWithController(ctx, ioctx, args, controller)
	}
	composed, err := deps.build(composition.Input{
		Roots:    roots,
		Versions: composition.DefaultSubsystemVersions(),
	})
	if err != nil {
		return failSemanticLifecycle(ioctx, command, args, semanticLifecycleReason(err))
	}
	if composed.Controller == nil {
		return failSemanticLifecycle(ioctx, command, args, contracts.ReasonRuntimeUnavailable)
	}
	return runSemanticWithController(ctx, ioctx, args, composed.Controller)
}

func semanticHostCommand(args []string) bool {
	return len(args) == 2 && args[0] == "host" && args[1] == "--launchd"
}

func semanticUninstallCommand(args []string) bool {
	return len(args) == 3 && args[0] == "service" && args[1] == "uninstall" && args[2] == "--confirm"
}

func failSemanticLifecycle(ioctx IO, command semanticCommand, args []string, code contracts.ReasonCode) error {
	if code == "" {
		code = contracts.ReasonRuntimeUnavailable
	}
	if command.name == "status" {
		return writeSemanticReport(ioctx, command.format, semanticUnavailableReport(command.name, code))
	}
	return failCLICommand(
		ioctx,
		command.format,
		joinCommand(append([]string{"semantic"}, args...)),
		&daemon.Error{Code: code, Message: semanticLifecycleMessage(code)},
	)
}

func semanticUnavailableReport(operation string, code contracts.ReasonCode) daemon.Report {
	return daemon.Report{
		Schema:    daemon.ReportSchema,
		Operation: operation,
		State:     daemon.StateUnavailable,
		Reason:    code,
		NextStep:  semanticLifecycleMessage(code),
	}
}

func semanticLifecycleReason(err error) contracts.ReasonCode {
	var compositionErr *composition.Error
	if errors.As(err, &compositionErr) && compositionErr.Code != "" {
		return compositionErr.Code
	}
	var daemonErr *daemon.Error
	if errors.As(err, &daemonErr) && daemonErr.Code != "" {
		return daemonErr.Code
	}
	return contracts.ReasonRuntimeUnavailable
}

func semanticLifecycleMessage(code contracts.ReasonCode) string {
	switch code {
	case contracts.ReasonBundleMissing, contracts.ReasonServiceNotInstalled, contracts.ReasonServiceIncompatible:
		return "worktrail init --semantic"
	case contracts.ReasonPlatformUnsupported:
		return "semantic runtime is unsupported on this platform"
	case contracts.ReasonProfileStale, contracts.ReasonGenerationMissing:
		return "worktrail semantic rebuild --scope all"
	default:
		return "semantic runtime is unavailable"
	}
}

func runSemanticWithController(ctx context.Context, ioctx IO, args []string, controller daemon.Controller) error {
	command, format, err := parseSemanticArgs(args)
	if err != nil {
		return failCLICommand(ioctx, semanticErrorFormat(args), joinCommand(append([]string{"semantic"}, args...)), err)
	}

	var report daemon.Report
	switch command {
	case "status":
		report, err = controller.Status(ctx)
	case "start":
		report, err = controller.Start(ctx)
	case "stop":
		report, err = controller.Stop(ctx)
	case "restart":
		report, err = controller.Restart(ctx)
	default:
		return failCLICommand(ioctx, format, joinCommand(append([]string{"semantic"}, args...)), semanticUsageError(args))
	}
	if err != nil {
		return failCLICommand(ioctx, format, joinCommand(append([]string{"semantic"}, args...)), err)
	}
	return writeSemanticReport(ioctx, format, report)
}

func parseSemanticArgs(args []string) (command, format string, err error) {
	parsed, err := parseSemanticCommand(args)
	if err != nil {
		return "", "", err
	}
	return parsed.name, parsed.format, nil
}

type semanticCommand struct {
	name   string
	scope  string
	format string
}

func parseSemanticCommand(args []string) (semanticCommand, error) {
	command := semanticCommand{format: "text"}
	var positional []string
	formatSet := false
	scopeSet := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--format":
			if formatSet || i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return semanticCommand{}, semanticUsageError(args)
			}
			i++
			command.format = args[i]
			formatSet = true
		case strings.HasPrefix(arg, "--format="):
			if formatSet {
				return semanticCommand{}, semanticUsageError(args)
			}
			command.format = strings.TrimPrefix(arg, "--format=")
			formatSet = true
		case arg == "--scope":
			if scopeSet || i+1 >= len(args) || strings.HasPrefix(args[i+1], "--") {
				return semanticCommand{}, semanticUsageError(args)
			}
			i++
			command.scope = args[i]
			scopeSet = true
		case strings.HasPrefix(arg, "--scope="):
			if scopeSet {
				return semanticCommand{}, semanticUsageError(args)
			}
			command.scope = strings.TrimPrefix(arg, "--scope=")
			scopeSet = true
		case strings.HasPrefix(arg, "--"):
			return semanticCommand{}, semanticUsageError(args)
		default:
			positional = append(positional, arg)
		}
	}
	if len(positional) != 1 {
		return semanticCommand{}, semanticUsageError(args)
	}
	if command.format != "text" && command.format != "json" {
		return semanticCommand{}, semanticUsageError(args)
	}
	command.name = positional[0]
	switch command.name {
	case "status", "start", "stop", "restart":
		if scopeSet {
			return semanticCommand{}, semanticUsageError(args)
		}
		return command, nil
	case "rebuild":
		if !scopeSet || !semanticRebuildScope(command.scope) {
			return semanticCommand{}, semanticUsageError(args)
		}
		return command, nil
	default:
		return semanticCommand{}, semanticUsageError(args)
	}
}

func semanticErrorFormat(args []string) string {
	if inferJSONMode(args) {
		return "json"
	}
	return "text"
}

func semanticUsageError(args []string) error {
	const usage = "usage: worktrail semantic <status|start|stop|restart> [--format text|json]\n       worktrail semantic rebuild --scope project|user|all [--format text|json]\n       worktrail semantic service uninstall --confirm"
	if len(args) == 0 {
		return errors.New(usage)
	}
	return fmt.Errorf("%s: invalid arguments %q", usage, args)
}

func writeSemanticReport(ioctx IO, format string, report daemon.Report) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	if _, err := fmt.Fprintf(
		ioctx.Out,
		"schema\t%s\noperation\t%s\nstate\t%s\nreason\t%s\nnext_step\t%s\n",
		report.Schema,
		report.Operation,
		report.State,
		report.Reason,
		report.NextStep,
	); err != nil {
		return err
	}
	if report.SupportLevel != "" {
		if _, err := fmt.Fprintf(ioctx.Out, "support_level\t%s\n", report.SupportLevel); err != nil {
			return err
		}
	}
	if report.Chip != "" {
		if _, err := fmt.Fprintf(ioctx.Out, "chip\t%s\n", report.Chip); err != nil {
			return err
		}
	}
	if report.Warning != "" {
		if _, err := fmt.Fprintf(ioctx.Out, "warning\t%s\n", report.Warning); err != nil {
			return err
		}
	}
	for _, field := range []struct {
		name  string
		value string
	}{
		{"service_registration_state", report.ServiceRegistrationState},
		{"service_domain", report.ServiceDomain},
		{"host_build_id", report.HostBuildID},
		{"host_state", report.HostState},
		{"host_start_time", report.HostStartTime},
		{"worker_state", report.WorkerState},
		{"active_bundle_id", report.ActiveBundleID},
		{"worker_start_time", report.WorkerStartTime},
		{"last_request_completed_at", report.LastRequestCompletedAt},
		{"idle_timeout", report.IdleTimeout},
		{"idle_deadline", report.IdleDeadline},
		{"cold_start_latency", report.ColdStartLatency},
		{"last_failure_code", report.LastFailureCode},
	} {
		if field.value != "" {
			if _, err := fmt.Fprintf(ioctx.Out, "%s\t%s\n", field.name, field.value); err != nil {
				return err
			}
		}
	}
	if report.HostProtocolVersion != 0 {
		if _, err := fmt.Fprintf(ioctx.Out, "host_protocol_version\t%d\n", report.HostProtocolVersion); err != nil {
			return err
		}
	}
	if report.WorkerPID != 0 {
		if _, err := fmt.Fprintf(ioctx.Out, "worker_pid\t%d\n", report.WorkerPID); err != nil {
			return err
		}
	}
	if report.HostPID != 0 {
		if _, err := fmt.Fprintf(ioctx.Out, "host_pid\t%d\n", report.HostPID); err != nil {
			return err
		}
	}
	if report.ActiveRequests != 0 {
		if _, err := fmt.Fprintf(ioctx.Out, "active_requests\t%d\n", report.ActiveRequests); err != nil {
			return err
		}
	}
	return nil
}

const semanticRebuildReportSchema = "worktrail.semantic.rebuild.v1"

type semanticRebuildReport struct {
	Schema    string                       `json:"schema"`
	Operation string                       `json:"operation"`
	Scopes    []semanticRebuildScopeReport `json:"scopes"`
}

type semanticRebuildScopeReport struct {
	Scope             string `json:"scope"`
	GenerationID      string `json:"generation_id"`
	ProfileID         string `json:"profile_id"`
	BundleID          string `json:"bundle_id"`
	SnapshotHash      string `json:"snapshot_hash"`
	RetirementState   string `json:"retirement_state"`
	RetirementWarning string `json:"retirement_warning,omitempty"`
}

type semanticRebuildDependencies struct {
	discoverEnv     func() (paths.Env, error)
	discoverRoots   func() (paths.SemanticRoots, error)
	build           func(composition.Input) (composition.Result, error)
	rebuild         func(context.Context, generation.RebuildRequest) (generation.Pointer, error)
	retirePending   func(context.Context, string) error
	newMetadata     func(string, string, string, int) (generation.Metadata, error)
	newGenerationID func() (string, error)
}

func productionSemanticRebuildDependencies() semanticRebuildDependencies {
	return semanticRebuildDependencies{
		discoverEnv:     paths.Discover,
		discoverRoots:   paths.DiscoverSemanticRoots,
		build:           composition.Build,
		rebuild:         generation.Rebuild,
		retirePending:   generation.RetirePending,
		newMetadata:     generation.NewRebuildMetadata,
		newGenerationID: freshSemanticGenerationID,
	}
}

func runSemanticRebuild(ctx context.Context, ioctx IO, args []string, deps semanticRebuildDependencies) error {
	command, err := parseSemanticCommand(args)
	if err != nil {
		return failCLICommand(ioctx, semanticErrorFormat(args), joinCommand(append([]string{"semantic"}, args...)), err)
	}
	if command.name != "rebuild" {
		return failCLICommand(ioctx, command.format, joinCommand(append([]string{"semantic"}, args...)), semanticUsageError(args))
	}

	env, err := deps.discoverEnv()
	if err != nil {
		return failSemanticRebuild(ioctx, command.format, args, contracts.ReasonRuntimeUnavailable)
	}
	roots, err := deps.discoverRoots()
	if err != nil {
		return failSemanticRebuild(ioctx, command.format, args, contracts.ReasonRuntimeUnavailable)
	}
	composed, err := deps.build(composition.Input{
		Roots:    roots,
		Versions: composition.DefaultSubsystemVersions(),
	})
	if err != nil {
		return failSemanticRebuild(ioctx, command.format, args, semanticRebuildReason(err))
	}
	if _, err := composed.Controller.Start(ctx); err != nil {
		return failSemanticRebuild(ioctx, command.format, args, semanticRebuildReason(err))
	}
	metadata, err := deps.newMetadata(
		composed.Identity.RecallProfileID,
		composed.Identity.ModelSpaceID,
		composed.Identity.RecallProfile.SQLiteVecVersion,
		composed.Identity.ModelSpace.Dimension,
	)
	if err != nil {
		return failSemanticRebuild(ioctx, command.format, args, contracts.ReasonProfileStale)
	}

	report := semanticRebuildReport{Schema: semanticRebuildReportSchema, Operation: "rebuild"}
	for _, scope := range semanticRebuildScopes(command.scope) {
		root, err := env.ScopeRoot(scope)
		if err != nil {
			return failSemanticRebuild(ioctx, command.format, args, contracts.ReasonRuntimeUnavailable)
		}
		semanticDir, err := env.SemanticIndexRoot(scope)
		if err != nil {
			return failSemanticRebuild(ioctx, command.format, args, contracts.ReasonRuntimeUnavailable)
		}
		generationID, err := deps.newGenerationID()
		if err != nil {
			return failSemanticRebuild(ioctx, command.format, args, contracts.ReasonRuntimeUnavailable)
		}
		versions := composition.DefaultSubsystemVersions()
		active, err := deps.rebuild(ctx, generation.RebuildRequest{
			Scope:        scope,
			Root:         root,
			SemanticDir:  semanticDir,
			GenerationID: generationID,
			BundleID:     composed.Runtime.BundleID,
			Metadata:     metadata,
			Policy:       chunk.DefaultPolicy(),
			Versions: generation.SubsystemVersions{
				ChunkerVersion:   versions.ChunkerVersion,
				IndexingVersion:  versions.IndexingVersion,
				LexicalVersion:   versions.LexicalVersion,
				SQLiteVecVersion: versions.SQLiteVecVersion,
			},
			TokenCounter: composed.TokenCounter,
			Embedder:     composed.Embedder,
		})
		if err != nil {
			return failSemanticRebuild(ioctx, command.format, args, semanticRebuildReason(err))
		}

		scopeReport := semanticRebuildScopeReport{
			Scope:           active.Scope,
			GenerationID:    active.GenerationID,
			ProfileID:       active.RecallProfileID,
			BundleID:        active.BundleID,
			SnapshotHash:    active.SnapshotHash,
			RetirementState: "not_required",
		}
		if active.RetireGenerationID != "" {
			scopeReport.RetirementState = "retired"
		}
		if err := deps.retirePending(ctx, semanticDir); err != nil {
			scopeReport.RetirementState = "pending"
			scopeReport.RetirementWarning = "retirement will be retried later"
		}
		report.Scopes = append(report.Scopes, scopeReport)
	}
	return writeSemanticRebuildReport(ioctx, command.format, report)
}

func semanticRebuildScope(scope string) bool {
	return scope == "project" || scope == "user" || scope == "all"
}

func semanticRebuildScopes(scope string) []string {
	if scope == "all" {
		return []string{"user", "project"}
	}
	return []string{scope}
}

func failSemanticRebuild(ioctx IO, format string, args []string, code contracts.ReasonCode) error {
	err := &daemon.Error{Code: code, Message: semanticRebuildMessage(code)}
	return failCLICommand(ioctx, format, joinCommand(append([]string{"semantic"}, args...)), err)
}

func semanticRebuildReason(err error) contracts.ReasonCode {
	var compositionErr *composition.Error
	if errors.As(err, &compositionErr) {
		return compositionErr.Code
	}
	var daemonErr *daemon.Error
	if errors.As(err, &daemonErr) && daemonErr.Code != "" {
		return daemonErr.Code
	}
	if errors.Is(err, generation.ErrSourcesChanged) {
		return contracts.ReasonProfileStale
	}
	return contracts.ReasonProfileStale
}

func semanticRebuildMessage(code contracts.ReasonCode) string {
	switch code {
	case contracts.ReasonBundleMissing:
		return "worktrail init --semantic"
	case contracts.ReasonPlatformUnsupported:
		return "semantic runtime is unsupported on this platform"
	case contracts.ReasonProfileStale:
		return "semantic rebuild did not activate a compatible generation"
	default:
		return "semantic runtime is unavailable"
	}
}

func writeSemanticRebuildReport(ioctx IO, format string, report semanticRebuildReport) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	if _, err := fmt.Fprintf(ioctx.Out, "schema\t%s\noperation\t%s\n", report.Schema, report.Operation); err != nil {
		return err
	}
	for _, scope := range report.Scopes {
		if _, err := fmt.Fprintf(
			ioctx.Out,
			"scope\t%s\ngeneration_id\t%s\nprofile_id\t%s\nbundle_id\t%s\nsnapshot_hash\t%s\nretirement_state\t%s\n",
			scope.Scope,
			scope.GenerationID,
			scope.ProfileID,
			scope.BundleID,
			scope.SnapshotHash,
			scope.RetirementState,
		); err != nil {
			return err
		}
		if scope.RetirementWarning != "" {
			if _, err := fmt.Fprintf(ioctx.Out, "retirement_warning\t%s\n", scope.RetirementWarning); err != nil {
				return err
			}
		}
	}
	return nil
}

func freshSemanticGenerationID() (string, error) {
	var entropy [12]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return fmt.Sprintf("g-%d-%s", time.Now().UTC().UnixNano(), hex.EncodeToString(entropy[:])), nil
}
