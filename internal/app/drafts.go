package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/handoff"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	wtstate "github.com/nickdu2009/worktrail/internal/state"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

func runHandoff(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printHandoffHelp(ioctx.Out)
		return nil
	}
	if len(args) == 0 {
		return errors.New("handoff summary is required")
	}
	switch args[0] {
	case "create":
		return runHandoffCreate(ctx, env, ioctx, args[1:])
	case "list":
		return runHandoffList(env, ioctx, args[1:])
	case "show":
		return runHandoffShow(env, ioctx, args[1:])
	case "close":
		return runHandoffClose(env, ioctx, args[1:])
	case "publish":
		return runHandoffPublish(ctx, env, ioctx, args[1:])
	case "doctor":
		return runHandoffDoctor(env, ioctx, args[1:])
	case "repair":
		return runHandoffRepair(env, ioctx, args[1:])
	default:
		return runHandoffCreate(ctx, env, ioctx, args)
	}
}

type parsedHandoffArgs struct {
	Flags      map[string][]string
	Positional []string
}

func runHandoffCreate(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("create", parsed); err != nil {
		return err
	}
	if err := validateHandoffStdinArgs("handoff create", parsed, "scope", "format", "handoff-only", "new-task"); err != nil {
		return err
	}
	scope := parsed.first("scope", "project")
	format := parsed.first("format", "text")
	var request handoff.CreateRequest
	if parsed.boolean("stdin") {
		if ioctx.In == nil {
			return errors.New("--stdin requires JSON input")
		}
		decoder := json.NewDecoder(ioctx.In)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&request); err != nil {
			return fmt.Errorf("decode handoff CreateRequest JSON: %w", err)
		}
	} else {
		request = handoff.CreateRequest{
			Scope:         scope,
			Title:         parsed.first("title", ""),
			Summary:       strings.TrimSpace(strings.Join(parsed.Positional, " ")),
			Complete:      parsed.boolean("complete"),
			ProjectID:     parsed.first("project-id", ""),
			TaskID:        parsed.first("task-id", ""),
			NextSteps:     nextStepsFromFlags(parsed.values("next-step")),
			OpenQuestions: parsed.values("question"),
			Risks:         parsed.values("risk"),
			Tags:          splitCSV(strings.Join(parsed.values("tags"), ",")),
			Body:          parsed.first("body", ""),
			SourceTool:    parsed.first("source-tool", "worktrail"),
			Actor:         "cli:handoff-create",
		}
		validation, err := validationFromFlags(parsed)
		if err != nil {
			return err
		}
		request.Validation = validation
	}
	request.Scope = withDefaultString(request.Scope, scope)
	request.Actor = "cli:handoff-create"
	newTask := parsed.boolean("new-task")
	if newTask && strings.TrimSpace(request.TaskID) != "" {
		return errors.New("--new-task and --task-id are mutually exclusive")
	}
	var sourceState *wtstate.Capsule
	if newTask {
		request.TaskID, err = handoff.NewTaskID()
		if err != nil {
			return err
		}
	} else {
		sourceState, err = latestStateIfAny(env, request.Scope, request.TaskID)
		if err != nil {
			return handoffWriteError(env, request.Scope, err)
		}
	}
	if sourceState != nil {
		stateTaskID := wtstate.TaskID(*sourceState)
		if request.TaskID != "" && request.TaskID != stateTaskID {
			return fmt.Errorf("--task-id %q does not match active state task %q", request.TaskID, stateTaskID)
		}
		request.TaskID = stateTaskID
		if request.Title == "" {
			request.Title = sourceState.State.Title
		}
	}
	if strings.TrimSpace(request.TaskID) == "" {
		return errors.New("no active state supplies task_id; use --task-id or --new-task")
	}
	if request.Title == "" {
		request.Title = "Handoff"
	}
	handoffOnly := parsed.boolean("handoff-only")
	if sourceState != nil {
		relPath := filepath.ToSlash(filepath.Join("state", wtstate.DirActive, sourceState.State.ID+".md"))
		if !handoffOnly {
			relPath = filepath.ToSlash(filepath.Join("state", wtstate.DirArchived, sourceState.State.ID+".md"))
		}
		request.SourceState = &model.Ref{Scope: request.Scope, Kind: "state", ID: sourceState.State.ID, RelPath: relPath}
	}
	record, err := createAndMaybeCloseState(ctx, env, request, sourceState, !handoffOnly)
	if err != nil {
		return handoffWriteError(env, request.Scope, err)
	}
	return printHandoffRecord(ioctx, record, format)
}

func handoffWriteError(env paths.Env, scope string, err error) error {
	root, rootErr := env.ScopeRoot(scope)
	if rootErr != nil {
		return err
	}
	return fmt.Errorf("handoff write failed for target %s; ensure the sandbox allows writes to %s: %w", filepath.Join(root, "handoffs"), strings.Join(requiredWorktrailWriteDirs(root), ", "), err)
}

func requiredWorktrailWriteDirs(root string) []string {
	return []string{
		filepath.Join(root, "handoffs", "local"),
		filepath.Join(root, "handoffs", "team"),
		filepath.Join(root, "logs"),
		filepath.Join(root, "ops"),
	}
}

func printHandoffHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail handoff <create|list|show|close|publish|doctor|repair> [options]")
	fmt.Fprintln(out, "       worktrail handoff [create options] <summary>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "create: [--scope project|user] [--task-id <id>|--new-task] [--title <title>] [--project-id <id>]")
	fmt.Fprintln(out, "        [--next-step <action>]... [--complete] [--question <text>]... [--risk <text>]...")
	fmt.Fprintln(out, "        [--tags <csv>] [--body <markdown>] [--source-tool <tool>] [--handoff-only]")
	fmt.Fprintln(out, "        [--validation-status <status>] [--validation-command <command>]")
	fmt.Fprintln(out, "        [--validation-note <text>] [--validation-exit-code <code>] [--stdin] [--format text|json]")
	fmt.Fprintln(out, "list:   [--scope project|user] [--visibility local|team] [--task-id <id>] [--format text|json]")
	fmt.Fprintln(out, "show:   <id>|--id <id> [--scope project|user] [--visibility local|team] [--format markdown|json]")
	fmt.Fprintln(out, "close:  <local-id>|--id <id> [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "publish:<local-id>|--id <id> [--supersedes <team-id,...>] [--allow-dirty --confirm] [--format text|json]")
	fmt.Fprintln(out, "doctor: [--scope project|user] [--format text|json]")
	fmt.Fprintln(out, "repair: [--scope project|user] [--apply --confirm] [--format text|json]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Repair is dry-run by default. Team records are immutable.")
}

func printHandoffRecord(ioctx IO, rec handoff.Record, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(rec)
	}
	path := rec.RelPath
	if rec.Meta.Scope == "project" {
		path = filepath.ToSlash(filepath.Join(".worktrail", filepath.FromSlash(path)))
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\n", rec.Meta.ID, path)
	return nil
}

func latestStateIfAny(env paths.Env, scope, taskID string) (*wtstate.Capsule, error) {
	states, err := wtstate.List(env, wtstate.ListOptions{Scope: scope, Directory: wtstate.DirActive})
	if err != nil {
		return nil, err
	}
	taskID = strings.TrimSpace(taskID)
	byTask := map[string][]wtstate.Capsule{}
	for _, cap := range states {
		tool := strings.TrimSpace(cap.State.SourceTool)
		if tool != "" && tool != "worktrail" {
			continue
		}
		candidateTaskID := wtstate.TaskID(cap)
		if candidateTaskID == "" || (taskID != "" && candidateTaskID != taskID) {
			continue
		}
		byTask[candidateTaskID] = append(byTask[candidateTaskID], cap)
	}
	if taskID != "" {
		if candidates := byTask[taskID]; len(candidates) > 0 {
			cap := candidates[0]
			return &cap, nil
		}
		return nil, nil
	}
	if len(byTask) == 0 {
		return nil, nil
	}
	if len(byTask) > 1 {
		var ids []string
		for id := range byTask {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return nil, fmt.Errorf("multiple active tasks are available (%s); choose --task-id or --new-task", strings.Join(ids, ", "))
	}
	for _, candidates := range byTask {
		cap := candidates[0]
		return &cap, nil
	}
	return nil, nil
}

func runHandoffList(env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("list", parsed); err != nil {
		return err
	}
	result, err := handoff.ListWithDiagnostics(env, handoff.ListOptions{
		Scope:      parsed.first("scope", "project"),
		Visibility: parsed.first("visibility", ""),
		TaskID:     parsed.first("task-id", ""),
	})
	if err != nil {
		return err
	}
	if parsed.first("format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(result)
	}
	for _, record := range result.Records {
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\t%s\t%s\n", record.Meta.ID, record.Meta.Visibility, record.Meta.LifecycleStatus, record.Meta.TaskID, record.Meta.Summary)
	}
	for _, diagnostic := range result.Diagnostics {
		fmt.Fprintf(ioctx.Err, "diagnostic\t%s\t%s\t%s\n", diagnostic.Code, diagnostic.Path, diagnostic.Message)
	}
	return nil
}

func runHandoffShow(env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("show", parsed); err != nil {
		return err
	}
	id := firstArg(parsed.Positional, parsed.first("id", ""))
	record, err := handoff.Show(env, handoff.ShowRequest{
		Scope:      parsed.first("scope", "project"),
		ID:         id,
		Visibility: parsed.first("visibility", ""),
	})
	if err != nil {
		return err
	}
	if parsed.first("format", "markdown") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(record)
	}
	fmt.Fprint(ioctx.Out, record.Body)
	return nil
}

func runHandoffClose(env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("close", parsed); err != nil {
		return err
	}
	record, err := handoff.Close(env, handoff.CloseRequest{
		Scope: parsed.first("scope", "project"),
		ID:    firstArg(parsed.Positional, parsed.first("id", "")),
		Actor: "cli:handoff-close",
	})
	if err != nil {
		return err
	}
	return printHandoffRecord(ioctx, record, parsed.first("format", "text"))
}

func runHandoffPublish(ctx context.Context, env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("publish", parsed); err != nil {
		return err
	}
	record, err := handoff.Publish(ctx, env, handoff.PublishRequest{
		Scope:      parsed.first("scope", "project"),
		ID:         firstArg(parsed.Positional, parsed.first("id", "")),
		AllowDirty: parsed.boolean("allow-dirty"),
		Confirm:    parsed.boolean("confirm"),
		Supersedes: splitCSV(strings.Join(parsed.values("supersedes"), ",")),
		Actor:      "cli:handoff-publish",
	})
	if err != nil {
		return err
	}
	return printHandoffRecord(ioctx, record, parsed.first("format", "text"))
}

func runHandoffDoctor(env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("doctor", parsed); err != nil {
		return err
	}
	report, err := handoff.Doctor(env, handoff.DoctorRequest{Scope: parsed.first("scope", "project")})
	if err != nil {
		return err
	}
	if parsed.first("format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", diagnostic.Code, diagnostic.Path, diagnostic.Message)
	}
	return nil
}

func runHandoffRepair(env paths.Env, ioctx IO, args []string) error {
	parsed, err := parseHandoffArgs(args)
	if err != nil {
		return err
	}
	if err := validateHandoffFlags("repair", parsed); err != nil {
		return err
	}
	report, err := handoff.Repair(env, handoff.RepairRequest{
		Scope:   parsed.first("scope", "project"),
		Apply:   parsed.boolean("apply"),
		Confirm: parsed.boolean("confirm"),
		Actor:   "cli:handoff-repair",
	})
	if err != nil {
		return err
	}
	if parsed.first("format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(report)
	}
	fmt.Fprintf(ioctx.Out, "applied\t%t\n", report.Applied)
	for _, action := range report.Actions {
		fmt.Fprintf(ioctx.Out, "action\t%s\n", action)
	}
	for _, diagnostic := range report.Diagnostics {
		fmt.Fprintf(ioctx.Out, "diagnostic\t%s\t%s\t%s\n", diagnostic.Code, diagnostic.Path, diagnostic.Message)
	}
	return nil
}

func parseHandoffArgs(args []string) (parsedHandoffArgs, error) {
	result := parsedHandoffArgs{Flags: map[string][]string{}}
	boolean := map[string]bool{
		"stdin": true, "complete": true, "handoff-only": true, "new-task": true,
		"allow-dirty": true, "confirm": true, "apply": true,
	}
	for index := 0; index < len(args); index++ {
		arg := args[index]
		if !strings.HasPrefix(arg, "--") {
			result.Positional = append(result.Positional, arg)
			continue
		}
		keyValue := strings.TrimPrefix(arg, "--")
		if keyValue == "" {
			return result, errors.New("invalid empty flag")
		}
		if strings.Contains(keyValue, "=") {
			parts := strings.SplitN(keyValue, "=", 2)
			if !handoffKnownFlags[parts[0]] {
				return result, fmt.Errorf("unknown handoff flag --%s", parts[0])
			}
			if len(result.Flags[parts[0]]) > 0 && !handoffRepeatableFlags[parts[0]] {
				return result, fmt.Errorf("--%s may not be repeated", parts[0])
			}
			result.Flags[parts[0]] = append(result.Flags[parts[0]], parts[1])
			continue
		}
		if !handoffKnownFlags[keyValue] {
			return result, fmt.Errorf("unknown handoff flag --%s", keyValue)
		}
		if len(result.Flags[keyValue]) > 0 && !handoffRepeatableFlags[keyValue] {
			return result, fmt.Errorf("--%s may not be repeated", keyValue)
		}
		if boolean[keyValue] {
			result.Flags[keyValue] = append(result.Flags[keyValue], "true")
			continue
		}
		if index+1 >= len(args) || strings.HasPrefix(args[index+1], "--") {
			return result, fmt.Errorf("--%s requires a value", keyValue)
		}
		index++
		result.Flags[keyValue] = append(result.Flags[keyValue], args[index])
	}
	return result, nil
}

var handoffKnownFlags = map[string]bool{
	"stdin": true, "complete": true, "handoff-only": true, "new-task": true,
	"allow-dirty": true, "confirm": true, "apply": true,
	"scope": true, "format": true, "title": true, "project-id": true, "task-id": true,
	"next-step": true, "question": true, "risk": true, "tags": true, "body": true,
	"source-tool": true, "validation-status": true, "validation-command": true,
	"validation-note": true, "validation-exit-code": true, "visibility": true,
	"id": true, "supersedes": true,
	// State close --to handoff reuses this parser before dispatching the
	// handoff-specific subset.
	"to": true, "reason": true, "session": true, "type": true, "active": true,
	"directory": true,
}

var handoffRepeatableFlags = map[string]bool{
	"next-step": true,
	"question":  true,
	"risk":      true,
	"tags":      true,
}

func validateHandoffStdinArgs(command string, parsed parsedHandoffArgs, controlFlags ...string) error {
	if !parsed.boolean("stdin") {
		return nil
	}
	if len(parsed.Positional) != 0 {
		return fmt.Errorf("%s --stdin cannot be combined with a positional summary", command)
	}
	allowed := map[string]bool{"stdin": true}
	for _, flag := range controlFlags {
		allowed[flag] = true
	}
	for flag := range parsed.Flags {
		if !allowed[flag] {
			return fmt.Errorf("%s --stdin cannot be combined with payload flag --%s", command, flag)
		}
	}
	return nil
}

func validateHandoffFlags(command string, parsed parsedHandoffArgs) error {
	allowed := map[string]map[string]bool{
		"create": {
			"stdin": true, "complete": true, "handoff-only": true, "new-task": true,
			"scope": true, "format": true, "title": true, "project-id": true, "task-id": true,
			"next-step": true, "question": true, "risk": true, "tags": true, "body": true,
			"source-tool": true, "validation-status": true, "validation-command": true,
			"validation-note": true, "validation-exit-code": true,
		},
		"list":    {"scope": true, "visibility": true, "task-id": true, "format": true},
		"show":    {"scope": true, "visibility": true, "id": true, "format": true},
		"close":   {"scope": true, "id": true, "format": true},
		"publish": {"scope": true, "id": true, "format": true, "allow-dirty": true, "confirm": true, "supersedes": true},
		"doctor":  {"scope": true, "format": true},
		"repair":  {"scope": true, "format": true, "apply": true, "confirm": true},
	}
	commandAllowed, ok := allowed[command]
	if !ok {
		return fmt.Errorf("unknown handoff subcommand %q", command)
	}
	for flag := range parsed.Flags {
		if !commandAllowed[flag] {
			return fmt.Errorf("--%s is not valid for handoff %s", flag, command)
		}
	}
	switch command {
	case "list", "doctor", "repair":
		if len(parsed.Positional) != 0 {
			return fmt.Errorf("handoff %s does not accept positional arguments", command)
		}
	case "show", "close", "publish":
		if len(parsed.Positional) > 1 {
			return fmt.Errorf("handoff %s accepts exactly one handoff id", command)
		}
		if len(parsed.Positional) == 1 && parsed.first("id", "") != "" && parsed.Positional[0] != parsed.first("id", "") {
			return fmt.Errorf("positional handoff id and --id must match")
		}
	}
	return nil
}

func (p parsedHandoffArgs) values(key string) []string {
	return append([]string(nil), p.Flags[key]...)
}

func (p parsedHandoffArgs) first(key, fallback string) string {
	values := p.Flags[key]
	if len(values) == 0 {
		return fallback
	}
	return values[len(values)-1]
}

func (p parsedHandoffArgs) boolean(key string) bool {
	value := p.first(key, "")
	parsed, _ := strconv.ParseBool(value)
	return parsed
}

func nextStepsFromFlags(values []string) []model.NextStep {
	steps := make([]model.NextStep, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			steps = append(steps, model.NextStep{Action: value})
		}
	}
	return steps
}

func validationFromFlags(parsed parsedHandoffArgs) ([]model.ValidationEvidence, error) {
	status := parsed.first("validation-status", "")
	command := parsed.first("validation-command", "")
	summary := parsed.first("validation-note", "")
	exitCodeText := parsed.first("validation-exit-code", "")
	if status == "" && command == "" && summary == "" && exitCodeText == "" {
		return nil, nil
	}
	if status == "" {
		status = model.ValidationStatusUnknown
	}
	var exitCode *int
	if exitCodeText != "" {
		value, err := strconv.Atoi(exitCodeText)
		if err != nil {
			return nil, fmt.Errorf("--validation-exit-code: %w", err)
		}
		exitCode = &value
	}
	return []model.ValidationEvidence{{Command: command, Status: status, ExitCode: exitCode, Summary: summary}}, nil
}

func withDefaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func filepathToSlash(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}

func runADR(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 || wantsFlagHelpOrLeadingHelp(args) {
		printADRHelp(ioctx.Out)
		if len(args) == 0 {
			return errors.New("adr subcommand required")
		}
		return nil
	}
	if args[0] != "create" {
		return fmt.Errorf("unknown adr subcommand %q", args[0])
	}
	flags, positional := splitFlagsWithBooleans(args[1:], map[string]bool{"stdin": true})
	format := flagValue(flags, "format", "text")
	fail := func(err error) error {
		return failCLICommand(ioctx, format, "worktrail adr create", err)
	}
	fromFile := strings.TrimSpace(flagValue(flags, "from-file", ""))
	useStdin := flagValue(flags, "stdin", "") == "true"
	if fromFile != "" && useStdin {
		return fail(errors.New("--from-file and --stdin are mutually exclusive"))
	}

	scope := flagValue(flags, "scope", "project")
	explicitTitle := strings.TrimSpace(joinArgs(positional))
	if flagTitle := strings.TrimSpace(flagValue(flags, "title", "")); flagTitle != "" {
		if explicitTitle != "" && explicitTitle != flagTitle {
			return fail(errors.New("positional title and --title must match"))
		}
		explicitTitle = flagTitle
	}
	explicitID := strings.TrimSpace(flagValue(flags, "adr-id", ""))
	explicitStatus := strings.TrimSpace(flagValue(flags, "decision-status", ""))

	rawBody, supplied, err := readADRBody(ioctx.In, fromFile, useStdin)
	if err != nil {
		return fail(err)
	}
	now := time.Now().UTC()
	var doc adrDocument
	if supplied {
		doc, err = parseADRDocument(rawBody)
		if err != nil {
			return fail(err)
		}
		if explicitTitle != "" && explicitTitle != doc.Title {
			return fail(fmt.Errorf("title %q does not match ADR heading title %q", explicitTitle, doc.Title))
		}
		if explicitID != "" && explicitID != doc.ID {
			return fail(fmt.Errorf("--adr-id %q does not match ADR heading id %q", explicitID, doc.ID))
		}
		if explicitStatus != "" {
			status, statusErr := canonicalADRStatus(explicitStatus)
			if statusErr != nil {
				return fail(statusErr)
			}
			if status != doc.Status {
				return fail(fmt.Errorf("--decision-status %q does not match ADR status %q", status, doc.Status))
			}
		}
	} else {
		title := explicitTitle
		if title == "" {
			title = "Architecture Decision"
		}
		status := "Proposed"
		if explicitStatus != "" {
			status, err = canonicalADRStatus(explicitStatus)
			if err != nil {
				return fail(err)
			}
		}
		if status != "Proposed" {
			return fail(errors.New("non-Proposed ADRs require --from-file or --stdin with complete content"))
		}
		id := explicitID
		if id == "" {
			id = "ADR-" + now.Format("20060102") + "-" + util.Slug(title)
		}
		if err := validateADRID(id); err != nil {
			return fail(err)
		}
		doc = adrDocument{
			ID:     id,
			Title:  title,
			Status: status,
			Body:   renderADRTemplate(id, title, status, now),
			Meta:   map[string]any{},
		}
	}

	targetPath := adrTargetPath(doc.ID, doc.Title)
	supersedesPaths, err := validateADRSupersedes(env, scope, doc, splitCSV(flagValue(flags, "supersedes", "")))
	if err != nil {
		return fail(err)
	}
	meta, err := renderADRFrontmatter(doc, scope, supersedesPaths, now)
	if err != nil {
		return fail(err)
	}
	rendered, err := store.RenderMarkdown(meta, doc.Body)
	if err != nil {
		return fail(err)
	}
	rec, err := (candidate.Manager{Env: env, Actor: "cli:adr"}).Create(candidate.CreateRequest{
		Scope:         scope,
		ID:            flagValue(flags, "id", ""),
		CandidateType: "decision",
		TargetPath:    targetPath,
		Title:         doc.Title,
		Summary:       fmt.Sprintf("%s ADR candidate.", doc.Status),
		Operation:     candidate.OperationReplace,
		Body:          string(rendered),
	})
	if err != nil {
		return fail(err)
	}
	if isJSONFormat(format) {
		return printCandidate(ioctx, rec, "json")
	}
	if err := printCandidate(ioctx, rec, "text"); err != nil {
		return err
	}
	fmt.Fprintf(ioctx.Out, "next: worktrail review plan --format json --scope %s\n", rec.Meta.Scope)
	return nil
}

var (
	numericADRID = regexp.MustCompile(`^ADR-[0-9]{4}$`)
	dateADRID    = regexp.MustCompile(`^ADR-[0-9]{8}-[a-z0-9]+(?:-[a-z0-9]+)*$`)
)

type adrDocument struct {
	ID                  string
	Title               string
	Status              string
	Body                string
	Meta                map[string]any
	Supersedes          []string
	ProposesSuperseding []string
}

func readADRBody(in io.Reader, fromFile string, useStdin bool) (string, bool, error) {
	if fromFile != "" {
		if fromFile == "true" {
			return "", false, errors.New("--from-file requires a path")
		}
		data, err := os.ReadFile(fromFile)
		return string(data), true, err
	}
	if !useStdin {
		return "", false, nil
	}
	if in == nil {
		return "", false, errors.New("--stdin requires input")
	}
	data, err := io.ReadAll(in)
	if err != nil {
		return "", false, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return "", false, errors.New("--stdin requires non-empty input")
	}
	return string(data), true, nil
}

func parseADRDocument(raw string) (adrDocument, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return adrDocument{}, errors.New("ADR body is empty")
	}
	meta := map[string]any{}
	body := raw
	if strings.HasPrefix(raw, store.Marker+"\n") {
		parsed, err := store.ParseMarkdown([]byte(raw))
		if err != nil {
			return adrDocument{}, err
		}
		meta = parsed.Meta
		body = parsed.Body
	}
	id, title, err := parseADRHeading(body)
	if err != nil {
		return adrDocument{}, err
	}
	status, err := canonicalADRStatus(adrMetadataValue(body, "Status"))
	if err != nil {
		return adrDocument{}, err
	}
	for _, section := range []string{"Context", "Decision", "Consequences"} {
		if !adrSectionHasContent(adrSection(body, section)) {
			return adrDocument{}, fmt.Errorf("ADR section %q must be present and non-empty", section)
		}
	}
	links := adrSection(body, "Links")
	supersedes := adrLinkValues(links, "Supersedes")
	proposes := adrLinkValues(links, "Proposes to supersede")
	switch status {
	case "Proposed":
		if len(supersedes) > 0 {
			return adrDocument{}, errors.New("Proposed ADRs must use \"Proposes to supersede\", not \"Supersedes\"")
		}
	case "Accepted":
		if len(proposes) > 0 {
			return adrDocument{}, errors.New("Accepted ADRs must use \"Supersedes\", not \"Proposes to supersede\"")
		}
	}
	return adrDocument{
		ID:                  id,
		Title:               title,
		Status:              status,
		Body:                strings.TrimSpace(body),
		Meta:                meta,
		Supersedes:          supersedes,
		ProposesSuperseding: proposes,
	}, nil
}

func parseADRHeading(body string) (string, string, error) {
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "# ") {
			return "", "", errors.New("ADR must start with '# <ADR-ID>: <title>'")
		}
		parts := strings.SplitN(strings.TrimSpace(strings.TrimPrefix(line, "# ")), ":", 2)
		if len(parts) != 2 {
			return "", "", errors.New("ADR heading must be '# <ADR-ID>: <title>'")
		}
		id := strings.TrimSpace(parts[0])
		title := strings.TrimSpace(parts[1])
		if err := validateADRID(id); err != nil {
			return "", "", err
		}
		if title == "" {
			return "", "", errors.New("ADR heading title is required")
		}
		return id, title, nil
	}
	return "", "", errors.New("ADR heading is required")
}

func validateADRID(id string) error {
	if numericADRID.MatchString(id) || dateADRID.MatchString(id) {
		return nil
	}
	return fmt.Errorf("invalid ADR id %q; expected ADR-NNNN or ADR-YYYYMMDD-<slug>", id)
}

func canonicalADRStatus(status string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "proposed":
		return "Proposed", nil
	case "accepted":
		return "Accepted", nil
	case "deprecated":
		return "Deprecated", nil
	case "superseded":
		return "Superseded", nil
	default:
		return "", fmt.Errorf("invalid ADR status %q; expected Proposed, Accepted, Deprecated, or Superseded", status)
	}
}

func adrMetadataValue(body, key string) string {
	prefix := "- " + strings.ToLower(strings.TrimSpace(key)) + ":"
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(line), prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
	}
	return ""
}

func adrSection(body, name string) string {
	lines := strings.Split(body, "\n")
	target := "## " + strings.TrimSpace(name)
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == target {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := len(lines)
	for i := start; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "## ") {
			end = i
			break
		}
	}
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

func adrSectionHasContent(section string) bool {
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func adrLinkValues(section, key string) []string {
	prefix := "- " + strings.ToLower(strings.TrimSpace(key)) + ":"
	var values []string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(strings.ToLower(line), prefix) {
			continue
		}
		for _, value := range splitCSV(strings.TrimSpace(line[len(prefix):])) {
			if value != "" {
				values = append(values, value)
			}
		}
	}
	return uniqueStrings(values)
}

func renderADRTemplate(id, title, status string, now time.Time) string {
	return fmt.Sprintf(`# %s: %s

- Status: %s
- Date: %s

## Context

## Decision Drivers

## Considered Alternatives

## Decision

## Consequences

### Positive

### Negative

## Revisit Conditions

## Links
`, id, title, status, now.Format("2006-01-02"))
}

func adrTargetPath(id, title string) string {
	if dateADRID.MatchString(id) {
		return filepath.ToSlash(filepath.Join("decisions", id+".md"))
	}
	return filepath.ToSlash(filepath.Join("decisions", id+"-"+util.Slug(title)+".md"))
}

func validateADRSupersedes(env paths.Env, scope string, doc adrDocument, requested []string) ([]string, error) {
	frontmatterPaths, err := adrMetaStringList(doc.Meta["supersedes"])
	if err != nil {
		return nil, err
	}
	requested, err = normalizeDecisionPaths(requested)
	if err != nil {
		return nil, err
	}
	frontmatterPaths, err = normalizeDecisionPaths(frontmatterPaths)
	if err != nil {
		return nil, err
	}
	if len(requested) > 0 && len(frontmatterPaths) > 0 && !equalStringSets(requested, frontmatterPaths) {
		return nil, errors.New("--supersedes does not match Worktrail frontmatter supersedes")
	}
	pathsToUse := requested
	if len(pathsToUse) == 0 {
		pathsToUse = frontmatterPaths
	}
	if doc.Status != "Accepted" {
		if len(pathsToUse) > 0 {
			return nil, errors.New("--supersedes is only valid for Accepted ADRs")
		}
		return nil, nil
	}
	if len(doc.Supersedes) == 0 && len(pathsToUse) > 0 {
		return nil, errors.New("Worktrail supersedes paths require matching ADR Links > Supersedes entries")
	}
	if len(doc.Supersedes) > 0 && len(pathsToUse) == 0 {
		return nil, errors.New("Accepted ADR Links > Supersedes entries require --supersedes paths")
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return nil, err
	}
	var pathIDs []string
	for _, target := range pathsToUse {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(target)))
		if err != nil {
			return nil, fmt.Errorf("read superseded decision %q: %w", target, err)
		}
		body := string(data)
		if parsed, parseErr := store.ParseMarkdown(data); parseErr == nil {
			body = parsed.Body
		}
		id, _, err := parseADRHeading(body)
		if err != nil {
			return nil, fmt.Errorf("superseded decision %q: %w", target, err)
		}
		pathIDs = append(pathIDs, id)
	}
	if !equalStringSets(doc.Supersedes, pathIDs) {
		return nil, fmt.Errorf("ADR Links > Supersedes %v do not match decision paths %v", doc.Supersedes, pathsToUse)
	}
	return pathsToUse, nil
}

func normalizeDecisionPaths(values []string) ([]string, error) {
	var normalized []string
	for _, value := range values {
		value = filepath.ToSlash(filepath.Clean(strings.TrimSpace(value)))
		if value == "." || filepath.IsAbs(value) || value == ".." || strings.HasPrefix(value, "../") || !strings.HasPrefix(value, "decisions/") || filepath.Ext(value) != ".md" {
			return nil, fmt.Errorf("supersedes path %q must be a relative decisions/*.md path", value)
		}
		normalized = append(normalized, value)
	}
	return uniqueStrings(normalized), nil
}

func adrMetaStringList(value any) ([]string, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case string:
		return splitCSV(typed), nil
	case []string:
		return typed, nil
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, errors.New("Worktrail frontmatter supersedes must contain strings")
			}
			values = append(values, text)
		}
		return values, nil
	default:
		return nil, errors.New("Worktrail frontmatter supersedes must be a string or string array")
	}
}

func renderADRFrontmatter(doc adrDocument, scope string, supersedes []string, now time.Time) (map[string]any, error) {
	if _, ok := doc.Meta["superseded_by"]; ok {
		return nil, errors.New("Worktrail frontmatter superseded_by is derived and must not be provided")
	}
	meta := make(map[string]any, len(doc.Meta)+10)
	for key, value := range doc.Meta {
		meta[key] = value
	}
	required := map[string]string{
		"schema":    model.SchemaKnowledge,
		"id":        doc.ID,
		"scope":     scope,
		"type":      "decision",
		"title":     doc.Title,
		"status":    strings.ToLower(doc.Status),
		"lifecycle": "current",
		"stage":     "decision",
	}
	for key, value := range required {
		if existing, ok := meta[key]; ok {
			text, ok := existing.(string)
			if !ok {
				return nil, fmt.Errorf("Worktrail frontmatter %s must be a string", key)
			}
			got := strings.TrimSpace(text)
			want := value
			if key == "type" {
				got = model.CanonicalSemanticCandidateType(got)
			}
			if key == "status" || key == "lifecycle" || key == "stage" {
				got = strings.ToLower(got)
			}
			if got != want {
				return nil, fmt.Errorf("Worktrail frontmatter %s %q conflicts with ADR value %q", key, text, value)
			}
		}
		meta[key] = value
	}
	if len(supersedes) > 0 {
		meta["supersedes"] = supersedes
	} else {
		delete(meta, "supersedes")
	}
	if _, ok := meta["created_at"]; !ok {
		meta["created_at"] = now
	}
	meta["updated_at"] = now
	return meta, nil
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var result []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		result = append(result, value)
	}
	return result
}

func equalStringSets(left, right []string) bool {
	left = uniqueStrings(left)
	right = uniqueStrings(right)
	if len(left) != len(right) {
		return false
	}
	wanted := make(map[string]bool, len(left))
	for _, value := range left {
		wanted[value] = true
	}
	for _, value := range right {
		if !wanted[value] {
			return false
		}
	}
	return true
}

func printADRHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail adr create [<title>|--title <title>] [options]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Creates a pending decision candidate. It never edits formal knowledge or promotes candidates.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "options:")
	fmt.Fprintln(out, "  --id <candidate-id>           optional stable candidate id")
	fmt.Fprintln(out, "  --scope project|user          default project")
	fmt.Fprintln(out, "  --adr-id <ADR-ID>             ADR-NNNN or ADR-YYYYMMDD-<slug>")
	fmt.Fprintln(out, "  --decision-status <status>    Proposed, Accepted, Deprecated, or Superseded")
	fmt.Fprintln(out, "  --from-file <path>            read a complete ADR from a file")
	fmt.Fprintln(out, "  --stdin                       read a complete ADR from stdin")
	fmt.Fprintln(out, "  --supersedes <path,...>       formal decisions/*.md paths; Accepted ADRs only")
	fmt.Fprintln(out, "  --format text|json            output format")
}
