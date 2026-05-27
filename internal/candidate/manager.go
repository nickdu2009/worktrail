package candidate

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	wlog "github.com/nickdu2009/worktrail/internal/log"
	"github.com/nickdu2009/worktrail/internal/model"
	"github.com/nickdu2009/worktrail/internal/paths"
	"github.com/nickdu2009/worktrail/internal/redact"
	"github.com/nickdu2009/worktrail/internal/store"
	"github.com/nickdu2009/worktrail/internal/util"
)

const (
	StatusPending   = "pending"
	StatusPromoted  = "promoted"
	StatusMerged    = "merged"
	StatusDiscarded = "discarded"
	StatusRetired   = "retired"
	StatusArchived  = "archived"

	OperationReplace = "replace"
	OperationMerge   = "merge"
)

var (
	ErrBlocked              = errors.New("candidate content contains blocked sensitive material")
	ErrNotFound             = errors.New("candidate not found")
	ErrTranscriptNotesApply = errors.New("transcript notes are evidence and must be distilled before promote or merge")
	ErrMigrationSourceApply = errors.New("migration sources are evidence and must be distilled before promote or merge")
	ErrRestoreUnsupported   = errors.New("restore only supports promoted replace candidates with missing targets")
	ErrRetireUnsupported    = errors.New("retire only supports promoted or merged candidates with missing targets")
	ErrRetireReasonRequired = errors.New("retire reason is required")
	ErrEvidenceUnsupported  = errors.New("evidence lifecycle only supports transcript_notes, migration_source, and KDD split-source lessons")
)

type Manager struct {
	Env   paths.Env
	Actor string
	Now   func() time.Time
}

type CreateRequest struct {
	ID                 string
	Scope              string
	CandidateType      string
	TargetPath         string
	Title              string
	Summary            string
	Operation          string
	SourceSessions     []string
	SourceCandidateIDs []string
	EvidenceLabel      string
	Confidence         float64
	Tags               []string
	Body               string
}

type Record struct {
	Meta model.Candidate
	Body string
	Path string
}

type ApplyResult struct {
	Candidate  model.Candidate
	TargetPath string
	BackupPath string
	Status     string
}

func (m Manager) Create(req CreateRequest) (Record, error) {
	now := m.now()
	scope := normalizeScope(req.Scope)
	root, err := m.Env.ScopeRoot(scope)
	if err != nil {
		return Record{}, err
	}
	target, err := resolveTarget(root, req.TargetPath)
	if err != nil {
		return Record{}, err
	}
	scan := redact.Scan(req.Body)
	if scan.Status == redact.StatusBlocked {
		return Record{}, blockedError(scan)
	}

	id := req.ID
	if id == "" {
		id = fmt.Sprintf("%s-%s", util.Slug(req.Title), now.UTC().Format("20060102T150405Z"))
	}
	id = util.Slug(id)
	if id == "" {
		return Record{}, errors.New("candidate id is required")
	}
	operation := req.Operation
	if operation == "" {
		operation = OperationReplace
	}
	candidateType := req.CandidateType
	if candidateType == "" {
		candidateType = "knowledge"
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		title = id
	}

	meta := model.Candidate{
		Schema:             model.SchemaCandidate,
		ID:                 id,
		Scope:              scope,
		CandidateType:      candidateType,
		TargetPath:         targetPathForMeta(root, target),
		Title:              title,
		Summary:            strings.TrimSpace(req.Summary),
		Operation:          operation,
		Status:             StatusPending,
		SourceSessions:     append([]string(nil), req.SourceSessions...),
		SourceCandidateIDs: append([]string(nil), req.SourceCandidateIDs...),
		EvidenceLabel:      strings.TrimSpace(req.EvidenceLabel),
		Confidence:         req.Confidence,
		RedactionStatus:    string(scan.Status),
		CreatedAt:          now,
		UpdatedAt:          now,
		Tags:               append([]string(nil), req.Tags...),
	}
	rec := Record{Meta: meta, Body: scan.Text}
	rec.Path, err = m.candidatePath(scope, id)
	if err != nil {
		return Record{}, err
	}
	if _, err := os.Stat(rec.Path); err == nil {
		return Record{}, fmt.Errorf("candidate %q already exists", id)
	}
	if err := writeRecord(rec); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(root, "candidate.create", id, m.actor(), map[string]any{
		"target_path": meta.TargetPath,
		"redaction":   meta.RedactionStatus,
	}); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (m Manager) List(scope string) ([]Record, error) {
	scope = normalizeScope(scope)
	dir, err := m.candidateDir(scope)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var records []Record
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		rec, err := readRecord(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].Meta.CreatedAt.Equal(records[j].Meta.CreatedAt) {
			return records[i].Meta.ID < records[j].Meta.ID
		}
		return records[i].Meta.CreatedAt.Before(records[j].Meta.CreatedAt)
	})
	return records, nil
}

func (m Manager) Show(scope, id string) (Record, error) {
	scope = normalizeScope(scope)
	path, err := m.candidatePath(scope, util.Slug(id))
	if err != nil {
		return Record{}, err
	}
	rec, err := readRecord(path)
	if errors.Is(err, os.ErrNotExist) {
		return Record{}, ErrNotFound
	}
	return rec, err
}

func (m Manager) Diff(scope, id string) (string, error) {
	rec, err := m.Show(scope, id)
	if err != nil {
		return "", err
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return "", err
	}
	target, err := resolveTarget(root, rec.Meta.TargetPath)
	if err != nil {
		return "", err
	}
	var existing string
	if b, err := os.ReadFile(target); err == nil {
		existing = string(b)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	return unifiedTextDiff(rec.Meta.TargetPath, existing, rec.Body), nil
}

func (m Manager) Promote(scope, id string) (ApplyResult, error) {
	return m.apply(scope, id, OperationReplace)
}

func (m Manager) Merge(scope, id string) (ApplyResult, error) {
	return m.apply(scope, id, OperationMerge)
}

func (m Manager) Restore(scope, id string) (ApplyResult, error) {
	rec, err := m.Show(scope, id)
	if err != nil {
		return ApplyResult{}, err
	}
	if rec.Meta.Status != StatusPromoted || rec.Meta.Operation != OperationReplace {
		return ApplyResult{}, ErrRestoreUnsupported
	}
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return ApplyResult{}, ErrTranscriptNotesApply
	}
	if rec.Meta.CandidateType == model.CandidateTypeMigrationSource {
		return ApplyResult{}, ErrMigrationSourceApply
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return ApplyResult{}, err
	}
	target, err := resolveTarget(root, rec.Meta.TargetPath)
	if err != nil {
		return ApplyResult{}, err
	}
	if _, err := os.Stat(target); err == nil {
		return ApplyResult{}, fmt.Errorf("target %q already exists; restore only repairs missing targets", rec.Meta.TargetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return ApplyResult{}, err
	}
	scan := redact.Scan(rec.Body)
	if scan.Status == redact.StatusBlocked {
		_ = wlog.Append(root, "candidate.blocked", rec.Meta.ID, m.actor(), map[string]any{
			"target_path": rec.Meta.TargetPath,
			"operation":   "restore",
		})
		return ApplyResult{}, blockedError(scan)
	}
	if err := util.AtomicWrite(target, []byte(ensureTrailingNewline(scan.Text)), 0o644); err != nil {
		return ApplyResult{}, err
	}
	rec.Body = scan.Text
	rec.Meta.RedactionStatus = string(scan.Status)
	rec.Meta.UpdatedAt = m.now()
	if err := writeRecord(rec); err != nil {
		return ApplyResult{}, err
	}
	if err := wlog.Append(root, "candidate.restore", rec.Meta.ID, m.actor(), map[string]any{
		"target_path": rec.Meta.TargetPath,
		"redaction":   rec.Meta.RedactionStatus,
	}); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		Candidate:  rec.Meta,
		TargetPath: target,
		Status:     "restored",
	}, nil
}

func (m Manager) Retire(scope, id, reason string) (Record, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return Record{}, ErrRetireReasonRequired
	}
	rec, err := m.Show(scope, id)
	if err != nil {
		return Record{}, err
	}
	if rec.Meta.Status != StatusPromoted && rec.Meta.Status != StatusMerged {
		return Record{}, ErrRetireUnsupported
	}
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return Record{}, ErrTranscriptNotesApply
	}
	if rec.Meta.CandidateType == model.CandidateTypeMigrationSource {
		return Record{}, ErrMigrationSourceApply
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return Record{}, err
	}
	target, err := resolveTarget(root, rec.Meta.TargetPath)
	if err != nil {
		return Record{}, err
	}
	if _, err := os.Stat(target); err == nil {
		return Record{}, fmt.Errorf("target %q still exists; retire only acknowledges missing targets", rec.Meta.TargetPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return Record{}, err
	}

	previousStatus := rec.Meta.Status
	rec.Meta.Status = StatusRetired
	rec.Meta.RetireReason = reason
	rec.Meta.UpdatedAt = m.now()
	if err := writeRecord(rec); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(root, "candidate.retire", rec.Meta.ID, m.actor(), map[string]any{
		"target_path":     rec.Meta.TargetPath,
		"previous_status": previousStatus,
		"reason":          reason,
	}); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (m Manager) Discard(scope, id string) (Record, error) {
	rec, err := m.Show(scope, id)
	if err != nil {
		return Record{}, err
	}
	if terminalStatus(rec.Meta.Status) {
		return Record{}, fmt.Errorf("candidate %q is already %s", rec.Meta.ID, rec.Meta.Status)
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return Record{}, err
	}
	rec.Meta.Status = StatusDiscarded
	rec.Meta.UpdatedAt = m.now()
	if err := writeRecord(rec); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(root, "candidate.discard", rec.Meta.ID, m.actor(), map[string]any{
		"target_path": rec.Meta.TargetPath,
	}); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (m Manager) ArchiveEvidence(scope, id, reason string) (Record, error) {
	rec, err := m.Show(scope, id)
	if err != nil {
		return Record{}, err
	}
	if !isLifecycleEvidence(rec) {
		return Record{}, ErrEvidenceUnsupported
	}
	if rec.Meta.Status == StatusArchived {
		return Record{}, fmt.Errorf("candidate %q is already %s", rec.Meta.ID, rec.Meta.Status)
	}
	if rec.Meta.Status == StatusDiscarded || rec.Meta.Status == StatusRetired {
		return Record{}, fmt.Errorf("candidate %q is already %s", rec.Meta.ID, rec.Meta.Status)
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return Record{}, err
	}
	previousStatus := rec.Meta.Status
	rec.Meta.Status = StatusArchived
	rec.Meta.UpdatedAt = m.now()
	if err := writeRecord(rec); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(root, "candidate.evidence_archive", rec.Meta.ID, m.actor(), map[string]any{
		"target_path":     rec.Meta.TargetPath,
		"previous_status": previousStatus,
		"reason":          strings.TrimSpace(reason),
	}); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (m Manager) DiscardEvidence(scope, id, reason string) (Record, error) {
	rec, err := m.Show(scope, id)
	if err != nil {
		return Record{}, err
	}
	if !isLifecycleEvidence(rec) {
		return Record{}, ErrEvidenceUnsupported
	}
	if rec.Meta.Status == StatusDiscarded {
		return Record{}, fmt.Errorf("candidate %q is already %s", rec.Meta.ID, rec.Meta.Status)
	}
	if rec.Meta.Status == StatusArchived || rec.Meta.Status == StatusRetired {
		return Record{}, fmt.Errorf("candidate %q is already %s", rec.Meta.ID, rec.Meta.Status)
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return Record{}, err
	}
	previousStatus := rec.Meta.Status
	rec.Meta.Status = StatusDiscarded
	rec.Meta.UpdatedAt = m.now()
	if err := writeRecord(rec); err != nil {
		return Record{}, err
	}
	if err := wlog.Append(root, "candidate.evidence_discard", rec.Meta.ID, m.actor(), map[string]any{
		"target_path":     rec.Meta.TargetPath,
		"previous_status": previousStatus,
		"reason":          strings.TrimSpace(reason),
	}); err != nil {
		return Record{}, err
	}
	return rec, nil
}

func (m Manager) apply(scope, id, op string) (ApplyResult, error) {
	rec, err := m.Show(scope, id)
	if err != nil {
		return ApplyResult{}, err
	}
	if terminalStatus(rec.Meta.Status) {
		return ApplyResult{}, fmt.Errorf("candidate %q is already %s", rec.Meta.ID, rec.Meta.Status)
	}
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return ApplyResult{}, ErrTranscriptNotesApply
	}
	if rec.Meta.CandidateType == model.CandidateTypeMigrationSource {
		return ApplyResult{}, ErrMigrationSourceApply
	}
	root, err := m.Env.ScopeRoot(rec.Meta.Scope)
	if err != nil {
		return ApplyResult{}, err
	}
	target, err := resolveTarget(root, rec.Meta.TargetPath)
	if err != nil {
		return ApplyResult{}, err
	}

	scan := redact.Scan(rec.Body)
	if scan.Status == redact.StatusBlocked {
		_ = wlog.Append(root, "candidate.blocked", rec.Meta.ID, m.actor(), map[string]any{
			"target_path": rec.Meta.TargetPath,
			"operation":   op,
		})
		return ApplyResult{}, blockedError(scan)
	}

	var existing []byte
	if b, err := os.ReadFile(target); err == nil {
		existing = b
	} else if !errors.Is(err, os.ErrNotExist) {
		return ApplyResult{}, err
	}
	backup, err := m.backup(target, existing)
	if err != nil {
		return ApplyResult{}, err
	}

	body := scan.Text
	if op == OperationMerge {
		body = mergeText(string(existing), body)
	}
	if err := util.AtomicWrite(target, []byte(ensureTrailingNewline(body)), 0o644); err != nil {
		return ApplyResult{}, err
	}

	now := m.now()
	rec.Body = scan.Text
	rec.Meta.RedactionStatus = string(scan.Status)
	rec.Meta.UpdatedAt = now
	if op == OperationMerge {
		rec.Meta.Status = StatusMerged
	} else {
		rec.Meta.Status = StatusPromoted
	}
	if err := writeRecord(rec); err != nil {
		return ApplyResult{}, err
	}
	event := "candidate.promote"
	if op == OperationMerge {
		event = "candidate.merge"
	}
	if err := wlog.Append(root, event, rec.Meta.ID, m.actor(), map[string]any{
		"target_path": rec.Meta.TargetPath,
		"backup_path": backup,
		"redaction":   rec.Meta.RedactionStatus,
	}); err != nil {
		return ApplyResult{}, err
	}
	return ApplyResult{
		Candidate:  rec.Meta,
		TargetPath: target,
		BackupPath: backup,
		Status:     rec.Meta.Status,
	}, nil
}

func (m Manager) backup(target string, data []byte) (string, error) {
	if data == nil {
		return "", nil
	}
	stamp := m.now().UTC().Format("20060102T150405Z")
	backup := target + ".bak-" + stamp
	for i := 1; ; i++ {
		if _, err := os.Stat(backup); errors.Is(err, os.ErrNotExist) {
			return backup, util.AtomicWrite(backup, data, 0o644)
		} else if err != nil {
			return "", err
		}
		backup = fmt.Sprintf("%s.bak-%s-%d", target, stamp, i)
	}
}

func (m Manager) candidateDir(scope string) (string, error) {
	root, err := m.Env.ScopeRoot(scope)
	if err != nil {
		return "", err
	}
	return paths.SafeJoin(root, "candidates", scope)
}

func (m Manager) candidatePath(scope, id string) (string, error) {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return "", errors.New("candidate id is required")
	}
	dir, err := m.candidateDir(scope)
	if err != nil {
		return "", err
	}
	return paths.SafeJoin(dir, id+".md")
}

func (m Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m Manager) actor() string {
	if strings.TrimSpace(m.Actor) == "" {
		return "candidate-manager"
	}
	return m.Actor
}

func readRecord(path string) (Record, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Record{}, err
	}
	doc, err := store.ParseMarkdown(b)
	if err != nil {
		return Record{}, err
	}
	raw, err := json.Marshal(doc.Meta)
	if err != nil {
		return Record{}, err
	}
	var meta model.Candidate
	if err := json.Unmarshal(raw, &meta); err != nil {
		return Record{}, err
	}
	return Record{Meta: meta, Body: doc.Body, Path: path}, nil
}

func writeRecord(rec Record) error {
	b, err := store.RenderMarkdown(rec.Meta, rec.Body)
	if err != nil {
		return err
	}
	return util.AtomicWrite(rec.Path, b, 0o644)
}

func resolveTarget(root, target string) (string, error) {
	target = model.NormalizeTargetPath(target)
	if target == "" {
		return "", errors.New("target path is required")
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	var targetAbs string
	if filepath.IsAbs(target) {
		targetAbs, err = filepath.Abs(target)
	} else {
		targetAbs, err = paths.SafeJoin(rootAbs, target)
	}
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("target path escapes worktrail root")
	}
	return targetAbs, nil
}

func targetPathForMeta(root, target string) string {
	if rel, err := filepath.Rel(root, target); err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.ToSlash(rel)
	}
	return target
}

func normalizeScope(scope string) string {
	if scope == "" {
		return "project"
	}
	return scope
}

func terminalStatus(status string) bool {
	return status == StatusPromoted || status == StatusMerged || status == StatusDiscarded || status == StatusRetired || status == StatusArchived
}

func isLifecycleEvidence(rec Record) bool {
	if rec.Meta.CandidateType == model.CandidateTypeTranscriptNotes {
		return true
	}
	if rec.Meta.CandidateType == model.CandidateTypeMigrationSource {
		return true
	}
	if rec.Meta.CandidateType != "lesson" {
		return false
	}
	if rec.Meta.TargetPath == "lessons/kdd-active-knowledge-log.md" {
		return true
	}
	for _, tag := range rec.Meta.Tags {
		if tag == "split-source" {
			return true
		}
	}
	return strings.Contains(rec.Meta.Summary, "Do not promote directly") || strings.Contains(rec.Body, "Do not promote directly")
}

func blockedError(result redact.Result) error {
	labels := make([]string, 0, len(result.Findings))
	for _, finding := range result.Findings {
		labels = append(labels, finding.Label)
	}
	sort.Strings(labels)
	return fmt.Errorf("%w: %s", ErrBlocked, strings.Join(labels, ", "))
}

func mergeText(existing, candidate string) string {
	existing = strings.TrimSpace(existing)
	candidate = strings.TrimSpace(candidate)
	if existing == "" {
		return candidate
	}
	if candidate == "" || strings.Contains(existing, candidate) {
		return existing
	}
	return existing + "\n\n" + candidate
}

func ensureTrailingNewline(s string) string {
	return strings.TrimRight(s, "\r\n") + "\n"
}

func unifiedTextDiff(name, oldText, newText string) string {
	oldLines := splitLines(oldText)
	newLines := splitLines(newText)
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "--- %s\n", name)
	fmt.Fprintf(&buf, "+++ candidate\n")
	for _, line := range lineDiff(oldLines, newLines) {
		buf.WriteString(line)
		buf.WriteByte('\n')
	}
	return buf.String()
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func lineDiff(a, b []string) []string {
	m, n := len(a), len(b)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var out []string
	for i, j := 0, 0; i < m || j < n; {
		switch {
		case i < m && j < n && a[i] == b[j]:
			out = append(out, " "+a[i])
			i++
			j++
		case j < n && (i == m || lcs[i][j+1] >= lcs[i+1][j]):
			out = append(out, "+"+b[j])
			j++
		default:
			out = append(out, "-"+a[i])
			i++
		}
	}
	return out
}
