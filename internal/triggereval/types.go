package triggereval

import "time"

const (
	ToolCodex = "codex"
)

const (
	SkillContext    = "worktrail-context"
	SkillDocPreview = "worktrail-doc-preview"
	SkillSearch     = "worktrail-search"
	SkillState      = "worktrail-state"
	SkillResume     = "worktrail-resume"
	SkillHandoff    = "worktrail-handoff"
	SkillImport     = "worktrail-import"
	SkillDistill    = "worktrail-distill"
	SkillDraft      = "worktrail-draft"
	SkillADR        = "worktrail-adr"
	SkillReview     = "worktrail-review"
	SkillMaintain   = "worktrail-maintain"
)

const (
	BehaviorHit             = "hit"
	BehaviorMiss            = "miss"
	BehaviorTextOnlyFailure = "text_only_failure"
	BehaviorForbiddenHit    = "forbidden_hit"
	BehaviorFalsePositive   = "false_positive"
	BehaviorSkipped         = "skipped"
)

const (
	EvidenceStrong = "strong"
	EvidenceWeak   = "weak"
	EvidenceNone   = "none"
)

const (
	SafetyPass = "pass"
	SafetyFail = "fail"
)

type Case struct {
	ID                   string   `json:"id"`
	Tool                 string   `json:"tool"`
	Skill                string   `json:"skill"`
	Prompt               string   `json:"prompt"`
	ExpectedBehavior     string   `json:"expected_behavior"`
	ExpectedCommands     []string `json:"expected_commands,omitempty"`
	ExpectedArtifacts    []string `json:"expected_artifacts,omitempty"`
	ForbiddenPatterns    []string `json:"forbidden_patterns,omitempty"`
	NegativeCase         bool     `json:"negative_case"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
}

type Evidence struct {
	CaseID                   string           `json:"case_id"`
	Tool                     string           `json:"tool"`
	TranscriptPaths          []string         `json:"transcript_paths,omitempty"`
	AssistantMessages        []string         `json:"assistant_messages,omitempty"`
	CommandsObserved         []string         `json:"commands_observed,omitempty"`
	WorktrailArtifacts       []EvidenceRecord `json:"worktrail_artifacts,omitempty"`
	WorktrailLogs            []EvidenceRecord `json:"worktrail_logs,omitempty"`
	MutatingCommandsObserved []string         `json:"mutating_commands_observed,omitempty"`
	RunnerStdout             string           `json:"runner_stdout,omitempty"`
	RunnerStderr             string           `json:"runner_stderr,omitempty"`
	SkipReason               string           `json:"skip_reason,omitempty"`
}

type EvidenceRecord struct {
	Source  string            `json:"source,omitempty"`
	Path    string            `json:"path,omitempty"`
	Summary string            `json:"summary,omitempty"`
	Fields  map[string]string `json:"fields,omitempty"`
}

type Result struct {
	CaseID            string   `json:"case_id"`
	ExpectedSkill     string   `json:"expected_skill"`
	IntentMatch       bool     `json:"intent_match"`
	Behavior          string   `json:"behavior"`
	EvidenceStrength  string   `json:"evidence_strength"`
	ReasonCodes       []string `json:"reason_codes,omitempty"`
	Safety            string   `json:"safety"`
	NeedsHumanReview  bool     `json:"needs_human_review"`
	SkipReason        string   `json:"skip_reason,omitempty"`
	CommandsObserved  []string `json:"commands_observed,omitempty"`
	ArtifactsObserved []string `json:"artifacts_observed,omitempty"`
}

type Report struct {
	Date         time.Time         `json:"date"`
	Commit       string            `json:"commit,omitempty"`
	Tool         string            `json:"tool"`
	RunnerConfig string            `json:"runner_config,omitempty"`
	CaseCounts   map[string]int    `json:"case_counts"`
	ResultCounts map[string]int    `json:"result_counts"`
	Metrics      Metrics           `json:"metrics"`
	Results      []Result          `json:"results"`
	Evidence     []EvidenceSummary `json:"evidence,omitempty"`
	SkipReasons  []string          `json:"skip_reasons,omitempty"`
	JudgeSummary *JudgeSummary     `json:"judge_summary,omitempty"`
	KnownGaps    []string          `json:"known_gaps,omitempty"`
}

type Metrics struct {
	CommandHitRate        float64            `json:"command_hit_rate"`
	TextOnlyFailureRate   float64            `json:"text_only_failure_rate"`
	ForbiddenHitRate      float64            `json:"forbidden_hit_rate"`
	FalsePositiveRate     float64            `json:"false_positive_rate"`
	PerSkillHitRate       map[string]float64 `json:"per_skill_hit_rate"`
	SkipRate              float64            `json:"skip_rate"`
	TotalCases            int                `json:"total_cases"`
	SkippedCases          int                `json:"skipped_cases"`
	PositiveCases         int                `json:"positive_cases"`
	NegativeCases         int                `json:"negative_cases"`
	RunnablePositiveCases int                `json:"runnable_positive_cases"`
	RunnableNegativeCases int                `json:"runnable_negative_cases"`
}

type EvidenceSummary struct {
	CaseID           string   `json:"case_id"`
	CommandsObserved []string `json:"commands_observed,omitempty"`
	Artifacts        []string `json:"artifacts,omitempty"`
	Logs             []string `json:"logs,omitempty"`
	SkipReason       string   `json:"skip_reason,omitempty"`
}

type JudgeSummary struct {
	Enabled bool `json:"enabled"`
}

func WorktrailSkills() []string {
	return []string{
		SkillContext,
		SkillDocPreview,
		SkillSearch,
		SkillState,
		SkillResume,
		SkillHandoff,
		SkillImport,
		SkillDistill,
		SkillDraft,
		SkillADR,
		SkillReview,
		SkillMaintain,
	}
}
