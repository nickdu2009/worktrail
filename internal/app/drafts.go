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

func runHandoff(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printHandoffHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{"handoff-only": true})
	scope := flagValue(flags, "scope", "project")
	title := flagValue(flags, "title", "Handoff")
	summary := strings.TrimSpace(joinArgs(positional))
	if summary == "" {
		return errors.New("handoff summary is required")
	}
	handoffOnly := flagValue(flags, "handoff-only", "") == "true"
	sourceState, err := latestStateIfAny(env, scope)
	if err != nil {
		return handoffWriteError(env, scope, err)
	}
	if title == "Handoff" && sourceState != nil {
		title = sourceState.State.Title
	}
	latestHandoff, err := latestHandoffIfAny(env, scope)
	if err != nil {
		return handoffWriteError(env, scope, err)
	}
	sourceStatePath := ""
	if sourceState != nil && !handoffOnly {
		sourceStatePath = projectedArchivedStatePath(env, scope, sourceState.State.ID)
	}
	rec, err := createHandoffRecord(env, createHandoffRecordOptions{
		Scope:           scope,
		Title:           title,
		Summary:         summary,
		SourceState:     sourceState,
		SourceStatePath: sourceStatePath,
		Previous:        latestHandoff,
		Tags:            []string{"handoff", "manual"},
		Actor:           "cli:handoff",
	})
	if err != nil {
		return handoffWriteError(env, scope, err)
	}
	if sourceState != nil && !handoffOnly {
		if _, err := wtstate.Close(env, wtstate.CloseOptions{
			Scope:   scope,
			ID:      sourceState.State.ID,
			Summary: summary,
			Handoff: true,
			Actor:   "cli:handoff",
		}); err != nil {
			return err
		}
	}
	return printHandoffRecord(ioctx, rec, flagValue(flags, "format", "text"))
}

type createHandoffRecordOptions struct {
	Scope           string
	Title           string
	Summary         string
	SourceState     *wtstate.Capsule
	SourceStatePath string
	Previous        *handoff.Record
	Tags            []string
	Actor           string
}

func createHandoffRecord(env paths.Env, opts createHandoffRecordOptions) (handoff.Record, error) {
	summary := strings.TrimSpace(opts.Summary)
	if summary == "" {
		return handoff.Record{}, errors.New("handoff summary is required")
	}
	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = "Handoff"
	}
	return handoff.Create(env, handoff.CreateOptions{
		Scope:             opts.Scope,
		Title:             title,
		Summary:           summary,
		TaskID:            taskIDForHandoff(opts.SourceState, opts.Previous, title),
		SourceStateID:     sourceStateID(opts.SourceState),
		PreviousHandoffID: previousHandoffID(opts.Previous),
		Tags:              opts.Tags,
		Body:              renderHandoffRecordBody(title, summary, opts.SourceState, opts.SourceStatePath, opts.Previous),
		Actor:             opts.Actor,
	})
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
		filepath.Join(root, "handoffs"),
		filepath.Join(root, "logs"),
	}
}

func printHandoffHelp(out io.Writer) {
	fmt.Fprintln(out, "usage: worktrail handoff [--scope project|user] [--title <title>] [--format text|json] [--handoff-only] <summary>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Writes a durable handoff record under `.worktrail/handoffs/`. By default, an active explicit state is closed after the handoff is created; use `--handoff-only` to keep the active state open.")
}

func printHandoffRecord(ioctx IO, rec handoff.Record, format string) error {
	if format == "json" {
		return json.NewEncoder(ioctx.Out).Encode(rec)
	}
	fmt.Fprintf(ioctx.Out, "%s\t%s\n", rec.Meta.ID, rec.Path)
	return nil
}

func latestStateIfAny(env paths.Env, scope string) (*wtstate.Capsule, error) {
	cap, err := wtstate.LatestExplicit(env, scope)
	if err == nil {
		return &cap, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

func latestHandoffIfAny(env paths.Env, scope string) (*handoff.Record, error) {
	rec, err := handoff.Latest(env, scope)
	if err == nil {
		return &rec, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return nil, err
}

func taskIDForHandoff(sourceState *wtstate.Capsule, previous *handoff.Record, title string) string {
	if sourceState != nil {
		if taskID := wtstate.TaskID(*sourceState); taskID != "" {
			return taskID
		}
	}
	if previous != nil && strings.TrimSpace(previous.Meta.TaskID) != "" {
		return previous.Meta.TaskID
	}
	return "task-" + util.Slug(title)
}

func sourceStateID(sourceState *wtstate.Capsule) string {
	if sourceState == nil {
		return ""
	}
	return sourceState.State.ID
}

func previousHandoffID(previous *handoff.Record) string {
	if previous == nil {
		return ""
	}
	return previous.Meta.ID
}

func renderHandoffRecordBody(title, summary string, sourceState *wtstate.Capsule, sourceStatePath string, previous *handoff.Record) string {
	var b strings.Builder
	b.WriteString("# Handoff: ")
	b.WriteString(title)
	b.WriteString("\n\n## Summary\n\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n\n")
	if sourceState != nil {
		path := sourceStatePath
		if strings.TrimSpace(path) == "" {
			path = sourceState.Path
		}
		b.WriteString("## Source State\n\n")
		fmt.Fprintf(&b, "- State ID: %s\n- Task ID: %s\n- Path: `%s`\n\n", sourceState.State.ID, wtstate.TaskID(*sourceState), filepathToSlash(path))
		b.WriteString("## State Snapshot\n\n")
		b.WriteString(strings.TrimSpace(sourceState.Body))
		b.WriteString("\n\n")
	}
	if previous != nil {
		b.WriteString("## Previous Handoff\n\n")
		fmt.Fprintf(&b, "- Handoff ID: %s\n- Path: `%s`\n\n", previous.Meta.ID, filepathToSlash(previous.Path))
	}
	b.WriteString("## Next Step\n\n")
	if sourceState != nil {
		b.WriteString("Read the linked state and continue from the latest validated point.\n")
	} else {
		b.WriteString("Read the linked knowledge and update the active state before continuing.\n")
	}
	return b.String()
}

func projectedArchivedStatePath(env paths.Env, scope, id string) string {
	root, err := env.ScopeRoot(scope)
	if err != nil || strings.TrimSpace(id) == "" {
		return ""
	}
	return filepath.Join(root, "state", wtstate.DirArchived, id+".md")
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
