package model

import "time"

const (
	SchemaKnowledge = "worktrail.knowledge.v1"
	SchemaCandidate = "worktrail.candidate.v1"
	SchemaState     = "worktrail.state.v1"
)

const (
	CandidateTypeTranscriptNotes = "transcript_notes"
	CandidateTypeMigrationSource = "migration_source"
)

type Knowledge struct {
	Schema         string    `json:"schema"`
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	Type           string    `json:"type"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	Stage          string    `json:"stage,omitempty"`
	Topic          string    `json:"topic,omitempty"`
	SourceOfTruth  bool      `json:"source_of_truth,omitempty"`
	Supersedes     []string  `json:"supersedes,omitempty"`
	SupersededBy   []string  `json:"superseded_by,omitempty"`
	SourceSessions []string  `json:"source_sessions,omitempty"`
	SourceFiles    []string  `json:"source_files,omitempty"`
	Tags           []string  `json:"tags,omitempty"`
	Confidence     float64   `json:"confidence,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Candidate struct {
	Schema             string    `json:"schema"`
	ID                 string    `json:"id"`
	Scope              string    `json:"scope"`
	CandidateType      string    `json:"candidate_type"`
	TargetPath         string    `json:"target_path"`
	Title              string    `json:"title"`
	Summary            string    `json:"summary"`
	Operation          string    `json:"operation"`
	Status             string    `json:"status"`
	SourceSessions     []string  `json:"source_sessions,omitempty"`
	SourceCandidateIDs []string  `json:"source_candidate_ids,omitempty"`
	EvidenceLabel      string    `json:"evidence_label,omitempty"`
	Confidence         float64   `json:"confidence,omitempty"`
	RedactionStatus    string    `json:"redaction_status"`
	RetireReason       string    `json:"retire_reason,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
	Tags               []string  `json:"tags,omitempty"`
}

type State struct {
	Schema         string    `json:"schema"`
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	Type           string    `json:"type"`
	Title          string    `json:"title"`
	Status         string    `json:"status"`
	SourceTool     string    `json:"source_tool"`
	SourceSessions []string  `json:"source_sessions,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Tags           []string  `json:"tags,omitempty"`
}

type Event struct {
	Time  time.Time      `json:"time"`
	Event string         `json:"event"`
	ID    string         `json:"id,omitempty"`
	Actor string         `json:"actor,omitempty"`
	Data  map[string]any `json:"data,omitempty"`
}

type TranscriptMeta struct {
	Schema    string    `json:"schema"`
	ID        string    `json:"id"`
	Source    string    `json:"source"`
	Path      string    `json:"path"`
	Scope     string    `json:"scope"`
	Hash      string    `json:"hash,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}
