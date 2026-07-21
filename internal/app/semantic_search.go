package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/nickdu2009/worktrail/internal/index"
	"github.com/nickdu2009/worktrail/internal/semantic/contracts"
)

const semanticSearchResultSchema = "worktrail.search.results.v2"

// SemanticSearcher is the application boundary for semantic recall. Production
// wiring may adapt a semantic implementation to this interface without making
// the command layer depend on that implementation.
type SemanticSearcher interface {
	Search(context.Context, SemanticSearchRequest) (SemanticSearchResponse, error)
}

type SemanticSearchRequest struct {
	Query string
	Scope string
	Type  string
	Topic string
	Tag   string
	Mode  contracts.Mode
	Limit int
}

// SemanticSearchResponse keeps entry-level Results for text/JSON v1 while
// carrying JSON v2-only Details separately so evidence never round-trips
// through index.Result.
type SemanticSearchResponse struct {
	Results         []index.Result
	Details         []SemanticSearchResultDetail
	Policy          string
	Profile         string
	Lanes           []SemanticSearchLane
	Degraded        bool
	DegradedReasons []contracts.ReasonCode
	NextSteps       []string
}

type SemanticSearchResultDetail struct {
	Ranks        SemanticSearchRanks  `json:"ranks"`
	ChunkMatches []SemanticChunkMatch  `json:"chunk_matches,omitempty"`
}

type SemanticSearchRanks struct {
	Lexical  *int `json:"lexical,omitempty"`
	Semantic *int `json:"semantic,omitempty"`
	Final    int  `json:"final"`
}

type SemanticChunkMatch struct {
	ChunkID                    string                     `json:"chunk_id"`
	ChunkKind                  string                     `json:"chunk_kind"`
	StructuralGroupID          string                     `json:"structural_group_id"`
	HeadingBreadcrumb          []string                   `json:"heading_breadcrumb"`
	EvidenceRole               string                     `json:"evidence_role"`
	Lanes                      []string                   `json:"lanes,omitempty"`
	BestChunkRanks             map[string]int             `json:"best_chunk_ranks,omitempty"`
	PrimarySourceRange         SemanticByteRange          `json:"primary_source_range"`
	ContextSourceRange         *SemanticByteRange         `json:"context_source_range,omitempty"`
	StructuralGroupSourceRange *SemanticByteRange         `json:"structural_group_source_range,omitempty"`
}

type SemanticByteRange struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

type SemanticSearchLane struct {
	Scope            string               `json:"scope,omitempty"`
	Name             string               `json:"name"`
	Degraded         bool                 `json:"degraded,omitempty"`
	Reason           contracts.ReasonCode `json:"reason,omitempty"`
	RawHits          int                  `json:"raw_hits,omitempty"`
	FilterRejections int                  `json:"filter_rejections,omitempty"`
	EligibleEntries  int                  `json:"eligible_entries,omitempty"`
	RefillRounds     int                  `json:"refill_rounds,omitempty"`
	HardCap          int                  `json:"hard_cap,omitempty"`
	WindowSaturated  bool                 `json:"window_saturated,omitempty"`
}

type SemanticSearchError struct {
	Code contracts.ReasonCode
}

func (e *SemanticSearchError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("semantic search required but unavailable (%s)", e.Code)
}

type unavailableSemanticSearcher struct{}

func (unavailableSemanticSearcher) Search(_ context.Context, request SemanticSearchRequest) (SemanticSearchResponse, error) {
	reason := contracts.ReasonRuntimeUnavailable
	if request.Mode == contracts.ModeRequired {
		return SemanticSearchResponse{}, &SemanticSearchError{Code: reason}
	}
	return SemanticSearchResponse{
		Degraded:        true,
		DegradedReasons: []contracts.ReasonCode{reason},
		NextSteps:       []string{semanticSearchRebuildStep(request.Scope)},
	}, nil
}

type semanticSearchOptions struct {
	Mode    contracts.Mode
	Enabled bool
	Explain bool
	Args    []string
}

func parseSemanticSearchOptions(args []string) (semanticSearchOptions, error) {
	options := semanticSearchOptions{Mode: contracts.ModeLexical}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--semantic":
			if options.Enabled || i+1 < len(args) && (args[i+1] == "auto" || args[i+1] == "required") {
				return semanticSearchOptions{}, searchSemanticUsageError(args)
			}
			options.Enabled = true
			options.Mode = contracts.ModeAuto
		case strings.HasPrefix(arg, "--semantic="):
			if options.Enabled {
				return semanticSearchOptions{}, searchSemanticUsageError(args)
			}
			mode, err := contracts.ParseMode(strings.TrimPrefix(arg, "--semantic="))
			if err != nil || mode == contracts.ModeLexical {
				return semanticSearchOptions{}, searchSemanticUsageError(args)
			}
			options.Enabled = true
			options.Mode = mode
		case arg == "--explain":
			if options.Explain {
				return semanticSearchOptions{}, searchSemanticUsageError(args)
			}
			options.Explain = true
		case strings.HasPrefix(arg, "--explain="):
			return semanticSearchOptions{}, searchSemanticUsageError(args)
		default:
			options.Args = append(options.Args, arg)
		}
	}
	if options.Explain && !options.Enabled {
		return semanticSearchOptions{}, searchSemanticUsageError(args)
	}
	return options, nil
}

func searchSemanticUsageError(args []string) error {
	return fmt.Errorf("usage: worktrail search [--semantic|--semantic=auto|--semantic=required] [--explain] [--scope project|user|all] [--type <type>] [--topic <topic>] [--tag <tag>] [--format text|json|json-v2] <keyword>: invalid semantic arguments %q", args)
}

func semanticSearchRebuildStep(scope string) string {
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = "all"
	}
	return semanticRepairNextStep(contracts.ReasonProfileStale, scope)
}

func semanticRepairNextStep(reason contracts.ReasonCode, scope string) string {
	switch reason {
	case contracts.ReasonBundleMissing:
		return "worktrail init --semantic"
	default:
		if scope == "" {
			scope = "project"
		}
		return "worktrail semantic rebuild --scope " + scope
	}
}

func semanticSearchDegraded(response SemanticSearchResponse) bool {
	if response.Degraded || len(response.DegradedReasons) > 0 {
		return true
	}
	for _, lane := range response.Lanes {
		if lane.Degraded {
			return true
		}
	}
	return false
}

func normalizeSemanticSearchDiagnostics(response SemanticSearchResponse, scope string) SemanticSearchResponse {
	if !semanticSearchDegraded(response) {
		return response
	}
	response.Degraded = true
	if len(response.DegradedReasons) == 0 {
		for _, lane := range response.Lanes {
			if lane.Degraded && lane.Reason != "" {
				response.DegradedReasons = append(response.DegradedReasons, lane.Reason)
			}
		}
		if len(response.DegradedReasons) == 0 {
			response.DegradedReasons = []contracts.ReasonCode{contracts.ReasonRuntimeUnavailable}
		}
	}
	if len(response.NextSteps) == 0 {
		reason := contracts.ReasonProfileStale
		if len(response.DegradedReasons) > 0 {
			reason = response.DegradedReasons[0]
		}
		response.NextSteps = []string{semanticRepairNextStep(reason, scope)}
	}
	return response
}

func semanticSearchErrorDiagnostics(scope string) SemanticSearchResponse {
	return normalizeSemanticSearchDiagnostics(SemanticSearchResponse{Degraded: true}, scope)
}

func printSemanticSearchDiagnostics(ioctx IO, mode contracts.Mode, response SemanticSearchResponse, explain bool) {
	if !semanticSearchDegraded(response) && !explain {
		return
	}
	if semanticSearchDegraded(response) && len(response.Results) == 0 {
		fmt.Fprintf(ioctx.Err, "semantic search fallback (%s)\n", mode)
		for _, reason := range response.DegradedReasons {
			fmt.Fprintf(ioctx.Err, "reason: %s\n", reason)
		}
		for _, nextStep := range response.NextSteps {
			fmt.Fprintf(ioctx.Err, "next: %s\n", nextStep)
		}
	} else if semanticSearchDegraded(response) {
		for _, reason := range response.DegradedReasons {
			fmt.Fprintf(ioctx.Err, "reason: %s\n", reason)
		}
	}
	if !explain {
		return
	}
	if response.Policy != "" {
		fmt.Fprintf(ioctx.Err, "policy: %s\n", response.Policy)
	}
	if response.Profile != "" {
		fmt.Fprintf(ioctx.Err, "profile: %s\n", response.Profile)
	}
	for _, lane := range response.Lanes {
		if lane.Degraded {
			if lane.Scope != "" {
				fmt.Fprintf(ioctx.Err, "lane: %s/%s (degraded: %s)\n", lane.Scope, lane.Name, lane.Reason)
			} else {
				fmt.Fprintf(ioctx.Err, "lane: %s (degraded: %s)\n", lane.Name, lane.Reason)
			}
			continue
		}
		label := lane.Name
		if lane.Scope != "" {
			label = lane.Scope + "/" + lane.Name
		}
		fmt.Fprintf(ioctx.Err, "lane: %s raw_hits=%d filter_rejections=%d eligible_entries=%d refill_rounds=%d hard_cap=%d window_saturated=%t\n",
			label, lane.RawHits, lane.FilterRejections, lane.EligibleEntries, lane.RefillRounds, lane.HardCap, lane.WindowSaturated)
	}
	for i, result := range response.Results {
		detail := SemanticSearchResultDetail{Ranks: SemanticSearchRanks{Final: i + 1}}
		if i < len(response.Details) {
			detail = response.Details[i]
		}
		fmt.Fprintf(ioctx.Err, "result: %d scope=%s entry_id=%s final=%d", i+1, result.Entry.Scope, result.Entry.ID, detail.Ranks.Final)
		if detail.Ranks.Lexical != nil {
			fmt.Fprintf(ioctx.Err, " lexical=%d", *detail.Ranks.Lexical)
		}
		if detail.Ranks.Semantic != nil {
			fmt.Fprintf(ioctx.Err, " semantic=%d", *detail.Ranks.Semantic)
		}
		fmt.Fprintln(ioctx.Err)
		for _, match := range detail.ChunkMatches {
			fmt.Fprintf(ioctx.Err, "  %s chunk_id=%s kind=%s group=%s primary=[%d,%d)",
				match.EvidenceRole, match.ChunkID, match.ChunkKind, match.StructuralGroupID,
				match.PrimarySourceRange.StartByte, match.PrimarySourceRange.EndByte)
			if len(match.Lanes) > 0 {
				fmt.Fprintf(ioctx.Err, " lanes=%s", strings.Join(match.Lanes, ","))
			}
			fmt.Fprintln(ioctx.Err)
		}
	}
}

type semanticSearchEnvelope struct {
	Schema          string                    `json:"schema"`
	Results         []semanticSearchResultV2  `json:"results"`
	Policy          string                    `json:"policy,omitempty"`
	Profile         string                    `json:"profile,omitempty"`
	Lanes           []SemanticSearchLane      `json:"lanes,omitempty"`
	DegradedReasons []contracts.ReasonCode    `json:"degraded_reasons,omitempty"`
	NextSteps       []string                  `json:"next_steps,omitempty"`
}

type semanticSearchResultV2 struct {
	Entry        index.Entry             `json:"entry"`
	Score        float64                 `json:"score"`
	Ranks        SemanticSearchRanks     `json:"ranks"`
	ChunkMatches []SemanticChunkMatch    `json:"chunk_matches,omitempty"`
}

func semanticSearchJSONV2(results []index.Result, response SemanticSearchResponse) semanticSearchEnvelope {
	out := make([]semanticSearchResultV2, len(results))
	for i, result := range results {
		item := semanticSearchResultV2{
			Entry: result.Entry,
			Score: result.Score,
			Ranks: SemanticSearchRanks{Final: i + 1},
		}
		if i < len(response.Details) {
			item.Ranks = response.Details[i].Ranks
			if item.Ranks.Final == 0 {
				item.Ranks.Final = i + 1
			}
			item.ChunkMatches = response.Details[i].ChunkMatches
		}
		out[i] = item
	}
	return semanticSearchEnvelope{
		Schema:          semanticSearchResultSchema,
		Results:         out,
		Policy:          response.Policy,
		Profile:         response.Profile,
		Lanes:           response.Lanes,
		DegradedReasons: response.DegradedReasons,
		NextSteps:       response.NextSteps,
	}
}
