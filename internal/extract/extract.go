package extract

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/util"
)

var ErrProviderUnavailable = errors.New("extraction provider unavailable")

type Provider interface {
	Name() string
	Extract(input Input, schema Schema) (Output, error)
}

type Input struct {
	Scope          string
	Text           string
	SourceSessions []string
	Tags           []string
	Now            time.Time
}

type Schema struct {
	Name string `json:"name"`
}

type Output struct {
	Candidates []model.Candidate `json:"candidates"`
}

type ManualProvider struct{}

func (ManualProvider) Name() string {
	return "manual"
}

func (ManualProvider) Extract(input Input, schema Schema) (Output, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	candidates, err := parseManualCandidates(input, now)
	if err != nil {
		return Output{}, err
	}
	return Output{Candidates: candidates}, nil
}

type CodexProvider struct{}

func (CodexProvider) Name() string {
	return "codex"
}

func (CodexProvider) Extract(input Input, schema Schema) (Output, error) {
	return Output{}, fmt.Errorf("%w: codex extraction is disabled in the local package; no external command was executed", ErrProviderUnavailable)
}

type ClaudeProvider struct{}

func (ClaudeProvider) Name() string {
	return "claude"
}

func (ClaudeProvider) Extract(input Input, schema Schema) (Output, error) {
	return Output{}, fmt.Errorf("%w: claude extraction is disabled in the local package; no external command was executed", ErrProviderUnavailable)
}

func ProviderByName(name string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "manual":
		return ManualProvider{}, nil
	case "codex":
		return CodexProvider{}, nil
	case "claude":
		return ClaudeProvider{}, nil
	default:
		return nil, fmt.Errorf("unknown extraction provider %q", name)
	}
}

func parseManualCandidates(input Input, now time.Time) ([]model.Candidate, error) {
	text := strings.TrimSpace(input.Text)
	if text == "" {
		return nil, nil
	}
	var raw manualEnvelope
	if json.Unmarshal([]byte(text), &raw) == nil && len(raw.Candidates) > 0 {
		return raw.toModel(input, now), nil
	}
	if transcript := transcriptCandidate(input, now, text); transcript != nil {
		return []model.Candidate{*transcript}, nil
	}
	lines := splitManualItems(text)
	candidates := make([]model.Candidate, 0, len(lines))
	for _, line := range lines {
		title, summary := splitTitleSummary(line)
		candidates = append(candidates, newCandidate(input, now, manualCandidate{
			Title:     title,
			Summary:   summary,
			Operation: "create",
			Status:    "pending",
		}))
	}
	return candidates, nil
}

func transcriptCandidate(input Input, now time.Time, text string) *model.Candidate {
	if !looksLikeTranscript(text) {
		return nil
	}
	excerpt := compactText(text, 6000)
	candidate := newCandidate(input, now, manualCandidate{
		CandidateType: model.CandidateTypeTranscriptNotes,
		Title:         "Transcript notes",
		Summary:       "Evidence extracted from an imported AI coding transcript. Distill this into semantic knowledge candidates before review or promotion.\n\n" + excerpt,
		Operation:     "create",
		Status:        "pending",
		Tags:          []string{"transcript", "import", "evidence"},
	})
	candidate.ID = ""
	return &candidate
}

func looksLikeTranscript(text string) bool {
	count := 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.ToLower(line))
		if strings.HasPrefix(line, "- user:") || strings.HasPrefix(line, "- assistant:") {
			count++
		}
	}
	return count > 0
}

func compactText(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) <= limit {
		return text
	}
	return strings.TrimSpace(text[:limit]) + "\n\n[truncated]"
}

type manualCandidate struct {
	ID            string   `json:"id"`
	Scope         string   `json:"scope"`
	CandidateType string   `json:"candidate_type"`
	TargetPath    string   `json:"target_path"`
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	Operation     string   `json:"operation"`
	Status        string   `json:"status"`
	Tags          []string `json:"tags"`
}

type manualEnvelope struct {
	Candidates []manualCandidate `json:"candidates"`
}

func (raw manualEnvelope) toModel(input Input, now time.Time) []model.Candidate {
	candidates := make([]model.Candidate, 0, len(raw.Candidates))
	for _, candidate := range raw.Candidates {
		candidates = append(candidates, newCandidate(input, now, candidate))
	}
	return candidates
}

func newCandidate(input Input, now time.Time, raw manualCandidate) model.Candidate {
	scope := raw.Scope
	if scope == "" {
		scope = input.Scope
	}
	if scope == "" {
		scope = "project"
	}
	title := strings.TrimSpace(raw.Title)
	if title == "" {
		title = "Manual candidate"
	}
	status := raw.Status
	if status == "" {
		status = "pending"
	}
	operation := raw.Operation
	if operation == "" {
		operation = "create"
	}
	candidateType := raw.CandidateType
	if candidateType == "" {
		candidateType = "manual"
	}
	id := raw.ID
	if id == "" {
		id = util.Slug(title)
	}
	tags := raw.Tags
	if len(tags) == 0 {
		tags = input.Tags
	}
	return model.Candidate{
		Schema:          model.SchemaCandidate,
		ID:              id,
		Scope:           scope,
		CandidateType:   candidateType,
		TargetPath:      raw.TargetPath,
		Title:           title,
		Summary:         strings.TrimSpace(raw.Summary),
		Operation:       operation,
		Status:          status,
		SourceSessions:  input.SourceSessions,
		RedactionStatus: "unreviewed",
		CreatedAt:       now,
		UpdatedAt:       now,
		Tags:            tags,
	}
}

func splitManualItems(text string) []string {
	var items []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimPrefix(line, "-")
		line = strings.TrimPrefix(line, "*")
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, line)
		}
	}
	return items
}

func splitTitleSummary(line string) (string, string) {
	for _, sep := range []string{" - ", ": "} {
		if before, after, ok := strings.Cut(line, sep); ok {
			return strings.TrimSpace(before), strings.TrimSpace(after)
		}
	}
	return strings.TrimSpace(line), ""
}
