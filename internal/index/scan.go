package index

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nickdu2009/worktrail/internal/model"
	wtruntime "github.com/nickdu2009/worktrail/internal/runtime"
	"github.com/nickdu2009/worktrail/internal/util"
)

func scan(root, scope string) ([]Entry, error) {
	entries, _, err := scanAt(root, scope, time.Now().UTC())
	return entries, err
}

func scanAt(root, scope string, now time.Time) ([]Entry, []IgnoredEntry, error) {
	var entries []Entry
	var ignored []IgnoredEntry
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("index walk refuses symbolic link %q", path)
		}
		if d.IsDir() {
			switch d.Name() {
			case "index", "logs", "raw", "exports":
				if path != root {
					return filepath.SkipDir
				}
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("index walk refuses non-regular file %q", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if shouldSkip(rel) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext != ".md" && ext != ".json" {
			return nil
		}
		entry, ok, err := buildEntry(root, path, rel, scope)
		if err != nil {
			if isIgnoredEntryError(err) {
				ignored = append(ignored, IgnoredEntry{Path: rel, Reason: err.Error()})
				return nil
			}
			return err
		}
		if ok {
			entries = append(entries, entry)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	deriveTeamHandoffHeads(entries)
	entries = latestValidRuntimeEntries(entries, now)
	sort.Slice(ignored, func(i, j int) bool { return ignored[i].Path < ignored[j].Path })
	if err := writeIgnoredSidecar(root, ignored, now); err != nil {
		return nil, nil, err
	}
	if err := resolveEntryIDCollisions(entries); err != nil {
		return nil, ignored, err
	}
	return entries, ignored, nil
}

func deriveTeamHandoffHeads(entries []Entry) {
	byID := make(map[string]int, len(entries))
	for i := range entries {
		if entries[i].Type == "handoff" && entries[i].Visibility == model.VisibilityTeam {
			entries[i].Lifecycle = model.LifecycleCurrent
			entries[i].Status = model.LifecycleCurrent
			entries[i].SupersededBy = nil
			byID[entries[i].ID] = i
		}
	}
	for i := range entries {
		entry := &entries[i]
		if entry.Type != "handoff" || entry.Visibility != model.VisibilityTeam {
			continue
		}
		for _, supersededID := range entry.Supersedes {
			index, ok := byID[supersededID]
			if !ok || index == i {
				continue
			}
			entries[index].Lifecycle = model.LifecycleSuperseded
			entries[index].Status = model.LifecycleSuperseded
			entries[index].SupersededBy = appendUnique(entries[index].SupersededBy, entry.ID)
		}
	}
}

func latestValidRuntimeEntries(entries []Entry, now time.Time) []Entry {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	sort.SliceStable(entries, func(i, j int) bool {
		if isEphemeralRuntimeEntry(entries[i]) != isEphemeralRuntimeEntry(entries[j]) {
			return !isEphemeralRuntimeEntry(entries[i])
		}
		if !entries[i].UpdatedAt.Equal(entries[j].UpdatedAt) {
			return entries[i].UpdatedAt.After(entries[j].UpdatedAt)
		}
		return entries[i].Path < entries[j].Path
	})
	counts := map[string]int{}
	out := make([]Entry, 0, len(entries))
	for _, entry := range entries {
		if !isEphemeralRuntimeEntry(entry) {
			out = append(out, entry)
			continue
		}
		expiresAt := wtruntime.EffectiveExpiry(entry.ExpiresAt, entry.CreatedAt)
		entry.ExpiresAt = expiresAt
		if !expiresAt.IsZero() && !expiresAt.After(now) {
			continue
		}
		projectID := strings.TrimSpace(entry.ProjectID)
		taskID := strings.TrimSpace(entry.TaskID)
		if projectID == "" || taskID == "" {
			out = append(out, entry)
			continue
		}
		key := projectID + "\x00" + taskID
		if counts[key] >= wtruntime.LatestPerTaskLimit {
			continue
		}
		counts[key]++
		out = append(out, entry)
	}
	return out
}

func isEphemeralRuntimeEntry(entry Entry) bool {
	return strings.HasPrefix(filepath.ToSlash(entry.Path), "runtime/")
}

func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}

func writeIgnoredSidecar(root string, ignored []IgnoredEntry, now time.Time) error {
	sidecar := IgnoredSidecar{
		Schema:      "worktrail.index.ignored.v1",
		GeneratedAt: now,
		Entries:     ignored,
	}
	data, err := json.MarshalIndent(sidecar, "", "  ")
	if err != nil {
		return err
	}
	if _, err := ensureIndexOutputDir(root); err != nil {
		return err
	}
	path := filepath.Join(root, "index", "ignored.json")
	if _, _, err := indexArtifactStatus(root, "ignored.json"); err != nil {
		return err
	}
	return util.AtomicWrite(path, append(data, '\n'), 0o600)
}

func resolveEntryIDCollisions(entries []Entry) error {
	byID := make(map[string][]int, len(entries))
	for i := range entries {
		byID[entries[i].ID] = append(byID[entries[i].ID], i)
	}

	var duplicateIDs []string
	for id, indexes := range byID {
		if len(indexes) > 1 {
			duplicateIDs = append(duplicateIDs, id)
		}
	}
	sort.Strings(duplicateIDs)

	for _, id := range duplicateIDs {
		indexes := byID[id]
		var explicitPaths []string
		for _, index := range indexes {
			if !entries[index].generatedID {
				explicitPaths = append(explicitPaths, entries[index].Path)
			}
		}
		if len(explicitPaths) > 1 {
			return fmt.Errorf("duplicate explicit Worktrail id %q in %s", id, strings.Join(explicitPaths, ", "))
		}
		for _, index := range indexes {
			if !entries[index].generatedID {
				continue
			}
			sum := sha256.Sum256([]byte(entries[index].Path))
			entries[index].ID = fmt.Sprintf("%s-%x", id, sum[:6])
		}
	}

	seen := make(map[string]string, len(entries))
	for _, entry := range entries {
		if previousPath, ok := seen[entry.ID]; ok {
			return fmt.Errorf("duplicate Worktrail index id %q in %s and %s", entry.ID, previousPath, entry.Path)
		}
		seen[entry.ID] = entry.Path
	}
	return nil
}

func shouldSkip(rel string) bool {
	base := filepath.Base(rel)
	return base == "config.json" || base == ".DS_Store" || rel == "state/active/latest.md"
}
