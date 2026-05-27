package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nickdu2009/worktrail/internal/candidate"
	"github.com/nickdu2009/worktrail/internal/contextpack"
	"github.com/nickdu2009/worktrail/internal/extract"
	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/knowledge"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/transcript"
)

func runIndex(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("index subcommand required")
	}
	flags, _ := splitFlags(args[1:])
	scope := flagValue(flags, "scope", "project")
	switch args[0] {
	case "rebuild":
		scopes := []string{scope}
		if scope == "all" {
			scopes = []string{"user", "project"}
		}
		var manifests []index.Manifest
		for _, s := range scopes {
			manifest, err := index.RebuildEnv(env, s)
			if err != nil {
				return err
			}
			manifests = append(manifests, manifest)
		}
		if flagValue(flags, "format", "text") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(manifests)
		}
		for _, manifest := range manifests {
			fmt.Fprintf(ioctx.Out, "%s\t%d\t%s\n", manifest.Scope, manifest.Entries, manifest.IndexPath)
		}
		return nil
	case "status":
		root, err := env.ScopeRoot(scope)
		if err != nil {
			return err
		}
		status, err := index.Status(root)
		if err != nil {
			return err
		}
		if flagValue(flags, "format", "text") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(status)
		}
		fmt.Fprintf(ioctx.Out, "exists: %v\nscope: %s\nentries: %d\nindex: %s\n", status.Exists, status.Scope, status.Entries, status.IndexPath)
		return nil
	case "diff":
		scopes := []string{scope}
		if scope == "all" {
			scopes = []string{"user", "project"}
		}
		var reports []index.DiffReport
		for _, s := range scopes {
			root, err := env.ScopeRoot(s)
			if err != nil {
				return err
			}
			report, err := index.Diff(root)
			if err != nil {
				return err
			}
			reports = append(reports, report)
		}
		if flagValue(flags, "format", "text") == "json" {
			return json.NewEncoder(ioctx.Out).Encode(reports)
		}
		for i, report := range reports {
			if i > 0 {
				fmt.Fprintln(ioctx.Out)
			}
			renderIndexDiff(ioctx.Out, report)
		}
		return nil
	default:
		return fmt.Errorf("unknown index subcommand %q", args[0])
	}
}

type indexRebuildResult struct {
	Scope     string `json:"scope"`
	Entries   int    `json:"entries,omitempty"`
	IndexPath string `json:"index_path,omitempty"`
	NextStep  string `json:"next_step,omitempty"`
	Error     string `json:"error,omitempty"`
}

func rebuildIndexForScope(env paths.Env, scope string) indexRebuildResult {
	if scope == "" {
		scope = "project"
	}
	nextStep := fmt.Sprintf("worktrail index rebuild --scope %s", scope)
	manifest, err := index.RebuildEnv(env, scope)
	if err != nil {
		return indexRebuildResult{
			Scope:    scope,
			NextStep: nextStep,
			Error:    err.Error(),
		}
	}
	return indexRebuildResult{
		Scope:     manifest.Scope,
		Entries:   manifest.Entries,
		IndexPath: manifest.IndexPath,
	}
}

func printIndexRebuildResult(ioctx IO, result indexRebuildResult) {
	if result.Error != "" {
		fmt.Fprintf(ioctx.Err, "index rebuild failed: %s\n", result.Error)
		fmt.Fprintf(ioctx.Out, "next: %s\n", result.NextStep)
		return
	}
	fmt.Fprintf(ioctx.Out, "index rebuilt\t%s\t%d\t%s\n", result.Scope, result.Entries, result.IndexPath)
}

func printIndexRebuildFailure(ioctx IO, result indexRebuildResult) {
	fmt.Fprintf(ioctx.Err, "index rebuild failed: %s\n", result.Error)
	fmt.Fprintf(ioctx.Err, "next: %s\n", result.NextStep)
}

func runSearch(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	query := joinArgs(positional)
	scope := flagValue(flags, "scope", "project")
	scopes := []string{scope}
	if scope == "all" {
		scopes = []string{"project", "user"}
	}
	var results []index.Result
	for _, s := range scopes {
		entries, err := loadFreshSearchEntries(env, s)
		if err != nil {
			return err
		}
		found := index.SearchEntries(entries, index.Query{
			Scope:   s,
			Type:    flagValue(flags, "type", ""),
			Tag:     flagValue(flags, "tag", ""),
			Content: query,
			Limit:   20,
		})
		results = append(results, found...)
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(results)
	}
	for _, result := range results {
		fmt.Fprintf(ioctx.Out, "%.1f\t%s\t%s\t%s\n", result.Score, result.Entry.Scope, result.Entry.Type, result.Entry.Title)
	}
	return nil
}

func loadFreshSearchEntries(env paths.Env, scope string) ([]index.Entry, error) {
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return nil, err
	}
	db, err := index.Load(root)
	if err != nil {
		if _, rebuildErr := index.RebuildEnv(env, scope); rebuildErr != nil {
			return nil, rebuildErr
		}
		db, err = index.Load(root)
	}
	if err != nil {
		return nil, err
	}
	entries, _, err := index.FilterFresh(root, db)
	if err != nil {
		return nil, err
	}
	return entries, nil
}

func renderIndexDiff(out interface{ Write([]byte) (int, error) }, report index.DiffReport) {
	fmt.Fprintf(out, "scope: %s\nstale: %t\ndeleted: %d\nunindexed: %d\nnew: %d\nchanged: %d\n", report.Scope, report.Stale, report.Summary.Deleted, report.Summary.Unindexed, report.Summary.New, report.Summary.Changed)
	renderIndexDiffItems(out, "deleted", report.Deleted)
	renderIndexDiffItems(out, "unindexed", report.Unindexed)
	renderIndexDiffItems(out, "new", report.New)
	renderIndexDiffItems(out, "changed", report.Changed)
}

func renderIndexDiffItems(out interface{ Write([]byte) (int, error) }, label string, items []index.DiffItem) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(out, "%s:\n", label)
	for _, item := range items {
		fmt.Fprintf(out, "- %s", item.Path)
		if item.Reason != "" {
			fmt.Fprintf(out, " (%s)", item.Reason)
		}
		fmt.Fprintln(out)
	}
}

func runContextPack(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if wantsFlagHelpOrLeadingHelp(args) {
		printContextHelp(ioctx.Out)
		return nil
	}
	flags, positional := splitFlagsWithBooleans(args, map[string]bool{"evidence": true})
	includeLifecycle, err := knowledge.ParseLifecycleList(flagValue(flags, "include-lifecycle", ""))
	if err != nil {
		return err
	}
	pack, err := contextpack.Build(env, contextpack.Options{
		Task:             joinArgs(positional),
		Stage:            flagValue(flags, "stage", ""),
		IncludeLifecycle: includeLifecycle,
		IncludeEvidence:  flagValue(flags, "evidence", "") == "true",
	})
	if err != nil {
		return err
	}
	if flagValue(flags, "format", "markdown") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(pack)
	}
	fmt.Fprint(ioctx.Out, contextpack.RenderMarkdown(pack))
	return nil
}

func printContextHelp(out interface{ Write([]byte) (int, error) }) {
	fmt.Fprintln(out, "usage: worktrail context [--stage <stage>] [--include-lifecycle <list>] [--evidence] [--format markdown|json] <task>")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Builds a read-only context pack from Worktrail knowledge, state, and pending maintenance hints.")
}

func runSync(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	if len(args) == 0 {
		return errors.New("sync source required")
	}
	source := args[0]
	flags, positional := splitFlags(args[1:])
	scope := flagValue(flags, "scope", "project")
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return err
	}
	if source == "all" {
		source = "manual"
	}
	path := firstArg(positional, flagValue(flags, "file", ""))
	if path == "" {
		return errors.New("sync requires a transcript file path")
	}
	meta, err := transcript.Sync(path, root, transcript.SyncOptions{Source: source, Scope: scope, RawMetadataOnly: flagValue(flags, "metadata-only", "") == "true"})
	if err != nil {
		return err
	}
	return json.NewEncoder(ioctx.Out).Encode(meta)
}

func runExtract(_ context.Context, env paths.Env, ioctx IO, args []string) error {
	flags, positional := splitFlags(args)
	scope := flagValue(flags, "scope", "project")
	providerName := flagValue(flags, "provider", "manual")
	source := flagValue(flags, "source", "")
	if source == "codex" || source == "claude" || source == "cursor" {
		providerName = "manual"
	}
	root, err := env.ScopeRoot(scope)
	if err != nil {
		return err
	}
	path := firstArg(positional, flagValue(flags, "file", ""))
	if path == "" && flagValue(flags, "session", "") == "latest" {
		path, err = latestTranscriptPath(root, source)
		if err != nil {
			return err
		}
	}
	if path == "" {
		return errors.New("extract requires a session file")
	}
	records, err := extractSession(env, scope, source, providerName, path)
	if err != nil {
		return err
	}
	if flagValue(flags, "format", "text") == "json" {
		return json.NewEncoder(ioctx.Out).Encode(records)
	}
	for _, rec := range records {
		fmt.Fprintf(ioctx.Out, "%s\t%s\t%s\n", rec.Meta.ID, rec.Meta.Status, rec.Meta.TargetPath)
	}
	return nil
}

func extractSession(env paths.Env, scope, source, providerName, path string) ([]candidate.Record, error) {
	text, err := transcriptText(source, path)
	if err != nil {
		return nil, err
	}
	provider, err := extract.ProviderByName(providerName)
	if err != nil {
		return nil, err
	}
	out, err := provider.Extract(extract.Input{Scope: scope, Text: text, SourceSessions: []string{sessionID(source, path)}}, extract.Schema{Name: "worktrail.candidate.v1"})
	if err != nil {
		return nil, err
	}
	manager := candidate.Manager{Env: env, Actor: "cli:extract"}
	var records []candidate.Record
	for i, cand := range out.Candidates {
		cand.ID = extractionCandidateID(source, path, i, cand.ID)
		target := cand.TargetPath
		if strings.TrimSpace(target) == "" {
			target = defaultCandidateTarget(cand)
		}
		rec, err := manager.Create(candidate.CreateRequest{
			Scope:          scope,
			ID:             cand.ID,
			CandidateType:  cand.CandidateType,
			TargetPath:     target,
			Title:          cand.Title,
			Summary:        cand.Summary,
			Operation:      cand.Operation,
			SourceSessions: cand.SourceSessions,
			Tags:           cand.Tags,
			Body:           candidateBody(cand),
		})
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

func transcriptText(source, path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	ext := strings.ToLower(filepath.Ext(path))
	var tr transcript.Transcript
	switch {
	case ext == ".md":
		tr, err = transcript.ParseMarkdown(source, f)
	case source == "claude":
		tr, err = transcript.ParseClaudeJSONL(f)
	case source == "cursor":
		tr, err = transcript.ParseCursorJSONL(f)
	default:
		tr, err = transcript.ParseCodexJSONL(f)
	}
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, msg := range tr.Messages {
		switch strings.ToLower(msg.Role) {
		case "user", "assistant":
		default:
			continue
		}
		fmt.Fprintf(&b, "- %s: %s\n", msg.Role, msg.Content)
	}
	return b.String(), nil
}

func latestTranscriptPath(root, source string) (string, error) {
	if source == "" || source == "all" {
		source = "codex"
	}
	dir := filepath.Join(root, "raw", source)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	var newest os.DirEntry
	var newestTime int64
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".metadata.json") || strings.HasPrefix(entry.Name(), "observed-") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if newest == nil || info.ModTime().UnixNano() > newestTime {
			newest = entry
			newestTime = info.ModTime().UnixNano()
		}
	}
	if newest == nil {
		return "", fmt.Errorf("no synced %s transcript metadata found", source)
	}
	b, err := os.ReadFile(filepath.Join(dir, newest.Name()))
	if err != nil {
		return "", err
	}
	var meta model.TranscriptMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		return "", err
	}
	if filepath.IsAbs(meta.Path) {
		return meta.Path, nil
	}
	return filepath.Join(root, filepath.FromSlash(meta.Path)), nil
}

func sessionID(source, path string) string {
	if source == "" {
		source = "manual"
	}
	return source + ":" + filepath.Base(path)
}

func extractionCandidateID(source, path string, index int, id string) string {
	prefix := source
	if prefix == "" {
		prefix = "manual"
	}
	base := strings.TrimSpace(id)
	if base == "" {
		base = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	return fmt.Sprintf("%s-%02d-%s", prefix, index+1, base)
}

func defaultCandidateTarget(cand model.Candidate) string {
	typ := cand.CandidateType
	if typ == "" || typ == "manual" {
		typ = "lesson"
	}
	switch typ {
	case "decision", "adr":
		return filepath.ToSlash(filepath.Join("decisions", "ADR-"+cand.ID+".md"))
	case "requirement":
		return filepath.ToSlash(filepath.Join("requirements", cand.ID+".md"))
	case "handoff":
		return filepath.ToSlash(filepath.Join("handoffs", cand.ID+".md"))
	case "prompt":
		return filepath.ToSlash(filepath.Join("prompts", cand.ID+".md"))
	case "rule":
		return filepath.ToSlash(filepath.Join("rules", cand.ID+".md"))
	case model.CandidateTypeTranscriptNotes:
		return filepath.ToSlash(filepath.Join("imports", "transcripts", cand.ID+".md"))
	default:
		return filepath.ToSlash(filepath.Join("lessons", typ+"-"+cand.ID+".md"))
	}
}

func candidateBody(cand model.Candidate) string {
	var b strings.Builder
	if cand.CandidateType == model.CandidateTypeTranscriptNotes {
		fmt.Fprintf(&b, "# Transcript Evidence: %s\n\n", cand.Title)
		if cand.Summary != "" {
			fmt.Fprintf(&b, "## Evidence\n\n%s\n", cand.Summary)
		}
		return b.String()
	}
	fmt.Fprintf(&b, "# Candidate: %s\n\n", cand.Title)
	if cand.Summary != "" {
		fmt.Fprintf(&b, "## Summary\n\n%s\n\n", cand.Summary)
	}
	fmt.Fprintln(&b, "## Proposed Content")
	if cand.Summary != "" {
		fmt.Fprintln(&b, cand.Summary)
	}
	return b.String()
}
