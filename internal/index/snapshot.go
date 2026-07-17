package index

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nickdu2009/worktrail/internal/store"
)

const (
	snapshotSchema         = "worktrail.index.snapshot.v1"
	semanticSnapshotSchema = "worktrail.index.semantic-snapshot.v1"
)

// SnapshotSelector selects entries from a freshly scanned source. The entries
// supplied to it are copies and may be reordered without affecting the scan.
type SnapshotSelector func([]Entry) []Entry

// Snapshot is a deterministic representation of the current source state.
type Snapshot struct {
	Schema        string             `json:"schema"`
	Scope         string             `json:"scope"`
	PolicyVersion string             `json:"policy_version"`
	Entries       []EntryFingerprint `json:"entries"`
	SnapshotHash  string             `json:"snapshot_hash"`
}

// SourceSnapshot contains a selected snapshot and the source entries used to
// create it.
type SourceSnapshot struct {
	Snapshot             Snapshot       `json:"snapshot"`
	SemanticSnapshotHash string         `json:"semantic_snapshot_hash"`
	Entries              []Entry        `json:"entries"`
	Records              []SourceRecord `json:"records"`
}

// SourceRecord retains the selected index entry with its original Markdown
// body, excluding frontmatter but preserving body whitespace.
type SourceRecord struct {
	Entry Entry  `json:"entry"`
	Body  string `json:"body"`
}

// EntryFingerprint contains the source fields that define an entry's snapshot
// identity. It intentionally excludes mutable timestamps such as updated_at.
type EntryFingerprint struct {
	Path          string   `json:"path"`
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Status        string   `json:"status"`
	Lifecycle     string   `json:"lifecycle"`
	Topic         string   `json:"topic"`
	SourceOfTruth bool     `json:"source_of_truth"`
	Supersedes    []string `json:"supersedes"`
	SupersededBy  []string `json:"superseded_by"`
	Tags          []string `json:"tags"`
	Active        bool     `json:"active"`
	ContentHash   string   `json:"content_hash"`
	Scope         string   `json:"scope"`
}

type snapshotCanonical struct {
	Schema        string             `json:"schema"`
	Scope         string             `json:"scope"`
	PolicyVersion string             `json:"policy_version"`
	Entries       []EntryFingerprint `json:"entries"`
}

type semanticSnapshotCanonical struct {
	Schema        string                    `json:"schema"`
	Scope         string                    `json:"scope"`
	PolicyVersion string                    `json:"policy_version"`
	Records       []semanticRecordCanonical `json:"records"`
}

type semanticRecordCanonical struct {
	Path          string   `json:"path"`
	ID            string   `json:"id"`
	Type          string   `json:"type"`
	Status        string   `json:"status"`
	Lifecycle     string   `json:"lifecycle"`
	Topic         string   `json:"topic"`
	SourceOfTruth bool     `json:"source_of_truth"`
	Supersedes    []string `json:"supersedes"`
	SupersededBy  []string `json:"superseded_by"`
	Tags          []string `json:"tags"`
	Active        bool     `json:"active"`
	Scope         string   `json:"scope"`
	BodyHash      string   `json:"body_hash"`
}

// FreshSnapshot scans the current source and returns its selected deterministic
// snapshot. It does not read or update a persisted index.
func FreshSnapshot(root, scope, policyVersion string, selector SnapshotSelector) (Snapshot, error) {
	_, selected, err := freshSelectedEntries(root, scope, policyVersion, selector)
	if err != nil {
		return Snapshot{}, err
	}
	return newSnapshot(scope, policyVersion, selected)
}

// FreshSourceSnapshot scans the current source once and returns the selected
// entries together with their deterministic snapshot. It does not read or
// update a persisted index.
func FreshSourceSnapshot(root, scope, policyVersion string, selector SnapshotSelector) (SourceSnapshot, error) {
	root, selected, err := freshSelectedEntries(root, scope, policyVersion, selector)
	if err != nil {
		return SourceSnapshot{}, err
	}
	records, err := sourceRecords(root, selected)
	if err != nil {
		return SourceSnapshot{}, err
	}
	semanticSnapshotHash, err := newSemanticSnapshotHash(scope, policyVersion, records)
	if err != nil {
		return SourceSnapshot{}, err
	}
	snapshot, err := newSnapshot(scope, policyVersion, selected)
	if err != nil {
		return SourceSnapshot{}, err
	}
	return SourceSnapshot{
		Snapshot:             snapshot,
		SemanticSnapshotHash: semanticSnapshotHash,
		Entries:              cloneSnapshotEntries(selected),
		Records:              records,
	}, nil
}

func freshSelectedEntries(root, scope, policyVersion string, selector SnapshotSelector) (string, []Entry, error) {
	if selector == nil {
		return "", nil, errors.New("snapshot selector is required")
	}
	if scope != "user" && scope != "project" {
		return "", nil, errors.New("scope must be user or project")
	}
	if strings.TrimSpace(root) == "" {
		return "", nil, errors.New("snapshot root is required")
	}
	if strings.TrimSpace(policyVersion) == "" {
		return "", nil, errors.New("policy version is required")
	}

	root, err := filepath.Abs(root)
	if err != nil {
		return "", nil, err
	}
	entries, err := scan(root, scope)
	if err != nil {
		return "", nil, err
	}
	return root, cloneSnapshotEntries(selector(cloneSnapshotEntries(entries))), nil
}

func sourceRecords(root string, entries []Entry) ([]SourceRecord, error) {
	entryCopies := cloneSnapshotEntries(entries)
	records := make([]SourceRecord, len(entryCopies))
	for i, entry := range entryCopies {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			return nil, fmt.Errorf("read source body %q: %w", entry.Path, err)
		}
		// Match lexical scan/buildEntry: frontmatter is preferred, but init seed
		// and overview docs may omit it. Semantic rebuild still indexes the raw
		// Markdown body so a fresh worktrail root remains rebuildable.
		body := string(data)
		if doc, err := store.ParseMarkdown(data); err == nil {
			body = doc.Body
		}
		records[i] = SourceRecord{
			Entry: entry,
			Body:  body,
		}
	}
	return records, nil
}

func newSemanticSnapshotHash(scope, policyVersion string, records []SourceRecord) (string, error) {
	canonicalRecords := make([]semanticRecordCanonical, len(records))
	for i, record := range records {
		canonicalRecords[i] = semanticRecord(record)
	}
	sort.Slice(canonicalRecords, func(i, j int) bool {
		if canonicalRecords[i].Path != canonicalRecords[j].Path {
			return canonicalRecords[i].Path < canonicalRecords[j].Path
		}
		if canonicalRecords[i].Scope != canonicalRecords[j].Scope {
			return canonicalRecords[i].Scope < canonicalRecords[j].Scope
		}
		return canonicalRecords[i].ID < canonicalRecords[j].ID
	})
	data, err := json.Marshal(semanticSnapshotCanonical{
		Schema:        semanticSnapshotSchema,
		Scope:         scope,
		PolicyVersion: policyVersion,
		Records:       canonicalRecords,
	})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func semanticRecord(record SourceRecord) semanticRecordCanonical {
	entry := record.Entry
	return semanticRecordCanonical{
		Path:          normalizeSnapshotPath(entry.Path),
		ID:            entry.ID,
		Type:          entry.Type,
		Status:        entry.Status,
		Lifecycle:     entry.Lifecycle,
		Topic:         entry.Topic,
		SourceOfTruth: entry.SourceOfTruth,
		Supersedes:    normalizedSnapshotStrings(entry.Supersedes),
		SupersededBy:  normalizedSnapshotStrings(entry.SupersededBy),
		Tags:          normalizedSnapshotStrings(entry.Tags),
		Active:        entry.Active,
		Scope:         entry.Scope,
		BodyHash:      contentHash(record.Body),
	}
}

func newSnapshot(scope, policyVersion string, entries []Entry) (Snapshot, error) {
	fingerprints := make([]EntryFingerprint, len(entries))
	for i, entry := range entries {
		fingerprints[i] = fingerprintEntry(entry)
	}
	sort.Slice(fingerprints, func(i, j int) bool {
		if fingerprints[i].Path != fingerprints[j].Path {
			return fingerprints[i].Path < fingerprints[j].Path
		}
		if fingerprints[i].Scope != fingerprints[j].Scope {
			return fingerprints[i].Scope < fingerprints[j].Scope
		}
		return fingerprints[i].ID < fingerprints[j].ID
	})

	canonical := snapshotCanonical{
		Schema:        snapshotSchema,
		Scope:         scope,
		PolicyVersion: policyVersion,
		Entries:       fingerprints,
	}
	data, err := json.Marshal(canonical)
	if err != nil {
		return Snapshot{}, err
	}
	sum := sha256.Sum256(data)
	return Snapshot{
		Schema:        canonical.Schema,
		Scope:         canonical.Scope,
		PolicyVersion: canonical.PolicyVersion,
		Entries:       canonical.Entries,
		SnapshotHash:  hex.EncodeToString(sum[:]),
	}, nil
}

func fingerprintEntry(entry Entry) EntryFingerprint {
	return EntryFingerprint{
		Path:          normalizeSnapshotPath(entry.Path),
		ID:            entry.ID,
		Type:          entry.Type,
		Status:        entry.Status,
		Lifecycle:     entry.Lifecycle,
		Topic:         entry.Topic,
		SourceOfTruth: entry.SourceOfTruth,
		Supersedes:    normalizedSnapshotStrings(entry.Supersedes),
		SupersededBy:  normalizedSnapshotStrings(entry.SupersededBy),
		Tags:          normalizedSnapshotStrings(entry.Tags),
		Active:        entry.Active,
		ContentHash:   contentHash(entry.Content),
		Scope:         entry.Scope,
	}
}

func normalizeSnapshotPath(path string) string {
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(path))
}

func normalizedSnapshotStrings(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	normalized := make([]string, len(values))
	copy(normalized, values)
	sort.Strings(normalized)
	return normalized
}

func cloneSnapshotEntries(entries []Entry) []Entry {
	cloned := make([]Entry, len(entries))
	for i, entry := range entries {
		cloned[i] = entry
		cloned[i].Supersedes = append([]string(nil), entry.Supersedes...)
		cloned[i].SupersededBy = append([]string(nil), entry.SupersededBy...)
		cloned[i].Tags = append([]string(nil), entry.Tags...)
		cloned[i].SourceSessions = append([]string(nil), entry.SourceSessions...)
	}
	return cloned
}
